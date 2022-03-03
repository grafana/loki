package compactor

import (
	"context"
	"fmt"
	"io/fs"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/pkg/storage/chunk/local"
	"github.com/grafana/loki/pkg/storage/stores/shipper/compactor/retention"
	"github.com/grafana/loki/pkg/storage/stores/shipper/storage"
	"github.com/grafana/loki/pkg/storage/stores/shipper/testutil"
)

const (
	objectsStorageDirName = "objects"
	workingDirName        = "working-dir"
	tableName             = "test"
)

type indexSetState struct {
	uploadCompactedDB   bool
	removeSourceObjects bool
	recreateCompactedDB bool
}

func TestTable_Compaction(t *testing.T) {
	numUsers := 5

	for _, tc := range []struct {
		numUnCompactedCommonDBs  int
		numUnCompactedPerUserDBs int
		numCompactedDBs          int

		shouldInitializeCommonIndexSet bool
		commonIndexSetState            *indexSetState

		shouldInitializeUserIndexSet bool
		userIndexSetState            *indexSetState
	}{
		{},
		{
			numCompactedDBs: 1,
		},
		{
			numCompactedDBs: 2,
			commonIndexSetState: &indexSetState{
				uploadCompactedDB:   true,
				removeSourceObjects: true,
			},
			userIndexSetState: &indexSetState{
				uploadCompactedDB:   true,
				removeSourceObjects: true,
			},
		},
		{
			numUnCompactedCommonDBs: 1,
			commonIndexSetState: &indexSetState{
				uploadCompactedDB:   true,
				removeSourceObjects: true,
			},
		},
		{
			numUnCompactedCommonDBs: 10,
			commonIndexSetState: &indexSetState{
				uploadCompactedDB:   true,
				removeSourceObjects: true,
			},
		},
		{
			numUnCompactedCommonDBs: 10,
			numCompactedDBs:         1,
			commonIndexSetState: &indexSetState{
				uploadCompactedDB:   true,
				removeSourceObjects: true,
			},
		},
		{
			numUnCompactedCommonDBs: 10,
			numCompactedDBs:         2,
			commonIndexSetState: &indexSetState{
				uploadCompactedDB:   true,
				removeSourceObjects: true,
			},
			userIndexSetState: &indexSetState{
				uploadCompactedDB:   true,
				removeSourceObjects: true,
			},
		},
		{
			numUnCompactedPerUserDBs: 1,
			commonIndexSetState: &indexSetState{
				removeSourceObjects: true,
			},
			userIndexSetState: &indexSetState{
				uploadCompactedDB:   true,
				removeSourceObjects: true,
			},
		},
		{
			numUnCompactedPerUserDBs: 1,
			numCompactedDBs:          1,
			commonIndexSetState: &indexSetState{
				uploadCompactedDB:   true,
				removeSourceObjects: true,
			},
			userIndexSetState: &indexSetState{
				uploadCompactedDB:   true,
				removeSourceObjects: true,
			},
		},
		{
			numUnCompactedPerUserDBs: 1,
			numCompactedDBs:          2,
			commonIndexSetState: &indexSetState{
				uploadCompactedDB:   true,
				removeSourceObjects: true,
			},
			userIndexSetState: &indexSetState{
				uploadCompactedDB:   true,
				removeSourceObjects: true,
			},
		},
		{
			numUnCompactedPerUserDBs: 10,
			commonIndexSetState: &indexSetState{
				removeSourceObjects: true,
			},
			userIndexSetState: &indexSetState{
				uploadCompactedDB:   true,
				removeSourceObjects: true,
			},
		},
		{
			numUnCompactedCommonDBs:  10,
			numUnCompactedPerUserDBs: 10,
			commonIndexSetState: &indexSetState{
				uploadCompactedDB:   true,
				removeSourceObjects: true,
			},
			userIndexSetState: &indexSetState{
				uploadCompactedDB:   true,
				removeSourceObjects: true,
			},
		},
		{
			numUnCompactedCommonDBs:  10,
			numUnCompactedPerUserDBs: 10,
			numCompactedDBs:          1,
			commonIndexSetState: &indexSetState{
				uploadCompactedDB:   true,
				removeSourceObjects: true,
			},
			userIndexSetState: &indexSetState{
				uploadCompactedDB:   true,
				removeSourceObjects: true,
			},
		},
		{
			numUnCompactedCommonDBs:  10,
			numUnCompactedPerUserDBs: 10,
			numCompactedDBs:          2,
			commonIndexSetState: &indexSetState{
				uploadCompactedDB:   true,
				removeSourceObjects: true,
			},
			userIndexSetState: &indexSetState{
				uploadCompactedDB:   true,
				removeSourceObjects: true,
			},
		},
	} {
		commonDBsConfig := testutil.DBsConfig{
			NumCompactedDBs:   tc.numCompactedDBs,
			NumUnCompactedDBs: tc.numUnCompactedCommonDBs,
		}
		perUserDBsConfig := testutil.PerUserDBsConfig{
			DBsConfig: testutil.DBsConfig{
				NumCompactedDBs:   tc.numCompactedDBs,
				NumUnCompactedDBs: tc.numUnCompactedPerUserDBs,
			},
			NumUsers: numUsers,
		}

		t.Run(fmt.Sprintf("%s ; %s", commonDBsConfig.String(), perUserDBsConfig.String()), func(t *testing.T) {
			tempDir := t.TempDir()

			objectStoragePath := filepath.Join(tempDir, objectsStorageDirName)
			tablePathInStorage := filepath.Join(objectStoragePath, tableName)
			tableWorkingDirectory := filepath.Join(tempDir, workingDirName, tableName)

			testutil.SetupTable(t, filepath.Join(objectStoragePath, tableName), commonDBsConfig, perUserDBsConfig)
			testutil.SetupTable(t, filepath.Join(objectStoragePath, fmt.Sprintf("%s-copy", tableName)), commonDBsConfig, perUserDBsConfig)

			// do the compaction
			objectClient, err := local.NewFSObjectClient(local.FSConfig{Directory: objectStoragePath})
			require.NoError(t, err)

			table, err := newTable(context.Background(), tableWorkingDirectory, storage.NewIndexStorageClient(objectClient, ""),
				nil, nil)
			require.NoError(t, err)

			require.NoError(t, table.compact(false))

			numUserIndexSets, numCommonIndexSets := 0, 0
			for _, is := range table.indexSets {
				if is.baseIndexSet.IsUserBasedIndexSet() {
					require.Equal(t, tc.userIndexSetState.uploadCompactedDB, is.uploadCompactedDB)
					require.Equal(t, tc.userIndexSetState.removeSourceObjects, is.removeSourceObjects)
					numUserIndexSets++
				} else {
					require.Equal(t, tc.commonIndexSetState.uploadCompactedDB, is.uploadCompactedDB)
					require.Equal(t, tc.commonIndexSetState.removeSourceObjects, is.removeSourceObjects)
					numCommonIndexSets++
				}
				require.False(t, is.compactedDBRecreated)
			}

			if tc.commonIndexSetState != nil {
				require.Equal(t, 1, numCommonIndexSets)
			} else {
				require.Equal(t, 0, numCommonIndexSets)
			}

			if tc.userIndexSetState != nil {
				require.Equal(t, numUsers, numUserIndexSets)
			} else {
				require.Equal(t, 0, numUserIndexSets)
			}

			// verify the state in the storage after compaction.
			expectedNumCommonDBs := 0
			if (commonDBsConfig.NumUnCompactedDBs + commonDBsConfig.NumCompactedDBs) > 0 {
				expectedNumCommonDBs = 1
			}
			numExpectedUsers := 0
			if (perUserDBsConfig.NumUnCompactedDBs + perUserDBsConfig.NumCompactedDBs) > 0 {
				numExpectedUsers = numUsers
			}
			validateTable(t, tablePathInStorage, expectedNumCommonDBs, numExpectedUsers, func(filename string) {
				require.True(t, strings.HasSuffix(filename, ".gz"))
			})

			// verify we have all the kvs in compacted db which were there in source dbs.
			compareCompactedTable(t, tablePathInStorage, filepath.Join(objectStoragePath, "test-copy"))

			// running compaction again should not do anything.
			table, err = newTable(context.Background(), tableWorkingDirectory, storage.NewIndexStorageClient(objectClient, ""),
				nil, nil)
			require.NoError(t, err)

			require.NoError(t, table.compact(false))

			for _, is := range table.indexSets {
				require.False(t, is.uploadCompactedDB)
				require.False(t, is.removeSourceObjects)
				require.False(t, is.compactedDBRecreated)
			}
		})
	}
}

