package tsdb

import (
	"context"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper/compactor"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper/compactor/retention"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper/storage"
	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
	util_log "github.com/grafana/loki/pkg/util/log"
)

const (
	objectsStorageDirName = "objects"
	workingDirName        = "working-dir"
)

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

func (m *mockIndexSet) SetCompactedIndex(compactedIndex compactor.CompactedIndex, consumedFiles []storage.IndexFile, removeSourceFiles bool) error {
	m.compactedIndex = compactedIndex
	m.removeSourceFiles = removeSourceFiles
	return nil
}

func setupMultiTenantIndex(t *testing.T, userStreams map[string][]stream, destDir string, ts time.Time) string {
	require.NoError(t, util.EnsureDirectory(destDir))
	b := NewBuilder()
	for userID, streams := range userStreams {
		for _, stream := range streams {
			lb := labels.NewBuilder(stream.labels)
			lb.Set(TenantLabel, userID)
			withTenant := lb.Labels()

			b.AddSeries(
				withTenant,
				stream.fp,
				stream.chunks,
			)
		}
	}

	dst := newPrefixedIdentifier(
		MultitenantTSDBIdentifier{
			nodeName: "test",
			ts:       ts,
		},
		destDir,
		"",
	)

	_, err := b.Build(
		context.Background(),
		t.TempDir(),
		func(from, through model.Time, checksum uint32) Identifier {
			return dst
		},
	)

	require.NoError(t, err)
	return dst.Path()
}

func setupPerTenantIndex(t *testing.T, streams []stream, destDir string, ts time.Time) string {
	require.NoError(t, util.EnsureDirectory(destDir))
	b := NewBuilder()
	for _, stream := range streams {
		b.AddSeries(
			stream.labels,
			stream.fp,
			stream.chunks,
		)
	}

	id, err := b.Build(
		context.Background(),
		t.TempDir(),
		func(from, through model.Time, checksum uint32) Identifier {
			id := SingleTenantTSDBIdentifier{
				TS:       ts,
				From:     from,
				Through:  through,
				Checksum: checksum,
			}
			return newPrefixedIdentifier(id, destDir, "")
		},
	)

	require.NoError(t, err)
	return id.Path()
}

func buildStream(lbls labels.Labels, chunks index.ChunkMetas, userLabel string) stream {
	if userLabel != "" {
		lbls = labels.NewBuilder(lbls.Copy()).Set("user_id", userLabel).Labels()
	}
	return stream{
		labels: lbls,
		fp:     model.Fingerprint(lbls.Hash()),
		chunks: chunks,
	}
}

func buildChunkMetas(from, to int64) index.ChunkMetas {
	var chunkMetas index.ChunkMetas
	for i := from; i <= to; i++ {
		chunkMetas = append(chunkMetas, index.ChunkMeta{
			MinTime:  i,
			MaxTime:  i,
			Checksum: uint32(i),
		})
	}

	return chunkMetas
}

func buildUserID(i int) string {
	return fmt.Sprintf("user_%d", i)
}

type streamConfig struct {
	labels     labels.Labels
	chunkMetas index.ChunkMetas
}

type multiTenantIndexConfig struct {
	createdAt     time.Time
	streamsConfig []streamConfig
}

type perTenantIndexConfig struct {
	createdAt     time.Time
	streamsConfig []streamConfig
}

