package retention

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/grafana/dskit/backoff"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/compression"
	ingesterclient "github.com/grafana/loki/v3/pkg/ingester/client"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/util/filter"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/validation"
)

type mockChunkClient struct {
	mtx              sync.Mutex
	deletedChunks    map[string]struct{}
	unstableDeletion bool
	perObjectCounter map[string]uint32
}

// newMockChunkClient creates a client that fails every first call to DeleteChunk if `unstableDeletion` is true.
func newMockChunkClient(unstableDeletion bool) *mockChunkClient {
	return &mockChunkClient{
		deletedChunks:    map[string]struct{}{},
		unstableDeletion: unstableDeletion,
		perObjectCounter: map[string]uint32{},
	}
}

// shouldFail returns true for every first call
func (m *mockChunkClient) shouldFail(objectKey string) bool {
	if !m.unstableDeletion {
		return false
	}
	shouldFail := m.perObjectCounter[objectKey]%2 == 0
	m.perObjectCounter[objectKey]++
	return shouldFail
}

func (m *mockChunkClient) DeleteChunk(_ context.Context, _, chunkID string) error {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	if m.shouldFail(chunkID) {
		return fmt.Errorf("chunk deletion for chunkID:%s is failed by mockChunkClient", chunkID)
	}
	m.deletedChunks[string([]byte(chunkID))] = struct{}{} // forces a copy, because this string is only valid within the delete fn.
	return nil
}

func (m *mockChunkClient) IsChunkNotFoundErr(_ error) bool {
	return false
}

func (m *mockChunkClient) getDeletedChunkIDs() []string {
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
				createChunk(t, "1", labels.FromStrings("foo", "bar"), start, start.Add(1*time.Hour)),
				createChunk(t, "2", labels.FromStrings("foo", "buzz"), start.Add(26*time.Hour), start.Add(27*time.Hour)),
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
				createChunk(t, "1", labels.FromStrings("foo", "bar"), start, start.Add(1*time.Hour)),
				createChunk(t, "2", labels.FromStrings("foo", "buzz"), start.Add(26*time.Hour), start.Add(27*time.Hour)),
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
				createChunk(t, "1", labels.FromStrings("foo", "bar"), start, start.Add(1*time.Hour)),
				createChunk(t, "2", labels.FromStrings("foo", "fuzz"), start.Add(26*time.Hour), start.Add(27*time.Hour)),
				createChunk(t, "1", labels.FromStrings("foo", "buzz"), model.Now().Add(-2*time.Hour), model.Now().Add(-1*time.Hour)),
				createChunk(t, "1", labels.FromStrings("foo", "buzz"), model.Now().Add(-7*time.Hour), model.Now().Add(-6*time.Hour)),
			},
			[]bool{
				false,
				true,
				true,
				false,
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// insert in the store.
			var (
				store         = newTestStore(t)
				expectDeleted = []string{}
			)
			for _, c := range tt.chunks {
				require.NoError(t, store.Put(context.TODO(), []chunk.Chunk{c}))
			}
			store.Stop()

			// marks and sweep
			expiration := NewExpirationChecker(tt.limits)
			workDir := filepath.Join(t.TempDir(), "retention")
			// must not fail the process because deletion must be retried
			chunkClient := newMockChunkClient(true)
			sweep, err := NewSweeper(workDir, chunkClient, 10, 0, backoff.Config{MaxRetries: 2}, nil)
			require.NoError(t, err)
			sweep.Start()
			defer sweep.Stop()

			marker, err := NewMarker(workDir, expiration, time.Hour, nil, prometheus.NewRegistry())
			require.NoError(t, err)
			for _, table := range store.indexTables() {
				_, _, err := marker.FindAndMarkChunksForDeletion(context.Background(), table.name, "", table, util_log.Logger)
				require.Nil(t, err)
			}

			for i, e := range tt.alive {
				require.Equal(t, e, store.HasChunk(tt.chunks[i]), "chunk %d should be %t", i, e)
				if !e {
					expectDeleted = append(expectDeleted, getChunkID(tt.chunks[i].ChunkRef))
				}
			}
			sort.Strings(expectDeleted)
			store.Stop()
			if len(expectDeleted) != 0 {
				require.Eventually(t, func() bool {
					actual := chunkClient.getDeletedChunkIDs()
					sort.Strings(actual)
					return assert.ObjectsAreEqual(expectDeleted, actual)
				}, 10*time.Second, 1*time.Second)
			}
		})
	}
}

func Test_Sweeper_deleteChunk(t *testing.T) {
	chunkID := "1/3fff2c2d7595e046:1916fa8c4bd:1916fdfb33d:bd55fc5"
	tests := map[string]struct {
		maxRetries    int
		expectedError error
	}{
		"expected error if chunk is not deleted and retry is disabled": {
			maxRetries:    1,
			expectedError: fmt.Errorf("chunk deletion for chunkID:%s is failed by mockChunkClient", chunkID),
		},
		"expected  no error if chunk is not deleted at the first attempt but retried": {
			maxRetries: 2,
		},
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			workDir := filepath.Join(t.TempDir(), "retention")
			chunkClient := newMockChunkClient(true)
			sweep, err := NewSweeper(workDir, chunkClient, 10, 0, backoff.Config{MaxRetries: data.maxRetries}, nil)
			require.NoError(t, err)

			err = sweep.deleteChunk(context.Background(), []byte(chunkID))
			if data.expectedError != nil {
				require.Equal(t, data.expectedError, err)
			} else {
				require.NoError(t, err)
			}
		})
	}

}