type TableMarkerFunc func(ctx context.Context, tableName, userID string, db *bbolt.DB, logger log.Logger) (bool, bool, error)

func (t TableMarkerFunc) MarkForDelete(ctx context.Context, tableName, userID string, db *bbolt.DB, logger log.Logger) (bool, bool, error) {
	return t(ctx, tableName, userID, db, logger)
}

type IntervalMayHaveExpiredChunksFunc func(interval model.Interval, userID string) bool

func (f IntervalMayHaveExpiredChunksFunc) IntervalMayHaveExpiredChunks(interval model.Interval, userID string) bool {
	return f(interval, userID)
}

func TestTable_CompactionRetention(t *testing.T) {
	type dbsSetup struct {
		withCompactedDBs, withUnCompactedDBs bool
	}
	for _, setup := range []dbsSetup{
		{
			withUnCompactedDBs: true,
		},
		{
			withCompactedDBs: true,
		},
		{
			withCompactedDBs:   true,
			withUnCompactedDBs: true,
		},
	} {
		for name, tt := range map[string]struct {
			dbsSetup    dbsSetup
			dbCount     int
			assert      func(t *testing.T, storagePath, tableName string)
			tableMarker retention.TableMarker
		}{
			"emptied table": {
				dbsSetup: setup,
				assert: func(t *testing.T, storagePath, tableName string) {
					_, err := ioutil.ReadDir(filepath.Join(storagePath, tableName))
					require.True(t, os.IsNotExist(err))
				},
				tableMarker: TableMarkerFunc(func(ctx context.Context, tableName, userID string, db *bbolt.DB, logger log.Logger) (bool, bool, error) {
					return true, true, nil
				}),
			},
			"marked table": {
				dbsSetup: setup,
				assert: func(t *testing.T, storagePath, tableName string) {
					validateTable(t, filepath.Join(storagePath, tableName), 1, 10, func(filename string) {
						require.True(t, strings.HasSuffix(filename, ".gz"))
					})
					compareCompactedTable(t, filepath.Join(storagePath, tableName), filepath.Join(storagePath, fmt.Sprintf("%s-copy", tableName)))
				},
				tableMarker: TableMarkerFunc(func(ctx context.Context, tableName, userID string, db *bbolt.DB, logger log.Logger) (bool, bool, error) {
					return false, true, nil
				}),
			},
			"not modified": {
				dbsSetup: setup,
				assert: func(t *testing.T, storagePath, tableName string) {
					validateTable(t, filepath.Join(storagePath, tableName), 1, 10, func(filename string) {
						require.True(t, strings.HasSuffix(filename, ".gz"))
					})
					compareCompactedTable(t, filepath.Join(storagePath, tableName), filepath.Join(storagePath, fmt.Sprintf("%s-copy", tableName)))
				},
				tableMarker: TableMarkerFunc(func(ctx context.Context, tableName, userID string, db *bbolt.DB, logger log.Logger) (bool, bool, error) {
					return false, false, nil
				}),
			},
		} {
			tt := tt
			t.Run(name, func(t *testing.T) {
				tempDir := t.TempDir()
				tableName := fmt.Sprintf("%s12345", tableName)

				objectStoragePath := filepath.Join(tempDir, objectsStorageDirName)
				tableWorkingDirectory := filepath.Join(tempDir, workingDirName, tableName)

				commonDBsConfig := testutil.DBsConfig{}
				perUserDBsConfig := testutil.PerUserDBsConfig{}
				if tt.dbsSetup.withUnCompactedDBs {
					commonDBsConfig.NumCompactedDBs = 10
					perUserDBsConfig.NumCompactedDBs = 10
				}
				if tt.dbsSetup.withCompactedDBs {
					commonDBsConfig.NumCompactedDBs = 1
					perUserDBsConfig.NumCompactedDBs = 1
				}
				perUserDBsConfig.NumUsers = 10

				testutil.SetupTable(t, filepath.Join(objectStoragePath, tableName), commonDBsConfig, perUserDBsConfig)
				testutil.SetupTable(t, filepath.Join(objectStoragePath, fmt.Sprintf("%s-copy", tableName)), commonDBsConfig, perUserDBsConfig)

				// do the compaction
				objectClient, err := local.NewFSObjectClient(local.FSConfig{Directory: objectStoragePath})
				require.NoError(t, err)

				table, err := newTable(context.Background(), tableWorkingDirectory, storage.NewIndexStorageClient(objectClient, ""),
					tt.tableMarker, IntervalMayHaveExpiredChunksFunc(func(interval model.Interval, userID string) bool {
						return true
					}))
				require.NoError(t, err)

				require.NoError(t, table.compact(true))
				tt.assert(t, objectStoragePath, tableName)
			})
		}
	}
}