func TestCompactor_Compact(t *testing.T) {
	now := model.Now()
	periodConfig := config.PeriodConfig{
		IndexTables: config.PeriodicTableConfig{Period: config.ObjectStorageIndexRequiredPeriod},
		Schema:      "v12",
	}
	indexBkts, err := indexBuckets(now, now, []config.TableRange{periodConfig.GetIndexTableNumberRange(config.DayTime{Time: now})})
	require.NoError(t, err)

	tableName := indexBkts[0]
	lbls1 := mustParseLabels(`{foo="bar", a="b"}`)
	lbls2 := mustParseLabels(`{fizz="buzz", a="b"}`)

	for _, numUsers := range []int{5, 10, 20} {
		t.Run(fmt.Sprintf("numUsers=%d", numUsers), func(t *testing.T) {
			for name, tc := range map[string]struct {
				multiTenantIndexConfigs []multiTenantIndexConfig
				perTenantIndexConfigs   []perTenantIndexConfig

				expectedNumCompactedIndexes     int
				shouldRemoveCommonSourceIndexes bool
				shouldRemoveUserSourceIndexes   bool
				expectedStreams                 []streamConfig
			}{
				"no data in storage": {},
				"only one multi-tenant index file": {
					expectedNumCompactedIndexes:     numUsers,
					shouldRemoveCommonSourceIndexes: true,
					shouldRemoveUserSourceIndexes:   true,
					multiTenantIndexConfigs: []multiTenantIndexConfig{
						{
							createdAt: time.Unix(0, 0),
							streamsConfig: []streamConfig{
								{
									labels:     lbls1,
									chunkMetas: buildChunkMetas(0, 5),
								},
							},
						},
					},
					expectedStreams: []streamConfig{
						{
							labels:     lbls1,
							chunkMetas: buildChunkMetas(0, 5),
						},
					},
				},
				"multiple multi-tenant index files": {
					expectedNumCompactedIndexes:     numUsers,
					shouldRemoveCommonSourceIndexes: true,
					shouldRemoveUserSourceIndexes:   true,
					multiTenantIndexConfigs: []multiTenantIndexConfig{
						{
							createdAt: time.Unix(0, 0),
							streamsConfig: []streamConfig{
								{
									labels:     lbls1,
									chunkMetas: buildChunkMetas(0, 5),
								},
								{
									labels:     lbls2,
									chunkMetas: buildChunkMetas(0, 5),
								},
							},
						},
						{
							createdAt: time.Unix(1, 0),
							streamsConfig: []streamConfig{
								{
									labels:     lbls1,
									chunkMetas: buildChunkMetas(0, 10),
								},
							},
						},
						{
							createdAt: time.Unix(2, 0),
							streamsConfig: []streamConfig{
								{
									labels:     lbls2,
									chunkMetas: buildChunkMetas(0, 10),
								},
							},
						},
					},
					expectedStreams: []streamConfig{
						{
							labels:     lbls1,
							chunkMetas: buildChunkMetas(0, 10),
						},
						{
							labels:     lbls2,
							chunkMetas: buildChunkMetas(0, 10),
						},
					},
				},
				"both multi-tenant and per-tenant index files with no duplicates": {
					expectedNumCompactedIndexes:     numUsers,
					shouldRemoveCommonSourceIndexes: true,
					shouldRemoveUserSourceIndexes:   true,
					multiTenantIndexConfigs: []multiTenantIndexConfig{
						{
							createdAt: time.Unix(0, 0),
							streamsConfig: []streamConfig{
								{
									labels:     lbls1,
									chunkMetas: buildChunkMetas(0, 5),
								},
								{
									labels:     lbls2,
									chunkMetas: buildChunkMetas(0, 5),
								},
							},
						},
					},
					perTenantIndexConfigs: []perTenantIndexConfig{
						{
							createdAt: time.Unix(0, 0),
							streamsConfig: []streamConfig{
								{
									labels:     lbls1,
									chunkMetas: buildChunkMetas(6, 10),
								},
								{
									labels:     lbls2,
									chunkMetas: buildChunkMetas(6, 10),
								},
							},
						},
					},
					expectedStreams: []streamConfig{
						{
							labels:     lbls1,
							chunkMetas: buildChunkMetas(0, 10),
						},
						{
							labels:     lbls2,
							chunkMetas: buildChunkMetas(0, 10),
						},
					},
				},
				"both multi-tenant and per-tenant index files with duplicates": {
					expectedNumCompactedIndexes:     numUsers,
					shouldRemoveCommonSourceIndexes: true,
					shouldRemoveUserSourceIndexes:   true,
					multiTenantIndexConfigs: []multiTenantIndexConfig{
						{
							createdAt: time.Unix(0, 0),
							streamsConfig: []streamConfig{
								{
									labels:     lbls1,
									chunkMetas: buildChunkMetas(0, 5),
								},
								{
									labels:     lbls2,
									chunkMetas: buildChunkMetas(0, 5),
								},
							},
						},
					},
					perTenantIndexConfigs: []perTenantIndexConfig{
						{
							createdAt: time.Unix(0, 0),
							streamsConfig: []streamConfig{
								{
									labels:     lbls1,
									chunkMetas: buildChunkMetas(0, 5),
								},
								{
									labels:     lbls2,
									chunkMetas: buildChunkMetas(0, 5),
								},
							},
						},
					},
					expectedStreams: []streamConfig{
						{
							labels:     lbls1,
							chunkMetas: buildChunkMetas(0, 5),
						},
						{
							labels:     lbls2,
							chunkMetas: buildChunkMetas(0, 5),
						},
					},
				},
				"multiple per-tenant index files with no duplicates": {
					expectedNumCompactedIndexes:   numUsers,
					shouldRemoveUserSourceIndexes: true,
					perTenantIndexConfigs: []perTenantIndexConfig{
						{
							createdAt: time.Unix(0, 0),
							streamsConfig: []streamConfig{
								{
									labels:     lbls1,
									chunkMetas: buildChunkMetas(0, 5),
								},
							},
						},
						{
							createdAt: time.Unix(1, 0),
							streamsConfig: []streamConfig{
								{
									labels:     lbls1,
									chunkMetas: buildChunkMetas(6, 10),
								},
							},
						},
					},
					expectedStreams: []streamConfig{
						{
							labels:     lbls1,
							chunkMetas: buildChunkMetas(0, 10),
						},
					},
				},
				"multiple per-tenant index files with duplicates": {
					expectedNumCompactedIndexes:   numUsers,
					shouldRemoveUserSourceIndexes: true,
					perTenantIndexConfigs: []perTenantIndexConfig{
						{
							createdAt: time.Unix(0, 0),
							streamsConfig: []streamConfig{
								{
									labels:     lbls1,
									chunkMetas: buildChunkMetas(0, 5),
								},
							},
						},
						{
							createdAt: time.Unix(1, 0),
							streamsConfig: []streamConfig{
								{
									labels:     lbls1,
									chunkMetas: buildChunkMetas(0, 5),
								},
							},
						},
					},
					expectedStreams: []streamConfig{
						{
							labels:     lbls1,
							chunkMetas: buildChunkMetas(0, 5),
						},
					},
				},
				"nothing to compact": {
					perTenantIndexConfigs: []perTenantIndexConfig{
						{
							createdAt: time.Unix(0, 0),
							streamsConfig: []streamConfig{
								{
									labels:     lbls1,
									chunkMetas: buildChunkMetas(0, 5),
								},
							},
						},
					},
				},
			} {
				t.Run(name, func(t *testing.T) {
					tempDir := t.TempDir()
					objectStoragePath := filepath.Join(tempDir, objectsStorageDirName)
					tablePathInStorage := filepath.Join(objectStoragePath, tableName)
					tableWorkingDirectory := filepath.Join(tempDir, workingDirName, tableName)

					require.NoError(t, util.EnsureDirectory(objectStoragePath))
					require.NoError(t, util.EnsureDirectory(tablePathInStorage))
					require.NoError(t, util.EnsureDirectory(tableWorkingDirectory))

					// setup multi-tenant indexes
					for _, multiTenantIndexConfig := range tc.multiTenantIndexConfigs {
						userStreams := map[string][]stream{}
						for i := 0; i < numUsers; i++ {
							userID := buildUserID(i)
							userStreams[userID] = []stream{}

							for _, streamConfig := range multiTenantIndexConfig.streamsConfig {
								// unique stream for user with user_id label
								stream := buildStream(streamConfig.labels, streamConfig.chunkMetas, userID)
								userStreams[userID] = append(userStreams[userID], stream)

								// without user_id label
								stream = buildStream(streamConfig.labels, streamConfig.chunkMetas, "")
								userStreams[userID] = append(userStreams[userID], stream)
							}
						}
						setupMultiTenantIndex(t, userStreams, tablePathInStorage, multiTenantIndexConfig.createdAt)
					}

					// setup per-tenant indexes i.e compacted ones
					for _, perTenantIndexConfig := range tc.perTenantIndexConfigs {
						for i := 0; i < numUsers; i++ {
							userID := buildUserID(i)

							var streams []stream
							for _, streamConfig := range perTenantIndexConfig.streamsConfig {
								// unique stream for user with user_id label
								stream := buildStream(streamConfig.labels, streamConfig.chunkMetas, userID)
								streams = append(streams, stream)

								// without user_id label
								stream = buildStream(streamConfig.labels, streamConfig.chunkMetas, "")
								streams = append(streams, stream)
							}
							setupPerTenantIndex(t, streams, filepath.Join(tablePathInStorage, userID), perTenantIndexConfig.createdAt)
						}
					}

					// build the clients and index sets
					objectClient, err := local.NewFSObjectClient(local.FSConfig{Directory: objectStoragePath})
					require.NoError(t, err)

					_, commonPrefixes, err := objectClient.List(context.Background(), tableName, "/")
					require.NoError(t, err)

					initializedIndexSets := map[string]compactor.IndexSet{}
					initializedIndexSetsMtx := sync.Mutex{}
					existingUserIndexSets := make(map[string]compactor.IndexSet, len(commonPrefixes))
					for _, commonPrefix := range commonPrefixes {
						userID := path.Base(string(commonPrefix))
						idxSet, err := newMockIndexSet(userID, tableName, filepath.Join(tableWorkingDirectory, userID), objectClient)
						require.NoError(t, err)

						existingUserIndexSets[userID] = idxSet
						initializedIndexSets[userID] = idxSet
					}

					commonIndexSet, err := newMockIndexSet("", tableName, tableWorkingDirectory, objectClient)
					require.NoError(t, err)

					// build TableCompactor and compact the index
					tCompactor := newTableCompactor(context.Background(), commonIndexSet, existingUserIndexSets, func(userID string) (compactor.IndexSet, error) {
						idxSet, err := newMockIndexSet(userID, tableName, filepath.Join(tableWorkingDirectory, userID), objectClient)
						require.NoError(t, err)

						initializedIndexSetsMtx.Lock()
						defer initializedIndexSetsMtx.Unlock()
						initializedIndexSets[userID] = idxSet
						return idxSet, nil
					}, config.PeriodConfig{})

					require.NoError(t, tCompactor.CompactTable(0*time.Second))

					// verify that we have CompactedIndex for numUsers
					require.Len(t, tCompactor.compactedIndexes, tc.expectedNumCompactedIndexes)
					for userID, compactedIdx := range tCompactor.compactedIndexes {
						require.Equal(t, tc.shouldRemoveUserSourceIndexes, initializedIndexSets[userID].(*mockIndexSet).removeSourceFiles)
						require.NotNil(t, initializedIndexSets[userID].(*mockIndexSet).compactedIndex)

						expectedChunks := map[string]index.ChunkMetas{}
						for _, streamsConfig := range tc.expectedStreams {
							// we should have both streams with user_id label and without user_id label
							seriesID := buildStream(streamsConfig.labels, index.ChunkMetas{}, userID).labels.String()
							expectedChunks[seriesID] = streamsConfig.chunkMetas

							seriesID = buildStream(streamsConfig.labels, index.ChunkMetas{}, "").labels.String()
							expectedChunks[seriesID] = streamsConfig.chunkMetas
						}

						// verify the chunkmetas in the builder
						actualChunks := map[string]index.ChunkMetas{}
						for seriesID, stream := range initializedIndexSets[userID].(*mockIndexSet).compactedIndex.(*compactedIndex).builder.streams {
							actualChunks[seriesID] = stream.chunks
						}

						// now convert the compactedIndex to index.Index and verify the chunkmetas again
						indexFile, err := compactedIdx.ToIndexFile()
						require.NoError(t, err)

						actualChunks = map[string]index.ChunkMetas{}
						err = indexFile.(*TSDBFile).Index.(*TSDBIndex).forSeries(context.Background(), nil, func(lbls labels.Labels, fp model.Fingerprint, chks []index.ChunkMeta) {
							actualChunks[lbls.String()] = chks
						}, labels.MustNewMatcher(labels.MatchEqual, "", ""))
						require.NoError(t, err)

						require.Equal(t, expectedChunks, actualChunks)
					}

					require.Nil(t, commonIndexSet.(*mockIndexSet).compactedIndex)
					require.Equal(t, tc.shouldRemoveCommonSourceIndexes, commonIndexSet.(*mockIndexSet).removeSourceFiles)
				})
			}
		})
	}
}

