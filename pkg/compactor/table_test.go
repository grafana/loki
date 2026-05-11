package compactor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/compactor/deletion"
	"github.com/grafana/loki/v3/pkg/compactor/retention"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
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

					require.NoError(t, table.compact())
					require.NoError(t, table.done())
					table.cleanup()

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

					require.NoError(t, table.compact())
					require.NoError(t, table.done())
					table.cleanup()

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

func (t TableMarkerFunc) FindAndMarkChunksForDeletion(ctx context.Context, tableName, userID string, indexFile retention.IndexProcessor, logger log.Logger) (bool, bool, error) {
	return t(ctx, tableName, userID, indexFile, logger)
}

func (t TableMarkerFunc) MarkChunksForDeletion(_ string, _ []string) error {
	return nil
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

				defer table.cleanup()

				require.NoError(t, table.compact())
				require.NoError(t, table.applyRetention())
				require.NoError(t, table.done())

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
	require.Error(t, table.compact())
	table.cleanup()

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
	require.NoError(t, table.compact())
	require.NoError(t, table.done())
	table.cleanup()

	// ensure that cleanup removes the local working directory.
	require.NoFileExists(t, tableWorkingDirectory)
}

type chunkDeletionMarkerRecorder struct {
	retention.TableMarker
	chunkIDs []string
}

func (r *chunkDeletionMarkerRecorder) MarkChunksForDeletion(_ string, chunks []string) error {
	r.chunkIDs = append(r.chunkIDs, chunks...)
	return nil
}

func (r *chunkDeletionMarkerRecorder) sortEntries() {
	sort.Strings(r.chunkIDs)
}

