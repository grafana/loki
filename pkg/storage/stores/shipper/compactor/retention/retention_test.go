package retention

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/pkg/chunkenc"
	ingesterclient "github.com/grafana/loki/pkg/ingester/client"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/pkg/storage/stores/shipper/util"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/validation"
)

type mockChunkClient struct {
	mtx           sync.Mutex
	deletedChunks map[string]struct{}
}

func (m *mockChunkClient) DeleteChunk(_ context.Context, _, chunkID string) error {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	m.deletedChunks[string([]byte(chunkID))] = struct{}{} // forces a copy, because this string is only valid within the delete fn.
	return nil
}

func (m *mockChunkClient) IsChunkNotFoundErr(err error) bool {
	return false
}

func (m *mockChunkClient) getDeletedChunkIds() []string {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	chunkIDs := make([]string, 0, len(m.deletedChunks))
	for chunkID := range m.deletedChunks {
		chunkIDs = append(chunkIDs, chunkID)
	}

	return chunkIDs
}

func Test_Retention(t *testing.T) {
	minListMarkDelay = 1 * time.Second
	for _, tt := range []struct {
		name   string
		limits Limits
		chunks []chunk.Chunk
		alive  []bool
	}{
		{
			"nothing is expiring",
			fakeLimits{
				perTenant: map[string]retentionLimit{
					"1": {retentionPeriod: 1000 * time.Hour},
					"2": {retentionPeriod: 1000 * time.Hour},
				},
			},
			[]chunk.Chunk{
				createChunk(t, "1", labels.Labels{labels.Label{Name: "foo", Value: "bar"}}, start, start.Add(1*time.Hour)),
				createChunk(t, "2", labels.Labels{labels.Label{Name: "foo", Value: "buzz"}}, start.Add(26*time.Hour), start.Add(27*time.Hour)),
			},
			[]bool{
				true,
				true,
			},
		},
		{
			"one global expiration",
			fakeLimits{
				perTenant: map[string]retentionLimit{
					"1": {retentionPeriod: 10 * time.Hour},
					"2": {retentionPeriod: 1000 * time.Hour},
				},
			},
			[]chunk.Chunk{
				createChunk(t, "1", labels.Labels{labels.Label{Name: "foo", Value: "bar"}}, start, start.Add(1*time.Hour)),
				createChunk(t, "2", labels.Labels{labels.Label{Name: "foo", Value: "buzz"}}, start.Add(26*time.Hour), start.Add(27*time.Hour)),
			},
			[]bool{
				false,
				true,
			},
		},
		{
			"one global expiration and stream",
			fakeLimits{
				perTenant: map[string]retentionLimit{
					"1": {
						retentionPeriod: 10 * time.Hour,
						streamRetention: []validation.StreamRetention{
							{Period: model.Duration(5 * time.Hour), Matchers: []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "foo", "buzz")}},
						},
					},
					"2": {retentionPeriod: 1000 * time.Hour},
				},
			},
			[]chunk.Chunk{
				createChunk(t, "1", labels.Labels{labels.Label{Name: "foo", Value: "bar"}}, start, start.Add(1*time.Hour)),
				createChunk(t, "2", labels.Labels{labels.Label{Name: "foo", Value: "fuzz"}}, start.Add(26*time.Hour), start.Add(27*time.Hour)),
				createChunk(t, "1", labels.Labels{labels.Label{Name: "foo", Value: "buzz"}}, model.Now().Add(-2*time.Hour), model.Now().Add(-1*time.Hour)),
				createChunk(t, "1", labels.Labels{labels.Label{Name: "foo", Value: "buzz"}}, model.Now().Add(-7*time.Hour), model.Now().Add(-6*time.Hour)),
			},
			[]bool{
				false,
				true,
				true,
				false,
			},
		},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			// insert in the store.
			var (
				clientMetrics = storage.NewClientMetrics()
				store         = newTestStore(t, clientMetrics)
				expectDeleted = []string{}
			)
			defer clientMetrics.Unregister()
			for _, c := range tt.chunks {
				require.NoError(t, store.Put(context.TODO(), []chunk.Chunk{c}))
			}
			store.Stop()

			// marks and sweep
			expiration := NewExpirationChecker(tt.limits)
			workDir := filepath.Join(t.TempDir(), "retention")
			chunkClient := &mockChunkClient{deletedChunks: map[string]struct{}{}}
			sweep, err := NewSweeper(workDir, chunkClient, 10, 0, nil)
			require.NoError(t, err)
			sweep.Start()
			defer sweep.Stop()

			marker, err := NewMarker(workDir, store.schemaCfg, expiration, nil, prometheus.NewRegistry())
			require.NoError(t, err)
			for _, table := range store.indexTables() {
				_, _, err := marker.MarkForDelete(context.Background(), table.name, "", table.DB, util_log.Logger)
				require.Nil(t, err)
				table.Close()

			}

			// assert using the store again.
			store.open()

			for i, e := range tt.alive {
				require.Equal(t, e, store.HasChunk(tt.chunks[i]), "chunk %d should be %t", i, e)
				if !e {
					expectDeleted = append(expectDeleted, store.schemaCfg.ExternalKey(tt.chunks[i].ChunkRef))
				}
			}
			sort.Strings(expectDeleted)
			store.Stop()
			if len(expectDeleted) != 0 {
				require.Eventually(t, func() bool {
					actual := chunkClient.getDeletedChunkIds()
					sort.Strings(actual)
					fmt.Println(expectDeleted, actual)
					return assert.ObjectsAreEqual(expectDeleted, actual)
				}, 10*time.Second, 1*time.Second)
			}
		})
	}
}

