package compactor

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/pkg/storage/chunk/local"
	"github.com/grafana/loki/pkg/storage/chunk/util"
	"github.com/grafana/loki/pkg/storage/stores/shipper/compactor/retention"
	"github.com/grafana/loki/pkg/storage/stores/shipper/storage"
	shipper_util "github.com/grafana/loki/pkg/storage/stores/shipper/util"
	util_log "github.com/grafana/loki/pkg/util/log"
)

const userIndexReadinessTimeout = 15 * time.Minute

// indexSet helps with doing operations on a set of index files belonging to a single user or common index files shared by users.
type indexSet struct {
	ctx               context.Context
	tableName, userID string
	workingDir        string
	baseIndexSet      storage.IndexSet

	compactedDBRecreated bool
	uploadCompactedDB    bool
	removeSourceObjects  bool

	compactedDB   *bbolt.DB
	sourceObjects []storage.IndexFile
	logger        log.Logger
	ready         chan struct{}
	err           error
}

// newCommonIndex initializes a new index set for common index. It simply creates instance of indexSet without any processing.
func newCommonIndex(ctx context.Context, tableName, workingDir string, compactedDB *bbolt.DB, uploadCompactedDB bool,
	sourceFiles []storage.IndexFile, removeSourceFiles bool, baseCommonIndexSet storage.IndexSet, logger log.Logger) (*indexSet, error) {
	if baseCommonIndexSet.IsUserBasedIndexSet() {
		return nil, fmt.Errorf("base index set is not for common index")
	}

	ui := indexSet{
		ctx:                 ctx,
		tableName:           tableName,
		workingDir:          workingDir,
		baseIndexSet:        baseCommonIndexSet,
		compactedDB:         compactedDB,
		uploadCompactedDB:   uploadCompactedDB,
		sourceObjects:       sourceFiles,
		removeSourceObjects: removeSourceFiles,
		logger:              logger,
		ready:               make(chan struct{}),
	}
	close(ui.ready)

	return &ui, nil
}

// newUserIndex intializes a new index set for user index. Other than creating instance of indexSet, it also compacts down the source index.
func newUserIndex(ctx context.Context, tableName, userID string, baseUserIndexSet storage.IndexSet, workingDir string, logger log.Logger) (*indexSet, error) {
	if !baseUserIndexSet.IsUserBasedIndexSet() {
		return nil, fmt.Errorf("base index set is not for user index")
	}

	if err := util.EnsureDirectory(workingDir); err != nil {
		return nil, err
	}

	ui := &indexSet{
		ctx:          ctx,
		tableName:    tableName,
		userID:       userID,
		workingDir:   workingDir,
		baseIndexSet: baseUserIndexSet,
		logger:       log.With(logger, "user-id", userID),
		ready:        make(chan struct{}),
	}

	ui.initUserIndexSet(workingDir)
	return ui, nil
}

// initUserIndexSet downloads the source index files and compacts them down to a single index file.
func (is *indexSet) initUserIndexSet(workingDir string) {
	defer close(is.ready)
	ctx, cancelFunc := context.WithTimeout(is.ctx, userIndexReadinessTimeout)
	defer cancelFunc()

	is.sourceObjects, is.err = is.baseIndexSet.ListFiles(is.ctx, is.tableName, is.userID)
	if is.err != nil {
		return
	}

	compactedDBName := filepath.Join(workingDir, fmt.Sprint(time.Now().Unix()))
	seedFileIdx := compactedFileIdx(is.sourceObjects)

	if len(is.sourceObjects) > 0 {
		// we would only have compacted files in user index folder, so it is not expected to have -1 for seedFileIdx but
		// let's still check it as a safety mechanism to avoid panics.
		if seedFileIdx == -1 {
			seedFileIdx = 0
		}
		compactedDBName = filepath.Join(workingDir, is.sourceObjects[seedFileIdx].Name)
		is.err = shipper_util.DownloadFileFromStorage(compactedDBName, shipper_util.IsCompressedFile(is.sourceObjects[seedFileIdx].Name),
			false, shipper_util.LoggerWithFilename(is.logger, is.sourceObjects[seedFileIdx].Name),
			func() (io.ReadCloser, error) {
				return is.baseIndexSet.GetFile(ctx, is.tableName, is.userID, is.sourceObjects[seedFileIdx].Name)
			})
		if is.err != nil {
			return
		}

		// if we have more than 1 file, we would compact it down so upload the compacted DB and remove the source objects
		if len(is.sourceObjects) > 1 {
			is.uploadCompactedDB = true
			is.removeSourceObjects = true
		}
	}

	is.compactedDB, is.err = openBoltdbFileWithNoSync(compactedDBName)
	if is.err != nil {
		return
	}

	for i, object := range is.sourceObjects {
		if i == seedFileIdx {
			continue
		}
		downloadAt := filepath.Join(workingDir, object.Name)

		is.err = shipper_util.DownloadFileFromStorage(downloadAt, shipper_util.IsCompressedFile(object.Name),
			false, shipper_util.LoggerWithFilename(is.logger, object.Name),
			func() (io.ReadCloser, error) {
				return is.baseIndexSet.GetFile(ctx, is.tableName, is.userID, object.Name)
			})
		if is.err != nil {
			return
		}

		is.err = readFile(util_log.Logger, downloadAt, is.writeBatch)
		if is.err != nil {
			return
		}
	}
}

