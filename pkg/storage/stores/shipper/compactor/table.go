package compactor

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/concurrency"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/pkg/storage/chunk/local"
	chunk_util "github.com/grafana/loki/pkg/storage/chunk/util"
	"github.com/grafana/loki/pkg/storage/stores/shipper/compactor/retention"
	"github.com/grafana/loki/pkg/storage/stores/shipper/storage"
	shipper_util "github.com/grafana/loki/pkg/storage/stores/shipper/util"
	util_log "github.com/grafana/loki/pkg/util/log"
)

////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////
//                    Below we show various formats that we have for structuring index in the object store.                       //
//                                                                                                                                //
//                    FORMAT1                         FORMAT2                                      FORMAT3                        //
//                                                                                                                                //
//               table1                        table1                                 table1                                      //
//                |                             |                                      |                                          //
//                 ----> db1.gz                  ----> db1.gz                           ----> user1                               //
//                         |                             |                             |        |                                 //
//                         ----> index                    ----> user1                  |         ----> db1.gz                     //
//                                                        ----> user2                  |                 |                        //
//                                                                                     |                  ----> index             //
//                                                                                      ----> user2                               //
//                                                                                              |                                 //
//                                                                                               ----> db1.gz                     //
//                                                                                                       |                        //
//                                                                                                        ----> index             //
//                                                                                                                                //
// FORMAT1 - `table1` has 1 db named db1.gz and 1 boltdb bucket named `index` which contains index for all the users.             //
//           It is in use when the flag to build per user index is not enabled.                                                   //
//           Ingesters write the index in Format1 which then compactor compacts down in same format.                              //
//                                                                                                                                //
// FORMAT2 - `table1` has 1 db named db1.gz and 1 boltdb bucket each for `user1` and `user2` containing                       //
//           index just for those users.                                                                                          //
//           It is an intermediate format built by ingesters when the flag to build per user index is enabled.                    //
//                                                                                                                                //
// FORMAT3 - `table1` has 1 folder each for `user1` and `user2` containing index files having index just for those users.         //
//            Compactor builds index in this format from Format2.                                                                 //
//                                                                                                                                //
//                THING TO NOTE HERE IS COMPACTOR BUILDS INDEX IN FORMAT1 FROM FORMAT1 AND FORMAT3 FROM FORMAT2.                  //
////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

const (
	uploaderName = "compactor"

	readDBsConcurrency = 50
	batchSize          = 1000

	// we want to recreate compactedDB when the chances of it changing due to compaction or deletion of data are low.
	// this is to avoid recreation of the DB too often which would be too costly in a large cluster.
	recreateCompactedDBOlderThan = 12 * time.Hour
	dropFreePagesTxMaxSize       = 100 * 1024 * 1024 // 100MB
	recreatedCompactedDBSuffix   = ".r.gz"
)

type indexEntry struct {
	k, v []byte
}

type tableExpirationChecker interface {
	IntervalMayHaveExpiredChunks(interval model.Interval, userID string) bool
}

type table struct {
	name               string
	workingDirectory   string
	indexStorageClient storage.Client
	tableMarker        retention.TableMarker
	expirationChecker  tableExpirationChecker

	baseUserIndexSet, baseCommonIndexSet storage.IndexSet

	indexSets             map[string]*indexSet
	indexSetsMtx          sync.RWMutex
	usersWithPerUserIndex []string
	uploadCompactedDB     bool
	compactedDB           *bbolt.DB
	logger                log.Logger

	ctx context.Context
}

func newTable(ctx context.Context, workingDirectory string, indexStorageClient storage.Client,
	tableMarker retention.TableMarker, expirationChecker tableExpirationChecker) (*table, error) {
	err := chunk_util.EnsureDirectory(workingDirectory)
	if err != nil {
		return nil, err
	}

	table := table{
		ctx:                ctx,
		name:               filepath.Base(workingDirectory),
		workingDirectory:   workingDirectory,
		indexStorageClient: indexStorageClient,
		tableMarker:        tableMarker,
		expirationChecker:  expirationChecker,
		indexSets:          map[string]*indexSet{},
		baseUserIndexSet:   storage.NewIndexSet(indexStorageClient, true),
		baseCommonIndexSet: storage.NewIndexSet(indexStorageClient, false),
	}
	table.logger = log.With(util_log.Logger, "table-name", table.name)

	return &table, nil
}

