package downloads

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/concurrency"
	"github.com/grafana/dskit/tenant"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/pkg/storage/chunk/client/local"
	chunk_util "github.com/grafana/loki/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/pkg/storage/stores/series/index"
	"github.com/grafana/loki/pkg/storage/stores/shipper/storage"
	shipper_util "github.com/grafana/loki/pkg/storage/stores/shipper/util"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/util/spanlogger"
)

var errIndexListCacheTooStale = fmt.Errorf("index list cache too stale")

type IndexSet interface {
	Init(forQuerying bool) error
	Close()
	MultiQueries(ctx context.Context, queries []index.Query, callback index.QueryPagesCallback) error
	DropAllDBs() error
	Err() error
	LastUsedAt() time.Time
	UpdateLastUsedAt()
	Sync(ctx context.Context) (err error)
	AwaitReady(ctx context.Context) error
}

// indexSet is a collection of multiple files created for a same table by various ingesters.
// All the public methods are concurrency safe and take care of mutexes to avoid any data race.
type indexSet struct {
	baseIndexSet      storage.IndexSet
	tableName, userID string
	cacheLocation     string
	boltDBIndexClient BoltDBIndexClient
	logger            log.Logger

	lastUsedAt time.Time
	dbs        map[string]*bbolt.DB
	dbsMtx     *mtxWithReadiness
	err        error

	cancelFunc context.CancelFunc // helps with cancellation of initialization if we are asked to stop.
}

func NewIndexSet(tableName, userID, cacheLocation string, baseIndexSet storage.IndexSet,
	boltDBIndexClient BoltDBIndexClient, logger log.Logger) (IndexSet, error) {
	if baseIndexSet.IsUserBasedIndexSet() && userID == "" {
		return nil, fmt.Errorf("userID must not be empty")
	} else if !baseIndexSet.IsUserBasedIndexSet() && userID != "" {
		return nil, fmt.Errorf("userID must be empty")
	}

	err := chunk_util.EnsureDirectory(cacheLocation)
	if err != nil {
		return nil, err
	}

	is := indexSet{
		baseIndexSet:      baseIndexSet,
		tableName:         tableName,
		userID:            userID,
		cacheLocation:     cacheLocation,
		boltDBIndexClient: boltDBIndexClient,
		logger:            logger,
		lastUsedAt:        time.Now(),
		dbs:               map[string]*bbolt.DB{},
		dbsMtx:            newMtxWithReadiness(),
		cancelFunc:        func() {},
	}

	return &is, nil
}

// Init downloads all the db files for the table from object storage.
func (t *indexSet) Init(forQuerying bool) (err error) {
	// Using background context to avoid cancellation of download when request times out.
	// We would anyways need the files for serving next requests.
	ctx, cancelFunc := context.WithTimeout(context.Background(), downloadTimeout)
	t.cancelFunc = cancelFunc

	logger := spanlogger.FromContextWithFallback(ctx, t.logger)

	defer func() {
		if err != nil {
			level.Error(t.logger).Log("msg", "failed to initialize table, cleaning it up", "err", err)
			t.err = err

			// cleaning up files due to error to avoid returning invalid results.
			for fileName := range t.dbs {
				if err := t.cleanupDB(fileName); err != nil {
					level.Error(t.logger).Log("msg", "failed to cleanup partially downloaded file", "filename", fileName, "err", err)
				}
			}
		}
		t.cancelFunc()
		t.dbsMtx.markReady()
	}()

	filesInfo, err := ioutil.ReadDir(t.cacheLocation)
	if err != nil {
		return err
	}

	// open all the locally present files first to avoid downloading them again during sync operation below.
	for _, fileInfo := range filesInfo {
		if fileInfo.IsDir() {
			continue
		}

		fullPath := filepath.Join(t.cacheLocation, fileInfo.Name())
		// if we fail to open a boltdb file, lets skip it and let sync operation re-download the file from storage.
		boltdb, err := shipper_util.SafeOpenBoltdbFile(fullPath)
		if err != nil {
			level.Error(util_log.Logger).Log("msg", fmt.Sprintf("failed to open existing boltdb file %s, removing the file and continuing without it to let the sync operation catch up", fullPath), "err", err)
			// Sometimes files get corrupted when the process gets killed in the middle of a download operation which causes boltdb client to panic.
			// We already recover the panic but the lock on the file is not released by boltdb client which causes the reopening of the file to fail when the sync operation tries it.
			// We want to remove the file failing to open to get rid of the lock.
			if err := os.Remove(fullPath); err != nil {
				level.Error(util_log.Logger).Log("msg", fmt.Sprintf("failed to remove boltdb file %s which failed to open", fullPath))
			}
			continue
		}

		t.dbs[fileInfo.Name()] = boltdb
	}

	level.Debug(logger).Log("msg", fmt.Sprintf("opened %d local files, now starting sync operation", len(t.dbs)))

	// sync the table to get new files and remove the deleted ones from storage.
	err = t.sync(ctx, false, forQuerying)
	if err != nil {
		return
	}

	level.Debug(logger).Log("msg", "finished syncing files")

	return
}