type noopWriter struct{}

func (noopWriter) Put(chunkID []byte) error { return nil }
func (noopWriter) Count() int64             { return 0 }
func (noopWriter) Close() error             { return nil }

type noopCleaner struct{}

func (noopCleaner) Cleanup(userID []byte, lbls labels.Labels) error { return nil }

func Test_EmptyTable(t *testing.T) {
	schema := allSchemas[0]
	cm := storage.NewClientMetrics()
	defer cm.Unregister()
	store := newTestStore(t, cm)
	c1 := createChunk(t, "1", labels.Labels{labels.Label{Name: "foo", Value: "bar"}}, schema.from, schema.from.Add(1*time.Hour))
	c2 := createChunk(t, "2", labels.Labels{labels.Label{Name: "foo", Value: "buzz"}, labels.Label{Name: "bar", Value: "foo"}}, schema.from, schema.from.Add(1*time.Hour))
	c3 := createChunk(t, "2", labels.Labels{labels.Label{Name: "foo", Value: "buzz"}, labels.Label{Name: "bar", Value: "buzz"}}, schema.from, schema.from.Add(1*time.Hour))

	require.NoError(t, store.Put(context.TODO(), []chunk.Chunk{
		c1, c2, c3,
	}))

	store.Stop()

	tables := store.indexTables()
	require.Len(t, tables, 1)
	err := tables[0].DB.Update(func(tx *bbolt.Tx) error {
		it, err := NewChunkIndexIterator(tx.Bucket(local.IndexBucketName), schema.config)
		require.NoError(t, err)
		empty, _, err := markforDelete(context.Background(), tables[0].name, noopWriter{}, it, noopCleaner{},
			NewExpirationChecker(&fakeLimits{perTenant: map[string]retentionLimit{"1": {retentionPeriod: 0}, "2": {retentionPeriod: 0}}}), nil)
		require.NoError(t, err)
		require.True(t, empty)
		return nil
	})
	require.NoError(t, err)

	// table with no chunks should error
	tempDir := t.TempDir()
	emptyDB, err := util.SafeOpenBoltdbFile(filepath.Join(tempDir, "empty.db"))
	require.NoError(t, err)
	err = emptyDB.Update(func(tx *bbolt.Tx) error {
		bucket, err := tx.CreateBucket(local.IndexBucketName)
		require.NoError(t, err)

		it, err := NewChunkIndexIterator(bucket, schema.config)
		require.NoError(t, err)
		_, _, err = markforDelete(context.Background(), tables[0].name, noopWriter{}, it, noopCleaner{},
			NewExpirationChecker(&fakeLimits{}), nil)
		require.Equal(t, err, errNoChunksFound)
		return nil
	})
	require.NoError(t, err)
}

