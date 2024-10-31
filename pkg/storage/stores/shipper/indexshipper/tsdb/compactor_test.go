package tsdb

import (
	"context"
	"fmt"
	"io"
	"math"
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

	"github.com/grafana/loki/v3/pkg/compactor"
	"github.com/grafana/loki/v3/pkg/compactor/retention"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
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

func (m *mockIndexSet) SetCompactedIndex(compactedIndex compactor.CompactedIndex, removeSourceFiles bool) error {
	m.compactedIndex = compactedIndex
	m.removeSourceFiles = removeSourceFiles
	return nil
}

func setupMultiTenantIndex(t *testing.T, indexFormat int, userStreams map[string][]stream, destDir string, ts time.Time) string {
	require.NoError(t, util.EnsureDirectory(destDir))
	b := NewBuilder(indexFormat)
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

	dst := NewPrefixedIdentifier(
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
		func(_, _ model.Time, _ uint32) Identifier {
			return dst
		},
	)

	require.NoError(t, err)
	return dst.Path()
}

func setupPerTenantIndex(t *testing.T, indexFormat int, streams []stream, destDir string, ts time.Time) string {
	require.NoError(t, util.EnsureDirectory(destDir))
	b := NewBuilder(indexFormat)
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
			return NewPrefixedIdentifier(id, destDir, "")
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

// buildChunkMetas builds `span[0]` ms wide chunk metas from -> to.
func buildChunkMetas(from, to int64, span ...int64) index.ChunkMetas {
	var s int64 = 1
	if len(span) > 0 {
		s = span[0]
	}
	var chunkMetas index.ChunkMetas
	for i := from; i <= to; i += s {
		chunkMetas = append(chunkMetas, index.ChunkMeta{
			MinTime:  i,
			MaxTime:  i + s,
			Checksum: uint32(i),
			Entries:  uint32(s),
			KB:       uint32(s),
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
		IndexTables: config.IndexPeriodicTableConfig{
			PeriodicTableConfig: config.PeriodicTableConfig{Period: config.ObjectStorageIndexRequiredPeriod}},
		Schema: "v12",
	}
	indexBkts := indexBuckets(now, now, []config.TableRange{periodConfig.GetIndexTableNumberRange(config.DayTime{Time: now})})

	tableName := indexBkts[0]
	lbls1 := mustParseLabels(`{foo="bar", a="b"}`)
	lbls2 := mustParseLabels(`{fizz="buzz", a="b"}`)

	for _, numUsers := range []int{
		5,
		10,
	} {
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
					tablePathInStorage := filepath.Join(objectStoragePath, tableName.prefix)
					tableWorkingDirectory := filepath.Join(tempDir, workingDirName, tableName.prefix)

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
						indexFormat, err := periodConfig.TSDBFormat()
						require.NoError(t, err)
						setupMultiTenantIndex(t, indexFormat, userStreams, tablePathInStorage, multiTenantIndexConfig.createdAt)
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
							indexFormat, err := periodConfig.TSDBFormat()
							require.NoError(t, err)
							setupPerTenantIndex(t, indexFormat, streams, filepath.Join(tablePathInStorage, userID), perTenantIndexConfig.createdAt)
						}
					}

					// build the clients and index sets
					objectClient, err := local.NewFSObjectClient(local.FSConfig{Directory: objectStoragePath})
					require.NoError(t, err)

					_, commonPrefixes, err := objectClient.List(context.Background(), tableName.prefix, "/")
					require.NoError(t, err)

					initializedIndexSets := map[string]compactor.IndexSet{}
					initializedIndexSetsMtx := sync.Mutex{}
					existingUserIndexSets := make(map[string]compactor.IndexSet, len(commonPrefixes))
					for _, commonPrefix := range commonPrefixes {
						userID := path.Base(string(commonPrefix))
						idxSet, err := newMockIndexSet(userID, tableName.prefix, filepath.Join(tableWorkingDirectory, userID), objectClient)
						require.NoError(t, err)

						existingUserIndexSets[userID] = idxSet
						initializedIndexSets[userID] = idxSet
					}

					commonIndexSet, err := newMockIndexSet("", tableName.prefix, tableWorkingDirectory, objectClient)
					require.NoError(t, err)

					// build TableCompactor and compact the index
					tCompactor := newTableCompactor(context.Background(), commonIndexSet, existingUserIndexSets, func(userID string) (compactor.IndexSet, error) {
						idxSet, err := newMockIndexSet(userID, tableName.prefix, filepath.Join(tableWorkingDirectory, userID), objectClient)
						require.NoError(t, err)

						initializedIndexSetsMtx.Lock()
						defer initializedIndexSetsMtx.Unlock()
						initializedIndexSets[userID] = idxSet
						return idxSet, nil
					}, periodConfig)

					require.NoError(t, tCompactor.CompactTable())

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
						err = indexFile.(*TSDBFile).Index.(*TSDBIndex).ForSeries(context.Background(), "", nil, 0, math.MaxInt64, func(lbls labels.Labels, _ model.Fingerprint, chks []index.ChunkMeta) (stop bool) {
							actualChunks[lbls.String()] = chks
							return false
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
	testCtx := setupCompactedIndex(t)

	for name, tc := range map[string]struct {
		deleteChunks map[string]index.ChunkMetas
		addChunks    []chunk.Chunk
		deleteSeries []labels.Labels

		shouldErr           bool
		finalExpectedChunks map[string]index.ChunkMetas
	}{
		"no changes": {
			finalExpectedChunks: map[string]index.ChunkMetas{
				testCtx.lbls1.String(): buildChunkMetas(testCtx.shiftTableStart(0), testCtx.shiftTableStart(10)),
				testCtx.lbls2.String(): buildChunkMetas(testCtx.shiftTableStart(0), testCtx.shiftTableStart(20)),
			},
		},
		"delete some chunks from a stream": {
			deleteChunks: map[string]index.ChunkMetas{
				testCtx.lbls1.String(): append(buildChunkMetas(testCtx.shiftTableStart(3), testCtx.shiftTableStart(5)), buildChunkMetas(testCtx.shiftTableStart(7), testCtx.shiftTableStart(8))...),
			},
			finalExpectedChunks: map[string]index.ChunkMetas{
				testCtx.lbls1.String(): append(buildChunkMetas(testCtx.shiftTableStart(0), testCtx.shiftTableStart(2)), append(buildChunkMetas(testCtx.shiftTableStart(6), testCtx.shiftTableStart(6)), buildChunkMetas(testCtx.shiftTableStart(9), testCtx.shiftTableStart(10))...)...),
				testCtx.lbls2.String(): buildChunkMetas(testCtx.shiftTableStart(0), testCtx.shiftTableStart(20)),
			},
		},
		"delete all chunks from a stream": {
			deleteChunks: map[string]index.ChunkMetas{
				testCtx.lbls1.String(): buildChunkMetas(testCtx.shiftTableStart(0), testCtx.shiftTableStart(10)),
			},
			deleteSeries: []labels.Labels{testCtx.lbls1},
			finalExpectedChunks: map[string]index.ChunkMetas{
				testCtx.lbls2.String(): buildChunkMetas(testCtx.shiftTableStart(0), testCtx.shiftTableStart(20)),
			},
		},
		"add some chunks to a stream": {
			addChunks: []chunk.Chunk{
				{
					Metric:   testCtx.lbls1,
					ChunkRef: chunkMetaToChunkRef(testCtx.userID, buildChunkMetas(testCtx.shiftTableStart(11), testCtx.shiftTableStart(11))[0], testCtx.lbls1),
					Data:     dummyChunkData{},
				},
				{
					Metric:   testCtx.lbls1,
					ChunkRef: chunkMetaToChunkRef(testCtx.userID, buildChunkMetas(testCtx.shiftTableStart(12), testCtx.shiftTableStart(12))[0], testCtx.lbls1),
					Data:     dummyChunkData{},
				},
			},
			finalExpectedChunks: map[string]index.ChunkMetas{
				testCtx.lbls1.String(): buildChunkMetas(testCtx.shiftTableStart(0), testCtx.shiftTableStart(12)),
				testCtx.lbls2.String(): buildChunkMetas(testCtx.shiftTableStart(0), testCtx.shiftTableStart(20)),
			},
		},
		"__name__ label should get dropped while indexing chunks": {
			addChunks: []chunk.Chunk{
				{
					Metric:   labels.NewBuilder(testCtx.lbls1).Set(labels.MetricName, "log").Labels(),
					ChunkRef: chunkMetaToChunkRef(testCtx.userID, buildChunkMetas(testCtx.shiftTableStart(11), testCtx.shiftTableStart(11))[0], testCtx.lbls1),
					Data:     dummyChunkData{},
				},
				{
					Metric:   testCtx.lbls1,
					ChunkRef: chunkMetaToChunkRef(testCtx.userID, buildChunkMetas(testCtx.shiftTableStart(12), testCtx.shiftTableStart(12))[0], testCtx.lbls1),
					Data:     dummyChunkData{},
				},
			},
			finalExpectedChunks: map[string]index.ChunkMetas{
				testCtx.lbls1.String(): buildChunkMetas(testCtx.shiftTableStart(0), testCtx.shiftTableStart(12)),
				testCtx.lbls2.String(): buildChunkMetas(testCtx.shiftTableStart(0), testCtx.shiftTableStart(20)),
			},
		},
		"add some chunks out of table interval to a stream": {
			addChunks: []chunk.Chunk{
				{
					Metric:   testCtx.lbls1,
					ChunkRef: chunkMetaToChunkRef(testCtx.userID, buildChunkMetas(testCtx.shiftTableStart(11), testCtx.shiftTableStart(11))[0], testCtx.lbls1),
					Data:     dummyChunkData{},
				},
				{
					Metric:   testCtx.lbls1,
					ChunkRef: chunkMetaToChunkRef(testCtx.userID, buildChunkMetas(testCtx.shiftTableStart(12), testCtx.shiftTableStart(12))[0], testCtx.lbls1),
					Data:     dummyChunkData{},
				},
				// these chunks should not be added
				{
					Metric:   testCtx.lbls1,
					ChunkRef: chunkMetaToChunkRef(testCtx.userID, buildChunkMetas(int64(testCtx.tableInterval.End+100), int64(testCtx.tableInterval.End+100))[0], testCtx.lbls1),
					Data:     dummyChunkData{},
				},
				{
					Metric:   testCtx.lbls1,
					ChunkRef: chunkMetaToChunkRef(testCtx.userID, buildChunkMetas(int64(testCtx.tableInterval.End+200), int64(testCtx.tableInterval.End+200))[0], testCtx.lbls1),
					Data:     dummyChunkData{},
				},
			},
			finalExpectedChunks: map[string]index.ChunkMetas{
				testCtx.lbls1.String(): buildChunkMetas(testCtx.shiftTableStart(0), testCtx.shiftTableStart(12)),
				testCtx.lbls2.String(): buildChunkMetas(testCtx.shiftTableStart(0), testCtx.shiftTableStart(20)),
			},
		},
		"add and delete some chunks in a stream": {
			addChunks: []chunk.Chunk{
				{
					Metric:   testCtx.lbls1,
					ChunkRef: chunkMetaToChunkRef(testCtx.userID, buildChunkMetas(testCtx.shiftTableStart(11), testCtx.shiftTableStart(11))[0], testCtx.lbls1),
					Data:     dummyChunkData{},
				},
				{
					Metric:   testCtx.lbls1,
					ChunkRef: chunkMetaToChunkRef(testCtx.userID, buildChunkMetas(testCtx.shiftTableStart(12), testCtx.shiftTableStart(12))[0], testCtx.lbls1),
					Data:     dummyChunkData{},
				},
			},
			deleteChunks: map[string]index.ChunkMetas{
				testCtx.lbls1.String(): buildChunkMetas(testCtx.shiftTableStart(3), testCtx.shiftTableStart(5)),
			},
			finalExpectedChunks: map[string]index.ChunkMetas{
				testCtx.lbls1.String(): append(buildChunkMetas(testCtx.shiftTableStart(0), testCtx.shiftTableStart(2)), buildChunkMetas(testCtx.shiftTableStart(6), testCtx.shiftTableStart(12))...),
				testCtx.lbls2.String(): buildChunkMetas(testCtx.shiftTableStart(0), testCtx.shiftTableStart(20)),
			},
		},
		"adding chunk to non-existing stream should error": {
			addChunks: []chunk.Chunk{
				{
					Metric:   labels.NewBuilder(testCtx.lbls1).Set("new", "label").Labels(),
					ChunkRef: chunkMetaToChunkRef(testCtx.userID, buildChunkMetas(testCtx.shiftTableStart(11), testCtx.shiftTableStart(11))[0], testCtx.lbls1),
					Data:     dummyChunkData{},
				},
			},
			shouldErr: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			compactedIndex := testCtx.buildCompactedIndex()

			foundChunkEntries := map[string][]retention.ChunkEntry{}
			err := compactedIndex.ForEachChunk(context.Background(), func(chunkEntry retention.ChunkEntry) (deleteChunk bool, err error) {
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

			require.Equal(t, testCtx.expectedChunkEntries, foundChunkEntries)

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
			err = indexFile.(*TSDBFile).Index.(*TSDBIndex).ForSeries(context.Background(), "", nil, 0, math.MaxInt64, func(lbls labels.Labels, _ model.Fingerprint, chks []index.ChunkMeta) (stop bool) {
				foundChunks[lbls.String()] = append(index.ChunkMetas{}, chks...)
				return false
			}, labels.MustNewMatcher(labels.MatchEqual, "", ""))
			require.NoError(t, err)

			require.Equal(t, tc.finalExpectedChunks, foundChunks)
		})
	}

}

func TestIteratorContextCancelation(t *testing.T) {
	tc := setupCompactedIndex(t)
	compactedIndex := tc.buildCompactedIndex()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var foundChunkEntries []retention.ChunkEntry
	err := compactedIndex.ForEachChunk(ctx, func(chunkEntry retention.ChunkEntry) (deleteChunk bool, err error) {
		foundChunkEntries = append(foundChunkEntries, chunkEntry)

		return false, nil
	})

	require.ErrorIs(t, err, context.Canceled)
}

type testContext struct {
	lbls1                labels.Labels
	lbls2                labels.Labels
	userID               string
	tableInterval        model.Interval
	shiftTableStart      func(ms int64) int64
	buildCompactedIndex  func() *compactedIndex
	expectedChunkEntries map[string][]retention.ChunkEntry
}

func setupCompactedIndex(t *testing.T) *testContext {
	t.Helper()

	now := model.Now()
	periodConfig := config.PeriodConfig{
		IndexTables: config.IndexPeriodicTableConfig{
			PeriodicTableConfig: config.PeriodicTableConfig{Period: config.ObjectStorageIndexRequiredPeriod}},
		Schema: "v12",
	}
	schemaCfg := config.SchemaConfig{
		Configs: []config.PeriodConfig{periodConfig},
	}
	indexBuckets := indexBuckets(now, now, []config.TableRange{periodConfig.GetIndexTableNumberRange(config.DayTime{Time: now})})
	tableName := indexBuckets[0]
	tableInterval := retention.ExtractIntervalFromTableName(tableName.prefix)
	// shiftTableStart shift tableInterval.Start by the given amount of milliseconds.
	// It is used for building chunkmetas relative to start time of the table.
	shiftTableStart := func(ms int64) int64 {
		return int64(tableInterval.Start) + ms
	}

	lbls1 := mustParseLabels(`{foo="bar", a="b"}`)
	lbls2 := mustParseLabels(`{fizz="buzz", a="b"}`)
	userID := buildUserID(0)

	buildCompactedIndex := func() *compactedIndex {
		indexFormat, err := periodConfig.TSDBFormat()
		require.NoError(t, err)
		builder := NewBuilder(indexFormat)
		stream := buildStream(lbls1, buildChunkMetas(shiftTableStart(0), shiftTableStart(10)), "")
		builder.AddSeries(stream.labels, stream.fp, stream.chunks)

		stream = buildStream(lbls2, buildChunkMetas(shiftTableStart(0), shiftTableStart(20)), "")
		builder.AddSeries(stream.labels, stream.fp, stream.chunks)

		builder.FinalizeChunks()

		return newCompactedIndex(context.Background(), tableName.prefix, buildUserID(0), t.TempDir(), periodConfig, builder)
	}

	expectedChunkEntries := map[string][]retention.ChunkEntry{
		lbls1.String(): chunkMetasToChunkEntry(schemaCfg, userID, lbls1, buildChunkMetas(shiftTableStart(0), shiftTableStart(10))),
		lbls2.String(): chunkMetasToChunkEntry(schemaCfg, userID, lbls2, buildChunkMetas(shiftTableStart(0), shiftTableStart(20))),
	}

	return &testContext{lbls1, lbls2, userID, tableInterval, shiftTableStart, buildCompactedIndex, expectedChunkEntries}
}

type dummyChunkData struct {
	chunk.Data
}

func (d dummyChunkData) UncompressedSize() int {
	return 1 << 10 // 1KB
}

func (d dummyChunkData) Entries() int {
	return 1
}