func (t *table) compact(applyRetention bool) error {
	indexFiles, usersWithPerUserIndex, err := t.indexStorageClient.ListFiles(t.ctx, t.name)
	if err != nil {
		return err
	}

	if len(indexFiles) == 0 && len(usersWithPerUserIndex) == 0 {
		level.Info(t.logger).Log("msg", "no common index files and user index found")
		return nil
	}

	t.usersWithPerUserIndex = usersWithPerUserIndex

	level.Info(t.logger).Log("msg", "listed files", "count", len(indexFiles))

	defer func() {
		for _, is := range t.indexSets {
			is.cleanup()
		}

		if t.compactedDB != nil {
			if err := t.compactedDB.Close(); err != nil {
				level.Error(t.logger).Log("msg", "error closing compacted DB", "err", err)
			}
		}

		if err := os.RemoveAll(t.workingDirectory); err != nil {
			level.Error(t.logger).Log("msg", fmt.Sprintf("failed to remove working directory %s", t.workingDirectory), "err", err)
		}
	}()

	dbsCompacted := false

	if len(indexFiles) > 1 || (len(indexFiles) == 1 && !strings.HasPrefix(indexFiles[0].Name, uploaderName)) {
		// if we have more than 1 index file or the only file we have is not from the compactor then, we need to compact them.
		dbsCompacted = true
		if err := t.compactFiles(indexFiles); err != nil {
			return err
		}
	} else if len(indexFiles) == 1 && (applyRetention || mustRecreateCompactedDB(indexFiles)) {
		// we have just 1 common index file which is already compacted.
		// initialize common compacted db if we need to apply retention, or we need to recreate it
		downloadAt := filepath.Join(t.workingDirectory, indexFiles[0].Name)
		err = shipper_util.DownloadFileFromStorage(downloadAt, shipper_util.IsCompressedFile(indexFiles[0].Name),
			false, shipper_util.LoggerWithFilename(t.logger, indexFiles[0].Name),
			func() (io.ReadCloser, error) {
				return t.baseCommonIndexSet.GetFile(t.ctx, t.name, "", indexFiles[0].Name)
			})
		if err != nil {
			return err
		}

		t.compactedDB, err = openBoltdbFileWithNoSync(downloadAt)
		if err != nil {
			return err
		}
	}

	// initialize common index set if we have initialized compacted db.
	if t.compactedDB != nil {
		// remove the source files if we did a compaction which gets reflected in dbsCompacted
		t.indexSets[""], err = newCommonIndex(t.ctx, t.name, t.workingDirectory, t.compactedDB, t.uploadCompactedDB,
			indexFiles, dbsCompacted, t.baseCommonIndexSet, t.logger)
		if err != nil {
			return err
		}
	}

	if applyRetention {
		err := t.applyRetention()
		if err != nil {
			return err
		}
	}

	return t.done()
}

// done takes care of final operations which includes:
// - initializing user index sets which requires recreation of files
// - call indexSet.done() on all the index sets.
func (t *table) done() error {
	for _, userID := range t.usersWithPerUserIndex {
		if _, ok := t.indexSets[userID]; ok {
			continue
		}

		indexFiles, err := t.baseUserIndexSet.ListFiles(t.ctx, t.name, userID)
		if err != nil {
			return err
		}

		// initialize the user index sets for:
		// - compaction if we have more than 1 index file, taken care of by index set initialization
		// - recreation if mustRecreateCompactedDB says so, taken care of by indexSet.done call below
		if len(indexFiles) > 1 || mustRecreateCompactedDB(indexFiles) {
			t.indexSets[userID], err = t.getOrCreateUserIndex(userID)
			if err != nil {
				return err
			}
		}
	}

	for userID, is := range t.indexSets {
		// indexSet.done() uploads the compacted db and cleans up the source index files.
		// For user index sets, the files from common index sets are also a source of index.
		// if we cleanup common index sets first, and we fail to upload newly compacted dbs in user index sets, then we will lose data.
		// To avoid any data loss, we should call done() on common index sets at the end.
		if userID == "" {
			continue
		}

		if err := is.done(); err != nil {
			return err
		}
	}

	if commonIndexSet, ok := t.indexSets[""]; ok {
		if err := commonIndexSet.done(); err != nil {
			return err
		}
	}

	return nil
}

