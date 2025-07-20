package compactor

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/v3/pkg/compactor"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/testutil"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

const (
	objectsStorageDirName = "objects"
	workingDirName        = "working-dir"
	tableName             = "test"
)

type indexSetState struct {
	uploadCompactedDB   bool
	removeSourceObjects bool
}

type mockIndexSet struct {
	userID            string
	tableName         string
	workingDir        string
	sourceFiles       []storage.IndexFile
	objectClient      client.ObjectClient
	compactedIndex    compactor.CompactedIndex
	removeSourceFiles bool
}

func newMockIndexSet(userID, tableName, workingDir string, objectClient client.ObjectClient) (compactor.IndexSet, error) {
	err := util.EnsureDirectory(workingDir)
	if err != nil {
		return nil, err
	}
	objects, _, err := objectClient.List(context.Background(), path.Join(tableName, userID), "/")
	if err != nil {
		return nil, err
	}

	sourceFiles := make([]storage.IndexFile, 0, len(objects))
	for _, obj := range objects {
		sourceFiles = append(sourceFiles, storage.IndexFile{
			Name:       path.Base(obj.Key),
			ModifiedAt: obj.ModifiedAt,
		})
	}

	return &mockIndexSet{
		userID:       userID,
		tableName:    tableName,
		workingDir:   workingDir,
		sourceFiles:  sourceFiles,
		objectClient: objectClient,
	}, nil
}

func (m *mockIndexSet) GetTableName() string {
	return m.tableName
}

func (m *mockIndexSet) ListSourceFiles() []storage.IndexFile {
	return m.sourceFiles
}

func (m *mockIndexSet) GetSourceFile(indexFile storage.IndexFile) (string, error) {
	decompress := storage.IsCompressedFile(indexFile.Name)
	dst := filepath.Join(m.workingDir, indexFile.Name)
	if decompress {
		dst = strings.Trim(dst, ".gz")
	}

	err := storage.DownloadFileFromStorage(dst, storage.IsCompressedFile(indexFile.Name),
		false, storage.LoggerWithFilename(util_log.Logger, indexFile.Name),
		func() (io.ReadCloser, error) {
			rc, _, err := m.objectClient.GetObject(context.Background(), path.Join(m.tableName, m.userID, indexFile.Name))
			return rc, err
		})
	if err != nil {
		return "", err
	}

	return dst, nil

}

func (m *mockIndexSet) GetLogger() log.Logger {
	return util_log.Logger
}

func (m *mockIndexSet) GetWorkingDir() string {
	return m.workingDir
}

func (m *mockIndexSet) SetCompactedIndex(compactedIndex compactor.CompactedIndex, removeSourceFiles bool) error {
	m.compactedIndex = compactedIndex
	m.removeSourceFiles = removeSourceFiles
	return nil
}