func validateTable(t *testing.T, path string, expectedNumCommonDBs, numUsers int, filesCallback func(filename string)) {
	files, folders := listDir(t, path)
	require.Len(t, files, expectedNumCommonDBs)
	require.Len(t, folders, numUsers)

	for _, fileName := range files {
		filesCallback(fileName)
	}

	for _, folder := range folders {
		files, folders := listDir(t, filepath.Join(path, folder))
		require.Len(t, files, 1)
		require.Len(t, folders, 0)

		for _, fileName := range files {
			filesCallback(fileName)
		}
	}
}

func listDir(t *testing.T, path string) (files, folders []string) {
	filesInfo, err := ioutil.ReadDir(path)
	require.NoError(t, err)

	for _, fileInfo := range filesInfo {
		if fileInfo.IsDir() {
			folders = append(folders, fileInfo.Name())
		} else {
			files = append(files, fileInfo.Name())
		}
	}

	return
}

func TestTable_CompactionFailure(t *testing.T) {
	tempDir := t.TempDir()

	tableName := "test"
	objectStoragePath := filepath.Join(tempDir, objectsStorageDirName)
	tablePathInStorage := filepath.Join(objectStoragePath, tableName)
	tableWorkingDirectory := filepath.Join(tempDir, workingDirName, tableName)

	// setup some dbs
	numDBs := 10
	numRecordsPerDB := 100

	dbsToSetup := make(map[string]testutil.DBConfig)
	for i := 0; i < numDBs; i++ {
		dbsToSetup[fmt.Sprint(i)] = testutil.DBConfig{
			CompressFile: i%2 == 0,
			DBRecords: testutil.DBRecords{
				Start:      i * numRecordsPerDB,
				NumRecords: (i + 1) * numRecordsPerDB,
			},
		}
	}

	testutil.SetupDBsAtPath(t, filepath.Join(objectStoragePath, tableName), dbsToSetup, nil)

	// put a non-boltdb file in the table which should cause the compaction to fail in the middle because it would fail to open that file with boltdb client.
	require.NoError(t, ioutil.WriteFile(filepath.Join(tablePathInStorage, "fail.txt"), []byte("fail the compaction"), 0666))

	// do the compaction
	objectClient, err := local.NewFSObjectClient(local.FSConfig{Directory: objectStoragePath})
	require.NoError(t, err)

	table, err := newTable(context.Background(), tableWorkingDirectory, storage.NewIndexStorageClient(objectClient, ""), nil, nil)
	require.NoError(t, err)

	// compaction should fail due to a non-boltdb file.
	require.Error(t, table.compact(false))

	// ensure that files in storage are intact.
	files, err := ioutil.ReadDir(tablePathInStorage)
	require.NoError(t, err)
	require.Len(t, files, numDBs+1)

	// ensure that we have cleanup the local working directory after failing the compaction.
	require.NoFileExists(t, tableWorkingDirectory)

	// remove the non-boltdb file and ensure that compaction succeeds now.
	require.NoError(t, os.Remove(filepath.Join(tablePathInStorage, "fail.txt")))

	table, err = newTable(context.Background(), tableWorkingDirectory, storage.NewIndexStorageClient(objectClient, ""), nil, nil)
	require.NoError(t, err)
	require.NoError(t, table.compact(false))

	// ensure that we have cleanup the local working directory after successful compaction.
	require.NoFileExists(t, tableWorkingDirectory)
}

