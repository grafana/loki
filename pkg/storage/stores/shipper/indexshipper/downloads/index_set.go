package downloads

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/concurrency"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
	"github.com/grafana/loki/v3/pkg/util/spanlogger"
)

const (
	gzipExtension  = ".gz"
	maxSyncRetries = 1
)

var errIndexListCacheTooStale = fmt.Errorf("index list cache too stale")

type IndexSet interface {
	Init(forQuerying bool, logger log.Logger) error
	Close()
	ForEach(ctx context.Context, callback index.ForEachIndexCallback) error
	ForEachConcurrent(ctx context.Context, callback index.ForEachIndexCallback) error
	DropAllDBs() error
	Err() error
	LastUsedAt() time.Time
	UpdateLastUsedAt()
	Sync(ctx context.Context) (err error)
	AwaitReady(ctx context.Context, reason string) error
}

// indexSet is a collection of multiple files created for a same table by various ingesters.
// All the public methods are concurrency safe and take care of mutexes to avoid any data race.
type indexSet struct {
	openIndexFileFunc index.OpenIndexFileFunc
	baseIndexSet      storage.IndexSet
	tableName, userID string
	cacheLocation     string
	logger            log.Logger
	maxConcurrent     int

	lastUsedAt time.Time
	index      map[string]index.Index
	indexMtx   *mtxWithReadiness
	err        error

	cancelFunc context.CancelFunc // helps with cancellation of initialization if we are asked to stop.
}

func NewIndexSet(tableName, userID, cacheLocation string, baseIndexSet storage.IndexSet, openIndexFileFunc index.OpenIndexFileFunc, logger log.Logger) (IndexSet, error) {
	if baseIndexSet.IsUserBasedIndexSet() && userID == "" {
		return nil, fmt.Errorf("userID must not be empty")
	} else if !baseIndexSet.IsUserBasedIndexSet() && userID != "" {
		return nil, fmt.Errorf("userID must be empty")
	}

	err := util.EnsureDirectory(cacheLocation)
	if err != nil {
		return nil, err
	}

	maxConcurrent := max(runtime.GOMAXPROCS(0)/2, 1)

	is := indexSet{
		openIndexFileFunc: openIndexFileFunc,
		baseIndexSet:      baseIndexSet,
		tableName:         tableName,
		userID:            userID,
		cacheLocation:     cacheLocation,
		logger:            logger,
		maxConcurrent:     maxConcurrent,
		lastUsedAt:        time.Now(),
		index:             map[string]index.Index{},
		indexMtx:          newMtxWithReadiness(),
		cancelFunc:        func() {},
	}

	return &is, nil
}

// Init downloads all the db files for the table from object storage.
func (t *indexSet) Init(forQuerying bool, logger log.Logger) (err error) {
	// Using background context to avoid cancellation of download when request times out.
	// We would anyways need the files for serving next requests.
	ctx := context.Background()
	ctx, t.cancelFunc = context.WithTimeout(ctx, downloadTimeout)

	defer func() {
		if err != nil {
			level.Error(logger).Log("msg", "failed to initialize table, cleaning it up", "table", t.tableName, "err", err)
			t.err = err

			// cleaning up files due to error to avoid returning invalid results.
			for fileName := range t.index {
				if err := t.cleanupDB(fileName); err != nil {
					level.Error(logger).Log("msg", "failed to cleanup partially downloaded file", "filename", fileName, "err", err)
				}
			}
		}
		t.indexMtx.markReady()
		t.cancelFunc()
	}()

	dirEntries, err := os.ReadDir(t.cacheLocation)
	if err != nil {
		return err
	}

	// open all the locally present files first to avoid downloading them again during sync operation below.
	for _, entry := range dirEntries {
		if entry.IsDir() {
			continue
		}

		fullPath := filepath.Join(t.cacheLocation, entry.Name())
		// if we fail to open an index file, lets skip it and let sync operation re-download the file from storage.
		idx, err := t.openIndexFileFunc(fullPath)
		if err != nil {
			level.Error(logger).Log("msg", fmt.Sprintf("failed to open existing index file %s, removing the file and continuing without it to let the sync operation catch up", fullPath), "err", err)
			// Sometimes files get corrupted when the process gets killed in the middle of a download operation which can cause problems in reading the file.
			// Implementation of openIndexFileFunc should take care of gracefully handling corrupted files.
			// Let us just remove the file and let the sync operation re-download it.
			if err := os.Remove(fullPath); err != nil {
				level.Error(logger).Log("msg", fmt.Sprintf("failed to remove index file %s which failed to open", fullPath))
			}
			continue
		}

		t.index[entry.Name()] = idx
	}

	level.Debug(logger).Log("msg", fmt.Sprintf("opened %d local files, now starting sync operation", len(t.index)))

	// sync the table to get new files and remove the deleted ones from storage.
	err = t.syncWithRetry(ctx, false, forQuerying)
	if err != nil {
		return
	}

	level.Debug(logger).Log("msg", "finished syncing files")

	return
}

