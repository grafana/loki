package compactor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/concurrency"
	"github.com/pkg/errors"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/v3/pkg/compactor"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
	shipper_util "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/util"
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
// FORMAT2 - `table1` has 1 db named db1.gz and 1 boltdb bucket each for `user1` and `user2` containing                           //
//           index just for those users.                                                                                          //
//           It is an intermediate format built by ingesters when the flag to build per user index is enabled.                    //
//                                                                                                                                //
// FORMAT3 - `table1` has 1 folder each for `user1` and `user2` containing index files having index just for those users.         //
//            Compactor builds index in this format from Format2.                                                                 //
//                                                                                                                                //
//                THING TO NOTE HERE IS COMPACTOR BUILDS INDEX IN FORMAT1 FROM FORMAT1 AND FORMAT3 FROM FORMAT2.                  //
////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

const (
	readDBsConcurrency = 50
	uploaderName       = "compactor"

	// we want to recreate compactedDB when the chances of it changing due to compaction or deletion of data are low.
	// this is to avoid recreation of the DB too often which would be too costly in a large cluster.
	recreateCompactedDBOlderThan = 12 * time.Hour
	dropFreePagesTxMaxSize       = 100 * 1024 * 1024 // 100MB
	recreatedCompactedDBSuffix   = ".r"
)

// compactedIndexSet holds both the IndexSet and the CompactedIndex for ease.
type compactedIndexSet struct {
	compactor.IndexSet
	compactedIndex *CompactedIndex
}

func newCompactedIndexSet(indexSet compactor.IndexSet, compactedIndex *CompactedIndex) *compactedIndexSet {
	return &compactedIndexSet{
		IndexSet:       indexSet,
		compactedIndex: compactedIndex,
	}
}

type downloadedDb struct {
	path string
	db   *bbolt.DB
}

func (d *downloadedDb) cleanup(logger log.Logger) {
	if d.db != nil {
		if err := d.db.Close(); err != nil {
			level.Error(logger).Log("msg", "failed to close db", "path", d.path, "err", err)
		}
	}
	if d.path != "" {
		if err := os.Remove(d.path); err != nil {
			level.Error(logger).Log("msg", "failed to remove file", "path", d.path, "err", err)
		}
	}
}

type tableCompactor struct {
	ctx                     context.Context
	commonIndexSet          compactor.IndexSet
	existingUserIndexSet    map[string]compactor.IndexSet
	userIndexSetFactoryFunc compactor.MakeEmptyUserIndexSetFunc
	periodConfig            config.PeriodConfig

	userCompactedIndexSet    map[string]*compactedIndexSet
	userCompactedIndexSetMtx sync.RWMutex
}

func newTableCompactor(
	ctx context.Context,
	commonIndexSet compactor.IndexSet,
	existingUserIndexSet map[string]compactor.IndexSet,
	userIndexSetFactoryFunc compactor.MakeEmptyUserIndexSetFunc,
	periodConfig config.PeriodConfig,
) *tableCompactor {
	return &tableCompactor{
		ctx:                     ctx,
		commonIndexSet:          commonIndexSet,
		existingUserIndexSet:    existingUserIndexSet,
		userIndexSetFactoryFunc: userIndexSetFactoryFunc,
		userCompactedIndexSet:   map[string]*compactedIndexSet{},
		periodConfig:            periodConfig,
	}
}

