package downloads

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/local"
	chunk_util "github.com/cortexproject/cortex/pkg/chunk/util"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
	"go.etcd.io/bbolt"
)

// timeout for downloading initial files for a table to avoid leaking resources by allowing it to take all the time.
const downloadTimeout = 5 * time.Minute

type BoltDBIndexClient interface {
	QueryDB(ctx context.Context, db *bbolt.DB, query chunk.IndexQuery, callback func(chunk.IndexQuery, chunk.ReadBatch) (shouldContinue bool)) error
}

type downloadedFile struct {
	mtime  time.Time
	boltdb *bbolt.DB
}

// Table is a collection of multiple files created for a same table by various ingesters.
// All the public methods are concurrency safe and take care of mutexes to avoid any data race.
type Table struct {
	name              string
	cacheLocation     string
	metrics           *metrics
	storageClient     chunk.ObjectClient
	boltDBIndexClient BoltDBIndexClient

	lastUsedAt time.Time
	dbs        map[string]*downloadedFile
	dbsMtx     sync.RWMutex
	err        error

	ready      chan struct{}      // helps with detecting initialization of table which downloads all the existing files.
	cancelFunc context.CancelFunc // helps with cancellation of initialization if we are asked to stop.
}

func NewTable(name, cacheLocation string, storageClient chunk.ObjectClient, boltDBIndexClient BoltDBIndexClient, metrics *metrics) *Table {
	table := Table{
		name:              name,
		cacheLocation:     cacheLocation,
		metrics:           metrics,
		storageClient:     storageClient,
		boltDBIndexClient: boltDBIndexClient,
		dbs:               map[string]*downloadedFile{},
		ready:             make(chan struct{}),
	}

	// keep the files collection locked until all the files are downloaded.
	table.dbsMtx.Lock()
	go func() {
		defer table.dbsMtx.Unlock()
		defer close(table.ready)

		ctx, cancel := context.WithTimeout(context.Background(), downloadTimeout)
		defer cancel()

		table.cancelFunc = cancel

		// Using background context to avoid cancellation of download when request times out.
		// We would anyways need the files for serving next requests.
		if err := table.init(ctx); err != nil {
			level.Error(util.Logger).Log("msg", "failed to download files", "name", table.name)
		}
	}()

	return &table
}

// init downloads all the db files for the table from object storage.
// it assumes the locking of mutex is taken care of by the caller.
func (t *Table) init(ctx context.Context) (err error) {
	defer func() {
		status := statusSuccess
		if err != nil {
			status = statusFailure
			t.err = err

			// cleaning up files due to error to avoid returning invalid results.
			for fileName := range t.dbs {
				if err := t.cleanupDB(fileName); err != nil {
					level.Error(util.Logger).Log("msg", "failed to cleanup partially downloaded file", "filename", fileName, "err", err)
				}
			}
		}
		t.metrics.filesDownloadOperationTotal.WithLabelValues(status).Inc()
	}()

	startTime := time.Now()
	totalFilesSize := int64(0)

	objects, _, err := t.storageClient.List(ctx, t.name+"/")
	if err != nil {
		return
	}

	level.Debug(util.Logger).Log("msg", fmt.Sprintf("list of files to download for period %s: %s", t.name, objects))

	folderPath, err := t.folderPathForTable(true)
	if err != nil {
		return
	}

	for _, object := range objects {
		dbName, err := getDBNameFromObjectKey(object.Key)
		if err != nil {
			return err
		}

		filePath := path.Join(folderPath, dbName)
		df := downloadedFile{}

		err = t.getFileFromStorage(ctx, object.Key, filePath)
		if err != nil {
			return err
		}

		df.mtime = object.ModifiedAt
		df.boltdb, err = local.OpenBoltdbFile(filePath)
		if err != nil {
			return err
		}

		var stat os.FileInfo
		stat, err = os.Stat(filePath)
		if err != nil {
			return err
		}

		totalFilesSize += stat.Size()

		t.dbs[dbName] = &df
	}

	duration := time.Since(startTime).Seconds()
	t.metrics.filesDownloadDurationSeconds.add(t.name, duration)
	t.metrics.filesDownloadSizeBytes.add(t.name, totalFilesSize)

	return
}

// Closes references to all the dbs.
func (t *Table) Close() {
	t.dbsMtx.Lock()
	defer t.dbsMtx.Unlock()

	// stop the initialization if it is still ongoing.
	t.cancelFunc()

	for name, db := range t.dbs {
		if err := db.boltdb.Close(); err != nil {
			level.Error(util.Logger).Log("msg", fmt.Errorf("failed to close file %s for table %s", name, t.name))
		}
	}

	t.dbs = map[string]*downloadedFile{}
}

// Queries all the dbs for index.
func (t *Table) Query(ctx context.Context, query chunk.IndexQuery, callback chunk_util.Callback) error {
	if t.err != nil {
		return t.err
	}

	t.dbsMtx.RLock()
	defer t.dbsMtx.RUnlock()

	t.lastUsedAt = time.Now()

	for _, db := range t.dbs {
		if err := t.boltDBIndexClient.QueryDB(ctx, db.boltdb, query, callback); err != nil {
			return err
		}
	}

	return nil
}

// Closes reference to all the open dbs and removes the local file.
func (t *Table) CleanupAllDBs() error {
	t.dbsMtx.Lock()
	defer t.dbsMtx.Unlock()

	for fileName := range t.dbs {
		if err := t.cleanupDB(fileName); err != nil {
			return err
		}
	}
	return nil
}