func TestTable_applyStorageUpdates(t *testing.T) {
	user1 := "user1"

	schemaCfg := config.SchemaConfig{
		Configs: []config.PeriodConfig{
			{
				From:       dayFromTime(0),
				IndexType:  "tsdb",
				ObjectType: "filesystem",
				Schema:     "v13",
				IndexTables: config.IndexPeriodicTableConfig{
					PeriodicTableConfig: config.PeriodicTableConfig{
						Prefix: "index_",
						Period: time.Hour * 24,
					}},
				RowShards: 16,
			},
		},
	}

	var chunksToDeIndex []string
	for i := 0; i < 10; i++ {
		chunkID := schemaCfg.ExternalKey(logproto.ChunkRef{
			Fingerprint: uint64(i),
			UserID:      user1,
			From:        model.Time(i),
			Through:     model.Time(i + 1),
			Checksum:    uint32(i),
		})
		chunksToDeIndex = append(chunksToDeIndex, chunkID)
	}

	// build 10 chunks with only the first 5 having a new chunk built out of them
	var sourceChunkIDs []string
	var newChunkIDs []string
	rebuiltChunks := make(map[string]deletion.Chunk)
	for i := 10; i < 20; i++ {
		chunkID := schemaCfg.ExternalKey(logproto.ChunkRef{
			Fingerprint: uint64(i),
			UserID:      user1,
			From:        model.Time(i),
			Through:     model.Time(i + 1),
			Checksum:    uint32(i),
		})
		var newChunk deletion.Chunk
		if i >= 15 {
			newChunk = dummyChunk{
				from:        model.Time(i),
				through:     model.Time(i + 1),
				fingerprint: uint64(i),
				checksum:    uint32(i + 1),
				kb:          uint32(i),
				entries:     uint32(i),
			}
			newChunkID := schemaCfg.ExternalKey(logproto.ChunkRef{
				Fingerprint: newChunk.GetFingerprint(),
				UserID:      user1,
				From:        newChunk.GetFrom(),
				Through:     newChunk.GetThrough(),
				Checksum:    newChunk.GetChecksum(),
			})
			newChunkIDs = append(newChunkIDs, newChunkID)
		}
		rebuiltChunks[chunkID] = newChunk
		sourceChunkIDs = append(sourceChunkIDs, chunkID)
	}

	lblFoo := labels.FromStrings("foo", "bar")

	for _, tc := range []struct {
		name                            string
		indexUpdatesRecorder            *indexUpdatesRecorder
		expectedChunksToRemoveFromIndex func() map[string][]string
		expectedChunksToAddToIndex      func() map[string][]deletion.Chunk
		expectedChunksMarkedForDeletion func() []string
		noUserIndex                     bool
	}{
		{
			name:                 "no source chunks missing",
			indexUpdatesRecorder: newIndexUpdatesRecorder(schemaCfg, nil),
			expectedChunksToRemoveFromIndex: func() map[string][]string {
				entries := make([]string, 0, len(chunksToDeIndex)+len(rebuiltChunks))
				entries = append(entries, chunksToDeIndex...)
				for chunkID := range rebuiltChunks {
					entries = append(entries, chunkID)
				}

				sort.Strings(entries)

				return map[string][]string{lblFoo.String(): entries}
			},
			expectedChunksToAddToIndex: func() map[string][]deletion.Chunk {
				chunks := make([]deletion.Chunk, 0, len(rebuiltChunks))
				for _, newChunk := range rebuiltChunks {
					if newChunk != nil {
						chunks = append(chunks, newChunk)
					}
				}

				slices.SortFunc(chunks, func(a, b deletion.Chunk) int {
					if a.GetFrom() < b.GetFrom() {
						return -1
					} else if a.GetFrom() > b.GetFrom() {
						return 1
					}
					return 0
				})

				return map[string][]deletion.Chunk{lblFoo.String(): chunks}
			},
			expectedChunksMarkedForDeletion: func() []string {
				resp := make([]string, 0, len(rebuiltChunks))
				for chunkID := range rebuiltChunks {
					resp = append(resp, chunkID)
				}

				sort.Strings(resp)
				return resp
			},
		},
		{
			name:                 "some source chunks missing with all the new chunks already indexed",
			indexUpdatesRecorder: newIndexUpdatesRecorder(schemaCfg, sourceChunkIDs[3:7]),
			expectedChunksToRemoveFromIndex: func() map[string][]string {
				entries := append([]string{}, chunksToDeIndex...)
				entries = append(entries, sourceChunkIDs[:3]...)
				entries = append(entries, sourceChunkIDs[7:]...)

				sort.Strings(entries)

				return map[string][]string{lblFoo.String(): entries}
			},
			expectedChunksToAddToIndex: func() map[string][]deletion.Chunk {
				chunks := make([]deletion.Chunk, 0, len(rebuiltChunks))
				for _, sourceChunkID := range sourceChunkIDs[7:] {
					newChunk := rebuiltChunks[sourceChunkID]
					if newChunk == nil {
						continue
					}

					chunks = append(chunks, newChunk)
				}

				slices.SortFunc(chunks, func(a, b deletion.Chunk) int {
					if a.GetFrom() < b.GetFrom() {
						return -1
					} else if a.GetFrom() > b.GetFrom() {
						return 1
					}
					return 0
				})

				return map[string][]deletion.Chunk{lblFoo.String(): chunks}
			},
			expectedChunksMarkedForDeletion: func() []string {
				chunkIDs := make([]string, 0, len(rebuiltChunks))
				// add all the source chunkIDs for deletion
				for chunkID := range rebuiltChunks {
					chunkIDs = append(chunkIDs, chunkID)
				}

				sort.Strings(chunkIDs)
				return chunkIDs
			},
		},
		{
			name:                 "some source chunks missing with none of the new chunks indexed",
			indexUpdatesRecorder: newIndexUpdatesRecorder(schemaCfg, append(append([]string{}, sourceChunkIDs[3:7]...), newChunkIDs...)),
			expectedChunksToRemoveFromIndex: func() map[string][]string {
				entries := append([]string{}, chunksToDeIndex...)
				entries = append(entries, sourceChunkIDs[:3]...)
				entries = append(entries, sourceChunkIDs[7:]...)

				sort.Strings(entries)

				return map[string][]string{lblFoo.String(): entries}
			},
			expectedChunksToAddToIndex: func() map[string][]deletion.Chunk {
				chunks := make([]deletion.Chunk, 0, len(rebuiltChunks))
				for _, sourceChunkID := range sourceChunkIDs[7:] {
					newChunk := rebuiltChunks[sourceChunkID]
					if newChunk == nil {
						continue
					}

					chunks = append(chunks, newChunk)
				}

				slices.SortFunc(chunks, func(a, b deletion.Chunk) int {
					if a.GetFrom() < b.GetFrom() {
						return -1
					} else if a.GetFrom() > b.GetFrom() {
						return 1
					}
					return 0
				})

				return map[string][]deletion.Chunk{lblFoo.String(): chunks}
			},
			expectedChunksMarkedForDeletion: func() []string {
				chunkIDs := make([]string, 0, len(rebuiltChunks))
				// add all the source chunkIDs for deletion
				for chunkID := range rebuiltChunks {
					chunkIDs = append(chunkIDs, chunkID)
				}

				// also add the new chunks for deletion which have their source chunks missing
				for _, sourceChunkID := range sourceChunkIDs[5:7] {
					newChunk := rebuiltChunks[sourceChunkID]
					chunkID := schemaCfg.ExternalKey(logproto.ChunkRef{
						Fingerprint: newChunk.GetFingerprint(),
						UserID:      user1,
						From:        newChunk.GetFrom(),
						Through:     newChunk.GetThrough(),
						Checksum:    newChunk.GetChecksum(),
					})
					chunkIDs = append(chunkIDs, chunkID)
				}

				sort.Strings(chunkIDs)
				return chunkIDs
			},
		},
		{
			name:                 "all the source chunks missing with all the new chunks already indexed",
			indexUpdatesRecorder: newIndexUpdatesRecorder(schemaCfg, sourceChunkIDs),
			expectedChunksToRemoveFromIndex: func() map[string][]string {
				// only the chunksToDeIndex should be removed from index
				return map[string][]string{lblFoo.String(): chunksToDeIndex}
			},
			expectedChunksToAddToIndex: func() map[string][]deletion.Chunk {
				// no chunks to index since we have no source chunks in the index
				return map[string][]deletion.Chunk{}
			},
			expectedChunksMarkedForDeletion: func() []string {
				chunkIDs := make([]string, 0, len(rebuiltChunks))
				// add all the source chunkIDs for deletion
				for chunkID := range rebuiltChunks {
					chunkIDs = append(chunkIDs, chunkID)
				}

				sort.Strings(chunkIDs)
				return chunkIDs
			},
		},
		{
			name:                 "all the source chunks missing with none of the new chunks indexed",
			indexUpdatesRecorder: newIndexUpdatesRecorder(schemaCfg, append(append([]string{}, sourceChunkIDs...), newChunkIDs...)),
			expectedChunksToRemoveFromIndex: func() map[string][]string {
				// only the chunksToDeIndex should be removed from index
				return map[string][]string{lblFoo.String(): chunksToDeIndex}
			},
			expectedChunksToAddToIndex: func() map[string][]deletion.Chunk {
				// no chunks to index since we have no source chunks in the index
				return map[string][]deletion.Chunk{}
			},
			expectedChunksMarkedForDeletion: func() []string {
				chunkIDs := make([]string, 0, len(rebuiltChunks))
				// add all the source chunkIDs for deletion
				for chunkID := range rebuiltChunks {
					chunkIDs = append(chunkIDs, chunkID)
				}

				// all the newly built chunks should be marked for deletion since their source chunks are missing from index
				for _, newChunk := range rebuiltChunks {
					if newChunk == nil {
						continue
					}

					chunkID := schemaCfg.ExternalKey(logproto.ChunkRef{
						Fingerprint: newChunk.GetFingerprint(),
						UserID:      user1,
						From:        newChunk.GetFrom(),
						Through:     newChunk.GetThrough(),
						Checksum:    newChunk.GetChecksum(),
					})
					chunkIDs = append(chunkIDs, chunkID)
				}
				sort.Strings(chunkIDs)
				return chunkIDs
			},
		},
		{
			name:        "user without any index",
			noUserIndex: true,
			expectedChunksToRemoveFromIndex: func() map[string][]string {
				return map[string][]string{}
			},
			expectedChunksToAddToIndex: func() map[string][]deletion.Chunk {
				return map[string][]deletion.Chunk{}
			},
			expectedChunksMarkedForDeletion: func() []string {
				chunkIDs := make([]string, 0, len(rebuiltChunks))

				// all the newly built chunks should be marked for deletion since the whole user index is missing
				for _, newChunk := range rebuiltChunks {
					if newChunk == nil {
						continue
					}

					chunkID := schemaCfg.ExternalKey(logproto.ChunkRef{
						Fingerprint: newChunk.GetFingerprint(),
						UserID:      user1,
						From:        newChunk.GetFrom(),
						Through:     newChunk.GetThrough(),
						Checksum:    newChunk.GetChecksum(),
					})
					chunkIDs = append(chunkIDs, chunkID)
				}

				sort.Strings(chunkIDs)
				return chunkIDs
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chunkDeletionMarkerRecorder := &chunkDeletionMarkerRecorder{}

			table := &table{
				name:         "t1",
				periodConfig: schemaCfg.Configs[0],
				indexSets: map[string]*indexSet{user1: {
					userID:         user1,
					compactedIndex: tc.indexUpdatesRecorder,
				}},
				logger:      util_log.Logger,
				ctx:         context.Background(),
				tableMarker: chunkDeletionMarkerRecorder,
			}

			if tc.noUserIndex {
				table.indexSets = map[string]*indexSet{}
			}

			lblFoo := labels.FromStrings("foo", "bar")
			err := table.applyStorageUpdates(user1, lblFoo.String(), rebuiltChunks, chunksToDeIndex)
			require.NoError(t, err)

			expectedChunksMarkedForDeletion := tc.expectedChunksMarkedForDeletion()
			chunkDeletionMarkerRecorder.sortEntries()
			require.Equal(t, expectedChunksMarkedForDeletion, chunkDeletionMarkerRecorder.chunkIDs)

			if !tc.noUserIndex {
				tc.indexUpdatesRecorder.sortEntries()

				expectedChunksToRemoveFromIndex := tc.expectedChunksToRemoveFromIndex()
				require.Equal(t, expectedChunksToRemoveFromIndex, tc.indexUpdatesRecorder.removedChunks)

				expectedChunksToAddToIndex := tc.expectedChunksToAddToIndex()
				require.Equal(t, expectedChunksToAddToIndex, tc.indexUpdatesRecorder.indexedChunks)
			}
		})
	}

}