func (t *tableCompactor) CompactTable() error {
	commonIndexes := t.commonIndexSet.ListSourceFiles()

	// we need to perform compaction if we have more than 1 files in the storage or the only file we have is not a compaction file.
	// if the files are already compacted we need to see if we need to recreate the compacted DB to reduce its space.
	if len(commonIndexes) > 1 || (len(commonIndexes) == 1 && !strings.HasPrefix(commonIndexes[0].Name, uploaderName)) || mustRecreateCompactedDB(commonIndexes) {
		var err error
		commonIndex, err := t.compactCommonIndexes(t.ctx)
		if err != nil {
			return err
		}

		commonIndexEmpty, err := commonIndex.isEmpty()
		if err != nil {
			return err
		}

		var commonCompactedIndex compactor.CompactedIndex
		if commonIndexEmpty {
			// compaction has resulted into empty commonIndex due to all the files being compacted away to per user index.
			commonIndex.Cleanup()
			commonIndex = nil
		} else {
			if mustRecreateCompactedDB(commonIndexes) {
				if err := commonIndex.recreateCompactedDB(); err != nil {
					return err
				}
			}
			commonCompactedIndex = commonIndex
		}

		if err := t.commonIndexSet.SetCompactedIndex(commonCompactedIndex, true); err != nil {
			return err
		}
	}

	// Make sure that compacted user indexes that received no
	// updates from ingesters during this compactor run get
	// processed: They may need to have multiple files compacted into
	// one or be recreated to pack records
	for userID, indexSet := range t.existingUserIndexSet {
		if _, ok := t.userCompactedIndexSet[userID]; ok {
			continue
		}
		// We did not have any updates for this indexSet during compaction.
		// Now see if it has more than one files to compact it down to a single file or if it requires recreation to save space.
		sourceFiles := indexSet.ListSourceFiles()
		if len(sourceFiles) > 1 || mustRecreateCompactedDB(sourceFiles) {
			userCompactedIndexSet, err := t.fetchUserCompactedIndexSet(userID)
			if err != nil {
				level.Error(indexSet.GetLogger()).Log("msg", "unable to fetch a non-updated compacted index. skipping", "err", err)
				continue
			}
			t.userCompactedIndexSet[userID] = userCompactedIndexSet

			if mustRecreateCompactedDB(sourceFiles) {
				if err := userCompactedIndexSet.compactedIndex.recreateCompactedDB(); err != nil {
					return err
				}
			}

		}
	}

	for _, userCompactedIndexSet := range t.userCompactedIndexSet {
		if err := userCompactedIndexSet.SetCompactedIndex(userCompactedIndexSet.compactedIndex, true); err != nil {
			return err
		}
	}

	return nil
}

func (t *tableCompactor) fetchUserCompactedIndexSet(userID string) (*compactedIndexSet, error) {
	userIndexSet, ok := t.existingUserIndexSet[userID]
	if !ok {
		return nil, errors.New("requested non-existing compacted tenant index")
	}

	sourceFiles := userIndexSet.ListSourceFiles()
	if len(sourceFiles) > 1 {
		compactedIndex, err := t.compactUserIndexes(userIndexSet)
		if err != nil {
			return nil, err
		}
		return newCompactedIndexSet(userIndexSet, compactedIndex), nil
	} else if len(sourceFiles) == 1 {
		indexFile, err := userIndexSet.GetSourceFile(sourceFiles[0])
		if err != nil {
			return nil, err
		}
		boltdb, err := openBoltdbFileWithNoSync(indexFile)
		if err != nil {
			return nil, err
		}

		return newCompactedIndexSet(userIndexSet, newCompactedIndex(boltdb, userIndexSet.GetTableName(), userIndexSet.GetWorkingDir(), t.periodConfig, userIndexSet.GetLogger())), nil
	}
	return nil, errors.New("attempted to fetch empty index set")

}

// This function should be safe to call from multiple concurrent
// goroutines. However, the caller should guarantee that only a single
// call is made per userID. This function does not guarantee that two
// concurrent invocations will not fetch/create the same index twice.
func (t *tableCompactor) fetchOrCreateUserCompactedIndexSet(userID string) error {
	t.userCompactedIndexSetMtx.RLock()
	_, ok := t.userCompactedIndexSet[userID]
	t.userCompactedIndexSetMtx.RUnlock()
	if ok {
		return nil
	}

	//nolint:staticcheck
	userIndexSet, ok := t.existingUserIndexSet[userID]
	var result *compactedIndexSet
	if !ok {
		var err error
		userIndexSet, err = t.userIndexSetFactoryFunc(userID)
		if err != nil {
			return err
		}

		compactedFile, err := openBoltdbFileWithNoSync(filepath.Join(userIndexSet.GetWorkingDir(), fmt.Sprint(time.Now().UnixNano())))
		if err != nil {
			return err
		}
		compactedIndex := newCompactedIndex(compactedFile, userIndexSet.GetTableName(), userIndexSet.GetWorkingDir(), t.periodConfig, userIndexSet.GetLogger())
		result = newCompactedIndexSet(userIndexSet, compactedIndex)
	} else {
		r, err := t.fetchUserCompactedIndexSet(userID)
		result = r
		if err != nil {
			return err
		}
	}

	t.userCompactedIndexSetMtx.Lock()
	defer t.userCompactedIndexSetMtx.Unlock()
	t.userCompactedIndexSet[userID] = result
	return nil
}

