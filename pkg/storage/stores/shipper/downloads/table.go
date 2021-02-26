package downloads

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	chunk_util "github.com/cortexproject/cortex/pkg/chunk/util"
	util_log "github.com/cortexproject/cortex/pkg/util/log"
	util_math "github.com/cortexproject/cortex/pkg/util/math"
	"github.com/cortexproject/cortex/pkg/util/spanlogger"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"go.etcd.io/bbolt"

	shipper_util "github.com/grafana/loki/pkg/storage/stores/shipper/util"
)

// timeout for downloading initial files for a table to avoid leaking resources by allowing it to take all the time.
const (
	downloadTimeout     = 5 * time.Minute
	downloadParallelism = 50
	delimiter           = "/"
)

var bucketName = []byte("index")

type BoltDBIndexClient interface {
	QueryWithCursor(_ context.Context, c *bbolt.Cursor, query chunk.IndexQuery, callback func(chunk.IndexQuery, chunk.ReadBatch) (shouldContinue bool)) error
}

type StorageClient interface {
	GetObject(ctx context.Context, objectKey string) (io.ReadCloser, error)
	List(ctx context.Context, prefix, delimiter string) ([]chunk.StorageObject, []chunk.StorageCommonPrefix, error)
}

// Table is a collection of multiple files created for a same table by various ingesters.
// All the public methods are concurrency safe and take care of mutexes to avoid any data race.
type Table struct {
	name              string
	cacheLocation     string
	metrics           *metrics
	storageClient     StorageClient
	boltDBIndexClient BoltDBIndexClient

	lastUsedAt time.Time
	dbs        map[string]*bbolt.DB
	dbsMtx     sync.RWMutex
	err        error

	ready      chan struct{}      // helps with detecting initialization of table which downloads all the existing files.
	cancelFunc context.CancelFunc // helps with cancellation of initialization if we are asked to stop.
}

func NewTable(spanCtx context.Context, name, cacheLocation string, storageClient StorageClient, boltDBIndexClient BoltDBIndexClient, metrics *metrics) *Table {
	ctx, cancel := context.WithCancel(context.Background())

	table := Table{
		name:              name,
		cacheLocation:     cacheLocation,
		metrics:           metrics,
		storageClient:     storageClient,
		boltDBIndexClient: boltDBIndexClient,
		lastUsedAt:        time.Now(),
		dbs:               map[string]*bbolt.DB{},
		ready:             make(chan struct{}),
		cancelFunc:        cancel,
	}

	// keep the files collection locked until all the files are downloaded.
	table.dbsMtx.Lock()
	go func() {
		defer table.dbsMtx.Unlock()
		defer close(table.ready)

		log, _ := spanlogger.New(spanCtx, "Shipper.DownloadTable")
		defer log.Span.Finish()

		ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
		defer cancel()

		// Using background context to avoid cancellation of download when request times out.
		// We would anyways need the files for serving next requests.
		if err := table.init(ctx, log); err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to download table", "name", table.name)
		}
	}()

	return &table
}

// LoadTable loads a table from local storage(syncs the table too if we have it locally) or downloads it from the shared store.
func LoadTable(ctx context.Context, name, cacheLocation string, storageClient StorageClient, boltDBIndexClient BoltDBIndexClient, metrics *metrics) (*Table, error) {
	// see if folder for table already exists.
	folderPath := path.Join(cacheLocation, name)
	_, err := os.Stat(folderPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}

		// folder for table doesn't exist, this means we have to download it from the shared store.
		table := NewTable(ctx, name, cacheLocation, storageClient, boltDBIndexClient, metrics)
		<-table.ready
		if table.err != nil {
			return nil, table.err
		}
		return table, nil
	}

	// folder for table already exists, open all the boltdb files from it.
	filesInfo, err := ioutil.ReadDir(folderPath)
	if err != nil {
		return nil, err
	}

	table := Table{
		name:              name,
		cacheLocation:     cacheLocation,
		metrics:           metrics,
		storageClient:     storageClient,
		boltDBIndexClient: boltDBIndexClient,
		lastUsedAt:        time.Now(),
		dbs:               map[string]*bbolt.DB{},
		ready:             make(chan struct{}),
		cancelFunc:        func() {},
	}

	level.Debug(util_log.Logger).Log("msg", fmt.Sprintf("opening locally present files for table %s", name), "files", fmt.Sprint(filesInfo))

	for _, fileInfo := range filesInfo {
		if fileInfo.IsDir() {
			continue
		}

		// if we fail to open a boltdb file, lets skip it and let sync operation re-download the file from storage.
		boltdb, err := shipper_util.SafeOpenBoltdbFile(filepath.Join(folderPath, fileInfo.Name()))
		if err != nil {
			level.Error(util_log.Logger).Log("msg", fmt.Sprintf("failed to open existing boltdb file %s, continuing without it to let the sync operation catch up", filepath.Join(folderPath, fileInfo.Name())), "err", err)
			continue
		}

		table.dbs[fileInfo.Name()] = boltdb
	}

	level.Debug(util_log.Logger).Log("msg", fmt.Sprintf("syncing files for table %s", name))
	// sync the table to get new files and remove the deleted ones from storage.
	err = table.Sync(ctx)
	if err != nil {
		return nil, err
	}

	// close the ready channel because the query function waits for it to be closed before performing queries.
	close(table.ready)

	return &table, nil
}