func createChunk(t testing.TB, userID string, lbs labels.Labels, from model.Time, through model.Time) chunk.Chunk {
	t.Helper()
	const (
		targetSize = 1500 * 1024
		blockSize  = 256 * 1024
	)
	labelsBuilder := labels.NewBuilder(lbs)
	labelsBuilder.Set(labels.MetricName, "logs")
	metric := labelsBuilder.Labels()
	fp := ingesterclient.Fingerprint(lbs)
	chunkEnc := chunkenc.NewMemChunk(chunkenc.EncSnappy, chunkenc.UnorderedHeadBlockFmt, blockSize, targetSize)

	for ts := from; !ts.After(through); ts = ts.Add(1 * time.Minute) {
		require.NoError(t, chunkEnc.Append(&logproto.Entry{
			Timestamp: ts.Time(),
			Line:      ts.String(),
		}))
	}

	require.NoError(t, chunkEnc.Close())
	c := chunk.NewChunk(userID, fp, metric, chunkenc.NewFacade(chunkEnc, blockSize, targetSize), from, through)
	require.NoError(t, c.Encode())
	return c
}

func labelsSeriesID(ls labels.Labels) []byte {
	h := sha256.Sum256([]byte(labelsString(ls)))
	return encodeBase64Bytes(h[:])
}

func encodeBase64Bytes(bytes []byte) []byte {
	encodedLen := base64.RawStdEncoding.EncodedLen(len(bytes))
	encoded := make([]byte, encodedLen)
	base64.RawStdEncoding.Encode(encoded, bytes)
	return encoded
}

// Backwards-compatible with model.Metric.String()
func labelsString(ls labels.Labels) string {
	metricName := ls.Get(labels.MetricName)
	if metricName != "" && len(ls) == 1 {
		return metricName
	}
	var b strings.Builder
	b.Grow(1000)

	b.WriteString(metricName)
	b.WriteByte('{')
	i := 0
	for _, l := range ls {
		if l.Name == labels.MetricName {
			continue
		}
		if i > 0 {
			b.WriteByte(',')
			b.WriteByte(' ')
		}
		b.WriteString(l.Name)
		b.WriteByte('=')
		var buf [1000]byte
		b.Write(strconv.AppendQuote(buf[:0], l.Value))
		i++
	}
	b.WriteByte('}')

	return b.String()
}