// Specialized compaction for user index files produced by the compactor
func (t *tableCompactor) compactUserIndexes(idxSet compactor.IndexSet) (*CompactedIndex, error) {
	indexes := idxSet.ListSourceFiles()
	workingDir := idxSet.GetWorkingDir()
	compactedDBName := filepath.Join(workingDir, fmt.Sprint(time.Now().UnixNano()))

	compactedFile, err := openBoltdbFileWithNoSync(compactedDBName)
	if err != nil {
		return nil, err
	}

	// go through each file and dump records in the local bucket of the new compacted file
	err = concurrency.ForEachJob(t.ctx, len(indexes), readDBsConcurrency, func(_ context.Context, idx int) error {
		downloadAt, err := idxSet.GetSourceFile(indexes[idx])
		if err != nil {
			return err
		}
		dbPair := downloadedDb{
			path: downloadAt,
		}
		defer dbPair.cleanup(idxSet.GetLogger())

		db, err := openBoltdbFileWithNoSync(downloadAt)
		if err != nil {
			return err
		}
		dbPair.db = db

		err = readFile(idxSet.GetLogger(), dbPair, func(_ string, batch []indexEntry) error {
			return writeBatch(compactedFile, batch)
		})
		if err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return newCompactedIndex(compactedFile, idxSet.GetTableName(), workingDir, t.periodConfig, idxSet.GetLogger()), nil
}

func (t *tableCompactor) compactCommonIndexes(ctx context.Context) (*CompactedIndex, error) {
	idxSet := t.commonIndexSet
	indexes := idxSet.ListSourceFiles()
	compactedFileIdx := compactedFileIdx(indexes)
	workingDir := idxSet.GetWorkingDir()
	compactedDBName := filepath.Join(workingDir, fmt.Sprint(time.Now().UnixNano()))

	// if we find a previously compacted file, use it as a seed file to copy other index into it
	if compactedFileIdx != -1 {
		level.Info(idxSet.GetLogger()).Log("msg", fmt.Sprintf("using %s as seed file", indexes[compactedFileIdx].Name))

		var err error
		compactedDBName, err = idxSet.GetSourceFile(indexes[compactedFileIdx])
		if err != nil {
			return nil, err
		}
	}

	compactedFile, err := openBoltdbFileWithNoSync(compactedDBName)
	if err != nil {
		return nil, err
	}

	dbsToRead := make([]downloadedDb, len(indexes))
	tenantsToFetch := make(map[string]struct{})
	var fetchStateMx sync.Mutex

	defer func() {
		err := concurrency.ForEachJob(ctx, len(dbsToRead), readDBsConcurrency, func(_ context.Context, idx int) error {
			dbsToRead[idx].cleanup(idxSet.GetLogger())
			return nil
		})
		if err != nil {
			level.Error(idxSet.GetLogger()).Log("msg", "failed to cleanup downloaded indexes", "err", err)
		}
	}()

	// fetch common index files and extract information about tenants that have records in a given file
	err = concurrency.ForEachJob(ctx, len(indexes), readDBsConcurrency, func(_ context.Context, idx int) error {
		workNum := idx
		// skip seed file
		if workNum == compactedFileIdx {
			return nil
		}
		downloadAt, err := idxSet.GetSourceFile(indexes[idx])
		if err != nil {
			return err
		}

		// We're updating the list of dbs to read in paralle,
		// however the array's pre-allocated so this access is
		// threadsafe
		// NB: It is also important for the updates to happen as
		// we fetch/open. In an error condition, this ensures
		// cleanup happens
		dbsToRead[idx].path = downloadAt

		db, err := openBoltdbFileWithNoSync(downloadAt)
		if err != nil {
			return err
		}

		dbsToRead[idx].db = db

		return db.View(func(tx *bbolt.Tx) error {
			return tx.ForEach(func(name []byte, _ *bbolt.Bucket) error {
				bucketNameStr := string(name)
				if bucketNameStr == shipper_util.GetUnsafeString(local.IndexBucketName) {
					return nil
				}
				fetchStateMx.Lock()
				defer fetchStateMx.Unlock()
				tenantsToFetch[bucketNameStr] = struct{}{}
				return nil
			})
		})

	})

	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch index files and extract tenants: ")
	}

	tenantIDsSlice := make([]string, 0, len(tenantsToFetch))
	for tenant := range tenantsToFetch {
		tenantIDsSlice = append(tenantIDsSlice, tenant)
	}

	err = concurrency.ForEachJob(ctx, len(tenantIDsSlice), readDBsConcurrency, func(_ context.Context, idx int) error {
		userID := tenantIDsSlice[idx]
		return t.fetchOrCreateUserCompactedIndexSet(userID)
	})

	if err != nil {
		return nil, errors.Wrap(err, "unable to fetch tenant seed index: ")
	}

	// go through each file and build index in FORMAT1 from FORMAT1 indexes and FORMAT3 from FORMAT2 indexes
	err = concurrency.ForEachJob(ctx, len(indexes), readDBsConcurrency, func(_ context.Context, idx int) error {
		workNum := idx
		// skip seed file
		if workNum == compactedFileIdx {
			return nil
		}
		// not locking the mutex here since there should be no writers at this point
		downloadedDB := dbsToRead[workNum]

		return readFile(idxSet.GetLogger(), downloadedDB, func(bucketName string, batch []indexEntry) error {
			indexFile := compactedFile
			if bucketName != shipper_util.GetUnsafeString(local.IndexBucketName) {
				t.userCompactedIndexSetMtx.RLock()
				userIndexSet, ok := t.userCompactedIndexSet[bucketName]
				t.userCompactedIndexSetMtx.RUnlock()
				if !ok || userIndexSet.compactedIndex == nil {
					return fmt.Errorf("index set for user %s is not initialized", bucketName)
				}

				indexFile = userIndexSet.compactedIndex.compactedFile
			}

			return writeBatch(indexFile, batch)
		})
	})

	if err != nil {
		return nil, err
	}

	return newCompactedIndex(compactedFile, idxSet.GetTableName(), workingDir, t.periodConfig, idxSet.GetLogger()), nil
}

// compactedFileIdx returns index of previously compacted file(which starts with uploaderName).
// If it can't find a previously compacted file, it would return -1.
func compactedFileIdx(commonIndexes []storage.IndexFile) int {
	for i, file := range commonIndexes {
		if strings.HasPrefix(file.Name, uploaderName) {
			return i
		}
	}

	return -1
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

// readFile reads an index file and sends batch of index to writeBatch func.
func readFile(_ log.Logger, db downloadedDb, writeBatch func(userID string, batch []indexEntry) error) error {
	batch := make([]indexEntry, 0, batchSize)

	return db.db.View(func(tx *bbolt.Tx) error {
		return tx.ForEach(func(name []byte, b *bbolt.Bucket) error {
			batch = batch[:0]
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
					batch = batch[:0]
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

// mustRecreateCompactedDB returns true if the compacted db should be recreated
func mustRecreateCompactedDB(sourceFiles []storage.IndexFile) bool {
	if len(sourceFiles) != 1 {
		// do not recreate if there are multiple source files
		return false
	} else if !strings.HasPrefix(sourceFiles[0].Name, uploaderName) {
		// do not recreate if the only file we have is not the compacted one
		return false
	} else if time.Since(sourceFiles[0].ModifiedAt) < recreateCompactedDBOlderThan {
		// do not recreate if the source file is younger than the threshold
		return false
	}

	// recreate the compacted db only if we have not recreated it before
	return !strings.Contains(sourceFiles[0].Name, recreatedCompactedDBSuffix)
}