// Close Closes references to all the index.
func (t *indexSet) Close() {
	// stop the initialization if it is still ongoing.
	t.cancelFunc()

	err := t.indexMtx.lock(context.Background())
	if err != nil {
		level.Error(t.logger).Log("msg", "failed to acquire lock for closing index", "err", err)
		return
	}
	defer t.indexMtx.unlock()

	for name, db := range t.index {
		if err := db.Close(); err != nil {
			level.Error(t.logger).Log("msg", fmt.Sprintf("failed to close file %s", name), "err", err)
		}
	}

	t.index = map[string]index.Index{}
}

func (t *indexSet) ForEach(ctx context.Context, callback index.ForEachIndexCallback) error {
	if err := t.indexMtx.rLock(ctx); err != nil {
		return err
	}
	defer t.indexMtx.rUnlock()

	logger := spanlogger.FromContextWithFallback(ctx, t.logger)
	level.Debug(logger).Log("index-files-count", len(t.index))

	for _, idx := range t.index {
		if err := callback(t.userID == "", idx); err != nil {
			return err
		}
	}

	return nil
}

func (t *indexSet) ForEachConcurrent(ctx context.Context, callback index.ForEachIndexCallback) error {

	if err := t.indexMtx.rLock(ctx); err != nil {
		return err
	}
	defer t.indexMtx.rUnlock()

	logger := spanlogger.FromContextWithFallback(ctx, t.logger)
	level.Debug(logger).Log("index-files-count", len(t.index))

	if len(t.index) == 0 {
		return nil
	}

	// shortcut; if there's only one index, there's no need for bounded concurrency
	if len(t.index) == 1 {
		for i := range t.index {
			idx := t.index[i]
			return callback(t.userID == "", idx)
		}
	}

	//nolint:ineffassign,staticcheck
	g, ctx := errgroup.WithContext(ctx)
	if t.maxConcurrent == 0 {
		panic("maxConcurrent cannot be 0, indexSet is being initialized without setting maxConcurrent")
	}
	g.SetLimit(t.maxConcurrent)

	for i := range t.index {
		idx := t.index[i]
		g.Go(func() error {
			return callback(t.userID == "", idx)
		})
	}
	return g.Wait()
}