func TestChunkRewriter(t *testing.T) {
	minListMarkDelay = 1 * time.Second
	now := model.Now()
	for _, tt := range []struct {
		name                   string
		chunk                  chunk.Chunk
		rewriteIntervalFilters []IntervalFilter
	}{
		{
			name:  "no rewrites",
			chunk: createChunk(t, "1", labels.Labels{labels.Label{Name: "foo", Value: "bar"}}, now.Add(-time.Hour), now),
		},
		{
			name:  "no rewrites with chunk spanning multiple tables",
			chunk: createChunk(t, "1", labels.Labels{labels.Label{Name: "foo", Value: "bar"}}, now.Add(-48*time.Hour), now),
		},
		{
			name:  "rewrite first half",
			chunk: createChunk(t, "1", labels.Labels{labels.Label{Name: "foo", Value: "bar"}}, now.Add(-2*time.Hour), now),
			rewriteIntervalFilters: []IntervalFilter{
				{
					Interval: model.Interval{
						Start: now.Add(-2 * time.Hour),
						End:   now.Add(-1 * time.Hour),
					},
				},
			},
		},
		{
			name:  "rewrite second half",
			chunk: createChunk(t, "1", labels.Labels{labels.Label{Name: "foo", Value: "bar"}}, now.Add(-2*time.Hour), now),
			rewriteIntervalFilters: []IntervalFilter{
				{
					Interval: model.Interval{
						Start: now.Add(-time.Hour),
						End:   now,
					},
				},
			},
		},
		{
			name:  "rewrite multiple intervals",
			chunk: createChunk(t, "1", labels.Labels{labels.Label{Name: "foo", Value: "bar"}}, now.Add(-12*time.Hour), now),
			rewriteIntervalFilters: []IntervalFilter{
				{
					Interval: model.Interval{
						Start: now.Add(-12 * time.Hour),
						End:   now.Add(-10 * time.Hour),
					},
				},
				{
					Interval: model.Interval{
						Start: now.Add(-9 * time.Hour),
						End:   now.Add(-5 * time.Hour),
					},
				},
				{
					Interval: model.Interval{
						Start: now.Add(-2 * time.Hour),
						End:   now,
					},
				},
			},
		},
		{
			name:  "rewrite chunk spanning multiple days with multiple intervals",
			chunk: createChunk(t, "1", labels.Labels{labels.Label{Name: "foo", Value: "bar"}}, now.Add(-72*time.Hour), now),
			rewriteIntervalFilters: []IntervalFilter{
				{
					Interval: model.Interval{
						Start: now.Add(-71 * time.Hour),
						End:   now.Add(-47 * time.Hour),
					},
				},
				{
					Interval: model.Interval{
						Start: now.Add(-40 * time.Hour),
						End:   now.Add(-30 * time.Hour),
					},
				},
				{
					Interval: model.Interval{
						Start: now.Add(-2 * time.Hour),
						End:   now,
					},
				},
			},
		},
		{
			name:  "remove no lines using a filter function",
			chunk: createChunk(t, "1", labels.Labels{labels.Label{Name: "foo", Value: "bar"}}, now.Add(-2*time.Hour), now),
			rewriteIntervalFilters: []IntervalFilter{
				{
					Interval: model.Interval{
						Start: now.Add(-1 * time.Hour),
						End:   now,
					},
					Filter: func(s string) bool {
						return false
					},
				},
			},
		},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			cm := storage.NewClientMetrics()
			defer cm.Unregister()
			store := newTestStore(t, cm)
			require.NoError(t, store.Put(context.TODO(), []chunk.Chunk{tt.chunk}))
			store.Stop()

			chunkClient := client.NewClient(newTestObjectClient(store.chunkDir, cm), client.FSEncoder, schemaCfg)
			for _, indexTable := range store.indexTables() {
				err := indexTable.DB.Update(func(tx *bbolt.Tx) error {
					bucket := tx.Bucket(local.IndexBucketName)
					if bucket == nil {
						return nil
					}

					cr, err := newChunkRewriter(chunkClient, store.schemaCfg.Configs[0], indexTable.name, bucket)
					require.NoError(t, err)

					wroteChunks, err := cr.rewriteChunk(context.Background(), entryFromChunk(store.schemaCfg, tt.chunk), tt.rewriteIntervalFilters)
					require.NoError(t, err)
					if len(tt.rewriteIntervalFilters) == 0 {
						require.False(t, wroteChunks)
					}
					return nil
				})
				require.NoError(t, err)
				require.NoError(t, indexTable.DB.Close())
			}

			store.open()
			chunks := store.GetChunks(tt.chunk.UserID, tt.chunk.From, tt.chunk.Through, tt.chunk.Metric)

			// number of chunks should be the new re-written chunks + the source chunk
			require.Len(t, chunks, len(tt.rewriteIntervalFilters)+1)
			for _, ivf := range tt.rewriteIntervalFilters {
				expectedChk := createChunk(t, tt.chunk.UserID, labels.Labels{labels.Label{Name: "foo", Value: "bar"}}, ivf.Interval.Start, ivf.Interval.End)
				for i, chk := range chunks {
					if store.schemaCfg.ExternalKey(chk.ChunkRef) == store.schemaCfg.ExternalKey(expectedChk.ChunkRef) {
						chunks = append(chunks[:i], chunks[i+1:]...)
						break
					}
				}
			}

			// the source chunk should still be there in the store
			require.Len(t, chunks, 1)
			require.Equal(t, store.schemaCfg.ExternalKey(tt.chunk.ChunkRef), store.schemaCfg.ExternalKey(chunks[0].ChunkRef))
			store.Stop()
		})
	}
}