func TestTable_Compaction(t *testing.T) {
	for _, numUsers := range []int{5, 10, 20} {
		t.Run(fmt.Sprintf("numUsers=%d", numUsers), func(t *testing.T) {
			for _, tc := range []struct {
				numUnCompactedCommonDBs  int
				numUnCompactedPerUserDBs int
				numCompactedDBs          int

				commonIndexSetState *indexSetState
				userIndexSetState   *indexSetState
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

					// do the compaction
					objectClient, err := local.NewFSObjectClient(local.FSConfig{Directory: objectStoragePath})
					require.NoError(t, err)

					_, commonPrefixes, err := objectClient.List(context.Background(), tableName, "/")
					require.NoError(t, err)

					existingUserIndexSets := make(map[string]compactor.IndexSet, len(commonPrefixes))
					for _, commonPrefix := range commonPrefixes {
						userID := path.Base(string(commonPrefix))
						idxSet, err := newMockIndexSet(userID, tableName, filepath.Join(tableWorkingDirectory, userID), objectClient)
						require.NoError(t, err)

						existingUserIndexSets[userID] = idxSet
					}

					commonIndexSet, err := newMockIndexSet("", tableName, tableWorkingDirectory, objectClient)
					require.NoError(t, err)

					tCompactor := newTableCompactor(context.Background(), commonIndexSet, existingUserIndexSets, func(userID string) (compactor.IndexSet, error) {
						return newMockIndexSet(userID, tableName, filepath.Join(tableWorkingDirectory, userID), objectClient)
					}, config.PeriodConfig{})

					require.NoError(t, tCompactor.CompactTable())

					if len(commonIndexSet.ListSourceFiles()) > 1 || (commonDBsConfig.NumUnCompactedDBs+perUserDBsConfig.NumUnCompactedDBs) > 0 {
						commonIndexSet := tCompactor.commonIndexSet.(*mockIndexSet)
						require.True(t, commonIndexSet.removeSourceFiles)
						if (commonDBsConfig.NumUnCompactedDBs + commonDBsConfig.NumCompactedDBs) > 0 {
							require.NotNil(t, commonIndexSet.compactedIndex)
						} else {
							require.Nil(t, commonIndexSet.compactedIndex)
						}
					} else {
						commonIndexSet := tCompactor.commonIndexSet.(*mockIndexSet)
						require.Nil(t, commonIndexSet.compactedIndex)
						require.False(t, commonIndexSet.removeSourceFiles, fmt.Sprint(commonIndexSet.removeSourceFiles))
					}

					if perUserDBsConfig.NumCompactedDBs+perUserDBsConfig.NumUnCompactedDBs > 1 {
						require.Equal(t, numUsers, len(tCompactor.userCompactedIndexSet))
					}

					// make sure we have same data after compaction
					compareCompactedTable(t, tablePathInStorage, tCompactor)

					// cleanup
					if compactedIndex := tCompactor.commonIndexSet.(*mockIndexSet).compactedIndex; compactedIndex != nil {
						compactedIndex.Cleanup()
					}
					for _, cui := range tCompactor.userCompactedIndexSet {
						cui.compactedIndex.Cleanup()
					}
				})
			}
		})
	}
}

func TestTable_RecreateCompactedDB(t *testing.T) {
	for name, tt := range map[string]struct {
		dbCount                   int
		compactedDBMtime          time.Time
		alreadyRecreated          bool
		shouldRecreateCompactedDB bool
	}{
		// must not recreate compacted db test cases:
		"more than 1 file in table": {
			dbCount: 2,
		},
		"compacted db not old enough": {
			dbCount:          1,
			compactedDBMtime: time.Now().Add(-recreateCompactedDBOlderThan / 2),
		},
		"compacted db old enough but already recreated": {
			dbCount:          1,
			compactedDBMtime: time.Now().Add(-(recreateCompactedDBOlderThan + time.Minute)),
			alreadyRecreated: true,
		},

		// must recreate compacted db test cases
		"compacted db old enough": {
			dbCount:                   1,
			compactedDBMtime:          time.Now().Add(-(recreateCompactedDBOlderThan + time.Minute)),
			shouldRecreateCompactedDB: true,
		},
	} {
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
						if tt.alreadyRecreated {
							oldPath := path
							path = fmt.Sprintf("%s%s", path, recreatedCompactedDBSuffix)
							require.NoError(t, os.Rename(oldPath, path))
						}
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

			_, commonPrefixes, err := objectClient.List(context.Background(), tableName, "/")
			require.NoError(t, err)

			existingUserIndexSets := make(map[string]compactor.IndexSet, len(commonPrefixes))
			for _, commonPrefix := range commonPrefixes {
				userID := path.Base(string(commonPrefix))
				idxSet, err := newMockIndexSet(userID, tableName, filepath.Join(tableWorkingDirectory, userID), objectClient)
				require.NoError(t, err)

				existingUserIndexSets[userID] = idxSet
			}

			commonIndexSet, err := newMockIndexSet("", tableName, tableWorkingDirectory, objectClient)
			require.NoError(t, err)

			tCompactor := newTableCompactor(context.Background(), commonIndexSet, existingUserIndexSets, func(userID string) (compactor.IndexSet, error) {
				return newMockIndexSet(userID, tableName, filepath.Join(tableWorkingDirectory, userID), objectClient)
			}, config.PeriodConfig{})

			require.NoError(t, tCompactor.CompactTable())

			if tt.shouldRecreateCompactedDB {
				require.True(t, tCompactor.commonIndexSet.(*mockIndexSet).compactedIndex.(*CompactedIndex).compactedFileRecreated)
				for _, userCompactedIndexSet := range tCompactor.userCompactedIndexSet {
					require.True(t, userCompactedIndexSet.compactedIndex.compactedFileRecreated)
				}

				// ensure that we have right data in db after recreation
				compareCompactedTable(t, tablePathInStorage, tCompactor)
			} else if tt.dbCount <= 1 {
				require.Nil(t, tCompactor.commonIndexSet.(*mockIndexSet).compactedIndex)
				uploadedCompactedIndexSets := make([]*compactedIndexSet, 0, len(tCompactor.userCompactedIndexSet))
				for _, is := range tCompactor.userCompactedIndexSet {
					uploadedCompactedIndexSets = append(uploadedCompactedIndexSets, is)
				}
				require.Len(t, uploadedCompactedIndexSets, 0)
			} else {
				require.False(t, tCompactor.commonIndexSet.(*mockIndexSet).compactedIndex.(*CompactedIndex).compactedFileRecreated)
				for _, userCompactedIndexSet := range tCompactor.userCompactedIndexSet {
					require.False(t, userCompactedIndexSet.compactedIndex.compactedFileRecreated)
				}
			}

			// cleanup
			if compactedIndex := tCompactor.commonIndexSet.(*mockIndexSet).compactedIndex; compactedIndex != nil {
				compactedIndex.Cleanup()
			}
			for _, cui := range tCompactor.userCompactedIndexSet {
				cui.compactedIndex.Cleanup()
			}
		})
	}
}