// init downloads all the db files for the table from object storage.
// it assumes the locking of mutex is taken care of by the caller.
func (t *Table) init(ctx context.Context, spanLogger log.Logger) (err error) {
	defer func() {
		status := statusSuccess
		if err != nil {
			status = statusFailure
			t.err = err

			level.Error(util_log.Logger).Log("msg", fmt.Sprintf("failed to initialize table %s, cleaning it up", t.name), "err", err)

			// cleaning up files due to error to avoid returning invalid results.
			for fileName := range t.dbs {
				if err := t.cleanupDB(fileName); err != nil {
					level.Error(util_log.Logger).Log("msg", "failed to cleanup partially downloaded file", "filename", fileName, "err", err)
				}
			}
		}
		t.metrics.tablesSyncOperationTotal.WithLabelValues(status).Inc()
	}()

	startTime := time.Now()
	totalFilesSize := int64(0)

	// The forward slash here needs to stay because we are trying to list contents of a directory without it we will get the name of the same directory back with hosted object stores.
	// This is due to the object stores not having a concept of directories.
	objects, _, err := t.storageClient.List(ctx, t.name+delimiter, delimiter)
	if err != nil {
		return
	}

	level.Debug(util_log.Logger).Log("msg", fmt.Sprintf("list of files to download for period %s: %s", t.name, objects))

	folderPath, err := t.folderPathForTable(true)
	if err != nil {
		return
	}

	// download the dbs parallelly
	err = t.doParallelDownload(ctx, objects, folderPath)
	if err != nil {
		return err
	}

	level.Debug(spanLogger).Log("total-files-downloaded", len(objects))

	objects = shipper_util.RemoveDirectories(objects)

	// open all the downloaded dbs
	for _, object := range objects {

		dbName, err := getDBNameFromObjectKey(object.Key)
		if err != nil {
			return err
		}

		filePath := path.Join(folderPath, dbName)
		boltdb, err := shipper_util.SafeOpenBoltdbFile(filePath)
		if err != nil {
			return err
		}

		var stat os.FileInfo
		stat, err = os.Stat(filePath)
		if err != nil {
			return err
		}

		totalFilesSize += stat.Size()

		t.dbs[dbName] = boltdb
	}

	duration := time.Since(startTime).Seconds()
	t.metrics.tablesDownloadDurationSeconds.add(t.name, duration)
	t.metrics.tablesDownloadSizeBytes.add(t.name, totalFilesSize)
	level.Debug(spanLogger).Log("total-files-size", totalFilesSize)

	return
}

// Closes references to all the dbs.
func (t *Table) Close() {
	// stop the initialization if it is still ongoing.
	t.cancelFunc()

	t.dbsMtx.Lock()
	defer t.dbsMtx.Unlock()

	for name, db := range t.dbs {
		if err := db.Close(); err != nil {
			level.Error(util_log.Logger).Log("msg", fmt.Sprintf("failed to close file %s for table %s", name, t.name), "err", err)
		}
	}

	t.dbs = map[string]*bbolt.DB{}
}

// MultiQueries runs multiple queries without having to take lock multiple times for each query.
func (t *Table) MultiQueries(ctx context.Context, queries []chunk.IndexQuery, callback chunk_util.Callback) error {
	// let us check if table is ready for use while also honoring the context timeout
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.ready:
	}

	t.dbsMtx.RLock()
	defer t.dbsMtx.RUnlock()

	if t.err != nil {
		return t.err
	}

	t.lastUsedAt = time.Now()

	log, ctx := spanlogger.New(ctx, "Shipper.Downloads.Table.MultiQueries")
	defer log.Span.Finish()

	level.Debug(log).Log("table-name", t.name, "query-count", len(queries))

	for name, db := range t.dbs {
		err := db.View(func(tx *bbolt.Tx) error {
			bucket := tx.Bucket(bucketName)
			if bucket == nil {
				return nil
			}

			for _, query := range queries {
				if err := t.boltDBIndexClient.QueryWithCursor(ctx, bucket.Cursor(), query, callback); err != nil {
					return err
				}
			}

			return nil
		})

		if err != nil {
			return err
		}

		level.Debug(log).Log("queried-db", name)
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

	tablePath, err := t.folderPathForTable(false)
	if err != nil {
		return err
	}
	return os.RemoveAll(tablePath)
}