type seriesCleanedRecorder struct {
	// map of userID -> map of labels hash -> struct{}
	deletedSeries map[string]map[uint64]struct{}
}

func newSeriesCleanRecorder() *seriesCleanedRecorder {
	return &seriesCleanedRecorder{map[string]map[uint64]struct{}{}}
}

func (s *seriesCleanedRecorder) Cleanup(userID []byte, lbls labels.Labels) error {
	s.deletedSeries[string(userID)] = map[uint64]struct{}{lbls.Hash(): {}}
	return nil
}

type chunkExpiry struct {
	isExpired                 bool
	nonDeletedIntervalFilters []IntervalFilter
}

type mockExpirationChecker struct {
	ExpirationChecker
	chunksExpiry map[string]chunkExpiry
}

func newMockExpirationChecker(chunksExpiry map[string]chunkExpiry) mockExpirationChecker {
	return mockExpirationChecker{chunksExpiry: chunksExpiry}
}

func (m mockExpirationChecker) Expired(ref ChunkEntry, now model.Time) (bool, []IntervalFilter) {
	ce := m.chunksExpiry[string(ref.ChunkID)]
	return ce.isExpired, ce.nonDeletedIntervalFilters
}

func (m mockExpirationChecker) DropFromIndex(ref ChunkEntry, tableEndTime model.Time, now model.Time) bool {
	return false
}

