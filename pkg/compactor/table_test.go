package compactor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/compactor/retention"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
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

func TestTable_Compaction(t *testing.T) {
	// user counts are aligned with default upload parallelism
	for _, numUsers := range []int{5, 10, 20} {
		t.Run(fmt.Sprintf("numUsers=%d", numUsers), func(t *testing.T) {
			for _, tc := range []struct {
				numUnCompactedCommonDBs  int
				numUnCompactedPerUserDBs int
				numCompactedDBs          int

				commonIndexSetState indexSetState
				userIndexSetState   indexSetState
			}{
				{},
				{
					numCompactedDBs: 1,
				},
				{
					numCompactedDBs: 2,
					commonIndexSetState: indexSetState{
						uploadCompactedDB:   true,
						removeSourceObjects: true,
					},
					userIndexSetState: indexSetState{
						uploadCompactedDB:   true,
						removeSourceObjects: true,
					},
				},
				{
					numUnCompactedCommonDBs: 1,
					commonIndexSetState: indexSetState{
						uploadCompactedDB:   true,
						removeSourceObjects: true,
					},
				},
				{
					numUnCompactedCommonDBs: 10,
					commonIndexSetState: indexSetState{
						uploadCompactedDB:   true,
						removeSourceObjects: true,
					},
				},
				{
					numUnCompactedCommonDBs: 10,
					numCompactedDBs:         1,
					commonIndexSetState: indexSetState{
						uploadCompactedDB:   true,
						removeSourceObjects: true,
					},
				},
				{
					numUnCompactedCommonDBs: 10,
					numCompactedDBs:         2,
					commonIndexSetState: indexSetState{
						uploadCompactedDB:   true,
						removeSourceObjects: true,
					},
					userIndexSetState: indexSetState{
						uploadCompactedDB:   true,
						removeSourceObjects: true,
					},
				},
				{
					numUnCompactedPerUserDBs: 1,
					commonIndexSetState: indexSetState{
						removeSourceObjects: true,
					},
					userIndexSetState: indexSetState{
						uploadCompactedDB:   true,
						removeSourceObjects: true,
					},
				},
				{
					numUnCompactedPerUserDBs: 1,
					numCompactedDBs:          1,
					commonIndexSetState: indexSetState{
						uploadCompactedDB:   true,
						removeSourceObjects: true,
					},
					userIndexSetState: indexSetState{
						uploadCompactedDB:   true,
						removeSourceObjects: true,
					},
				},
				{
					numUnCompactedPerUserDBs: 1,
					numCompactedDBs:          2,
					commonIndexSetState: indexSetState{
						uploadCompactedDB:   true,
						removeSourceObjects: true,
					},
					userIndexSetState: indexSetState{
						uploadCompactedDB:   true,
						removeSourceObjects: true,
					},
				},
				{
					numUnCompactedPerUserDBs: 10,
					commonIndexSetState: indexSetState{
						removeSourceObjects: true,
					},
					userIndexSetState: indexSetState{
						uploadCompactedDB:   true,
						removeSourceObjects: true,
					},
				},
				{
					numUnCompactedCommonDBs:  10,
					numUnCompactedPerUserDBs: 10,
					commonIndexSetState: indexSetState{
						uploadCompactedDB:   true,
						removeSourceObjects: true,
					},
					userIndexSetState: indexSetState{
						uploadCompactedDB:   true,
						removeSourceObjects: true,
					},
				},
				{
					numUnCompactedCommonDBs:  10,
					numUnCompactedPerUserDBs: 10,
					numCompactedDBs:          1,
					commonIndexSetState: indexSetState{
						uploadCompactedDB:   true,
						removeSourceObjects: true,
					},
					userIndexSetState: indexSetState{
						uploadCompactedDB:   true,
						removeSourceObjects: true,
					},
				},
				{
					numUnCompactedCommonDBs:  10,
					numUnCompactedPerUserDBs: 10,
					numCompactedDBs:          2,
					commonIndexSetState: indexSetState{
						uploadCompactedDB:   true,
						removeSourceObjects: true,
					},
					userIndexSetState: indexSetState{
						uploadCompactedDB:   true,
						removeSourceObjects: true,
					},
				},
			} {
				commonDBsConfig := IndexesConfig{
					NumCompactedFiles:   tc.numCompactedDBs,
					NumUnCompactedFiles: tc.numUnCompactedCommonDBs,
				}
				perUserDBsConfig := PerUserIndexesConfig{
					IndexesConfig: IndexesConfig{
						NumCompactedFiles:   tc.numCompactedDBs,
						NumUnCompactedFiles: tc.numUnCompactedPerUserDBs,
					},
					NumUsers: numUsers,
				}

				t.Run(fmt.Sprintf("%s ; %s", commonDBsConfig.String(), perUserDBsConfig.String()), func(t *testing.T) {
					tempDir := t.TempDir()

					objectStoragePath := filepath.Join(tempDir, objectsStorageDirName)
					tablePathInStorage := filepath.Join(objectStoragePath, tableName)
					tableWorkingDirectory := filepath.Join(tempDir, workingDirName, tableName)

					SetupTable(t, filepath.Join(objectStoragePath, tableName), commonDBsConfig, perUserDBsConfig)

					// do the compaction
					objectClient, err := local.NewFSObjectClient(local.FSConfig{Directory: objectStoragePath})
					require.NoError(t, err)

					table, err := newTable(context.Background(), tableWorkingDirectory, storage.NewIndexStorageClient(objectClient, ""),
						newTestIndexCompactor(), config.PeriodConfig{}, nil, nil, 10)
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
					}

					// verify the state in the storage after compaction.
					expectedNumCommonDBs := 0
					if (commonDBsConfig.NumUnCompactedFiles + commonDBsConfig.NumCompactedFiles) > 0 {
						require.Equal(t, 1, numCommonIndexSets)
						expectedNumCommonDBs = 1
					}
					numExpectedUsers := 0
					if (perUserDBsConfig.NumUnCompactedFiles + perUserDBsConfig.NumCompactedFiles) > 0 {
						require.Equal(t, numUsers, numUserIndexSets)
						numExpectedUsers = numUsers
					}
					validateTable(t, tablePathInStorage, expectedNumCommonDBs, numExpectedUsers, func(filename string) {
						require.True(t, strings.HasSuffix(filename, ".gz"), filename)
					})

					verifyCompactedIndexTable(t, commonDBsConfig, perUserDBsConfig, tablePathInStorage)

					// running compaction again should not do anything.
					table, err = newTable(context.Background(), tableWorkingDirectory, storage.NewIndexStorageClient(objectClient, ""),
						newTestIndexCompactor(), config.PeriodConfig{}, nil, nil, 10)
					require.NoError(t, err)

					require.NoError(t, table.compact(false))

					for _, is := range table.indexSets {
						require.False(t, is.uploadCompactedDB)
						require.False(t, is.removeSourceObjects)
					}
				})
			}
		})
	}
}