// recreateCompactedDB just copies the old db to the new one using bbolt.Compact for following reasons:
// 1. When index entries are deleted, boltdb leaves free pages in the file. The only way to drop those free pages is to re-create the file.
//    See https://github.com/boltdb/bolt/issues/308 for more details.
// 2. boltdb by default fills only about 50% of the page in the file. See https://github.com/etcd-io/bbolt/blob/master/bucket.go#L26.
//    This setting is optimal for unordered writes.
//    bbolt.Compact fills the whole page by setting FillPercent to 1 which works well here since while copying the data, it receives the index entries in order.
//    The storage space goes down from anywhere between 25% to 50% as per my(Sandeep) tests.
func (is *indexSet) recreateCompactedDB() error {
	destDB, err := openBoltdbFileWithNoSync(filepath.Join(is.workingDir, fmt.Sprint(time.Now().Unix())))
	if err != nil {
		return err
	}

	level.Info(is.logger).Log("msg", "recreating compacted db")

	err = bbolt.Compact(destDB, is.compactedDB, dropFreePagesTxMaxSize)
	if err != nil {
		return err
	}

	sourceSize := int64(0)
	destSize := int64(0)

	if err := is.compactedDB.View(func(tx *bbolt.Tx) error {
		sourceSize = tx.Size()
		return nil
	}); err != nil {
		return err
	}

	if err := destDB.View(func(tx *bbolt.Tx) error {
		destSize = tx.Size()
		return nil
	}); err != nil {
		return err
	}

	level.Info(is.logger).Log("msg", "recreated compacted db", "src_size_bytes", sourceSize, "dest_size_bytes", destSize)

	err = is.compactedDB.Close()
	if err != nil {
		return err
	}

	is.compactedDB = destDB
	is.uploadCompactedDB = true
	is.removeSourceObjects = true
	is.compactedDBRecreated = true
	return nil
}

// isReady checks whether indexSet is ready for usage and whether there was an error during initialization.
// All the external usages should check the readiness before using an instance of indexSet.
func (is *indexSet) isReady() error {
	select {
	case <-is.ready:
		return is.err
	case <-is.ctx.Done():
		return is.ctx.Err()
	}
}

// writeBatch writes a batch to compactedDB
func (is *indexSet) writeBatch(_ string, batch []indexEntry) error {
	is.uploadCompactedDB = true
	is.removeSourceObjects = true
	return is.compactedDB.Batch(func(tx *bbolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(local.IndexBucketName)
		if err != nil {
			return err
		}

		for _, w := range batch {
			err = b.Put(w.k, w.v)
			if err != nil {
				return err
			}
		}

		return nil
	})
}

// runRetention runs the retention on index set
func (is *indexSet) runRetention(tableMarker retention.TableMarker) error {
	empty, modified, err := tableMarker.MarkForDelete(is.ctx, is.tableName, is.userID, is.compactedDB, is.logger)
	if err != nil {
		return err
	}

	if empty {
		is.uploadCompactedDB = false
		is.removeSourceObjects = true
	} else if modified {
		is.uploadCompactedDB = true
		is.removeSourceObjects = true
	}

	return nil
}

// upload uploads the compacted db in compressed format.
func (is *indexSet) upload() error {
	compactedDBPath := is.compactedDB.Path()

	// close the compactedDB to make sure all the writes are processed.
	err := is.compactedDB.Close()
	if err != nil {
		return err
	}

	is.compactedDB = nil

	fileNameFormat := "%s.gz"
	if is.compactedDBRecreated {
		fileNameFormat = "%s" + recreatedCompactedDBSuffix
	}
	fileName := fmt.Sprintf(fileNameFormat, shipper_util.BuildIndexFileName(is.tableName, uploaderName, fmt.Sprint(time.Now().Unix())))

	return uploadFile(compactedDBPath, func(file io.ReadSeeker) error {
		return is.baseIndexSet.PutFile(is.ctx, is.tableName, is.userID, fileName, file)
	}, is.logger)
}

// removeFilesFromStorage deletes source objects from storage.
func (is *indexSet) removeFilesFromStorage() error {
	level.Info(is.logger).Log("msg", "removing source db files from storage", "count", len(is.sourceObjects))

	for _, object := range is.sourceObjects {
		err := is.baseIndexSet.DeleteFile(is.ctx, is.tableName, is.userID, object.Name)
		if err != nil {
			return err
		}
	}

	return nil
}

// done takes care of file operations which includes:
// - recreate the compacted db if required.
// - upload the compacted db if required.
// - remove the source objects from storage if required.
func (is *indexSet) done() error {
	if !is.uploadCompactedDB && !is.removeSourceObjects && mustRecreateCompactedDB(is.sourceObjects) {
		if err := is.recreateCompactedDB(); err != nil {
			return err
		}
	}

	if is.uploadCompactedDB {
		if err := is.upload(); err != nil {
			return err
		}
	}

	if is.removeSourceObjects {
		return is.removeFilesFromStorage()
	}

	return nil
}

func (is *indexSet) cleanup() {
	if is.compactedDB != nil {
		err := is.compactedDB.Close()
		if err != nil {
			level.Error(is.logger).Log("msg", fmt.Sprintf("failed to close compacted db %s", is.compactedDB.Path()), "err", err)
		}
	}
}