func compareCompactedTable(t *testing.T, srcTable string, tableCompactor *tableCompactor) {
	expectedRecords := make(map[string]map[string]string)
	compactedRecords := make(map[string]map[string]string)

	commonIndexSet := tableCompactor.commonIndexSet.(*mockIndexSet)
	if commonIndexSet.compactedIndex != nil {
		for bucketName, records := range readDB(t, commonIndexSet.compactedIndex.(*CompactedIndex).compactedFile) {
			if _, ok := compactedRecords[bucketName]; !ok {
				compactedRecords[bucketName] = make(map[string]string)
			}
			for k, v := range records {
				compactedRecords[bucketName][k] = v
			}
		}
	}

	if commonIndexSet.removeSourceFiles {
		for bucketName, records := range readIndexFromFiles(t, srcTable) {
			if _, ok := expectedRecords[bucketName]; !ok {
				expectedRecords[bucketName] = make(map[string]string)
			}
			for k, v := range records {
				expectedRecords[bucketName][k] = v
			}
		}
	}

	for userID, compactedIndex := range tableCompactor.userCompactedIndexSet {
		for _, userRecords := range readDB(t, compactedIndex.IndexSet.(*mockIndexSet).compactedIndex.(*CompactedIndex).compactedFile) {
			if _, ok := compactedRecords[userID]; !ok {
				compactedRecords[userID] = make(map[string]string)
			}
			for k, v := range userRecords {
				compactedRecords[userID][k] = v
			}
		}

		for bucketName, records := range readIndexFromFiles(t, filepath.Join(srcTable, userID)) {
			require.Equal(t, string(local.IndexBucketName), bucketName)
			if _, ok := expectedRecords[userID]; !ok {
				expectedRecords[userID] = make(map[string]string)
			}
			for k, v := range records {
				expectedRecords[userID][k] = v
			}
		}
	}

	require.Equal(t, expectedRecords, compactedRecords)
}

func readIndexFromFiles(t *testing.T, tablePath string) map[string]map[string]string {
	tempDir := t.TempDir()

	dirEntries, err := os.ReadDir(tablePath)
	if err != nil && os.IsNotExist(err) {
		return map[string]map[string]string{}
	}
	require.NoError(t, err)

	dbRecords := make(map[string]map[string]string)

	for _, entry := range dirEntries {
		if entry.IsDir() {
			continue
		}

		filePath := filepath.Join(tablePath, entry.Name())
		if strings.HasSuffix(filePath, ".gz") {
			filePath = filepath.Join(tempDir, entry.Name())
			testutil.DecompressFile(t, filepath.Join(tablePath, entry.Name()), filePath)
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