func compareCompactedTable(t *testing.T, srcTable, compactedTable string) {
	require.Equal(t, readTable(t, srcTable), readTable(t, compactedTable))
}

func readTable(t *testing.T, tablePath string) map[string]map[string]string {
	tempDir := t.TempDir()

	filesInfo, err := ioutil.ReadDir(tablePath)
	require.NoError(t, err)

	dbRecords := make(map[string]map[string]string)

	for _, fileInfo := range filesInfo {
		if fileInfo.IsDir() {
			for _, userRecords := range readTable(t, filepath.Join(tablePath, fileInfo.Name())) {
				if _, ok := dbRecords[fileInfo.Name()]; !ok {
					dbRecords[fileInfo.Name()] = make(map[string]string)
				}
				for k, v := range userRecords {
					dbRecords[fileInfo.Name()][k] = v
				}
			}
			continue
		}

		filePath := filepath.Join(tablePath, fileInfo.Name())
		if strings.HasSuffix(filePath, ".gz") {
			filePath = filepath.Join(tempDir, fileInfo.Name())
			testutil.DecompressFile(t, filepath.Join(tablePath, fileInfo.Name()), filePath)
		}

		db, err := openBoltdbFileWithNoSync(filePath)
		require.NoError(t, err)
		for bucketName, records := range readDB(t, db) {
			if _, ok := dbRecords[bucketName]; !ok {
				dbRecords[bucketName] = make(map[string]string)
			}
			for k, v := range records {
				dbRecords[bucketName][k] = v
			}
		}
		require.NoError(t, db.Close())
	}

	return dbRecords
}