func chunkMetasToChunkEntry(schemaCfg config.SchemaConfig, userID string, lbls labels.Labels, chunkMetas index.ChunkMetas) []retention.ChunkEntry {
	chunkEntries := make([]retention.ChunkEntry, 0, len(chunkMetas))
	for _, chunkMeta := range chunkMetas {
		chunkEntries = append(chunkEntries, retention.ChunkEntry{
			ChunkRef: retention.ChunkRef{
				UserID:   []byte(userID),
				SeriesID: []byte(lbls.String()),
				ChunkID:  []byte(schemaCfg.ExternalKey(chunkMetaToChunkRef(userID, chunkMeta, lbls))),
				From:     chunkMeta.From(),
				Through:  chunkMeta.Through(),
			},
			Labels: lbls,
		})
	}

	return chunkEntries
}

func chunkMetaToChunkRef(userID string, chunkMeta index.ChunkMeta, lbls labels.Labels) logproto.ChunkRef {
	return logproto.ChunkRef{
		Fingerprint: lbls.Hash(),
		UserID:      userID,
		From:        chunkMeta.From(),
		Through:     chunkMeta.Through(),
		Checksum:    chunkMeta.Checksum,
	}
}

func TestCompactedIndex(t *testing.T) {
	now := model.Now()
	periodConfig := config.PeriodConfig{
		IndexTables: config.PeriodicTableConfig{Period: config.ObjectStorageIndexRequiredPeriod},
		Schema:      "v12",
	}
	schemaCfg := config.SchemaConfig{
		Configs: []config.PeriodConfig{periodConfig},
	}
	indexBuckets, err := indexBuckets(now, now, []config.TableRange{periodConfig.GetIndexTableNumberRange(config.DayTime{Time: now})})
	require.NoError(t, err)
	tableName := indexBuckets[0]
	tableInterval := retention.ExtractIntervalFromTableName(tableName)
	// shiftTableStart shift tableInterval.Start by the given amount of milliseconds.
	// It is used for building chunkmetas relative to start time of the table.
	shiftTableStart := func(ms int64) int64 {
		return int64(tableInterval.Start) + ms
	}

	lbls1 := mustParseLabels(`{foo="bar", a="b"}`)
	lbls2 := mustParseLabels(`{fizz="buzz", a="b"}`)
	userID := buildUserID(0)

	buildCompactedIndex := func() *compactedIndex {
		builder := NewBuilder()
		stream := buildStream(lbls1, buildChunkMetas(shiftTableStart(0), shiftTableStart(10)), "")
		builder.AddSeries(stream.labels, stream.fp, stream.chunks)

		stream = buildStream(lbls2, buildChunkMetas(shiftTableStart(0), shiftTableStart(20)), "")
		builder.AddSeries(stream.labels, stream.fp, stream.chunks)

		builder.FinalizeChunks()

		return newCompactedIndex(context.Background(), tableName, buildUserID(0), t.TempDir(), periodConfig, builder)
	}

	expectedChunkEntries := map[string][]retention.ChunkEntry{
		lbls1.String(): chunkMetasToChunkEntry(schemaCfg, userID, lbls1, buildChunkMetas(shiftTableStart(0), shiftTableStart(10))),
		lbls2.String(): chunkMetasToChunkEntry(schemaCfg, userID, lbls2, buildChunkMetas(shiftTableStart(0), shiftTableStart(20))),
	}

	for name, tc := range map[string]struct {
		deleteChunks map[string]index.ChunkMetas
		addChunks    []chunk.Chunk
		deleteSeries []labels.Labels

		shouldErr           bool
		finalExpectedChunks map[string]index.ChunkMetas
	}{
		"no changes": {
			finalExpectedChunks: map[string]index.ChunkMetas{
				lbls1.String(): buildChunkMetas(shiftTableStart(0), shiftTableStart(10)),
				lbls2.String(): buildChunkMetas(shiftTableStart(0), shiftTableStart(20)),
			},
		},
		"delete some chunks from a stream": {
			deleteChunks: map[string]index.ChunkMetas{
				lbls1.String(): append(buildChunkMetas(shiftTableStart(3), shiftTableStart(5)), buildChunkMetas(shiftTableStart(7), shiftTableStart(8))...),
			},
			finalExpectedChunks: map[string]index.ChunkMetas{
				lbls1.String(): append(buildChunkMetas(shiftTableStart(0), shiftTableStart(2)), append(buildChunkMetas(shiftTableStart(6), shiftTableStart(6)), buildChunkMetas(shiftTableStart(9), shiftTableStart(10))...)...),
				lbls2.String(): buildChunkMetas(shiftTableStart(0), shiftTableStart(20)),
			},
		},
		"delete all chunks from a stream": {
			deleteChunks: map[string]index.ChunkMetas{
				lbls1.String(): buildChunkMetas(shiftTableStart(0), shiftTableStart(10)),
			},
			deleteSeries: []labels.Labels{lbls1},
			finalExpectedChunks: map[string]index.ChunkMetas{
				lbls2.String(): buildChunkMetas(shiftTableStart(0), shiftTableStart(20)),
			},
		},
		"add some chunks to a stream": {
			addChunks: []chunk.Chunk{
				{
					Metric:   lbls1,
					ChunkRef: chunkMetaToChunkRef(userID, buildChunkMetas(shiftTableStart(11), shiftTableStart(11))[0], lbls1),
					Data:     dummyChunkData{},
				},
				{
					Metric:   lbls1,
					ChunkRef: chunkMetaToChunkRef(userID, buildChunkMetas(shiftTableStart(12), shiftTableStart(12))[0], lbls1),
					Data:     dummyChunkData{},
				},
			},
			finalExpectedChunks: map[string]index.ChunkMetas{
				lbls1.String(): buildChunkMetas(shiftTableStart(0), shiftTableStart(12)),
				lbls2.String(): buildChunkMetas(shiftTableStart(0), shiftTableStart(20)),
			},
		},
		"add some chunks out of table interval to a stream": {
			addChunks: []chunk.Chunk{
				{
					Metric:   lbls1,
					ChunkRef: chunkMetaToChunkRef(userID, buildChunkMetas(shiftTableStart(11), shiftTableStart(11))[0], lbls1),
					Data:     dummyChunkData{},
				},
				{
					Metric:   lbls1,
					ChunkRef: chunkMetaToChunkRef(userID, buildChunkMetas(shiftTableStart(12), shiftTableStart(12))[0], lbls1),
					Data:     dummyChunkData{},
				},
				// these chunks should not be added
				{
					Metric:   lbls1,
					ChunkRef: chunkMetaToChunkRef(userID, buildChunkMetas(int64(tableInterval.End+100), int64(tableInterval.End+100))[0], lbls1),
					Data:     dummyChunkData{},
				},
				{
					Metric:   lbls1,
					ChunkRef: chunkMetaToChunkRef(userID, buildChunkMetas(int64(tableInterval.End+200), int64(tableInterval.End+200))[0], lbls1),
					Data:     dummyChunkData{},
				},
			},
			finalExpectedChunks: map[string]index.ChunkMetas{
				lbls1.String(): buildChunkMetas(shiftTableStart(0), shiftTableStart(12)),
				lbls2.String(): buildChunkMetas(shiftTableStart(0), shiftTableStart(20)),
			},
		},
		"add and delete some chunks in a stream": {
			addChunks: []chunk.Chunk{
				{
					Metric:   lbls1,
					ChunkRef: chunkMetaToChunkRef(userID, buildChunkMetas(shiftTableStart(11), shiftTableStart(11))[0], lbls1),
					Data:     dummyChunkData{},
				},
				{
					Metric:   lbls1,
					ChunkRef: chunkMetaToChunkRef(userID, buildChunkMetas(shiftTableStart(12), shiftTableStart(12))[0], lbls1),
					Data:     dummyChunkData{},
				},
			},
			deleteChunks: map[string]index.ChunkMetas{
				lbls1.String(): buildChunkMetas(shiftTableStart(3), shiftTableStart(5)),
			},
			finalExpectedChunks: map[string]index.ChunkMetas{
				lbls1.String(): append(buildChunkMetas(shiftTableStart(0), shiftTableStart(2)), buildChunkMetas(shiftTableStart(6), shiftTableStart(12))...),
				lbls2.String(): buildChunkMetas(shiftTableStart(0), shiftTableStart(20)),
			},
		},
		"adding chunk to non-existing stream should error": {
			addChunks: []chunk.Chunk{
				{
					Metric:   labels.NewBuilder(lbls1).Set("new", "label").Labels(),
					ChunkRef: chunkMetaToChunkRef(userID, buildChunkMetas(shiftTableStart(11), shiftTableStart(11))[0], lbls1),
					Data:     dummyChunkData{},
				},
			},
			shouldErr: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			compactedIndex := buildCompactedIndex()

			foundChunkEntries := map[string][]retention.ChunkEntry{}
			err := compactedIndex.ForEachChunk(func(chunkEntry retention.ChunkEntry) (deleteChunk bool, err error) {
				seriesIDStr := string(chunkEntry.SeriesID)
				foundChunkEntries[seriesIDStr] = append(foundChunkEntries[seriesIDStr], chunkEntry)
				if chks, ok := tc.deleteChunks[string(chunkEntry.SeriesID)]; ok {
					for _, chk := range chks {
						if chk.MinTime == int64(chunkEntry.From) && chk.MaxTime == int64(chunkEntry.Through) {
							return true, nil
						}
					}
				}

				return false, nil
			})
			require.NoError(t, err)

			require.Equal(t, expectedChunkEntries, foundChunkEntries)

			for _, lbls := range tc.deleteSeries {
				require.NoError(t, compactedIndex.CleanupSeries(nil, lbls))
			}

			for _, chk := range tc.addChunks {
				_, err := compactedIndex.IndexChunk(chk)
				require.NoError(t, err)
			}

			indexFile, err := compactedIndex.ToIndexFile()
			if tc.shouldErr {
				require.NotNil(t, err)
				return
			}
			require.NoError(t, err)

			foundChunks := map[string]index.ChunkMetas{}
			err = indexFile.(*TSDBFile).Index.(*TSDBIndex).forSeries(context.Background(), nil, func(lbls labels.Labels, fp model.Fingerprint, chks []index.ChunkMeta) {
				foundChunks[lbls.String()] = append(index.ChunkMetas{}, chks...)
			}, labels.MustNewMatcher(labels.MatchEqual, "", ""))
			require.NoError(t, err)

			require.Equal(t, tc.finalExpectedChunks, foundChunks)
		})
	}

}

type dummyChunkData struct {
	chunk.Data
}

func (d dummyChunkData) Entries() int {
	return 0
}