type noopWriter struct {
	count int64
}

func (n *noopWriter) Put(_ []byte) error {
	n.count++
	return nil
}
func (n *noopWriter) Count() int64 { return n.count }
func (n *noopWriter) Close() error { return nil }

func Test_EmptyTable(t *testing.T) {
	schema := allSchemas[0]
	store := newTestStore(t)
	c1 := createChunk(t, "1", labels.FromStrings("foo", "bar"), schema.from, schema.from.Add(1*time.Hour))
	c2 := createChunk(t, "2", labels.FromStrings("foo", "buzz", "bar", "foo"), schema.from, schema.from.Add(1*time.Hour))
	c3 := createChunk(t, "2", labels.FromStrings("foo", "buzz", "bar", "buzz"), schema.from, schema.from.Add(1*time.Hour))

	require.NoError(t, store.Put(context.TODO(), []chunk.Chunk{
		c1, c2, c3,
	}))

	store.Stop()

	tables := store.indexTables()
	require.Len(t, tables, 1)

	// disabled retention should not do anything to the table
	empty, modified, err := markForDelete(context.Background(), 0, tables[0].name, &noopWriter{}, tables[0], NewExpirationChecker(&fakeLimits{}), nil, util_log.Logger)
	require.NoError(t, err)
	require.False(t, empty)
	require.False(t, modified)

	// Set a very low retention to make sure all chunks are marked for deletion which will create an empty table.
	empty, modified, err = markForDelete(context.Background(), 0, tables[0].name, &noopWriter{}, tables[0], NewExpirationChecker(&fakeLimits{perTenant: map[string]retentionLimit{"1": {retentionPeriod: time.Second}, "2": {retentionPeriod: time.Second}}}), nil, util_log.Logger)
	require.NoError(t, err)
	require.True(t, empty)
	require.True(t, modified)

	_, _, err = markForDelete(context.Background(), 0, tables[0].name, &noopWriter{}, newTable("test"), NewExpirationChecker(&fakeLimits{}), nil, util_log.Logger)
	require.Equal(t, err, errNoChunksFound)
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
	chunkEnc := chunkenc.NewMemChunk(chunkenc.ChunkFormatV4, compression.Snappy, chunkenc.UnorderedWithStructuredMetadataHeadBlockFmt, blockSize, targetSize)

	for ts := from; !ts.After(through); ts = ts.Add(1 * time.Minute) {
		dup, err := chunkEnc.Append(&logproto.Entry{
			Timestamp:          ts.Time(),
			Line:               ts.String(),
			StructuredMetadata: logproto.FromLabelsToLabelAdapters(labels.FromStrings("foo", ts.String())),
		})
		require.False(t, dup)
		require.NoError(t, err)
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
	if metricName != "" && ls.Len() == 1 {
		return metricName
	}
	var b strings.Builder
	b.Grow(1000)

	b.WriteString(metricName)
	b.WriteByte('{')
	i := 0
	ls.Range(func(l labels.Label) {
		if l.Name == labels.MetricName {
			return
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
	})
	b.WriteByte('}')

	return b.String()
}

func TestChunkRewriter(t *testing.T) {
	minListMarkDelay = 1 * time.Second
	now := model.Now()
	schema := allSchemas[3] // v12
	todaysTableInterval := ExtractIntervalFromTableName(schema.config.IndexTables.TableFor(now))
	type tableResp struct {
		mustDeleteLines  bool
		mustRewriteChunk bool
	}

	for _, tt := range []struct {
		name                   string
		chunk                  chunk.Chunk
		filterFunc             filter.Func
		expectedRespByTables   map[string]tableResp
		retainedChunkIntervals []model.Interval
	}{
		{
			name:  "no rewrites",
			chunk: createChunk(t, "1", labels.FromStrings("foo", "bar"), todaysTableInterval.Start, todaysTableInterval.Start.Add(time.Hour)),
			filterFunc: func(_ time.Time, _ string, _ labels.Labels) bool {
				return false
			},
			expectedRespByTables: map[string]tableResp{
				schema.config.IndexTables.TableFor(todaysTableInterval.Start): {},
			},
		},
		{
			name:  "no rewrites with chunk spanning multiple tables",
			chunk: createChunk(t, "1", labels.FromStrings("foo", "bar"), todaysTableInterval.End.Add(-48*time.Hour), todaysTableInterval.End),
			filterFunc: func(_ time.Time, _ string, _ labels.Labels) bool {
				return false
			},
			expectedRespByTables: map[string]tableResp{
				schema.config.IndexTables.TableFor(todaysTableInterval.End):                      {},
				schema.config.IndexTables.TableFor(todaysTableInterval.End.Add(-24 * time.Hour)): {},
				schema.config.IndexTables.TableFor(todaysTableInterval.End.Add(-48 * time.Hour)): {},
			},
		},
		{
			name:  "rewrite first half",
			chunk: createChunk(t, "1", labels.FromStrings("foo", "bar"), todaysTableInterval.Start, todaysTableInterval.Start.Add(2*time.Hour)),
			filterFunc: func(ts time.Time, _ string, _ labels.Labels) bool {
				tsUnixNano := ts.UnixNano()
				if todaysTableInterval.Start.UnixNano() <= tsUnixNano && tsUnixNano <= todaysTableInterval.Start.Add(time.Hour).UnixNano() {
					return true
				}

				return false
			},
			expectedRespByTables: map[string]tableResp{
				schema.config.IndexTables.TableFor(todaysTableInterval.Start): {
					mustDeleteLines:  true,
					mustRewriteChunk: true,
				},
			},
			retainedChunkIntervals: []model.Interval{
				{
					Start: todaysTableInterval.Start.Add(time.Hour).Add(time.Minute),
					End:   todaysTableInterval.Start.Add(2 * time.Hour),
				},
			},
		},
		{
			name:  "rewrite first half using structured metadata",
			chunk: createChunk(t, "1", labels.FromStrings("foo", "bar"), todaysTableInterval.Start, todaysTableInterval.Start.Add(2*time.Hour)),
			filterFunc: func(ts time.Time, _ string, structuredMetadata labels.Labels) bool {
				tsUnixNano := ts.UnixNano()
				if structuredMetadata.Get("foo") == model.TimeFromUnixNano(ts.UnixNano()).String() &&
					todaysTableInterval.Start.UnixNano() <= tsUnixNano &&
					tsUnixNano <= todaysTableInterval.Start.Add(time.Hour).UnixNano() {
					return true
				}

				return false
			},
			expectedRespByTables: map[string]tableResp{
				schema.config.IndexTables.TableFor(todaysTableInterval.Start): {
					mustDeleteLines:  true,
					mustRewriteChunk: true,
				},
			},
			retainedChunkIntervals: []model.Interval{
				{
					Start: todaysTableInterval.Start.Add(time.Hour).Add(time.Minute),
					End:   todaysTableInterval.Start.Add(2 * time.Hour),
				},
			},
		},
		{
			name:  "rewrite second half",
			chunk: createChunk(t, "1", labels.FromStrings("foo", "bar"), todaysTableInterval.Start, todaysTableInterval.Start.Add(2*time.Hour)),
			filterFunc: func(ts time.Time, _ string, _ labels.Labels) bool {
				tsUnixNano := ts.UnixNano()
				if todaysTableInterval.Start.Add(time.Hour).UnixNano() <= tsUnixNano && tsUnixNano <= todaysTableInterval.Start.Add(2*time.Hour).UnixNano() {
					return true
				}

				return false
			},
			expectedRespByTables: map[string]tableResp{
				schema.config.IndexTables.TableFor(todaysTableInterval.Start): {
					mustDeleteLines:  true,
					mustRewriteChunk: true,
				},
			},
			retainedChunkIntervals: []model.Interval{
				{
					Start: todaysTableInterval.Start,
					End:   todaysTableInterval.Start.Add(time.Hour).Add(-time.Minute),
				},
			},
		},
		{
			name:  "rewrite multiple intervals",
			chunk: createChunk(t, "1", labels.FromStrings("foo", "bar"), todaysTableInterval.Start, todaysTableInterval.Start.Add(12*time.Hour)),
			filterFunc: func(ts time.Time, _ string, _ labels.Labels) bool {
				tsUnixNano := ts.UnixNano()
				if (todaysTableInterval.Start.UnixNano() <= tsUnixNano && tsUnixNano <= todaysTableInterval.Start.Add(2*time.Hour).UnixNano()) ||
					(todaysTableInterval.Start.Add(5*time.Hour).UnixNano() <= tsUnixNano && tsUnixNano <= todaysTableInterval.Start.Add(9*time.Hour).UnixNano()) ||
					(todaysTableInterval.Start.Add(10*time.Hour).UnixNano() <= tsUnixNano && tsUnixNano <= todaysTableInterval.Start.Add(12*time.Hour).UnixNano()) {
					return true
				}

				return false
			},
			expectedRespByTables: map[string]tableResp{
				schema.config.IndexTables.TableFor(todaysTableInterval.Start): {
					mustDeleteLines:  true,
					mustRewriteChunk: true,
				},
			},
			retainedChunkIntervals: []model.Interval{
				{
					Start: todaysTableInterval.Start.Add(2 * time.Hour).Add(time.Minute),
					End:   todaysTableInterval.Start.Add(5 * time.Hour).Add(-time.Minute),
				},
				{
					Start: todaysTableInterval.Start.Add(9 * time.Hour).Add(time.Minute),
					End:   todaysTableInterval.Start.Add(10 * time.Hour).Add(-time.Minute),
				},
			},
		},
		{
			name:  "rewrite chunk spanning multiple days with multiple intervals - delete partially for each day",
			chunk: createChunk(t, "1", labels.FromStrings("foo", "bar"), todaysTableInterval.End.Add(-72*time.Hour), todaysTableInterval.End),
			filterFunc: func(ts time.Time, _ string, _ labels.Labels) bool {
				tsUnixNano := ts.UnixNano()
				if (todaysTableInterval.End.Add(-71*time.Hour).UnixNano() <= tsUnixNano && tsUnixNano <= todaysTableInterval.End.Add(-47*time.Hour).UnixNano()) ||
					(todaysTableInterval.End.Add(-40*time.Hour).UnixNano() <= tsUnixNano && tsUnixNano <= todaysTableInterval.End.Add(-30*time.Hour).UnixNano()) ||
					(todaysTableInterval.End.Add(-2*time.Hour).UnixNano() <= tsUnixNano && tsUnixNano <= todaysTableInterval.End.UnixNano()) {
					return true
				}

				return false
			},
			expectedRespByTables: map[string]tableResp{
				schema.config.IndexTables.TableFor(todaysTableInterval.End): {
					mustDeleteLines:  true,
					mustRewriteChunk: true,
				},
				schema.config.IndexTables.TableFor(todaysTableInterval.End.Add(-24 * time.Hour)): {
					mustDeleteLines:  true,
					mustRewriteChunk: true,
				},
				schema.config.IndexTables.TableFor(todaysTableInterval.End.Add(-48 * time.Hour)): {
					mustDeleteLines:  true,
					mustRewriteChunk: true,
				},
				schema.config.IndexTables.TableFor(todaysTableInterval.End.Add(-72 * time.Hour)): {
					mustDeleteLines:  true,
					mustRewriteChunk: true,
				},
			},
			retainedChunkIntervals: []model.Interval{
				{
					Start: todaysTableInterval.End.Add(-72 * time.Hour),
					End:   todaysTableInterval.End.Add(-71 * time.Hour).Add(-time.Minute),
				},
				{
					Start: todaysTableInterval.End.Add(-47 * time.Hour).Add(time.Minute),
					End:   todaysTableInterval.End.Add(-40 * time.Hour).Add(-time.Minute),
				},
				{
					Start: todaysTableInterval.End.Add(-30 * time.Hour).Add(time.Minute),
					End:   todaysTableInterval.End.Add(-2 * time.Hour).Add(-time.Minute),
				},
			},
		},
		{
			name:  "rewrite chunk spanning multiple days with multiple intervals - delete just one whole day",
			chunk: createChunk(t, "1", labels.FromStrings("foo", "bar"), todaysTableInterval.End.Add(-72*time.Hour), todaysTableInterval.End),
			filterFunc: func(ts time.Time, _ string, _ labels.Labels) bool {
				tsUnixNano := ts.UnixNano()
				if todaysTableInterval.Start.UnixNano() <= tsUnixNano && tsUnixNano <= todaysTableInterval.End.UnixNano() {
					return true
				}

				return false
			},
			expectedRespByTables: map[string]tableResp{
				schema.config.IndexTables.TableFor(todaysTableInterval.End): {
					mustDeleteLines:  true,
					mustRewriteChunk: false,
				},
				schema.config.IndexTables.TableFor(todaysTableInterval.End.Add(-24 * time.Hour)): {
					mustDeleteLines:  true,
					mustRewriteChunk: true,
				},
				schema.config.IndexTables.TableFor(todaysTableInterval.End.Add(-48 * time.Hour)): {
					mustDeleteLines:  true,
					mustRewriteChunk: true,
				},
				schema.config.IndexTables.TableFor(todaysTableInterval.End.Add(-72 * time.Hour)): {
					mustDeleteLines:  true,
					mustRewriteChunk: true,
				},
			},
			retainedChunkIntervals: []model.Interval{
				{
					Start: todaysTableInterval.End.Add(-72 * time.Hour),
					End:   todaysTableInterval.End.Add(-24 * time.Hour),
				},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			store := newTestStore(t)
			require.NoError(t, store.Put(context.TODO(), []chunk.Chunk{tt.chunk}))
			store.Stop()

			indexTables := store.indexTables()
			require.Len(t, indexTables, len(tt.expectedRespByTables))
			for _, indexTable := range indexTables {
				cr := newChunkRewriter(store.chunkClient, indexTable.name, indexTable)

				wroteChunks, linesDeleted, err := cr.rewriteChunk(context.Background(), []byte(tt.chunk.UserID), Chunk{
					ChunkID: getChunkID(tt.chunk.ChunkRef),
					From:    tt.chunk.From,
					Through: tt.chunk.Through,
				}, ExtractIntervalFromTableName(indexTable.name), tt.filterFunc)
				require.NoError(t, err)
				require.Equal(t, tt.expectedRespByTables[indexTable.name].mustDeleteLines, linesDeleted)
				require.Equal(t, tt.expectedRespByTables[indexTable.name].mustRewriteChunk, wroteChunks)
			}

			// we should have original chunk in the store
			expectedChunks := [][]model.Interval{
				{
					{
						Start: tt.chunk.From,
						End:   tt.chunk.Through,
					},
				},
			}

			chunks := store.GetChunks(tt.chunk.UserID, tt.chunk.From, tt.chunk.Through, tt.chunk.Metric)
			// if we rewrote the chunk, we should have that too
			if len(tt.retainedChunkIntervals) > 0 {
				expectedChunks = append(expectedChunks, tt.retainedChunkIntervals)
				if chunks[1].Checksum == tt.chunk.Checksum {
					chunks[0], chunks[1] = chunks[1], chunks[0]
				}
			}
			require.Len(t, chunks, len(expectedChunks))

			// now verify the contents of the chunks
			for i := 0; i < len(expectedChunks); i++ {
				require.Equal(t, expectedChunks[i][0].Start, chunks[i].From)
				require.Equal(t, expectedChunks[i][len(expectedChunks[i])-1].End, chunks[i].Through)

				lokiChunk := chunks[i].Data.(*chunkenc.Facade).LokiChunk()
				newChunkItr, err := lokiChunk.Iterator(context.Background(), chunks[i].From.Time(), chunks[i].Through.Add(time.Minute).Time(), logproto.FORWARD, log.NewNoopPipeline().ForStream(labels.Labels{}))
				require.NoError(t, err)

				for _, interval := range expectedChunks[i] {
					for curr := interval.Start; curr <= interval.End; curr = curr.Add(time.Minute) {
						expectedStructuredMetadata := labels.FromStrings("foo", curr.String())

						require.True(t, newChunkItr.Next())
						require.Equal(t, logproto.Entry{
							Timestamp:          curr.Time(),
							Line:               curr.String(),
							StructuredMetadata: logproto.FromLabelsToLabelAdapters(expectedStructuredMetadata),
							Parsed:             logproto.EmptyLabelAdapters(),
						}, newChunkItr.At())
						require.Equal(t, expectedStructuredMetadata.String(), newChunkItr.Labels())
					}
				}

				// the iterator should not have any more entries left to iterate
				require.False(t, newChunkItr.Next())
			}
			store.Stop()
		})
	}
}

type seriesCleanedRecorder struct {
	IndexProcessor
	// map of userID -> map of labels hash -> struct{}
	deletedSeries map[string]map[uint64]struct{}
}

func newSeriesCleanRecorder(indexProcessor IndexProcessor) *seriesCleanedRecorder {
	return &seriesCleanedRecorder{
		IndexProcessor: indexProcessor,
		deletedSeries:  map[string]map[uint64]struct{}{},
	}
}

func (s *seriesCleanedRecorder) CleanupSeries(userID []byte, lbls labels.Labels) error {
	s.deletedSeries[string(userID)] = map[uint64]struct{}{lbls.Hash(): {}}
	return s.IndexProcessor.CleanupSeries(userID, lbls)
}

type chunkExpiry struct {
	isExpired  bool
	filterFunc filter.Func
}

type mockExpirationChecker struct {
	ExpirationChecker
	chunksExpiry    map[string]chunkExpiry
	skipSeries      map[string]bool
	delay           time.Duration
	numExpiryChecks int
	timedOut        bool
}

func newMockExpirationChecker(chunksExpiry map[string]chunkExpiry, skipSeries map[string]bool) *mockExpirationChecker {
	return &mockExpirationChecker{chunksExpiry: chunksExpiry, skipSeries: skipSeries}
}

func (m *mockExpirationChecker) Expired(_ []byte, chk Chunk, _ labels.Labels, _ []byte, _ string, _ model.Time) (bool, filter.Func) {
	time.Sleep(m.delay)
	m.numExpiryChecks++

	ce := m.chunksExpiry[chk.ChunkID]
	return ce.isExpired, ce.filterFunc
}

func (m *mockExpirationChecker) DropFromIndex(_ []byte, _ Chunk, _ labels.Labels, _ model.Time, _ model.Time) bool {
	return false
}

func (m *mockExpirationChecker) MarkPhaseTimedOut() {
	m.timedOut = true
}

func (m *mockExpirationChecker) CanSkipSeries(_ []byte, lbls labels.Labels, _ []byte, _ model.Time, _ string, _ model.Time) bool {
	return m.skipSeries[lbls.String()]
}

func (m *mockExpirationChecker) MarkSeriesAsProcessed(_, _ []byte, _ labels.Labels, _ string) error {
	return nil
}

func TestMarkForDelete_SeriesCleanup(t *testing.T) {
	now := model.Now()
	schema := allSchemas[2]
	userID := "1"
	todaysTableInterval := ExtractIntervalFromTableName(schema.config.IndexTables.TableFor(now))

	for _, tc := range []struct {
		name                    string
		chunks                  []chunk.Chunk
		expiry                  []chunkExpiry
		skipSeries              map[string]bool
		expectedDeletedSeries   []map[uint64]struct{}
		expectedEmpty           []bool
		expectedModified        []bool
		numChunksDeleted        []int64
		numExpectedExpiryChecks int
	}{
		{
			name: "no chunk and series deleted",
			chunks: []chunk.Chunk{
				createChunk(t, userID, labels.FromStrings("foo", "1"), todaysTableInterval.Start, todaysTableInterval.Start.Add(30*time.Minute)),
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
			numChunksDeleted: []int64{
				0,
			},
			numExpectedExpiryChecks: 1,
		},
		{
			name: "chunk deleted with filter but no lines matching",
			chunks: []chunk.Chunk{
				createChunk(t, userID, labels.FromStrings("foo", "1"), todaysTableInterval.Start, todaysTableInterval.Start.Add(30*time.Minute)),
			},
			expiry: []chunkExpiry{
				{
					isExpired: true,
					filterFunc: func(_ time.Time, _ string, _ labels.Labels) bool {
						return false
					},
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
			numChunksDeleted: []int64{
				0,
			},
			numExpectedExpiryChecks: 1,
		},
		{
			name: "only one chunk in store which gets deleted",
			chunks: []chunk.Chunk{
				createChunk(t, userID, labels.FromStrings("foo", "1"), todaysTableInterval.Start, todaysTableInterval.Start.Add(30*time.Minute)),
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
			numChunksDeleted: []int64{
				1,
			},
			numExpectedExpiryChecks: 1,
		},
		{
			name: "only one chunk in store which gets partially deleted",
			chunks: []chunk.Chunk{
				createChunk(t, userID, labels.FromStrings("foo", "1"), todaysTableInterval.Start, todaysTableInterval.Start.Add(30*time.Minute)),
			},
			expiry: []chunkExpiry{
				{
					isExpired: true,
					filterFunc: func(ts time.Time, _ string, _ labels.Labels) bool {
						tsUnixNano := ts.UnixNano()
						if todaysTableInterval.Start.UnixNano() <= tsUnixNano && tsUnixNano <= todaysTableInterval.Start.Add(15*time.Minute).UnixNano() {
							return true
						}

						return false
					},
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
			numChunksDeleted: []int64{
				1,
			},
			numExpectedExpiryChecks: 1,
		},
		{
			name: "one of two chunks deleted",
			chunks: []chunk.Chunk{
				createChunk(t, userID, labels.FromStrings("foo", "1"), todaysTableInterval.Start, todaysTableInterval.Start.Add(30*time.Minute)),
				createChunk(t, userID, labels.FromStrings("foo", "2"), todaysTableInterval.Start, todaysTableInterval.Start.Add(30*time.Minute)),
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
				{labels.FromStrings("foo", "2").Hash(): struct{}{}},
			},
			expectedEmpty: []bool{
				false,
			},
			expectedModified: []bool{
				true,
			},
			numChunksDeleted: []int64{
				1,
			},
			numExpectedExpiryChecks: 2,
		},
		{
			name: "one of two series skipped",
			chunks: []chunk.Chunk{
				createChunk(t, userID, labels.FromStrings("foo", "1"), todaysTableInterval.Start, todaysTableInterval.Start.Add(30*time.Minute)),
				createChunk(t, userID, labels.FromStrings("foo", "2"), todaysTableInterval.Start, todaysTableInterval.Start.Add(30*time.Minute)),
			},
			skipSeries: map[string]bool{`{foo="1"}`: true},
			expiry: []chunkExpiry{
				{
					isExpired: false,
				},
				{
					isExpired: true,
				},
			},
			expectedDeletedSeries: []map[uint64]struct{}{
				{labels.FromStrings("foo", "2").Hash(): struct{}{}},
			},
			expectedEmpty: []bool{
				false,
			},
			expectedModified: []bool{
				true,
			},
			numChunksDeleted: []int64{
				1,
			},
			numExpectedExpiryChecks: 1,
		},
		{
			name: "all series skipped",
			chunks: []chunk.Chunk{
				createChunk(t, userID, labels.FromStrings("foo", "1"), todaysTableInterval.Start, todaysTableInterval.Start.Add(30*time.Minute)),
				createChunk(t, userID, labels.FromStrings("foo", "2"), todaysTableInterval.Start, todaysTableInterval.Start.Add(30*time.Minute)),
			},
			skipSeries: map[string]bool{`{foo="1"}`: true, `{foo="2"}`: true},
			expiry: []chunkExpiry{
				{
					isExpired: false,
				},
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
			numChunksDeleted: []int64{
				0,
			},
			numExpectedExpiryChecks: 0,
		},
		{
			name: "one of two chunks partially deleted",
			chunks: []chunk.Chunk{
				createChunk(t, userID, labels.FromStrings("foo", "1"), todaysTableInterval.Start, todaysTableInterval.Start.Add(30*time.Minute)),
				createChunk(t, userID, labels.FromStrings("foo", "2"), todaysTableInterval.Start, todaysTableInterval.Start.Add(30*time.Minute)),
			},
			expiry: []chunkExpiry{
				{
					isExpired: false,
				},
				{
					isExpired: true,
					filterFunc: func(ts time.Time, _ string, _ labels.Labels) bool {
						tsUnixNano := ts.UnixNano()
						if todaysTableInterval.Start.UnixNano() <= tsUnixNano && tsUnixNano <= todaysTableInterval.Start.Add(15*time.Minute).UnixNano() {
							return true
						}

						return false
					},
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
			numChunksDeleted: []int64{
				1,
			},
			numExpectedExpiryChecks: 2,
		},
		{
			name: "one big chunk partially deleted for yesterdays table without rewrite",
			chunks: []chunk.Chunk{
				createChunk(t, userID, labels.FromStrings("foo", "1"), todaysTableInterval.Start.Add(-time.Hour), now),
			},
			expiry: []chunkExpiry{
				{
					isExpired: true,
					filterFunc: func(ts time.Time, _ string, _ labels.Labels) bool {
						return ts.UnixNano() < todaysTableInterval.Start.UnixNano()
					},
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
			numChunksDeleted: []int64{
				1, 0,
			},
			numExpectedExpiryChecks: 2,
		},
		{
			name: "one big chunk partially deleted for yesterdays table with rewrite",
			chunks: []chunk.Chunk{
				createChunk(t, userID, labels.FromStrings("foo", "1"), todaysTableInterval.Start.Add(-time.Hour), now),
			},
			expiry: []chunkExpiry{
				{
					isExpired: true,
					filterFunc: func(ts time.Time, _ string, _ labels.Labels) bool {
						return ts.UnixNano() < todaysTableInterval.Start.Add(-30*time.Minute).UnixNano()
					},
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
			numChunksDeleted: []int64{
				1, 0,
			},
			numExpectedExpiryChecks: 2,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newTestStore(t)

			require.NoError(t, store.Put(context.TODO(), tc.chunks))
			chunksExpiry := map[string]chunkExpiry{}
			for i, chunk := range tc.chunks {
				chunksExpiry[getChunkID(chunk.ChunkRef)] = tc.expiry[i]
			}

			expirationChecker := newMockExpirationChecker(chunksExpiry, tc.skipSeries)

			store.Stop()

			tables := store.indexTables()
			require.Len(t, tables, len(tc.expectedDeletedSeries))

			for i, table := range tables {
				seriesCleanRecorder := newSeriesCleanRecorder(table)

				cr := newChunkRewriter(store.chunkClient, table.name, table)
				marker := &noopWriter{}
				empty, isModified, err := markForDelete(context.Background(), 0, table.name, marker, seriesCleanRecorder, expirationChecker, cr, util_log.Logger)
				require.NoError(t, err)
				require.Equal(t, tc.expectedEmpty[i], empty)
				require.Equal(t, tc.expectedModified[i], isModified)
				require.Equal(t, tc.numChunksDeleted[i], marker.count)

				require.EqualValues(t, tc.expectedDeletedSeries[i], seriesCleanRecorder.deletedSeries[userID])
			}

			require.Equal(t, tc.numExpectedExpiryChecks, expirationChecker.numExpiryChecks)
		})
	}
}

func TestDeleteTimeout(t *testing.T) {
	chunks := []chunk.Chunk{
		createChunk(t, "user", labels.FromStrings("foo", "1"), model.Now(), model.Now().Add(270*time.Hour)),
		createChunk(t, "user", labels.FromStrings("foo", "2"), model.Now(), model.Now().Add(270*time.Hour)),
	}

	for _, tc := range []struct {
		timeout  time.Duration
		calls    int
		timedOut bool
	}{
		{timeout: 2 * time.Millisecond, calls: 1, timedOut: true},
		{timeout: 0, calls: 2, timedOut: false},
	} {
		store := newTestStore(t)
		require.NoError(t, store.Put(context.TODO(), chunks))
		store.Stop()

		expirationChecker := newMockExpirationChecker(map[string]chunkExpiry{}, nil)
		expirationChecker.delay = 10 * time.Millisecond

		table := store.indexTables()[0]
		empty, isModified, err := markForDelete(
			context.Background(),
			tc.timeout,
			table.name,
			&noopWriter{},
			newSeriesCleanRecorder(table),
			expirationChecker,
			newChunkRewriter(store.chunkClient, table.name, table),
			util_log.Logger,
		)

		require.NoError(t, err)
		require.False(t, empty)
		require.False(t, isModified)
		require.Equal(t, tc.calls, expirationChecker.numExpiryChecks)
		require.Equal(t, tc.timedOut, expirationChecker.timedOut)
	}
}

func TestMarkForDelete_DropChunkFromIndex(t *testing.T) {
	schema := allSchemas[2]
	store := newTestStore(t)
	now := model.Now()
	todaysTableInterval := ExtractIntervalFromTableName(schema.config.IndexTables.TableFor(now))
	retentionPeriod := now.Sub(todaysTableInterval.Start) / 2

	// chunks in retention
	c1 := createChunk(t, "1", labels.FromStrings("foo", "1"), todaysTableInterval.Start, now)
	c2 := createChunk(t, "1", labels.FromStrings("foo", "2"), todaysTableInterval.Start.Add(-7*24*time.Hour), now)

	// chunks out of retention
	c3 := createChunk(t, "1", labels.FromStrings("foo", "1"), todaysTableInterval.Start, now.Add(-retentionPeriod))
	c4 := createChunk(t, "1", labels.FromStrings("foo", "3"), todaysTableInterval.Start.Add(-12*time.Hour), todaysTableInterval.Start.Add(-10*time.Hour))
	c5 := createChunk(t, "1", labels.FromStrings("foo", "4"), todaysTableInterval.Start, now.Add(-retentionPeriod))

	require.NoError(t, store.Put(context.TODO(), []chunk.Chunk{
		c1, c2, c3, c4, c5,
	}))

	store.Stop()

	tables := store.indexTables()
	require.Len(t, tables, 8)

	for i, table := range tables {
		empty, _, err := markForDelete(context.Background(), 0, table.name, &noopWriter{}, table,
			NewExpirationChecker(fakeLimits{perTenant: map[string]retentionLimit{"1": {retentionPeriod: retentionPeriod}}}), nil, util_log.Logger)
		require.NoError(t, err)
		if i == 7 {
			require.False(t, empty)
		} else {
			require.True(t, empty, "table %s must be empty", table.name)
		}
	}

	// verify the chunks which were not supposed to be deleted are still there
	require.True(t, store.HasChunk(c1))
	require.True(t, store.HasChunk(c2))

	// verify the chunks which were supposed to be deleted are gone
	require.False(t, store.HasChunk(c3))
	require.False(t, store.HasChunk(c4))
	require.False(t, store.HasChunk(c5))
}

func TestMigrateMarkers(t *testing.T) {
	t.Run("nothing to migrate", func(t *testing.T) {
		workDir := t.TempDir()
		dst := path.Join(workDir, "store-1_2023-10-19")
		require.NoError(t, CopyMarkers(workDir, dst))
		require.NoDirExists(t, path.Join(workDir, dst, MarkersFolder))
	})

	t.Run("migrate markers dir", func(t *testing.T) {
		workDir := t.TempDir()
		dst := path.Join(workDir, "store-1_2023-10-19")
		require.NoError(t, os.Mkdir(path.Join(workDir, MarkersFolder), 0755))

		markers := []string{"foo", "bar", "buzz"}
		for _, marker := range markers {
			err := os.WriteFile(path.Join(workDir, MarkersFolder, marker), []byte(marker), 0o666)
			require.NoError(t, err)
		}

		require.NoError(t, CopyMarkers(workDir, dst))
		targetDir := path.Join(dst, MarkersFolder)
		require.DirExists(t, targetDir)
		for _, marker := range markers {
			require.FileExists(t, path.Join(targetDir, marker))
			b, err := os.ReadFile(path.Join(targetDir, marker))
			require.NoError(t, err)
			require.Equal(t, marker, string(b))
		}
	})

	t.Run("file named markers should not be migrated", func(t *testing.T) {
		workDir := t.TempDir()
		dst := path.Join(workDir, "store-1_2023-10-19")
		f, err := os.Create(path.Join(workDir, MarkersFolder))
		require.NoError(t, err)
		defer f.Close()

		require.NoError(t, CopyMarkers(workDir, dst))
		require.NoDirExists(t, path.Join(dst, MarkersFolder))
	})
}

func TestDuplicateSeriesDetection(t *testing.T) {
	chk := createChunk(t, "1", labels.FromStrings("foo", "1"), model.Now().Add(-time.Hour), model.Now())

	expirationChecker := newMockExpirationChecker(map[string]chunkExpiry{
		getChunkID(chk.ChunkRef): {isExpired: false},
	}, nil)

	// Create a custom index processor that will loop on same series
	loopIndexProcessor := &loopIndexProcessor{
		series: series{
			seriesID: []byte(chk.Metric.String()),
			userID:   []byte(chk.UserID),
			labels:   chk.Metric,
			chunks: []Chunk{
				{
					ChunkID: getChunkID(chk.ChunkRef),
					From:    chk.From,
					Through: chk.Through,
				},
			},
		},
	}

	// Attempt to mark for deletion - this should fail due to duplicate series
	empty, isModified, err := markForDelete(
		context.Background(),
		0,
		"test_table",
		&noopWriter{},
		loopIndexProcessor,
		expirationChecker,
		nil,
		util_log.Logger,
	)

	require.Error(t, err)
	require.Contains(t, err.Error(), "series should not be repeated")
	require.False(t, empty)
	require.False(t, isModified)
}

// loopIndexProcessor implements IndexProcessor which loops on same series for iteration
type loopIndexProcessor struct {
	IndexProcessor
	series series
}

func (p *loopIndexProcessor) ForEachSeries(_ context.Context, callback SeriesCallback) error {
	for {
		if err := callback(&p.series); err != nil {
			return err
		}
	}
}