// DropAllDBs closes reference to all the open index and removes the local files.
func (t *indexSet) DropAllDBs() error {
	err := t.indexMtx.lock(context.Background())
	if err != nil {
		return err
	}
	defer t.indexMtx.unlock()

	for fileName := range t.index {
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
	df, ok := t.index[fileName]
	if !ok {
		return fmt.Errorf("file %s not found in files collection for cleaning up", fileName)
	}

	filePath := df.Path()

	if err := df.Close(); err != nil {
		return err
	}

	delete(t.index, fileName)

	return os.Remove(filePath)
}

func (t *indexSet) Sync(ctx context.Context) (err error) {
	return t.syncWithRetry(ctx, true, false)
}

// syncWithRetry runs a sync with upto maxSyncRetries on failure
func (t *indexSet) syncWithRetry(ctx context.Context, lock, bypassListCache bool) error {
	var err error
	for i := 0; i <= maxSyncRetries; i++ {
		err = t.sync(ctx, lock, bypassListCache)
		if err == nil {
			return nil
		}

		if errors.Is(err, errIndexListCacheTooStale) && i < maxSyncRetries {
			level.Info(t.logger).Log("msg", "we have hit stale list cache, refreshing it before retrying")
			t.baseIndexSet.RefreshIndexTableCache(ctx, t.tableName)
		}

		level.Error(t.logger).Log("msg", "sync failed, retrying it", "err", err)
	}

	return err
}

// sync downloads updated and new files from the storage relevant for the table and removes the deleted ones
func (t *indexSet) sync(ctx context.Context, lock, bypassListCache bool) (err error) {
	level.Debug(t.logger).Log("msg", fmt.Sprintf("syncing files for table %s", t.tableName))

	toDownload, toDelete, err := t.checkStorageForUpdates(ctx, lock, bypassListCache)
	if err != nil {
		return err
	}

	level.Debug(t.logger).Log("msg", fmt.Sprintf("updates for table %s. toDownload: %s, toDelete: %s", t.tableName, toDownload, toDelete))

	downloadedFiles, err := t.doConcurrentDownload(ctx, toDownload)
	if err != nil {
		return err
	}

	// if we did not bypass list cache and skipped downloading all the new files due to them being removed by compaction,
	// it means the cache is not valid anymore since compaction would have happened after last index list cache refresh.
	// Let us return error to ask the caller to re-run the sync after the list cache refresh.
	if !bypassListCache && len(downloadedFiles) == 0 && len(toDownload) > 0 {
		level.Error(t.logger).Log("msg", "we skipped downloading all the new files, possibly removed by compaction", "files", fmt.Sprint(toDownload))
		return errIndexListCacheTooStale
	}

	if lock {
		err = t.indexMtx.lock(ctx)
		if err != nil {
			return err
		}
		defer t.indexMtx.unlock()
	}

	for _, fileName := range downloadedFiles {
		filePath := filepath.Join(t.cacheLocation, fileName)
		idx, err := t.openIndexFileFunc(filePath)
		if err != nil {
			return err
		}

		t.index[fileName] = idx
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
		err = t.indexMtx.rLock(ctx)
		if err != nil {
			return nil, nil, err
		}
		defer t.indexMtx.rUnlock()
	}

	for _, file := range files {
		normalized := strings.TrimSuffix(file.Name, gzipExtension)
		listedDBs[normalized] = struct{}{}

		// Checking whether file was already downloaded, if not, download it.
		// We do not ever upload files in the object store with the same name but different contents so we do not consider downloading modified files again.
		_, ok := t.index[normalized]
		if !ok {
			toDownload = append(toDownload, file)
		}
	}

	for db := range t.index {
		if _, isOK := listedDBs[db]; !isOK {
			toDelete = append(toDelete, db)
		}
	}

	return
}

func (t *indexSet) AwaitReady(ctx context.Context, reason string) error {
	start := time.Now()
	err := t.indexMtx.awaitReady(ctx)
	level.Info(t.logger).Log(
		"msg", "waited for index set to become ready",
		"reason", reason,
		"table", t.tableName,
		"user", t.userID,
		"wait_time", time.Since(start),
		"err", err,
	)
	return err
}

func (t *indexSet) downloadFileFromStorage(ctx context.Context, fileName, folderPathForTable string) (string, error) {
	decompress := storage.IsCompressedFile(fileName)
	dst := filepath.Join(folderPathForTable, fileName)
	if decompress {
		dst = strings.Trim(dst, gzipExtension)
	}
	return filepath.Base(dst), storage.DownloadFileFromStorage(
		dst,
		decompress,
		true,
		storage.LoggerWithFilename(t.logger, fileName),
		func() (io.ReadCloser, error) {
			return t.baseIndexSet.GetFile(ctx, t.tableName, t.userID, fileName)
		},
	)
}

// doConcurrentDownload downloads objects(files) concurrently. It ignores only missing file errors caused by removal of file by compaction.
// It returns the names of the files downloaded successfully and leaves it upto the caller to open those files.
func (t *indexSet) doConcurrentDownload(ctx context.Context, files []storage.IndexFile) ([]string, error) {
	downloadedFiles := make([]string, 0, len(files))
	downloadedFilesMtx := sync.Mutex{}

	err := concurrency.ForEachJob(ctx, len(files), maxDownloadConcurrency, func(ctx context.Context, idx int) error {
		fileName, err := t.downloadFileFromStorage(ctx, files[idx].Name, t.cacheLocation)
		if err != nil {
			if t.baseIndexSet.IsFileNotFoundErr(err) {
				level.Info(t.logger).Log("msg", fmt.Sprintf("ignoring missing file %s, possibly removed during compaction", fileName))
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