// IsReady returns a channel which gets closed when the Table is ready for queries.
func (t *Table) IsReady() chan struct{} {
	return t.ready
}

// Err returns the err which is usually set when there was any issue in init.
func (t *Table) Err() error {
	return t.err
}

// LastUsedAt returns the time at which table was last used for querying.
func (t *Table) LastUsedAt() time.Time {
	return t.lastUsedAt
}

func (t *Table) cleanupDB(fileName string) error {
	df, ok := t.dbs[fileName]
	if !ok {
		return fmt.Errorf("file %s not found in files collection for cleaning up", fileName)
	}

	filePath := df.boltdb.Path()

	if err := df.boltdb.Close(); err != nil {
		return err
	}

	delete(t.dbs, fileName)

	return os.Remove(filePath)
}

// Sync downloads updated and new files from the storage relevant for the table and removes the deleted ones
func (t *Table) Sync(ctx context.Context) error {
	level.Debug(util.Logger).Log("msg", fmt.Sprintf("syncing files for period %s", t.name))

	toDownload, toDelete, err := t.checkStorageForUpdates(ctx)
	if err != nil {
		return err
	}

	for _, storageObject := range toDownload {
		err = t.downloadFile(ctx, storageObject)
		if err != nil {
			return err
		}
	}

	t.dbsMtx.Lock()
	defer t.dbsMtx.Unlock()

	for _, db := range toDelete {
		err := t.cleanupDB(db)
		if err != nil {
			return err
		}
	}

	return nil
}

// checkStorageForUpdates compares files from cache with storage and builds the list of files to be downloaded from storage and to be deleted from cache
func (t *Table) checkStorageForUpdates(ctx context.Context) (toDownload []chunk.StorageObject, toDelete []string, err error) {
	// listing tables from store
	var objects []chunk.StorageObject
	objects, _, err = t.storageClient.List(ctx, t.name+"/")
	if err != nil {
		return
	}

	listedDBs := make(map[string]struct{}, len(objects))

	t.dbsMtx.RLock()
	defer t.dbsMtx.RUnlock()

	for _, object := range objects {
		dbName, err := getDBNameFromObjectKey(object.Key)
		if err != nil {
			return nil, nil, err
		}
		listedDBs[dbName] = struct{}{}

		// Checking whether file was updated in the store after we downloaded it, if not, no need to include it in updates
		downloadedFileDetails, ok := t.dbs[dbName]
		if !ok || downloadedFileDetails.mtime != object.ModifiedAt {
			toDownload = append(toDownload, object)
		}
	}

	for db := range t.dbs {
		if _, isOK := listedDBs[db]; !isOK {
			toDelete = append(toDelete, db)
		}
	}

	return
}

// downloadFile first downloads file to a temp location so that we can close the existing db(if already exists), replace it with new one and then reopen it.
func (t *Table) downloadFile(ctx context.Context, storageObject chunk.StorageObject) error {
	dbName, err := getDBNameFromObjectKey(storageObject.Key)
	if err != nil {
		return err
	}
	folderPath, _ := t.folderPathForTable(false)
	filePath := path.Join(folderPath, dbName)

	// download the file temporarily with some other name to allow boltdb client to close the existing file first if it exists
	tempFilePath := path.Join(folderPath, fmt.Sprintf("%s.%s", dbName, "temp"))

	err = t.getFileFromStorage(ctx, storageObject.Key, tempFilePath)
	if err != nil {
		return err
	}

	t.dbsMtx.Lock()
	defer t.dbsMtx.Unlock()

	df, ok := t.dbs[dbName]
	if ok {
		if err := df.boltdb.Close(); err != nil {
			return err
		}
	} else {
		df = &downloadedFile{}
	}

	// move the file from temp location to actual location
	err = os.Rename(tempFilePath, filePath)
	if err != nil {
		return err
	}

	df.mtime = storageObject.ModifiedAt
	df.boltdb, err = local.OpenBoltdbFile(filePath)
	if err != nil {
		return err
	}

	t.dbs[dbName] = df

	return nil
}

// getFileFromStorage downloads a file from storage to given location.
func (t *Table) getFileFromStorage(ctx context.Context, objectKey, destination string) error {
	readCloser, err := t.storageClient.GetObject(ctx, objectKey)
	if err != nil {
		return err
	}

	defer func() {
		if err := readCloser.Close(); err != nil {
			level.Error(util.Logger)
		}
	}()

	f, err := os.Create(destination)
	if err != nil {
		return err
	}

	_, err = io.Copy(f, readCloser)
	if err != nil {
		return err
	}

	level.Info(util.Logger).Log("msg", fmt.Sprintf("downloaded file %s", objectKey))

	return f.Sync()
}

func (t *Table) folderPathForTable(ensureExists bool) (string, error) {
	folderPath := path.Join(t.cacheLocation, t.name)

	if ensureExists {
		err := chunk_util.EnsureDirectory(folderPath)
		if err != nil {
			return "", err
		}
	}

	return folderPath, nil
}

func getDBNameFromObjectKey(objectKey string) (string, error) {
	ss := strings.Split(objectKey, "/")

	if len(ss) != 2 {
		return "", fmt.Errorf("invalid object key: %v", objectKey)
	}
	if ss[1] == "" {
		return "", fmt.Errorf("empty db name, object key: %v", objectKey)
	}
	return ss[1], nil
}