// applyRetention applies retention on the index sets
func (t *table) applyRetention() error {
	tableInterval := retention.ExtractIntervalFromTableName(t.name)
	// call runRetention on the already initialized index sets which may have expired chunks
	for userID, is := range t.indexSets {
		if !t.expirationChecker.IntervalMayHaveExpiredChunks(tableInterval, userID) {
			continue
		}
		err := is.runRetention(t.tableMarker)
		if err != nil {
			return err
		}
	}

	// find and call runRetention on the uninitialized index sets which may have expired chunks
	for _, userID := range t.usersWithPerUserIndex {
		if _, ok := t.indexSets[userID]; ok {
			continue
		}
		if !t.expirationChecker.IntervalMayHaveExpiredChunks(tableInterval, userID) {
			continue
		}

		var err error
		t.indexSets[userID], err = t.getOrCreateUserIndex(userID)
		if err != nil {
			return err
		}
		err = t.indexSets[userID].runRetention(t.tableMarker)
		if err != nil {
			return err
		}
	}

	return nil
}

// compactFiles compacts the given files into a single file.
func (t *table) compactFiles(files []storage.IndexFile) error {
	var err error
	level.Info(t.logger).Log("msg", "starting compaction of dbs")

	compactedDBName := filepath.Join(t.workingDirectory, fmt.Sprint(time.Now().Unix()))
	// if we find a previously compacted file, use it as a seed file to copy other index into it
	seedSourceFileIdx := compactedFileIdx(files)

	if seedSourceFileIdx != -1 {
		t.uploadCompactedDB = true
		compactedDBName = filepath.Join(t.workingDirectory, files[seedSourceFileIdx].Name)

		level.Info(t.logger).Log("msg", fmt.Sprintf("using %s as seed file", files[seedSourceFileIdx].Name))
		err = shipper_util.DownloadFileFromStorage(compactedDBName, shipper_util.IsCompressedFile(files[seedSourceFileIdx].Name),
			false, shipper_util.LoggerWithFilename(t.logger, files[seedSourceFileIdx].Name), func() (io.ReadCloser, error) {
				return t.baseCommonIndexSet.GetFile(t.ctx, t.name, "", files[seedSourceFileIdx].Name)
			})
		if err != nil {
			return err
		}
	}

	t.compactedDB, err = openBoltdbFileWithNoSync(compactedDBName)
	if err != nil {
		return err
	}

	// go through each file and build index in FORMAT1 from FORMAT1 files and FORMAT3 from FORMAT2 files
	return concurrency.ForEachJob(t.ctx, len(files), readDBsConcurrency, func(ctx context.Context, idx int) error {
		workNum := idx
		// skip seed file
		if workNum == seedSourceFileIdx {
			return nil
		}
		fileName := files[idx].Name
		downloadAt := filepath.Join(t.workingDirectory, fileName)

		err = shipper_util.DownloadFileFromStorage(downloadAt, shipper_util.IsCompressedFile(fileName),
			false, shipper_util.LoggerWithFilename(t.logger, fileName), func() (io.ReadCloser, error) {
				return t.baseCommonIndexSet.GetFile(t.ctx, t.name, "", fileName)
			})
		if err != nil {
			return err
		}

		return readFile(t.logger, downloadAt, t.writeBatch)
	})
}

// writeBatch writes a batch to compactedDB
func (t *table) writeBatch(bucketName string, batch []indexEntry) error {
	if bucketName == shipper_util.GetUnsafeString(local.IndexBucketName) {
		return t.writeCommonIndex(batch)
	}
	return t.writeUserIndex(bucketName, batch)
}