func TestMarkForDelete_SeriesCleanup(t *testing.T) {
	now := model.Now()
	schema := allSchemas[2]
	userID := "1"
	todaysTableInterval := ExtractIntervalFromTableName(schema.config.IndexTables.TableFor(now))

	for _, tc := range []struct {
		name                  string
		chunks                []chunk.Chunk
		expiry                []chunkExpiry
		expectedDeletedSeries []map[uint64]struct{}
		expectedEmpty         []bool
		expectedModified      []bool
	}{
		{
			name: "no chunk and series deleted",
			chunks: []chunk.Chunk{
				createChunk(t, userID, labels.Labels{labels.Label{Name: "foo", Value: "1"}}, todaysTableInterval.Start, todaysTableInterval.Start.Add(30*time.Minute)),
			},
			expiry: []chunkExpiry{
				{
					isExpired: false,
				},
			},
			expectedDeletedSeries: []map[uint64]struct{}{
				nil,
			},
			expectedEmpty: []bool{
				false,
			},
			expectedModified: []bool{
				false,
			},
		},
		{
			name: "only one chunk in store which gets deleted",
			chunks: []chunk.Chunk{
				createChunk(t, userID, labels.Labels{labels.Label{Name: "foo", Value: "1"}}, todaysTableInterval.Start, todaysTableInterval.Start.Add(30*time.Minute)),
			},
			expiry: []chunkExpiry{
				{
					isExpired: true,
				},
			},
			expectedDeletedSeries: []map[uint64]struct{}{
				nil,
			},
			expectedEmpty: []bool{
				true,
			},
			expectedModified: []bool{
				true,
			},
		},
		{
			name: "only one chunk in store which gets partially deleted",
			chunks: []chunk.Chunk{
				createChunk(t, userID, labels.Labels{labels.Label{Name: "foo", Value: "1"}}, todaysTableInterval.Start, todaysTableInterval.Start.Add(30*time.Minute)),
			},
			expiry: []chunkExpiry{
				{
					isExpired: true,
					nonDeletedIntervalFilters: []IntervalFilter{{
						Interval: model.Interval{
							Start: todaysTableInterval.Start,
							End:   todaysTableInterval.Start.Add(15 * time.Minute),
						},
					}},
				},
			},
			expectedDeletedSeries: []map[uint64]struct{}{
				nil,
			},
			expectedEmpty: []bool{
				false,
			},
			expectedModified: []bool{
				true,
			},
		},
		{
			name: "one of two chunks deleted",
			chunks: []chunk.Chunk{
				createChunk(t, userID, labels.Labels{labels.Label{Name: "foo", Value: "1"}}, todaysTableInterval.Start, todaysTableInterval.Start.Add(30*time.Minute)),
				createChunk(t, userID, labels.Labels{labels.Label{Name: "foo", Value: "2"}}, todaysTableInterval.Start, todaysTableInterval.Start.Add(30*time.Minute)),
			},
			expiry: []chunkExpiry{
				{
					isExpired: false,
				},
				{
					isExpired: true,
				},
			},
			expectedDeletedSeries: []map[uint64]struct{}{
				{labels.Labels{labels.Label{Name: "foo", Value: "2"}}.Hash(): struct{}{}},
			},
			expectedEmpty: []bool{
				false,
			},
			expectedModified: []bool{
				true,
			},
		},
		{
			name: "one of two chunks partially deleted",
			chunks: []chunk.Chunk{
				createChunk(t, userID, labels.Labels{labels.Label{Name: "foo", Value: "1"}}, todaysTableInterval.Start, todaysTableInterval.Start.Add(30*time.Minute)),
				createChunk(t, userID, labels.Labels{labels.Label{Name: "foo", Value: "2"}}, todaysTableInterval.Start, todaysTableInterval.Start.Add(30*time.Minute)),
			},
			expiry: []chunkExpiry{
				{
					isExpired: false,
				},
				{
					isExpired: true,
					nonDeletedIntervalFilters: []IntervalFilter{{
						Interval: model.Interval{
							Start: todaysTableInterval.Start,
							End:   todaysTableInterval.Start.Add(15 * time.Minute),
						},
					}},
				},
			},
			expectedDeletedSeries: []map[uint64]struct{}{
				nil,
			},
			expectedEmpty: []bool{
				false,
			},
			expectedModified: []bool{
				true,
			},
		},
		{
			name: "one big chunk partially deleted for yesterdays table without rewrite",
			chunks: []chunk.Chunk{
				createChunk(t, userID, labels.Labels{labels.Label{Name: "foo", Value: "1"}}, todaysTableInterval.Start.Add(-time.Hour), now),
			},
			expiry: []chunkExpiry{
				{
					isExpired: true,
					nonDeletedIntervalFilters: []IntervalFilter{{
						Interval: model.Interval{
							Start: todaysTableInterval.Start,
							End:   now,
						},
					}},
				},
			},
			expectedDeletedSeries: []map[uint64]struct{}{
				nil, nil,
			},
			expectedEmpty: []bool{
				true, false,
			},
			expectedModified: []bool{
				true, true,
			},
		},
		{
			name: "one big chunk partially deleted for yesterdays table with rewrite",
			chunks: []chunk.Chunk{
				createChunk(t, userID, labels.Labels{labels.Label{Name: "foo", Value: "1"}}, todaysTableInterval.Start.Add(-time.Hour), now),
			},
			expiry: []chunkExpiry{
				{
					isExpired: true,
					nonDeletedIntervalFilters: []IntervalFilter{{
						Interval: model.Interval{
							Start: todaysTableInterval.Start.Add(-30 * time.Minute),
							End:   now,
						},
					}},
				},
			},
			expectedDeletedSeries: []map[uint64]struct{}{
				nil, nil,
			},
			expectedEmpty: []bool{
				false, false,
			},
			expectedModified: []bool{
				true, true,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cm := storage.NewClientMetrics()
			defer cm.Unregister()
			store := newTestStore(t, cm)

			require.NoError(t, store.Put(context.TODO(), tc.chunks))
			chunksExpiry := map[string]chunkExpiry{}
			for i, chunk := range tc.chunks {
				chunksExpiry[store.schemaCfg.ExternalKey(chunk.ChunkRef)] = tc.expiry[i]
			}

			expirationChecker := newMockExpirationChecker(chunksExpiry)

			store.Stop()

			tables := store.indexTables()
			require.Len(t, tables, len(tc.expectedDeletedSeries))

			chunkClient := client.NewClient(newTestObjectClient(store.chunkDir, cm), client.FSEncoder, schemaCfg)

			for i, table := range tables {
				seriesCleanRecorder := newSeriesCleanRecorder()
				err := table.DB.Update(func(tx *bbolt.Tx) error {
					it, err := NewChunkIndexIterator(tx.Bucket(local.IndexBucketName), schema.config)
					require.NoError(t, err)

					cr, err := newChunkRewriter(chunkClient, schema.config, table.name, tx.Bucket(local.IndexBucketName))
					require.NoError(t, err)
					empty, isModified, err := markforDelete(context.Background(), table.name, noopWriter{}, it, seriesCleanRecorder,
						expirationChecker, cr)
					require.NoError(t, err)
					require.Equal(t, tc.expectedEmpty[i], empty)
					require.Equal(t, tc.expectedModified[i], isModified)
					return nil
				})
				require.NoError(t, err)

				require.EqualValues(t, tc.expectedDeletedSeries[i], seriesCleanRecorder.deletedSeries[userID])
			}
		})
	}
}