// Err returns the err which is usually set when there was any issue in init.
func (t *Table) Err() error {
	return t.err
}

// LastUsedAt returns the time at which table was last used for querying.
func (t *Table) LastUsedAt() time.Time {
	return t.lastUsedAt
}

func (t *Table) UpdateLastUsedAt() {
	t.lastUsedAt = time.Now()
}

func (t *Table) cleanupDB(fileName string) error {
	df, ok := t.dbs[fileName]
	if !ok {
		return fmt.Errorf("file %s not found in files collection for cleaning up", fileName)
	}

	filePath := df.Path()

	if err := df.Close(); err != nil {
		return err
	}

	delete(t.dbs, fileName)

	return os.Remove(filePath)
}

// Sync downloads updated and new files from the storage relevant for the table and removes the deleted ones
func (t *Table) Sync(ctx context.Context) error {
	level.Debug(util_log.Logger).Log("msg", fmt.Sprintf("syncing files for table %s", t.name))

	toDownload, toDelete, err := t.checkStorageForUpdates(ctx)
	if err != nil {
		return err
	}

	level.Debug(util_log.Logger).Log("msg", fmt.Sprintf("updates for table %s. toDownload: %s, toDelete: %s", t.name, toDownload, toDelete))

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

	// The forward slash here needs to stay because we are trying to list contents of a directory without it we will get the name of the same directory back with hosted object stores.
	// This is due to the object stores not having a concept of directories.
	objects, _, err = t.storageClient.List(ctx, t.name+delimiter, delimiter)
	if err != nil {
		return
	}

	listedDBs := make(map[string]struct{}, len(objects))

	t.dbsMtx.RLock()
	defer t.dbsMtx.RUnlock()

	objects = shipper_util.RemoveDirectories(objects)

	for _, object := range objects {

		dbName, err := getDBNameFromObjectKey(object.Key)
		if err != nil {
			return nil, nil, err
		}
		listedDBs[dbName] = struct{}{}

		// Checking whether file was already downloaded, if not, download it.
		// We do not ever upload files in the object store with the same name but different contents so we do not consider downloading modified files again.
		_, ok := t.dbs[dbName]
		if !ok {
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
	level.Info(util_log.Logger).Log("msg", fmt.Sprintf("downloading object from storage with key %s", storageObject.Key))

	dbName, err := getDBNameFromObjectKey(storageObject.Key)
	if err != nil {
		return err
	}
	folderPath, _ := t.folderPathForTable(false)
	filePath := path.Join(folderPath, dbName)

	err = shipper_util.GetFileFromStorage(ctx, t.storageClient, storageObject.Key, filePath)
	if err != nil {
		return err
	}

	t.dbsMtx.Lock()
	defer t.dbsMtx.Unlock()

	boltdb, err := shipper_util.SafeOpenBoltdbFile(filePath)
	if err != nil {
		return err
	}

	t.dbs[dbName] = boltdb

	return nil
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
	ss := strings.Split(objectKey, delimiter)

	if len(ss) != 2 {
		return "", fmt.Errorf("invalid object key: %v", objectKey)
	}
	if ss[1] == "" {
		return "", fmt.Errorf("empty db name, object key: %v", objectKey)
	}
	return ss[1], nil
}

// doParallelDownload downloads objects(dbs) parallelly. It is upto the caller to open the dbs after the download finishes successfully.
func (t *Table) doParallelDownload(ctx context.Context, objects []chunk.StorageObject, folderPathForTable string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	queue := make(chan chunk.StorageObject)
	n := util_math.Min(len(objects), downloadParallelism)
	incomingErrors := make(chan error)

	// Run n parallel goroutines fetching objects to download from the queue
	for i := 0; i < n; i++ {
		go func() {
			// when there is an error, break the loop and send the error to the channel to stop the operation.
			var err error
			for {
				object, ok := <-queue
				if !ok {
					break
				}

				// The s3 client can also return the directory itself in the ListObjects.
				if shipper_util.IsDirectory(object.Key) {
					continue
				}

				var dbName string
				dbName, err = getDBNameFromObjectKey(object.Key)
				if err != nil {
					break
				}

				filePath := path.Join(folderPathForTable, dbName)
				err = shipper_util.GetFileFromStorage(ctx, t.storageClient, object.Key, filePath)
				if err != nil {
					break
				}
			}

			incomingErrors <- err
		}()
	}

	// Send all the objects to download into the queue
	go func() {
		for _, object := range objects {
			select {
			case queue <- object:
			case <-ctx.Done():
				break
			}

		}
		close(queue)
	}()

	// receive all the errors which also lets us make sure all the goroutines have stopped.
	var firstErr error
	for i := 0; i < n; i++ {
		err := <-incomingErrors
		if err != nil && firstErr == nil {
			// cancel the download operation in case of error.
			cancel()
			firstErr = err
		}
	}

	return firstErr
}