func readDB(t *testing.T, db *bbolt.DB) map[string]map[string]string {
	t.Helper()
	dbRecords := map[string]map[string]string{}

	err := db.View(func(tx *bbolt.Tx) error {
		return tx.ForEach(func(name []byte, b *bbolt.Bucket) error {
			dbRecords[string(name)] = map[string]string{}
			return b.ForEach(func(k, v []byte) error {
				dbRecords[string(name)][string(k)] = string(v)
				return nil
			})
		})
	})

	require.NoError(t, err)
	return dbRecords
}

func TestTable_RecreateCompactedDB(t *testing.T) {
	for name, tt := range map[string]struct {
		dbCount                   int
		assert                    func(t *testing.T, storagePath, tableName string)
		tableMarker               retention.TableMarker
		compactedDBMtime          time.Time
		shouldRecreateCompactedDB bool
		expectedIndexSetState     indexSetState
	}{
		// must not recreate compacted db test cases:
		"more than 1 file in table": {
			dbCount: 2,
			assert: func(t *testing.T, storagePath, tableName string) {
				validateTable(t, filepath.Join(storagePath, tableName), 1, 10, func(filename string) {
					require.True(t, strings.HasSuffix(filename, ".gz"))
					require.False(t, strings.HasSuffix(filename, recreatedCompactedDBSuffix))
				})
				compareCompactedTable(t, filepath.Join(storagePath, tableName), filepath.Join(storagePath, fmt.Sprintf("%s-copy", tableName)))
			},
			tableMarker: TableMarkerFunc(func(ctx context.Context, tableName, userID string, db *bbolt.DB, logger log.Logger) (bool, bool, error) {
				return false, false, nil
			}),
			expectedIndexSetState: indexSetState{
				uploadCompactedDB:   true,
				removeSourceObjects: true,
			},
		},
		"compacted db not old enough": {
			dbCount: 1,
			assert: func(t *testing.T, storagePath, tableName string) {
				validateTable(t, filepath.Join(storagePath, tableName), 1, 10, func(filename string) {
					require.True(t, strings.HasSuffix(filename, ".gz"))
					require.False(t, strings.HasSuffix(filename, recreatedCompactedDBSuffix))
				})
				compareCompactedTable(t, filepath.Join(storagePath, tableName), filepath.Join(storagePath, fmt.Sprintf("%s-copy", tableName)))
			},
			tableMarker: TableMarkerFunc(func(ctx context.Context, tableName, userID string, db *bbolt.DB, logger log.Logger) (bool, bool, error) {
				return false, false, nil
			}),
			compactedDBMtime: time.Now().Add(-recreateCompactedDBOlderThan / 2),
		},
		"marked table": {
			dbCount: 1,
			assert: func(t *testing.T, storagePath, tableName string) {
				validateTable(t, filepath.Join(storagePath, tableName), 1, 10, func(filename string) {
					require.True(t, strings.HasSuffix(filename, ".gz"))
					require.False(t, strings.HasSuffix(filename, recreatedCompactedDBSuffix))
				})
				compareCompactedTable(t, filepath.Join(storagePath, tableName), filepath.Join(storagePath, fmt.Sprintf("%s-copy", tableName)))
			},
			tableMarker: TableMarkerFunc(func(ctx context.Context, tableName, userID string, db *bbolt.DB, logger log.Logger) (bool, bool, error) {
				return false, true, nil
			}),
			expectedIndexSetState: indexSetState{
				uploadCompactedDB:   true,
				removeSourceObjects: true,
			},
		},
		"emptied table": {
			dbCount: 2,
			assert: func(t *testing.T, storagePath, tableName string) {
				_, err := ioutil.ReadDir(filepath.Join(storagePath, tableName))
				require.True(t, os.IsNotExist(err))
			},
			tableMarker: TableMarkerFunc(func(ctx context.Context, tableName, userID string, db *bbolt.DB, logger log.Logger) (bool, bool, error) {
				return true, true, nil
			}),
			expectedIndexSetState: indexSetState{
				removeSourceObjects: true,
			},
		},

		// must recreate compacted db test cases
		"compacted db old enough": {
			dbCount: 1,
			assert: func(t *testing.T, storagePath, tableName string) {
				validateTable(t, filepath.Join(storagePath, tableName), 1, 10, func(filename string) {
					require.True(t, strings.HasSuffix(filename, recreatedCompactedDBSuffix))
				})
				compareCompactedTable(t, filepath.Join(storagePath, tableName), filepath.Join(storagePath, fmt.Sprintf("%s-copy", tableName)))
			},
			tableMarker: TableMarkerFunc(func(ctx context.Context, tableName, userID string, db *bbolt.DB, logger log.Logger) (bool, bool, error) {
				return false, false, nil
			}),
			compactedDBMtime:          time.Now().Add(-(recreateCompactedDBOlderThan + time.Minute)),
			shouldRecreateCompactedDB: true,
			expectedIndexSetState: indexSetState{
				uploadCompactedDB:   true,
				removeSourceObjects: true,
				recreateCompactedDB: true,
			},
		},
	} {
		tt := tt
		t.Run(name, func(t *testing.T) {
			if !tt.compactedDBMtime.IsZero() {
				require.Equal(t, 1, tt.dbCount)
			}
			tempDir := t.TempDir()
			tableName := fmt.Sprintf("%s12345", tableName)

			objectStoragePath := filepath.Join(tempDir, objectsStorageDirName)
			tableWorkingDirectory := filepath.Join(tempDir, workingDirName, tableName)
			tablePathInStorage := filepath.Join(objectStoragePath, tableName)

			commonDBsConfig := testutil.DBsConfig{}
			perUserDBsConfig := testutil.PerUserDBsConfig{}
			if tt.dbCount == 1 {
				commonDBsConfig.NumCompactedDBs = 1
				perUserDBsConfig.NumCompactedDBs = 1
			} else {
				commonDBsConfig.NumUnCompactedDBs = tt.dbCount
				perUserDBsConfig.NumUnCompactedDBs = tt.dbCount
			}
			perUserDBsConfig.NumUsers = 10
			testutil.SetupTable(t, filepath.Join(objectStoragePath, tableName), commonDBsConfig, perUserDBsConfig)

			if !tt.compactedDBMtime.IsZero() && tt.dbCount == 1 {
				err := filepath.WalkDir(tablePathInStorage, func(path string, d fs.DirEntry, err error) error {
					require.NoError(t, err)
					if !d.IsDir() {
						return os.Chtimes(path, tt.compactedDBMtime, tt.compactedDBMtime)
					}
					return nil
				})
				require.NoError(t, err)
			}
			// setup exact same copy of dbs for comparison.
			testutil.SetupTable(t, filepath.Join(objectStoragePath, fmt.Sprintf("%s-copy", tableName)), commonDBsConfig, perUserDBsConfig)

			// do the compaction
			objectClient, err := local.NewFSObjectClient(local.FSConfig{Directory: objectStoragePath})
			require.NoError(t, err)

			table, err := newTable(context.Background(), tableWorkingDirectory, storage.NewIndexStorageClient(objectClient, ""),
				tt.tableMarker, IntervalMayHaveExpiredChunksFunc(func(interval model.Interval, userID string) bool {
					return true
				}))
			require.NoError(t, err)

			require.NoError(t, table.compact(true))
			for _, indexSet := range table.indexSets {
				require.Equal(t, tt.expectedIndexSetState.recreateCompactedDB, indexSet.compactedDBRecreated, fmt.Sprint(indexSet))
				require.Equal(t, tt.expectedIndexSetState.uploadCompactedDB, indexSet.uploadCompactedDB)
				require.Equal(t, tt.expectedIndexSetState.removeSourceObjects, indexSet.removeSourceObjects)
			}
			tt.assert(t, objectStoragePath, tableName)

			// if the compacted db was recreated, running the compaction again must not recreate the file even if the mtime is older than the threshold
			if tt.expectedIndexSetState.recreateCompactedDB {
				err := filepath.WalkDir(tablePathInStorage, func(path string, d fs.DirEntry, err error) error {
					require.NoError(t, err)
					if !d.IsDir() {
						return os.Chtimes(path, tt.compactedDBMtime, tt.compactedDBMtime)
					}
					return nil
				})
				require.NoError(t, err)

				table, err := newTable(context.Background(), tableWorkingDirectory, storage.NewIndexStorageClient(objectClient, ""),
					tt.tableMarker, IntervalMayHaveExpiredChunksFunc(func(interval model.Interval, userID string) bool {
						return true
					}))
				require.NoError(t, err)

				require.NoError(t, table.compact(true))
				for _, indexSet := range table.indexSets {
					require.Equal(t, false, indexSet.compactedDBRecreated)
					require.Equal(t, false, indexSet.uploadCompactedDB)
					require.Equal(t, false, indexSet.removeSourceObjects)
				}
				tt.assert(t, objectStoragePath, tableName)
			}
		})
	}
}