func TestMarkForDelete_DropChunkFromIndex(t *testing.T) {
	schema := allSchemas[2]
	cm := storage.NewClientMetrics()
	defer cm.Unregister()
	store := newTestStore(t, cm)
	now := model.Now()
	todaysTableInterval := ExtractIntervalFromTableName(schema.config.IndexTables.TableFor(now))
	retentionPeriod := now.Sub(todaysTableInterval.Start) / 2

	// chunks in retention
	c1 := createChunk(t, "1", labels.Labels{labels.Label{Name: "foo", Value: "1"}}, todaysTableInterval.Start, now)
	c2 := createChunk(t, "1", labels.Labels{labels.Label{Name: "foo", Value: "2"}}, todaysTableInterval.Start.Add(-7*24*time.Hour), now)

	// chunks out of retention
	c3 := createChunk(t, "1", labels.Labels{labels.Label{Name: "foo", Value: "1"}}, todaysTableInterval.Start, now.Add(-retentionPeriod))
	c4 := createChunk(t, "1", labels.Labels{labels.Label{Name: "foo", Value: "3"}}, todaysTableInterval.Start.Add(-12*time.Hour), todaysTableInterval.Start.Add(-10*time.Hour))
	c5 := createChunk(t, "1", labels.Labels{labels.Label{Name: "foo", Value: "4"}}, todaysTableInterval.Start, now.Add(-retentionPeriod))

	require.NoError(t, store.Put(context.TODO(), []chunk.Chunk{
		c1, c2, c3, c4, c5,
	}))

	store.Stop()

	tables := store.indexTables()
	require.Len(t, tables, 8)

	for i, table := range tables {
		err := table.DB.Update(func(tx *bbolt.Tx) error {
			it, err := NewChunkIndexIterator(tx.Bucket(local.IndexBucketName), schema.config)
			require.NoError(t, err)
			empty, _, err := markforDelete(context.Background(), table.name, noopWriter{}, it, noopCleaner{},
				NewExpirationChecker(fakeLimits{perTenant: map[string]retentionLimit{"1": {retentionPeriod: retentionPeriod}}}), nil)
			require.NoError(t, err)
			if i == 7 {
				require.False(t, empty)
			} else {
				require.True(t, empty, "table %s must be empty", table.name)
			}
			return nil
		})
		require.NoError(t, err)
		require.NoError(t, table.Close())
	}

	store.open()

	// verify the chunks which were not supposed to be deleted are still there
	require.True(t, store.HasChunk(c1))
	require.True(t, store.HasChunk(c2))

	// verify the chunks which were supposed to be deleted are gone
	require.False(t, store.HasChunk(c3))
	require.False(t, store.HasChunk(c4))
	require.False(t, store.HasChunk(c5))
}