// Close Closes references to all the dbs.
func (t *indexSet) Close() {
	// stop the initialization if it is still ongoing.
	t.cancelFunc()

	err := t.dbsMtx.lock(context.Background())
	if err != nil {
		level.Error(t.logger).Log("msg", "failed to acquire lock for closing dbs", "err", err)
		return
	}
	defer t.dbsMtx.unlock()

	for name, db := range t.dbs {
		if err := db.Close(); err != nil {
			level.Error(t.logger).Log("msg", fmt.Sprintf("failed to close file %s", name), "err", err)
		}
	}

	t.dbs = map[string]*bbolt.DB{}
}

// MultiQueries runs multiple queries without having to take lock multiple times for each query.
func (t *indexSet) MultiQueries(ctx context.Context, queries []index.Query, callback index.QueryPagesCallback) error {
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return err
	}

	userIDBytes := shipper_util.GetUnsafeBytes(userID)

	err = t.dbsMtx.rLock(ctx)
	if err != nil {
		return err
	}
	defer t.dbsMtx.rUnlock()

	if t.err != nil {
		return t.err
	}

	t.lastUsedAt = time.Now()

	logger := util_log.WithContext(ctx, t.logger)
	level.Debug(logger).Log("query-count", len(queries), "dbs-count", len(t.dbs))

	for name, db := range t.dbs {
		err := db.View(func(tx *bbolt.Tx) error {
			bucket := tx.Bucket(userIDBytes)
			if bucket == nil {
				bucket = tx.Bucket(local.IndexBucketName)
				if bucket == nil {
					return nil
				}
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

		level.Debug(logger).Log("queried-db", name)
	}

	return nil
}

// DropAllDBs closes reference to all the open dbs and removes the local files.
func (t *indexSet) DropAllDBs() error {
	err := t.dbsMtx.lock(context.Background())
	if err != nil {
		return err
	}
	defer t.dbsMtx.unlock()

	for fileName := range t.dbs {
		if err := t.cleanupDB(fileName); err != nil {
			return err
		}
	}

	return os.RemoveAll(t.cacheLocation)
}

// Err returns the err which is usually set when there was any issue in Init.
func (t *indexSet) Err() error {
	return t.err
}

// LastUsedAt returns the time at which table was last used for querying.
func (t *indexSet) LastUsedAt() time.Time {
	return t.lastUsedAt
}

func (t *indexSet) UpdateLastUsedAt() {
	t.lastUsedAt = time.Now()
}

// cleanupDB closes and removes the local file.
func (t *indexSet) cleanupDB(fileName string) error {
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

func (t *indexSet) Sync(ctx context.Context) (err error) {
	return t.sync(ctx, true, false)
}

// sync downloads updated and new files from the storage relevant for the table and removes the deleted ones
func (t *indexSet) sync(ctx context.Context, lock, bypassListCache bool) (err error) {
	level.Debug(t.logger).Log("msg", "syncing index files")

	toDownload, toDelete, err := t.checkStorageForUpdates(ctx, lock, bypassListCache)
	if err != nil {
		return err
	}

	level.Debug(t.logger).Log("msg", "index sync updates", "toDownload", fmt.Sprint(toDownload), "toDelete", fmt.Sprint(toDelete))

	downloadedFiles, err := t.doConcurrentDownload(ctx, toDownload)
	if err != nil {
		return err
	}

	// if we did not bypass list cache and skipped downloading all the new files due to them being removed by compaction,
	// it means the cache is not valid anymore since compaction would have happened after last index list cache refresh.
	// Let us return error to ask the caller to re-run the sync after the list cache refresh.
	if !bypassListCache && len(downloadedFiles) == 0 && len(toDownload) > 0 {
		level.Error(t.logger).Log("msg", "we skipped downloading all the new files, possibly removed by compaction", "files", toDownload)
		return errIndexListCacheTooStale
	}

	if lock {
		err = t.dbsMtx.lock(ctx)
		if err != nil {
			return err
		}
		defer t.dbsMtx.unlock()
	}

	for _, fileName := range downloadedFiles {
		filePath := filepath.Join(t.cacheLocation, fileName)
		boltdb, err := shipper_util.SafeOpenBoltdbFile(filePath)
		if err != nil {
			return err
		}

		t.dbs[fileName] = boltdb
	}

	for _, db := range toDelete {
		err := t.cleanupDB(db)
		if err != nil {
			return err
		}
	}

	return nil
}

// checkStorageForUpdates compares files from cache with storage and builds the list of files to be downloaded from storage and to be deleted from cache
func (t *indexSet) checkStorageForUpdates(ctx context.Context, lock, bypassListCache bool) (toDownload []storage.IndexFile, toDelete []string, err error) {
	// listing tables from store
	var files []storage.IndexFile

	files, err = t.baseIndexSet.ListFiles(ctx, t.tableName, t.userID, bypassListCache)
	if err != nil {
		return
	}

	listedDBs := make(map[string]struct{}, len(files))

	if lock {
		err = t.dbsMtx.rLock(ctx)
		if err != nil {
			return nil, nil, err
		}
		defer t.dbsMtx.rUnlock()
	}

	for _, file := range files {
		listedDBs[file.Name] = struct{}{}

		// Checking whether file was already downloaded, if not, download it.
		// We do not ever upload files in the object store with the same name but different contents so we do not consider downloading modified files again.
		_, ok := t.dbs[file.Name]
		if !ok {
			toDownload = append(toDownload, file)
		}
	}

	for db := range t.dbs {
		if _, isOK := listedDBs[db]; !isOK {
			toDelete = append(toDelete, db)
		}
	}

	return
}

func (t *indexSet) AwaitReady(ctx context.Context) error {
	return t.dbsMtx.awaitReady(ctx)
}

func (t *indexSet) downloadFileFromStorage(ctx context.Context, fileName, folderPathForTable string) error {
	return shipper_util.DownloadFileFromStorage(filepath.Join(folderPathForTable, fileName), shipper_util.IsCompressedFile(fileName),
		true, shipper_util.LoggerWithFilename(t.logger, fileName), func() (io.ReadCloser, error) {
			return t.baseIndexSet.GetFile(ctx, t.tableName, t.userID, fileName)
		})
}

// doConcurrentDownload downloads objects(files) concurrently. It ignores only missing file errors caused by removal of file by compaction.
// It returns the names of the files downloaded successfully and leaves it upto the caller to open those files.
func (t *indexSet) doConcurrentDownload(ctx context.Context, files []storage.IndexFile) ([]string, error) {
	downloadedFiles := make([]string, 0, len(files))
	downloadedFilesMtx := sync.Mutex{}

	err := concurrency.ForEachJob(ctx, len(files), maxDownloadConcurrency, func(ctx context.Context, idx int) error {
		fileName := files[idx].Name
		err := t.downloadFileFromStorage(ctx, fileName, t.cacheLocation)
		if err != nil {
			if t.baseIndexSet.IsFileNotFoundErr(err) {
				level.Info(util_log.Logger).Log("msg", fmt.Sprintf("ignoring missing file %s, possibly removed during compaction", fileName))
				return nil
			}
			return err
		}

		downloadedFilesMtx.Lock()
		downloadedFiles = append(downloadedFiles, fileName)
		downloadedFilesMtx.Unlock()

		return nil
	})
	if err != nil {
		return nil, err
	}

	return downloadedFiles, nil
}