// writeCommonIndex writes a batch to compactedDB which is for FORMAT1 index
func (t *table) writeCommonIndex(batch []indexEntry) error {
	t.uploadCompactedDB = true
	return t.compactedDB.Batch(func(tx *bbolt.Tx) error {
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

// writeUserIndex sends a batch to write to the user index set which is for FORMAT3 index
func (t *table) writeUserIndex(userID string, batch []indexEntry) error {
	ui, err := t.getOrCreateUserIndex(userID)
	if err != nil {
		return errors.Wrapf(err, "failed to get user index for user %s", userID)
	}

	return ui.writeBatch(userID, batch)
}

func (t *table) getOrCreateUserIndex(userID string) (*indexSet, error) {
	// if index set is already there, use it.
	t.indexSetsMtx.RLock()
	ui, ok := t.indexSets[userID]
	t.indexSetsMtx.RUnlock()

	if !ok {
		t.indexSetsMtx.Lock()
		// check if some other competing goroutine got the lock before us and created the table, use it if so.
		ui, ok = t.indexSets[userID]
		if !ok {
			// table not found, creating one.
			level.Info(t.logger).Log("msg", fmt.Sprintf("initializing indexSet for user %s", userID))

			var err error
			ui, err = newUserIndex(t.ctx, t.name, userID, t.baseUserIndexSet, filepath.Join(t.workingDirectory, userID), t.logger)
			if err != nil {
				return nil, err
			}
			t.indexSets[userID] = ui
		}
		t.indexSetsMtx.Unlock()
	}

	return ui, ui.isReady()
}

// openBoltdbFileWithNoSync opens a boltdb file and configures it to not sync the file to disk.
// Compaction process is idempotent and we do not retain the files so there is no need to sync them to disk.
func openBoltdbFileWithNoSync(path string) (*bbolt.DB, error) {
	boltdb, err := shipper_util.SafeOpenBoltdbFile(path)
	if err != nil {
		return nil, err
	}

	// no need to enforce write to disk, we'll upload and delete the file anyway.
	boltdb.NoSync = true

	return boltdb, nil
}

// compactedFileIdx returns index of previously compacted file(which starts with uploaderName).
// If it can't find a previously compacted file, it would return -1.
func compactedFileIdx(files []storage.IndexFile) int {
	for i, file := range files {
		if strings.HasPrefix(file.Name, uploaderName) {
			return i
		}
	}

	return -1
}

// readFile reads an index file and sends batch of index to writeBatch func.
func readFile(logger log.Logger, path string, writeBatch func(userID string, batch []indexEntry) error) error {
	level.Debug(logger).Log("msg", "reading file for compaction", "path", path)

	db, err := openBoltdbFileWithNoSync(path)
	if err != nil {
		return err
	}

	defer func() {
		if err := db.Close(); err != nil {
			level.Error(logger).Log("msg", "failed to close db", "path", path, "err", err)
		}

		if err = os.Remove(path); err != nil {
			level.Error(logger).Log("msg", "failed to remove file", "path", path, "err", err)
		}
	}()

	return db.View(func(tx *bbolt.Tx) error {
		return tx.ForEach(func(name []byte, b *bbolt.Bucket) error {
			batch := make([]indexEntry, 0, batchSize)
			bucketNameStr := string(name)
			err := b.ForEach(func(k, v []byte) error {
				ie := indexEntry{
					k: make([]byte, len(k)),
					v: make([]byte, len(v)),
				}

				// make a copy since k, v are only valid for the life of the transaction.
				// See: https://godoc.org/github.com/boltdb/bolt#Cursor.Seek
				copy(ie.k, k)
				copy(ie.v, v)

				batch = append(batch, ie)

				if len(batch) == cap(batch) {
					// batch is full, write the batch and create a new one.
					err := writeBatch(bucketNameStr, batch)
					if err != nil {
						return err
					}
					// todo(cyriltovena) we should just re-slice to avoid allocations
					batch = make([]indexEntry, 0, batchSize)
				}

				return nil
			})
			if err != nil {
				return err
			}

			// write the remaining batch which might have been left unwritten due to it not being full yet.
			return writeBatch(bucketNameStr, batch)
		})
	})
}

// uploadFile uploads the compacted db in compressed format.
func uploadFile(compactedDBPath string, putFileFunc func(file io.ReadSeeker) error, logger log.Logger) error {
	// compress the compactedDB.
	compressedDBPath := fmt.Sprintf("%s.gz", compactedDBPath)
	err := shipper_util.CompressFile(compactedDBPath, compressedDBPath, false)
	if err != nil {
		return err
	}

	// open the file for reading.
	compressedDB, err := os.Open(compressedDBPath)
	if err != nil {
		return err
	}

	defer func() {
		if err := compressedDB.Close(); err != nil {
			level.Error(logger).Log("msg", "failed to close file", "path", compactedDBPath, "err", err)
		}

		if err := os.Remove(compressedDBPath); err != nil {
			level.Error(logger).Log("msg", "failed to remove file", "path", compressedDBPath, "err", err)
		}
	}()

	err = putFileFunc(compressedDB)
	if err != nil {
		return err
	}

	return nil
}

// mustRecreateCompactedDB returns true if the compacted db should be recreated
func mustRecreateCompactedDB(sourceFiles []storage.IndexFile) bool {
	if len(sourceFiles) != 1 {
		// do not recreate if there are multiple source files
		return false
	} else if time.Since(sourceFiles[0].ModifiedAt) < recreateCompactedDBOlderThan {
		// do not recreate if the source file is younger than the threshold
		return false
	}

	// recreate the compacted db only if we have not recreated it before
	return !strings.HasSuffix(sourceFiles[0].Name, recreatedCompactedDBSuffix)
}
