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

	"github.com/grafana/loki/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper/compactor"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper/storage"
	"github.com/grafana/loki/pkg/storage/stores/shipper/util"
	shipper_util "github.com/grafana/loki/pkg/storage/stores/shipper/util"
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
		commonIndex, err := compactIndexes(t.ctx, t.periodConfig, t.commonIndexSet, func(userID string) (*CompactedIndex, error) {
			userCompactedIndexSet, err := t.getOrCreateUserCompactedIndexSet(userID)
			if err != nil {
				return nil, err
			}

			return userCompactedIndexSet.compactedIndex, nil
		})
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

	for userID, indexSet := range t.existingUserIndexSet {
		if _, ok := t.userCompactedIndexSet[userID]; ok {
			continue
		}

		// We did not have any updates for this indexSet during compaction.
		// Now see if it has more than one files to compact it down to a single file or if it requires recreation to save space.
		sourceFiles := indexSet.ListSourceFiles()
		if len(sourceFiles) > 1 || mustRecreateCompactedDB(sourceFiles) {
			userCompactedIndexSet, err := t.getOrCreateUserCompactedIndexSet(userID)
			if err != nil {
				return err
			}
			if len(sourceFiles) == 1 {
				if err := userCompactedIndexSet.compactedIndex.recreateCompactedDB(); err != nil {
					return err
				}
			}

			if err := userCompactedIndexSet.SetCompactedIndex(userCompactedIndexSet.compactedIndex, true); err != nil {
				return err
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

func (t *tableCompactor) getOrCreateUserCompactedIndexSet(userID string) (*compactedIndexSet, error) {
	t.userCompactedIndexSetMtx.RLock()
	indexSet, ok := t.userCompactedIndexSet[userID]
	t.userCompactedIndexSetMtx.RUnlock()
	if ok {
		return indexSet, nil
	}

	t.userCompactedIndexSetMtx.Lock()
	defer t.userCompactedIndexSetMtx.Unlock()

	indexSet, ok = t.userCompactedIndexSet[userID]
	if ok {
		return indexSet, nil
	}

	userIndexSet, ok := t.existingUserIndexSet[userID]
	if !ok {
		var err error
		userIndexSet, err = t.userIndexSetFactoryFunc(userID)
		if err != nil {
			return nil, err
		}

		compactedFile, err := openBoltdbFileWithNoSync(filepath.Join(userIndexSet.GetWorkingDir(), fmt.Sprint(time.Now().Unix())))
		if err != nil {
			return nil, err
		}
		compactedIndex := newCompactedIndex(compactedFile, userIndexSet.GetTableName(), userIndexSet.GetWorkingDir(), t.periodConfig, userIndexSet.GetLogger())
		t.userCompactedIndexSet[userID] = newCompactedIndexSet(userIndexSet, compactedIndex)
	} else {
		compactedIndex, err := compactIndexes(t.ctx, t.periodConfig, userIndexSet, func(userID string) (*CompactedIndex, error) {
			return nil, errors.New("compacted user index set should not be requested while compacting user index")
		})
		if err != nil {
			return nil, err
		}
		t.userCompactedIndexSet[userID] = newCompactedIndexSet(userIndexSet, compactedIndex)
	}

	return t.userCompactedIndexSet[userID], nil
}

func compactIndexes(ctx context.Context, periodConfig config.PeriodConfig, idxSet compactor.IndexSet,
	getCompactedUserIndex func(userID string) (*CompactedIndex, error)) (*CompactedIndex, error) {
	indexes := idxSet.ListSourceFiles()
	compactedFileIdx := compactedFileIdx(indexes)
	workingDir := idxSet.GetWorkingDir()
	compactedDBName := filepath.Join(workingDir, fmt.Sprint(time.Now().Unix()))

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

	// go through each file and build index in FORMAT1 from FORMAT1 indexes and FORMAT3 from FORMAT2 indexes
	err = concurrency.ForEachJob(ctx, len(indexes), readDBsConcurrency, func(ctx context.Context, idx int) error {
		workNum := idx
		// skip seed file
		if workNum == compactedFileIdx {
			return nil
		}
		downloadAt, err := idxSet.GetSourceFile(indexes[idx])
		if err != nil {
			return err
		}

		return readFile(idxSet.GetLogger(), downloadAt, func(bucketName string, batch []indexEntry) error {
			indexFile := compactedFile
			if bucketName != shipper_util.GetUnsafeString(local.IndexBucketName) {
				userIndex, err := getCompactedUserIndex(bucketName)
				if err != nil {
					return err
				}

				indexFile = userIndex.compactedFile
			}

			return writeBatch(indexFile, batch)
		})
	})
	if err != nil {
		return nil, err
	}

	return newCompactedIndex(compactedFile, idxSet.GetTableName(), workingDir, periodConfig, idxSet.GetLogger()), nil
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
func readFile(logger log.Logger, path string, writeBatch func(userID string, batch []indexEntry) error) error {
	level.Debug(logger).Log("msg", "reading file for compaction", "path", path)

	db, err := util.SafeOpenBoltdbFile(path)
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

	batch := make([]indexEntry, 0, batchSize)

	return db.View(func(tx *bbolt.Tx) error {
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
