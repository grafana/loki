package retention

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/objectclient"
	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/validation"
)

type mockChunkClient struct {
	mtx           sync.Mutex
	deletedChunks []string
}

func (m *mockChunkClient) DeleteChunk(_ context.Context, _, chunkID string) error {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	m.deletedChunks = append(m.deletedChunks, string([]byte(chunkID))) // forces a copy, because this string is only valid within the delete fn.
	return nil
}

func (m *mockChunkClient) getDeletedChunkIds() []string {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	return m.deletedChunks
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
				perTenant: map[string]time.Duration{
					"1": 1000 * time.Hour,
					"2": 1000 * time.Hour,
				},
				perStream: map[string][]validation.StreamRetention{},
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
				perTenant: map[string]time.Duration{
					"1": 10 * time.Hour,
					"2": 1000 * time.Hour,
				},
				perStream: map[string][]validation.StreamRetention{},
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
				perTenant: map[string]time.Duration{
					"1": 10 * time.Hour,
					"2": 1000 * time.Hour,
				},
				perStream: map[string][]validation.StreamRetention{
					"1": {
						{Period: model.Duration(5 * time.Hour), Matchers: []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "foo", "buzz")}},
					},
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
			chunkClient := &mockChunkClient{}
			sweep, err := NewSweeper(workDir, chunkClient, 10, 0, nil)
			require.NoError(t, err)
			sweep.Start()
			defer sweep.Stop()

			marker, err := NewMarker(workDir, store.schemaCfg, expiration, nil, prometheus.NewRegistry())
			require.NoError(t, err)
			for _, table := range store.indexTables() {
				_, _, err := marker.MarkForDelete(context.Background(), table.name, table.DB)
				require.Nil(t, err)
				table.Close()

			}

			// assert using the store again.
			store.open()

			for i, e := range tt.alive {
				require.Equal(t, e, store.HasChunk(tt.chunks[i]), "chunk %d should be %t", i, e)
				if !e {
					expectDeleted = append(expectDeleted, tt.chunks[i].ExternalKey())
				}
			}
			store.Stop()
			if len(expectDeleted) != 0 {
				require.Eventually(t, func() bool {
					return assert.ObjectsAreEqual(expectDeleted, chunkClient.getDeletedChunkIds())
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

func (noopCleaner) Cleanup(seriesID []byte, userID []byte) error { return nil }

func Test_EmptyTable(t *testing.T) {
	schema := allSchemas[0]
	store := newTestStore(t)
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
		it, err := newChunkIndexIterator(tx.Bucket(bucketName), schema.config)
		require.NoError(t, err)
		empty, err := markforDelete(context.Background(), noopWriter{}, it, noopCleaner{},
			NewExpirationChecker(&fakeLimits{perTenant: map[string]time.Duration{"1": 0, "2": 0}}), nil)
		require.NoError(t, err)
		require.True(t, empty)
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
	fp := client.Fingerprint(lbs)
	chunkEnc := chunkenc.NewMemChunk(chunkenc.EncSnappy, blockSize, targetSize)

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
		name             string
		chunk            chunk.Chunk
		rewriteIntervals []model.Interval
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
			rewriteIntervals: []model.Interval{
				{
					Start: now.Add(-2 * time.Hour),
					End:   now.Add(-1 * time.Hour),
				},
			},
		},
		{
			name:  "rewrite second half",
			chunk: createChunk(t, "1", labels.Labels{labels.Label{Name: "foo", Value: "bar"}}, now.Add(-2*time.Hour), now),
			rewriteIntervals: []model.Interval{
				{
					Start: now.Add(-time.Hour),
					End:   now,
				},
			},
		},
		{
			name:  "rewrite multiple intervals",
			chunk: createChunk(t, "1", labels.Labels{labels.Label{Name: "foo", Value: "bar"}}, now.Add(-12*time.Hour), now),
			rewriteIntervals: []model.Interval{
				{
					Start: now.Add(-12 * time.Hour),
					End:   now.Add(-10 * time.Hour),
				},
				{
					Start: now.Add(-9 * time.Hour),
					End:   now.Add(-5 * time.Hour),
				},
				{
					Start: now.Add(-2 * time.Hour),
					End:   now,
				},
			},
		},
		{
			name:  "rewrite chunk spanning multiple days with multiple intervals",
			chunk: createChunk(t, "1", labels.Labels{labels.Label{Name: "foo", Value: "bar"}}, now.Add(-72*time.Hour), now),
			rewriteIntervals: []model.Interval{
				{
					Start: now.Add(-71 * time.Hour),
					End:   now.Add(-47 * time.Hour),
				},
				{
					Start: now.Add(-40 * time.Hour),
					End:   now.Add(-30 * time.Hour),
				},
				{
					Start: now.Add(-2 * time.Hour),
					End:   now,
				},
			},
		},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			store := newTestStore(t)
			require.NoError(t, store.Put(context.TODO(), []chunk.Chunk{tt.chunk}))
			store.Stop()

			chunkClient := objectclient.NewClient(newTestObjectClient(store.chunkDir), objectclient.Base64Encoder)
			for _, indexTable := range store.indexTables() {
				err := indexTable.DB.Update(func(tx *bbolt.Tx) error {
					bucket := tx.Bucket(bucketName)
					if bucket == nil {
						return nil
					}

					cr, err := newChunkRewriter(chunkClient, store.schemaCfg.SchemaConfig.Configs[0], indexTable.name, bucket)
					require.NoError(t, err)

					wroteChunks, err := cr.rewriteChunk(context.Background(), entryFromChunk(tt.chunk), tt.rewriteIntervals)
					require.NoError(t, err)
					if len(tt.rewriteIntervals) == 0 {
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
			require.Len(t, chunks, len(tt.rewriteIntervals)+1)
			for _, interval := range tt.rewriteIntervals {
				expectedChk := createChunk(t, tt.chunk.UserID, labels.Labels{labels.Label{Name: "foo", Value: "bar"}}, interval.Start, interval.End)
				for i, chk := range chunks {
					if chk.ExternalKey() == expectedChk.ExternalKey() {
						chunks = append(chunks[:i], chunks[i+1:]...)
						break
					}
				}
			}

			// the source chunk should still be there in the store
			require.Len(t, chunks, 1)
			require.Equal(t, tt.chunk.ExternalKey(), chunks[0].ExternalKey())
			store.Stop()
		})
	}
}
