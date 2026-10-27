package tsdb

import (
	"context"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/compactor"
	"github.com/grafana/loki/v3/pkg/compactor/retention"
	"github.com/grafana/loki/v3/pkg/compression"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	lokiutil "github.com/grafana/loki/v3/pkg/util"
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
		dst = strings.TrimSuffix(dst, ".gz")
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

func setupMultiTenantIndex(t testing.TB, indexFormat int, userStreams map[string][]stream, destDir string, ts time.Time) string {
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
			NodeName: "test",
			Ts:       ts,
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

func setupPerTenantIndex(t testing.TB, indexFormat int, streams []stream, destDir string, ts time.Time) string {
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
		fp:     model.Fingerprint(labels.StableHash(lbls)),
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

func withIngestedAt(chunkMetas index.ChunkMetas, ingestedAt int64) index.ChunkMetas {
	out := make(index.ChunkMetas, len(chunkMetas))
	copy(out, chunkMetas)

	for i := range out {
		out[i].IngestedAt = ingestedAt
	}

	return out
}

type recordingIndexWriter struct {
	metas index.ChunkMetas
}

func (w *recordingIndexWriter) Append(_ string, _ labels.Labels, _ uint64, chks index.ChunkMetas) error {
	w.metas = append(w.metas, chks...)
	return nil
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
	indexBkts := IndexBuckets(now, now, []config.TableRange{periodConfig.GetIndexTableNumberRange(config.DayTime{Time: now})})

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
					tablePathInStorage := filepath.Join(objectStoragePath, tableName.Prefix)
					tableWorkingDirectory := filepath.Join(tempDir, workingDirName, tableName.Prefix)

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

					_, commonPrefixes, err := objectClient.List(context.Background(), tableName.Prefix, "/")
					require.NoError(t, err)

					initializedIndexSets := map[string]compactor.IndexSet{}
					initializedIndexSetsMtx := sync.Mutex{}
					existingUserIndexSets := make(map[string]compactor.IndexSet, len(commonPrefixes))
					for _, commonPrefix := range commonPrefixes {
						userID := path.Base(string(commonPrefix))
						idxSet, err := newMockIndexSet(userID, tableName.Prefix, filepath.Join(tableWorkingDirectory, userID), objectClient)
						require.NoError(t, err)

						existingUserIndexSets[userID] = idxSet
						initializedIndexSets[userID] = idxSet
					}

					commonIndexSet, err := newMockIndexSet("", tableName.Prefix, tableWorkingDirectory, objectClient)
					require.NoError(t, err)

					// build TableCompactor and compact the index
					tCompactor := newTableCompactor(context.Background(), commonIndexSet, existingUserIndexSets, func(userID string) (compactor.IndexSet, error) {
						idxSet, err := newMockIndexSet(userID, tableName.Prefix, filepath.Join(tableWorkingDirectory, userID), objectClient)
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
						for stream := range initializedIndexSets[userID].(*mockIndexSet).compactedIndex.(*compactedIndex).builder.allStreams() {
							actualChunks[stream.labels.String()] = stream.chunks
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

func chunkMetasToRetentionChunk(schemaCfg config.SchemaConfig, userID string, lbls labels.Labels, chunkMetas index.ChunkMetas) []retention.Chunk {
	chunkEntries := make([]retention.Chunk, 0, len(chunkMetas))
	for _, chunkMeta := range chunkMetas {
		chunkEntries = append(chunkEntries, retention.Chunk{
			ChunkID:    schemaCfg.ExternalKey(chunkMetaToChunkRef(userID, chunkMeta, lbls)),
			From:       chunkMeta.From(),
			Through:    chunkMeta.Through(),
			IngestedAt: model.Time(chunkMeta.IngestedAt),
		})
	}

	return chunkEntries
}

func chunkMetaToChunkRef(userID string, chunkMeta index.ChunkMeta, lbls labels.Labels) logproto.ChunkRef {
	return logproto.ChunkRef{
		Fingerprint: labels.StableHash(lbls),
		UserID:      userID,
		From:        chunkMeta.From(),
		Through:     chunkMeta.Through(),
		Checksum:    chunkMeta.Checksum,
	}
}

func TestCompactedIndex(t *testing.T) {
	testCtx := setupCompactedIndex(t)

	for name, tc := range map[string]struct {
		deleteChunks map[string][]retention.Chunk
		addChunks    []chunk.Chunk
		deleteSeries []labels.Labels

		shouldErr           bool
		finalExpectedChunks map[string][]retention.Chunk
	}{
		"no changes": {
			finalExpectedChunks: map[string][]retention.Chunk{
				testCtx.lbls1.String(): chunkMetasToRetentionChunk(testCtx.schemaCfg, testCtx.userID, testCtx.lbls1, buildChunkMetas(testCtx.shiftTableStart(0), testCtx.shiftTableStart(10))),
				testCtx.lbls2.String(): chunkMetasToRetentionChunk(testCtx.schemaCfg, testCtx.userID, testCtx.lbls2, buildChunkMetas(testCtx.shiftTableStart(0), testCtx.shiftTableStart(20))),
			},
		},
		"delete some chunks from a stream": {
			deleteChunks: map[string][]retention.Chunk{
				testCtx.lbls1.String(): chunkMetasToRetentionChunk(
					testCtx.schemaCfg, testCtx.userID, testCtx.lbls1,
					append(
						buildChunkMetas(testCtx.shiftTableStart(3), testCtx.shiftTableStart(5)),
						buildChunkMetas(testCtx.shiftTableStart(7), testCtx.shiftTableStart(8))...,
					),
				),
			},
			finalExpectedChunks: map[string][]retention.Chunk{
				testCtx.lbls1.String(): chunkMetasToRetentionChunk(
					testCtx.schemaCfg, testCtx.userID, testCtx.lbls1,
					append(
						buildChunkMetas(testCtx.shiftTableStart(0), testCtx.shiftTableStart(2)),
						append(buildChunkMetas(testCtx.shiftTableStart(6), testCtx.shiftTableStart(6)),
							buildChunkMetas(testCtx.shiftTableStart(9), testCtx.shiftTableStart(10))...,
						)...,
					),
				),
				testCtx.lbls2.String(): chunkMetasToRetentionChunk(
					testCtx.schemaCfg, testCtx.userID, testCtx.lbls2,
					buildChunkMetas(testCtx.shiftTableStart(0), testCtx.shiftTableStart(20)),
				),
			},
		},
		"delete all chunks from a stream": {
			deleteChunks: map[string][]retention.Chunk{
				testCtx.lbls1.String(): chunkMetasToRetentionChunk(
					testCtx.schemaCfg, testCtx.userID, testCtx.lbls1,
					buildChunkMetas(testCtx.shiftTableStart(0), testCtx.shiftTableStart(10)),
				),
			},
			deleteSeries: []labels.Labels{testCtx.lbls1},
			finalExpectedChunks: map[string][]retention.Chunk{
				testCtx.lbls2.String(): chunkMetasToRetentionChunk(
					testCtx.schemaCfg, testCtx.userID, testCtx.lbls2,
					buildChunkMetas(testCtx.shiftTableStart(0), testCtx.shiftTableStart(20)),
				),
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
			finalExpectedChunks: map[string][]retention.Chunk{
				testCtx.lbls1.String(): chunkMetasToRetentionChunk(
					testCtx.schemaCfg, testCtx.userID, testCtx.lbls1,
					buildChunkMetas(testCtx.shiftTableStart(0), testCtx.shiftTableStart(12)),
				),
				testCtx.lbls2.String(): chunkMetasToRetentionChunk(
					testCtx.schemaCfg, testCtx.userID, testCtx.lbls2,
					buildChunkMetas(testCtx.shiftTableStart(0), testCtx.shiftTableStart(20)),
				),
			},
		},
		"__name__ label should get dropped while indexing chunks": {
			addChunks: []chunk.Chunk{
				{
					Metric:   labels.NewBuilder(testCtx.lbls1).Set(model.MetricNameLabel, "log").Labels(),
					ChunkRef: chunkMetaToChunkRef(testCtx.userID, buildChunkMetas(testCtx.shiftTableStart(11), testCtx.shiftTableStart(11))[0], testCtx.lbls1),
					Data:     dummyChunkData{},
				},
				{
					Metric:   testCtx.lbls1,
					ChunkRef: chunkMetaToChunkRef(testCtx.userID, buildChunkMetas(testCtx.shiftTableStart(12), testCtx.shiftTableStart(12))[0], testCtx.lbls1),
					Data:     dummyChunkData{},
				},
			},
			finalExpectedChunks: map[string][]retention.Chunk{
				testCtx.lbls1.String(): chunkMetasToRetentionChunk(
					testCtx.schemaCfg, testCtx.userID, testCtx.lbls1,
					buildChunkMetas(testCtx.shiftTableStart(0), testCtx.shiftTableStart(12)),
				),
				testCtx.lbls2.String(): chunkMetasToRetentionChunk(
					testCtx.schemaCfg, testCtx.userID, testCtx.lbls2,
					buildChunkMetas(testCtx.shiftTableStart(0), testCtx.shiftTableStart(20)),
				),
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
			finalExpectedChunks: map[string][]retention.Chunk{
				testCtx.lbls1.String(): chunkMetasToRetentionChunk(
					testCtx.schemaCfg, testCtx.userID, testCtx.lbls1,
					buildChunkMetas(testCtx.shiftTableStart(0), testCtx.shiftTableStart(12)),
				),
				testCtx.lbls2.String(): chunkMetasToRetentionChunk(
					testCtx.schemaCfg, testCtx.userID, testCtx.lbls2,
					buildChunkMetas(testCtx.shiftTableStart(0), testCtx.shiftTableStart(20)),
				),
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
			deleteChunks: map[string][]retention.Chunk{
				testCtx.lbls1.String(): chunkMetasToRetentionChunk(
					testCtx.schemaCfg, testCtx.userID, testCtx.lbls1,
					buildChunkMetas(testCtx.shiftTableStart(3), testCtx.shiftTableStart(5)),
				),
			},
			finalExpectedChunks: map[string][]retention.Chunk{
				testCtx.lbls1.String(): chunkMetasToRetentionChunk(
					testCtx.schemaCfg, testCtx.userID, testCtx.lbls1,
					append(
						buildChunkMetas(testCtx.shiftTableStart(0), testCtx.shiftTableStart(2)),
						buildChunkMetas(testCtx.shiftTableStart(6), testCtx.shiftTableStart(12))...,
					),
				),
				testCtx.lbls2.String(): chunkMetasToRetentionChunk(
					testCtx.schemaCfg, testCtx.userID, testCtx.lbls2,
					buildChunkMetas(testCtx.shiftTableStart(0), testCtx.shiftTableStart(20)),
				),
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

			existingChunk := compactedIndex.builder.getStream(testCtx.lbls1).chunks[0]

			// trying to remove a chunk for inexistent stream should return false for chunk existence
			inexistentStream := testCtx.lbls1.MatchLabels(false, "a")
			require.NotEqual(t, testCtx.lbls1.String(), inexistentStream.String())

			chunkExisted, err := compactedIndex.RemoveChunk(
				existingChunk.From(),
				existingChunk.Through(),
				[]byte(testCtx.userID),
				inexistentStream,
				testCtx.schemaCfg.ExternalKey(chunkMetaToChunkRef(testCtx.userID, existingChunk, testCtx.lbls1)),
			)
			require.NoError(t, err)
			require.False(t, chunkExisted)

			// trying to remove an inexistent chunk from an existing stream should return false for chunk existence
			inexistentChunk := existingChunk
			inexistentChunk.Checksum++
			chunkExisted, err = compactedIndex.RemoveChunk(
				inexistentChunk.From(),
				inexistentChunk.Through(),
				[]byte(testCtx.userID),
				testCtx.lbls1,
				testCtx.schemaCfg.ExternalKey(chunkMetaToChunkRef(testCtx.userID, inexistentChunk, testCtx.lbls1)),
			)
			require.NoError(t, err)
			require.False(t, chunkExisted)

			foundChunkEntries := map[string][]retention.Chunk{}
			err = compactedIndex.ForEachSeries(context.Background(), func(series retention.Series) error {
				seriesIDStr := string(series.SeriesID())
				foundChunkEntries[seriesIDStr] = append(foundChunkEntries[seriesIDStr], series.Chunks()...)
				if chks, ok := tc.deleteChunks[string(series.SeriesID())]; ok {
					for _, chk := range chks {
						chunkExisted, err := compactedIndex.RemoveChunk(chk.From, chk.Through, series.UserID(), series.Labels(), chk.ChunkID)
						require.NoError(t, err)
						require.True(t, chunkExisted)
					}
				}

				return nil
			})
			require.NoError(t, err)
			require.Equal(t, testCtx.expectedChunkEntries, foundChunkEntries)

			for _, lbls := range tc.deleteSeries {
				require.NoError(t, compactedIndex.CleanupSeries(nil, lbls))
			}

			for _, chk := range tc.addChunks {
				approxKB := math.Round(float64(chk.Data.UncompressedSize()) / float64(1<<10))
				_, err := compactedIndex.IndexChunk(chk.ChunkRef, chk.Metric, chk.IngestedAt, uint32(approxKB), uint32(chk.Data.Entries()))
				require.NoError(t, err)
			}

			indexFile, err := compactedIndex.ToIndexFile()
			if tc.shouldErr {
				require.NotNil(t, err)
				return
			}
			require.NoError(t, err)

			foundChunks := map[string][]retention.Chunk{}
			err = indexFile.(*TSDBFile).Index.(*TSDBIndex).ForSeries(context.Background(), "", nil, 0, math.MaxInt64, func(lbls labels.Labels, _ model.Fingerprint, chks []index.ChunkMeta) (stop bool) {
				foundChunks[lbls.String()] = chunkMetasToRetentionChunk(testCtx.schemaCfg, testCtx.userID, lbls, chks)
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

	err := compactedIndex.ForEachSeries(ctx, func(_ retention.Series) error {
		return nil
	})

	require.ErrorIs(t, err, context.Canceled)
}

func TestIteratorStopsEarly(t *testing.T) {
	errStop := fmt.Errorf("stop")
	for name, tc := range map[string]struct {
		callback    func(cancel context.CancelFunc) error
		expectedErr error
	}{
		"callback error": {
			callback:    func(context.CancelFunc) error { return errStop },
			expectedErr: errStop,
		},
		"context canceled during iteration": {
			callback:    func(cancel context.CancelFunc) error { cancel(); return nil },
			expectedErr: context.Canceled,
		},
	} {
		t.Run(name, func(t *testing.T) {
			compactedIndex := setupCompactedIndex(t).buildCompactedIndex()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			calls := 0
			err := compactedIndex.ForEachSeries(ctx, func(_ retention.Series) error {
				calls++
				return tc.callback(cancel)
			})
			require.ErrorIs(t, err, tc.expectedErr)
			require.Equal(t, 1, calls)
		})
	}
}

// TestCompactedIndexForEachSeriesPropagatesIngestedAt proves the passive
// IngestedAt metadata read from a FormatV4 builder survives the in-memory
// compaction read path. FormatV3 builders carry zero IngestedAt.
func TestCompactedIndexForEachSeriesPropagatesIngestedAt(t *testing.T) {
	for _, tc := range []struct {
		name        string
		indexFormat int
		chunkMetas  index.ChunkMetas
	}{
		{
			name:        "v3 leaves ingested at zero",
			indexFormat: index.FormatV3,
			chunkMetas:  buildChunkMetas(0, 0),
		},
		{
			name:        "v4 propagates ingested at",
			indexFormat: index.FormatV4,
			chunkMetas:  withIngestedAt(buildChunkMetas(0, 0), 1234),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Schema stays v13 (FormatV3) in Phase 1; the FormatV4 builder is
			// driven directly via the index constant, not schema mapping.
			periodConfig := config.PeriodConfig{
				IndexTables: config.IndexPeriodicTableConfig{
					PeriodicTableConfig: config.PeriodicTableConfig{Period: config.ObjectStorageIndexRequiredPeriod},
				},
				Schema: "v13",
			}

			builder := NewBuilder(tc.indexFormat)
			lbls := mustParseLabels(`{foo="bar"}`)
			stream := buildStream(lbls, tc.chunkMetas, "")
			builder.AddSeries(stream.labels, stream.fp, stream.chunks)
			builder.FinalizeChunks()

			indexBuckets := IndexBuckets(model.Now(), model.Now(), []config.TableRange{periodConfig.GetIndexTableNumberRange(config.DayTime{Time: model.Now()})})
			compactedIndex := newCompactedIndex(context.Background(), indexBuckets[0].Prefix, buildUserID(0), t.TempDir(), periodConfig, builder)

			var got []retention.Chunk
			err := compactedIndex.ForEachSeries(context.Background(), func(series retention.Series) error {
				got = append(got, series.Chunks()...)
				return nil
			})
			require.NoError(t, err)
			require.Equal(t, chunkMetasToRetentionChunk(config.SchemaConfig{Configs: []config.PeriodConfig{periodConfig}}, buildUserID(0), lbls, tc.chunkMetas), got)
		})
	}
}

// TestFormatV4IndexReadThroughStoreAndCompactor proves a chunk's IngestedAt
// survives the store writer, a FormatV4 index build, and the compacted-index
// read path. The FormatV4 encoder is exercised directly (no schema v14 mapping
// exists in Phase 1).
func TestFormatV4IndexReadThroughStoreAndCompactor(t *testing.T) {
	lbls := mustParseLabels(`{foo="bar"}`)
	metric := labels.NewBuilder(lbls).Set(model.MetricNameLabel, "logs").Labels()
	now := time.Now()
	memChunk := chunkenc.NewMemChunk(chunkenc.ChunkFormatV4, compression.GZIP, chunkenc.UnorderedWithStructuredMetadataHeadBlockFmt, 256*1024, 1500*1024)
	dup, err := memChunk.Append(&logproto.Entry{
		Timestamp: now,
		Line:      "a line",
	})
	require.False(t, dup)
	require.NoError(t, err)
	require.NoError(t, memChunk.Close())

	firstTime, lastTime := lokiutil.RoundToMilliseconds(memChunk.Bounds())
	flushedChunk := chunk.NewChunk("user-0", model.Fingerprint(labels.StableHash(metric)), metric, chunkenc.NewFacade(memChunk, 256*1024, 1500*1024), firstTime, lastTime)
	flushedChunk.IngestedAt = model.TimeFromUnix(1234)

	writer := &recordingIndexWriter{}
	store := &store{indexWriter: writer}
	require.NoError(t, store.IndexChunk(context.Background(), 0, 0, flushedChunk))
	require.Len(t, writer.metas, 1)
	require.Equal(t, int64(flushedChunk.IngestedAt), writer.metas[0].IngestedAt)

	periodConfig := config.PeriodConfig{
		IndexTables: config.IndexPeriodicTableConfig{
			PeriodicTableConfig: config.PeriodicTableConfig{Period: config.ObjectStorageIndexRequiredPeriod},
		},
		Schema: "v13",
	}

	// Build a FormatV4 index directly so the encoder is exercised without a
	// schema v14 mapping (which does not exist until Phase 2).
	builder := NewBuilder(index.FormatV4)
	builder.AddSeries(lbls, model.Fingerprint(labels.StableHash(lbls)), index.ChunkMetas{})
	builder.FinalizeChunks()

	indexBuckets := IndexBuckets(model.Now(), model.Now(), []config.TableRange{periodConfig.GetIndexTableNumberRange(config.DayTime{Time: model.Now()})})
	compactedIndex := newCompactedIndex(context.Background(), indexBuckets[0].Prefix, flushedChunk.UserID, t.TempDir(), periodConfig, builder)

	_, err = compactedIndex.IndexChunk(flushedChunk.ChunkRef, metric, flushedChunk.IngestedAt, writer.metas[0].KB, writer.metas[0].Entries)
	require.NoError(t, err)

	_, err = compactedIndex.ToIndexFile()
	require.NoError(t, err)

	var got []retention.Chunk
	err = compactedIndex.ForEachSeries(context.Background(), func(series retention.Series) error {
		got = append(got, series.Chunks()...)
		return nil
	})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, flushedChunk.IngestedAt, got[0].IngestedAt)
}

type testContext struct {
	lbls1                labels.Labels
	lbls2                labels.Labels
	userID               string
	tableInterval        model.Interval
	shiftTableStart      func(ms int64) int64
	buildCompactedIndex  func() *compactedIndex
	expectedChunkEntries map[string][]retention.Chunk
	schemaCfg            config.SchemaConfig
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
	indexBuckets := IndexBuckets(now, now, []config.TableRange{periodConfig.GetIndexTableNumberRange(config.DayTime{Time: now})})
	tableName := indexBuckets[0]
	tableInterval := retention.ExtractIntervalFromTableName(tableName.Prefix)
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

		return newCompactedIndex(context.Background(), tableName.Prefix, buildUserID(0), t.TempDir(), periodConfig, builder)
	}

	expectedChunkEntries := map[string][]retention.Chunk{
		lbls1.String(): chunkMetasToRetentionChunk(schemaCfg, userID, lbls1, buildChunkMetas(shiftTableStart(0), shiftTableStart(10))),
		lbls2.String(): chunkMetasToRetentionChunk(schemaCfg, userID, lbls2, buildChunkMetas(shiftTableStart(0), shiftTableStart(20))),
	}

	return &testContext{lbls1, lbls2, userID, tableInterval, shiftTableStart, buildCompactedIndex, expectedChunkEntries, schemaCfg}
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

// TestSetupBuilder_ManyFiles verifies that setupBuilder can handle processing
// many files without running into resource exhaustion issues.
func TestSetupBuilder_ManyFiles(t *testing.T) {
	now := model.Now()
	periodConfig := config.PeriodConfig{
		IndexTables: config.IndexPeriodicTableConfig{
			PeriodicTableConfig: config.PeriodicTableConfig{Period: config.ObjectStorageIndexRequiredPeriod}},
		Schema: "v12",
	}
	indexBkts := IndexBuckets(now, now, []config.TableRange{periodConfig.GetIndexTableNumberRange(config.DayTime{Time: now})})
	tableName := indexBkts[0]

	tempDir := t.TempDir()
	objectStoragePath := filepath.Join(tempDir, "objects")
	tablePathInStorage := filepath.Join(objectStoragePath, tableName.Prefix)
	tableWorkingDirectory := filepath.Join(tempDir, "working-dir", tableName.Prefix)

	require.NoError(t, util.EnsureDirectory(objectStoragePath))
	require.NoError(t, util.EnsureDirectory(tablePathInStorage))
	require.NoError(t, util.EnsureDirectory(tableWorkingDirectory))

	// Create a large number of files
	numFiles := 100
	indexFormat, err := periodConfig.TSDBFormat()
	require.NoError(t, err)

	// Create per-tenant index files in the user's directory
	userTablePath := filepath.Join(tablePathInStorage, "user1")
	require.NoError(t, util.EnsureDirectory(userTablePath))

	lbls := mustParseLabels(`{foo="bar"}`)
	for i := 0; i < numFiles; i++ {
		streams := []stream{
			buildStream(lbls, buildChunkMetas(int64(i*1000), int64(i*1000+100)), ""),
		}
		setupPerTenantIndex(t, indexFormat, streams, userTablePath, time.Unix(int64(i), 0))
	}

	objectClient, err := local.NewFSObjectClient(local.FSConfig{Directory: objectStoragePath})
	require.NoError(t, err)

	idxSet, err := newMockIndexSet("user1", tableName.Prefix, filepath.Join(tableWorkingDirectory, "user1"), objectClient)
	require.NoError(t, err)

	// This should complete without errors even with many files
	// because files are closed immediately after processing
	ctx := context.Background()
	builder, err := setupBuilder(ctx, indexFormat, "user1", idxSet, []Index{})
	require.NoError(t, err)
	require.NotNil(t, builder)

	// Verify builder has the expected data
	builder.FinalizeChunks()
	require.Greater(t, builder.numStreams(), 0)
}

func TestCompactTable_ClosesFilesOnError(t *testing.T) {
	now := model.Now()
	periodConfig := config.PeriodConfig{
		IndexTables: config.IndexPeriodicTableConfig{
			PeriodicTableConfig: config.PeriodicTableConfig{Period: config.ObjectStorageIndexRequiredPeriod}},
		Schema: "v12",
	}
	indexBkts := IndexBuckets(now, now, []config.TableRange{periodConfig.GetIndexTableNumberRange(config.DayTime{Time: now})})
	tableName := indexBkts[0]
	lbls := mustParseLabels(`{foo="bar", a="b"}`)

	tempDir := t.TempDir()
	objectStoragePath := filepath.Join(tempDir, objectsStorageDirName)
	tablePathInStorage := filepath.Join(objectStoragePath, tableName.Prefix)
	tableWorkingDirectory := filepath.Join(tempDir, workingDirName, tableName.Prefix)

	require.NoError(t, util.EnsureDirectory(objectStoragePath))
	require.NoError(t, util.EnsureDirectory(tablePathInStorage))
	require.NoError(t, util.EnsureDirectory(tableWorkingDirectory))

	indexFormat, err := periodConfig.TSDBFormat()
	require.NoError(t, err)

	// Create several valid multi-tenant index files.
	for i := 0; i < 5; i++ {
		userStreams := map[string][]stream{
			"user_0": {buildStream(lbls, buildChunkMetas(int64(i*10), int64(i*10+5)), "")},
			"user_1": {buildStream(lbls, buildChunkMetas(int64(i*10), int64(i*10+5)), "")},
		}
		setupMultiTenantIndex(t, indexFormat, userStreams, tablePathInStorage, time.Unix(int64(i), 0))
	}

	// Create a corrupt file with a valid multi-tenant TSDB filename.
	// It will be downloaded successfully but OpenShippableTSDB will fail
	// because the content is not a valid TSDB index.
	corruptFilePath := filepath.Join(tablePathInStorage, "100-corrupt-node.tsdb")
	require.NoError(t, os.WriteFile(corruptFilePath, []byte("invalid tsdb data"), 0o644))

	objectClient, err := local.NewFSObjectClient(local.FSConfig{Directory: objectStoragePath})
	require.NoError(t, err)

	commonIndexSet, err := newMockIndexSet("", tableName.Prefix, tableWorkingDirectory, objectClient)
	require.NoError(t, err)
	require.Len(t, commonIndexSet.ListSourceFiles(), 6)

	tc := newTableCompactor(
		context.Background(),
		commonIndexSet,
		map[string]compactor.IndexSet{},
		func(userID string) (compactor.IndexSet, error) {
			return newMockIndexSet(userID, tableName.Prefix,
				filepath.Join(tableWorkingDirectory, userID), objectClient)
		},
		periodConfig,
	)

	// CompactTable should return an error due to the corrupt file.
	err = tc.CompactTable()
	require.Error(t, err)

	// Verify that all downloaded temp files in the working directory are cleaned up.
	// Before the fix, the defer block was not registered on the error path,
	// leaving downloaded files and open file descriptors behind.
	entries, readErr := os.ReadDir(tableWorkingDirectory)
	require.NoError(t, readErr)

	var remainingTSDB []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".tsdb") {
			remainingTSDB = append(remainingTSDB, e.Name())
		}
	}
	require.Empty(t, remainingTSDB,
		"expected all downloaded TSDB files to be cleaned up after error, but found: %v", remainingTSDB)
}

// TestCompactedIndex_RoundTrip drives a compactedIndex over random series the way retention and deletion do:
// iterate the series, remove chunks, clean up emptied series and index rewritten chunks. It then verifies the
// built index holds exactly the expected series and chunks. Every case is repeated with hashes that force the
// Builder's stream collision handling, all of which must produce byte-identical indexes.
func TestCompactedIndex_RoundTrip(t *testing.T) {
	const numSeries = 500

	periodConfig := config.PeriodConfig{
		IndexTables: config.IndexPeriodicTableConfig{
			PeriodicTableConfig: config.PeriodicTableConfig{Period: config.ObjectStorageIndexRequiredPeriod}},
		Schema: "v13",
	}
	schemaCfg := config.SchemaConfig{Configs: []config.PeriodConfig{periodConfig}}
	now := model.Now()
	tableName := IndexBuckets(now, now, []config.TableRange{periodConfig.GetIndexTableNumberRange(config.DayTime{Time: now})})[0].Prefix
	tableStart := int64(retention.ExtractIntervalFromTableName(tableName).Start)
	indexFormat, err := periodConfig.TSDBFormat()
	require.NoError(t, err)
	userID := buildUserID(0)

	hashes := []func(labels.Labels) uint64{
		labels.Labels.Hash,
		func(labels.Labels) uint64 { return 0 },                  // every stream collides
		func(ls labels.Labels) uint64 { return ls.Hash() % 8 },   // many streams per hash
		func(ls labels.Labels) uint64 { return ls.Hash() % 251 }, // a few streams per hash
	}

	for seed := uint64(1); seed <= 5; seed++ {
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewPCG(seed, seed))
			series := genRoundTripSeries(rng, numSeries, tableStart)
			byID := make(map[string]*roundTripSeries, len(series))
			for _, rs := range series {
				byID[rs.ls.String()] = rs
			}

			var firstIndex []byte
			for i, hash := range hashes {
				builder := NewBuilder(indexFormat)
				builder.hash = hash
				for _, add := range roundTripAdds(rng, series) {
					// copy the labels on every call so lookups can't rely on sharing memory with the stored labels
					builder.AddSeries(add.s.ls.Copy(), add.s.fp, add.chunks)
				}
				builder.FinalizeChunks()
				c := newCompactedIndex(context.Background(), tableName, userID, t.TempDir(), periodConfig, builder)

				// remove chunks while iterating, as retention does, and check every series is visited once with its
				// original chunks
				visited := map[string]bool{}
				require.NoError(t, c.ForEachSeries(context.Background(), func(s retention.Series) error {
					rs := byID[string(s.SeriesID())]
					require.NotNil(t, rs, "unexpected series %s", s.SeriesID())
					require.True(t, labels.Equal(rs.ls, s.Labels()))
					require.False(t, visited[rs.ls.String()], "series %s visited twice", rs.ls)
					visited[rs.ls.String()] = true

					var chunkIDs []string
					for _, chk := range s.Chunks() {
						chunkIDs = append(chunkIDs, chk.ChunkID)
					}
					require.ElementsMatch(t, rs.chunkIDs(schemaCfg, userID, rs.initial), chunkIDs)

					for _, chk := range rs.removed {
						removed, err := c.RemoveChunk(chk.From(), chk.Through(), []byte(userID), s.Labels(), rs.chunkID(schemaCfg, userID, chk))
						require.NoError(t, err)
						require.True(t, removed)
					}
					// a chunk the series never had
					missing := index.ChunkMeta{MinTime: tableStart, MaxTime: tableStart + 1, Checksum: rng.Uint32()}
					removed, err := c.RemoveChunk(missing.From(), missing.Through(), []byte(userID), s.Labels(), rs.chunkID(schemaCfg, userID, missing))
					require.NoError(t, err)
					require.False(t, removed)
					return nil
				}))
				require.Len(t, visited, len(series))

				for _, rs := range series {
					if rs.cleanup {
						require.NoError(t, c.CleanupSeries([]byte(userID), rs.ls.Copy()))
					}
					for _, chk := range rs.added {
						// the store hands IndexChunk labels with the __name__ the chunk store convention adds
						lbls := labels.NewBuilder(rs.ls).Set(model.MetricNameLabel, "logs").Labels()
						indexed, err := c.IndexChunk(chunkMetaToChunkRef(userID, chk, rs.ls), lbls, 0, chk.KB, chk.Entries)
						require.NoError(t, err)
						require.True(t, indexed)
					}
					if !rs.cleanup {
						// removed and indexed chunks are only applied by ToIndexFile, so every initial chunk still exists
						chk := rs.initial[rng.IntN(len(rs.initial))]
						exists, err := c.ChunkExists([]byte(userID), rs.ls, chunkMetaToChunkRef(userID, chk, rs.ls))
						require.NoError(t, err)
						require.True(t, exists)
					}
				}
				require.Error(t, c.CleanupSeries([]byte(userID), labels.FromStrings("missing", "series")))

				data := verifyRoundTripIndex(t, c, byID)
				if i == 0 {
					firstIndex = data
					continue
				}
				require.Equal(t, firstIndex, data, "index built with hash %d differs from the real hash", i)
			}
		})
	}
}

type roundTripSeries struct {
	ls      labels.Labels
	fp      model.Fingerprint
	initial index.ChunkMetas
	removed index.ChunkMetas // removed during iteration
	cleanup bool             // CleanupSeries is called after all chunks are removed
	added   index.ChunkMetas // indexed after iteration
}

// expected returns the chunks the series should have in the built index.
func (s *roundTripSeries) expected() index.ChunkMetas {
	var out index.ChunkMetas
	for _, chk := range s.initial {
		if !slices.Contains(s.removed, chk) {
			out = append(out, chk)
		}
	}
	out = append(out, s.added...)
	sort.Sort(out)
	return out
}

func (s *roundTripSeries) chunkID(schemaCfg config.SchemaConfig, userID string, chk index.ChunkMeta) string {
	return schemaCfg.ExternalKey(logproto.ChunkRef{UserID: userID, Fingerprint: uint64(s.fp), From: chk.From(), Through: chk.Through(), Checksum: chk.Checksum})
}

func (s *roundTripSeries) chunkIDs(schemaCfg config.SchemaConfig, userID string, chks index.ChunkMetas) []string {
	ids := make([]string, 0, len(chks))
	for _, chk := range chks {
		ids = append(ids, s.chunkID(schemaCfg, userID, chk))
	}
	return ids
}

// genRoundTripSeries generates n distinct random series along with the updates to apply to each.
func genRoundTripSeries(rng *rand.Rand, n int, tableStart int64) []*roundTripSeries {
	// label names include ones that aren't legacy-valid and values that labels.String() has to quote/escape,
	// since the series ID handed to retention is labels.String()
	names := []string{"cluster", "namespace", "pod", "container", "job", "level", "service.name", "ünïcode"}
	values := []string{"prod", `with "quotes"`, `back\slash`, "new\nline", "ünïcode", "\xff\xfe", `{a="b"}`, ", "}

	const hourMs = int64(time.Hour / time.Millisecond)
	chunks := func(n int) index.ChunkMetas {
		out := make(index.ChunkMetas, 0, n)
		for range n {
			minTime := tableStart + rng.Int64N(22*hourMs) // keep chunks within the day table
			out = append(out, index.ChunkMeta{
				MinTime:  minTime,
				MaxTime:  minTime + 1 + rng.Int64N(hourMs),
				Checksum: rng.Uint32(),
				KB:       rng.Uint32N(1 << 10),
				Entries:  rng.Uint32N(1 << 10),
			})
		}
		return out
	}

	var series []*roundTripSeries
	seen := map[string]struct{}{}
	for len(series) < n {
		m := map[string]string{}
		for range 1 + rng.IntN(5) {
			value := fmt.Sprintf("value-%d", rng.IntN(1000))
			if rng.IntN(2) == 0 {
				value = values[rng.IntN(len(values))]
			}
			m[names[rng.IntN(len(names))]] = value
		}
		ls := labels.FromMap(m)
		if _, ok := seen[ls.String()]; ok {
			continue
		}
		seen[ls.String()] = struct{}{}
		s := &roundTripSeries{ls: ls, fp: model.Fingerprint(labels.StableHash(ls)), initial: chunks(1 + rng.IntN(8))}

		// an even mix of the updates retention and deletion make, including leaving a series as is
		switch rng.IntN(5) {
		case 0: // remove one chunk
			s.removed = s.initial[rng.IntN(len(s.initial)):][:1]
		case 1: // remove every chunk and clean up the series
			s.removed, s.cleanup = s.initial, true
		case 2: // remove every chunk without cleaning up, leaving an empty stream that must not be written
			s.removed = s.initial
		case 3: // rewrite a chunk: remove it and index its replacement
			s.removed = s.initial[:1]
			s.added = chunks(1)
		}
		series = append(series, s)
	}
	return series
}

type roundTripAdd struct {
	s      *roundTripSeries
	chunks index.ChunkMetas
}

// roundTripAdds splits each series' chunks over several shuffled AddSeries calls, with some chunks repeated, the
// way a series shows up in several source indexes during compaction.
func roundTripAdds(rng *rand.Rand, series []*roundTripSeries) []roundTripAdd {
	var adds []roundTripAdd
	for _, s := range series {
		chks := s.initial
		for len(chks) > 0 {
			n := 1 + rng.IntN(len(chks))
			batch := slices.Clone(chks[:n])
			if rng.IntN(3) == 0 {
				batch = append(batch, s.initial[rng.IntN(len(s.initial))])
			}
			adds = append(adds, roundTripAdd{s: s, chunks: batch})
			chks = chks[n:]
		}
	}
	rng.Shuffle(len(adds), func(i, j int) { adds[i], adds[j] = adds[j], adds[i] })
	return adds
}

// verifyRoundTripIndex builds the index, checks it against the expected series and returns the index file.
func verifyRoundTripIndex(t *testing.T, c *compactedIndex, series map[string]*roundTripSeries) []byte {
	idx, err := c.ToIndexFile()
	require.NoError(t, err)
	defer idx.Close()

	actual := map[string]index.ChunkMetas{}
	err = idx.(*TSDBFile).Index.(*TSDBIndex).ForSeries(context.Background(), "", nil, 0, math.MaxInt64, func(ls labels.Labels, fp model.Fingerprint, chks []index.ChunkMeta) (stop bool) {
		rs := series[ls.String()]
		require.NotNil(t, rs, "unexpected series %s", ls)
		require.Equal(t, rs.fp, fp)
		require.NotContains(t, actual, ls.String(), "series %s returned twice", ls)
		actual[ls.String()] = slices.Clone(chks)
		return false
	}, labels.MustNewMatcher(labels.MatchEqual, "", ""))
	require.NoError(t, err)

	expected := map[string]index.ChunkMetas{}
	for k, rs := range series {
		if chks := rs.expected(); len(chks) > 0 {
			expected[k] = chks
		}
	}
	require.Equal(t, expected, actual)

	data, err := os.ReadFile(idx.Path())
	require.NoError(t, err)
	return data
}

// compactionScenario models one tenant's day table being compacted: an existing compacted index plus the
// multi-tenant indexes uploaded since, where every ingester replica (RF=3) indexes the same chunks.
type compactionScenario struct {
	userID      string
	indexFormat int
	numStreams  int
	idxSet      compactor.IndexSet
	multiTenant []Index
}

func setupCompactionScenario(t testing.TB) *compactionScenario {
	const (
		userID          = "tenant-a"
		numSeries       = 50_000
		compactedChunks = 20  // chunks per series already in the compacted index
		uploadRounds    = 2   // multi-tenant upload rounds since the last compaction
		replicas        = 3   // each round is uploaded once per ingester replica
		roundChunks     = 2   // chunks per active series in each round
		activeEvery     = 3   // one in activeEvery series receives new chunks
		newSeries       = 500 // series that only exist in the multi-tenant indexes
	)

	periodConfig := config.PeriodConfig{
		IndexTables: config.IndexPeriodicTableConfig{
			PeriodicTableConfig: config.PeriodicTableConfig{Period: config.ObjectStorageIndexRequiredPeriod}},
		Schema: "v13",
	}
	indexFormat, err := periodConfig.TSDBFormat()
	require.NoError(t, err)

	chunks := func(series, from, n int) index.ChunkMetas {
		chks := make(index.ChunkMetas, 0, n)
		for c := from; c < from+n; c++ {
			chks = append(chks, index.ChunkMeta{
				MinTime:  int64(c) * 1000,
				MaxTime:  int64(c)*1000 + 999,
				Checksum: uint32(series*1000 + c),
				KB:       uint32(c),
				Entries:  uint32(c),
			})
		}
		return chks
	}

	tempDir := t.TempDir()
	const tableName = "index_1"
	objectStorage := filepath.Join(tempDir, objectsStorageDirName)

	compacted := make([]stream, 0, numSeries)
	for i := range numSeries {
		compacted = append(compacted, buildStream(benchSeriesLabels(i), chunks(i, 0, compactedChunks), ""))
	}
	setupPerTenantIndex(t, indexFormat, compacted, filepath.Join(objectStorage, tableName, userID), time.Unix(0, 0))

	var multiTenant []Index
	for round := range uploadRounds {
		var streams []stream
		for i := range numSeries + newSeries {
			if i < numSeries && i%activeEvery != round%activeEvery {
				continue
			}
			streams = append(streams, buildStream(benchSeriesLabels(i), chunks(i, compactedChunks+round*roundChunks, roundChunks), ""))
		}
		for replica := range replicas {
			indexPath := setupMultiTenantIndex(t, indexFormat, map[string][]stream{userID: streams}, filepath.Join(tempDir, "multi-tenant"), time.Unix(int64(round*replicas+replica), 0))
			idx, err := OpenShippableTSDB(indexPath, index.MmapOptions{})
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, idx.Close()) })
			multiTenant = append(multiTenant, idx.(Index))
		}
	}

	objectClient, err := local.NewFSObjectClient(local.FSConfig{Directory: objectStorage})
	require.NoError(t, err)
	idxSet, err := newMockIndexSet(userID, tableName, filepath.Join(tempDir, workingDirName, tableName, userID), objectClient)
	require.NoError(t, err)

	return &compactionScenario{
		userID:      userID,
		indexFormat: indexFormat,
		numStreams:  numSeries + newSeries,
		idxSet:      idxSet,
		multiTenant: multiTenant,
	}
}

func BenchmarkSetupBuilder(b *testing.B) {
	s := setupCompactionScenario(b)
	ctx := context.Background()

	b.ReportAllocs()
	var retained int64
	for b.Loop() {
		b.StopTimer()
		before := heapAlloc()
		b.StartTimer()

		builder, err := setupBuilder(ctx, s.indexFormat, s.userID, s.idxSet, s.multiTenant)
		require.NoError(b, err)

		b.StopTimer()
		retained = int64(heapAlloc()) - int64(before)
		require.Equal(b, s.numStreams, builder.numStreams())
		runtime.KeepAlive(builder)
		b.StartTimer()
	}
	b.ReportMetric(float64(retained)/float64(s.numStreams), "retained-B/series")
}