type TableMarkerFunc func(ctx context.Context, tableName, userID string, indexFile retention.IndexProcessor, logger log.Logger) (bool, bool, error)

func (t TableMarkerFunc) MarkForDelete(ctx context.Context, tableName, userID string, indexFile retention.IndexProcessor, logger log.Logger) (bool, bool, error) {
	return t(ctx, tableName, userID, indexFile, logger)
}

type IntervalMayHaveExpiredChunksFunc func(interval model.Interval, userID string) bool

func (f IntervalMayHaveExpiredChunksFunc) IntervalMayHaveExpiredChunks(interval model.Interval, userID string) bool {
	return f(interval, userID)
}

func TestTable_CompactionRetention(t *testing.T) {
	numUsers := 10
	type dbsSetup struct {
		numUnCompactedCommonDBs  int
		numUnCompactedPerUserDBs int
		numCompactedDBs          int
	}
	for _, setup := range []dbsSetup{
		{
			numUnCompactedCommonDBs:  10,
			numUnCompactedPerUserDBs: 10,
		},
		{
			numCompactedDBs: 1,
		},
		{
			numCompactedDBs: 10,
		},
		{
			numUnCompactedCommonDBs:  10,
			numUnCompactedPerUserDBs: 10,
			numCompactedDBs:          1,
		},
		{
			numUnCompactedCommonDBs: 1,
		},
		{
			numUnCompactedPerUserDBs: 1,
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
					_, err := os.ReadDir(filepath.Join(storagePath, tableName))
					require.True(t, os.IsNotExist(err))
				},
				tableMarker: TableMarkerFunc(func(_ context.Context, _, _ string, _ retention.IndexProcessor, _ log.Logger) (bool, bool, error) {
					return true, true, nil
				}),
			},
			"marked table": {
				dbsSetup: setup,
				assert: func(t *testing.T, storagePath, tableName string) {
					expectedNumCommonDBs := 0
					if setup.numUnCompactedCommonDBs+setup.numCompactedDBs > 0 {
						expectedNumCommonDBs = 1
					}

					expectedNumUsers := 0
					if setup.numUnCompactedPerUserDBs+setup.numCompactedDBs > 0 {
						expectedNumUsers = numUsers
					}
					validateTable(t, filepath.Join(storagePath, tableName), expectedNumCommonDBs, expectedNumUsers, func(filename string) {
						require.True(t, strings.HasSuffix(filename, ".gz"))
					})
				},
				tableMarker: TableMarkerFunc(func(_ context.Context, _, _ string, _ retention.IndexProcessor, _ log.Logger) (bool, bool, error) {
					return false, true, nil
				}),
			},
			"not modified": {
				dbsSetup: setup,
				assert: func(t *testing.T, storagePath, tableName string) {
					expectedNumCommonDBs := 0
					if setup.numUnCompactedCommonDBs+setup.numCompactedDBs > 0 {
						expectedNumCommonDBs = 1
					}

					expectedNumUsers := 0
					if setup.numUnCompactedPerUserDBs+setup.numCompactedDBs > 0 {
						expectedNumUsers = numUsers
					}
					validateTable(t, filepath.Join(storagePath, tableName), expectedNumCommonDBs, expectedNumUsers, func(filename string) {
						require.True(t, strings.HasSuffix(filename, ".gz"))
					})
				},
				tableMarker: TableMarkerFunc(func(_ context.Context, _, _ string, _ retention.IndexProcessor, _ log.Logger) (bool, bool, error) {
					return false, false, nil
				}),
			},
		} {
			commonDBsConfig := IndexesConfig{
				NumCompactedFiles:   tt.dbsSetup.numCompactedDBs,
				NumUnCompactedFiles: tt.dbsSetup.numUnCompactedCommonDBs,
			}
			perUserDBsConfig := PerUserIndexesConfig{
				IndexesConfig: IndexesConfig{
					NumUnCompactedFiles: tt.dbsSetup.numUnCompactedPerUserDBs,
					NumCompactedFiles:   tt.dbsSetup.numCompactedDBs,
				},
				NumUsers: numUsers,
			}
			t.Run(fmt.Sprintf("%s - %s ; %s", name, commonDBsConfig.String(), perUserDBsConfig.String()), func(t *testing.T) {
				tempDir := t.TempDir()
				tableName := fmt.Sprintf("%s12345", tableName)

				objectStoragePath := filepath.Join(tempDir, objectsStorageDirName)
				tableWorkingDirectory := filepath.Join(tempDir, workingDirName, tableName)

				SetupTable(t, filepath.Join(objectStoragePath, tableName), commonDBsConfig, perUserDBsConfig)

				// do the compaction
				objectClient, err := local.NewFSObjectClient(local.FSConfig{Directory: objectStoragePath})
				require.NoError(t, err)

				table, err := newTable(context.Background(), tableWorkingDirectory, storage.NewIndexStorageClient(objectClient, ""),
					newTestIndexCompactor(), config.PeriodConfig{},
					tt.tableMarker, IntervalMayHaveExpiredChunksFunc(func(_ model.Interval, _ string) bool {
						return true
					}), 10)
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
	dirEntries, err := os.ReadDir(path)
	require.NoError(t, err)

	for _, entry := range dirEntries {
		if entry.IsDir() {
			folders = append(folders, entry.Name())
		} else {
			files = append(files, entry.Name())
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

	dbsToSetup := make(map[string]IndexFileConfig)
	for i := 0; i < numDBs; i++ {
		dbsToSetup[fmt.Sprint(i)] = IndexFileConfig{
			CompressFile: i%2 == 0,
		}
	}

	SetupTable(t, filepath.Join(objectStoragePath, tableName), IndexesConfig{NumCompactedFiles: numDBs}, PerUserIndexesConfig{})

	// put a corrupt zip file in the table which should cause the compaction to fail in the middle because it would fail to open that file with boltdb client.
	require.NoError(t, os.WriteFile(filepath.Join(tablePathInStorage, "fail.gz"), []byte("fail the compaction"), 0o666))

	// do the compaction
	objectClient, err := local.NewFSObjectClient(local.FSConfig{Directory: objectStoragePath})
	require.NoError(t, err)

	table, err := newTable(context.Background(), tableWorkingDirectory, storage.NewIndexStorageClient(objectClient, ""),
		newTestIndexCompactor(), config.PeriodConfig{}, nil, nil, 10)
	require.NoError(t, err)

	// compaction should fail due to a non-boltdb file.
	require.Error(t, table.compact(false))

	// ensure that files in storage are intact.
	files, err := os.ReadDir(tablePathInStorage)
	require.NoError(t, err)
	require.Len(t, files, numDBs+1)

	// ensure that we have cleanup the local working directory after failing the compaction.
	require.NoFileExists(t, tableWorkingDirectory)

	// remove the corrupt zip file and ensure that compaction succeeds now.
	require.NoError(t, os.Remove(filepath.Join(tablePathInStorage, "fail.gz")))

	table, err = newTable(context.Background(), tableWorkingDirectory, storage.NewIndexStorageClient(objectClient, ""),
		newTestIndexCompactor(), config.PeriodConfig{}, nil, nil, 10)
	require.NoError(t, err)
	require.NoError(t, table.compact(false))

	// ensure that we have cleanup the local working directory after successful compaction.
	require.NoFileExists(t, tableWorkingDirectory)
}
