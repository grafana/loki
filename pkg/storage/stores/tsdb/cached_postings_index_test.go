package tsdb

import (
	"context"
	"math/rand"
	"sort"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/stores/index/stats"
	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
	"github.com/grafana/loki/pkg/util/flagext"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
)

func runTSDBIndexCache(t testing.TB) {
	cfg := cache.LRUCacheConfig{MaxSizeBytes: flagext.ByteSize(100000), MaxItems: 5000, MaxItemSizeBytes: flagext.ByteSize(10000), Enabled: true}
	c, e := cache.NewLRUCache("test-cache", cfg, nil, log.NewNopLogger(), "test")
	require.NoError(t, e)
	sharedCacheClient = c
}

func TestSingleIdxCached(t *testing.T) {
	runTSDBIndexCache(t)
	defer sharedCacheClient.Stop()
	cases := []LoadableSeries{
		{
			Labels: mustParseLabels(`{foo="bar"}`),
			Chunks: []index.ChunkMeta{
				{
					MinTime:  0,
					MaxTime:  3,
					Checksum: 0,
				},
				{
					MinTime:  1,
					MaxTime:  4,
					Checksum: 1,
				},
				{
					MinTime:  2,
					MaxTime:  5,
					Checksum: 2,
				},
			},
		},
		{
			Labels: mustParseLabels(`{foo="bar", bazz="buzz"}`),
			Chunks: []index.ChunkMeta{
				{
					MinTime:  1,
					MaxTime:  10,
					Checksum: 3,
				},
			},
		},
		{
			Labels: mustParseLabels(`{foo="bard", bazz="bozz", bonk="borb"}`),
			Chunks: []index.ChunkMeta{
				{
					MinTime:  1,
					MaxTime:  7,
					Checksum: 4,
				},
			},
		},
	}

	for _, variant := range []struct {
		desc string
		fn   func() Index
	}{
		{
			desc: "file",
			fn: func() Index {
				return BuildIndex(t, t.TempDir(), cases, IndexOpts{UsePostingsCache: true})
			},
		},
		{
			desc: "head",
			fn: func() Index {
				head := NewHead("fake", NewMetrics(nil), log.NewNopLogger())
				for _, x := range cases {
					_, _ = head.Append(x.Labels, x.Labels.Hash(), x.Chunks)
				}
				reader := head.Index()
				return NewTSDBIndex(reader, NewPostingsReader(reader))
			},
		},
	} {
		t.Run(variant.desc, func(t *testing.T) {
			idx := variant.fn()
			t.Run("GetChunkRefs", func(t *testing.T) {
				var err error
				refs := make([]ChunkRef, 0, 8)
				refs, err = idx.GetChunkRefs(context.Background(), "fake", 1, 5, refs, nil, labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"))
				require.Nil(t, err)

				expected := []ChunkRef{
					{
						User:        "fake",
						Fingerprint: model.Fingerprint(mustParseLabels(`{foo="bar"}`).Hash()),
						Start:       0,
						End:         3,
						Checksum:    0,
					},
					{
						User:        "fake",
						Fingerprint: model.Fingerprint(mustParseLabels(`{foo="bar"}`).Hash()),
						Start:       1,
						End:         4,
						Checksum:    1,
					},
					{
						User:        "fake",
						Fingerprint: model.Fingerprint(mustParseLabels(`{foo="bar"}`).Hash()),
						Start:       2,
						End:         5,
						Checksum:    2,
					},
					{
						User:        "fake",
						Fingerprint: model.Fingerprint(mustParseLabels(`{foo="bar", bazz="buzz"}`).Hash()),
						Start:       1,
						End:         10,
						Checksum:    3,
					},
				}
				require.Equal(t, expected, refs)
			})

			t.Run("GetChunkRefsSharded", func(t *testing.T) {
				shard := index.ShardAnnotation{
					Shard: 1,
					Of:    2,
				}
				var err error
				refs := make([]ChunkRef, 0, 8)
				refs, err = idx.GetChunkRefs(context.Background(), "fake", 1, 5, refs, &shard, labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"))

				require.Nil(t, err)

				require.Equal(t, []ChunkRef{{
					User:        "fake",
					Fingerprint: model.Fingerprint(mustParseLabels(`{foo="bar", bazz="buzz"}`).Hash()),
					Start:       1,
					End:         10,
					Checksum:    3,
				}}, refs)

			})

			t.Run("Series", func(t *testing.T) {
				xs, err := idx.Series(context.Background(), "fake", 8, 9, nil, nil, labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"))
				require.Nil(t, err)

				expected := []Series{
					{
						Labels:      mustParseLabels(`{foo="bar", bazz="buzz"}`),
						Fingerprint: model.Fingerprint(mustParseLabels(`{foo="bar", bazz="buzz"}`).Hash()),
					},
				}
				require.Equal(t, expected, xs)
			})

			t.Run("SeriesSharded", func(t *testing.T) {
				shard := index.ShardAnnotation{
					Shard: 0,
					Of:    2,
				}

				xs, err := idx.Series(context.Background(), "fake", 0, 10, nil, &shard, labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"))
				require.Nil(t, err)

				expected := []Series{
					{
						Labels:      mustParseLabels(`{foo="bar"}`),
						Fingerprint: model.Fingerprint(mustParseLabels(`{foo="bar"}`).Hash()),
					},
				}
				require.Equal(t, expected, xs)
			})

			t.Run("LabelNames", func(t *testing.T) {
				// request data at the end of the tsdb range, but it should return all labels present
				ls, err := idx.LabelNames(context.Background(), "fake", 9, 10)
				require.Nil(t, err)
				sort.Strings(ls)
				require.Equal(t, []string{"bazz", "bonk", "foo"}, ls)
			})

			t.Run("LabelNamesWithMatchers", func(t *testing.T) {
				// request data at the end of the tsdb range, but it should return all labels present
				ls, err := idx.LabelNames(context.Background(), "fake", 9, 10, labels.MustNewMatcher(labels.MatchEqual, "bazz", "buzz"))
				require.Nil(t, err)
				sort.Strings(ls)
				require.Equal(t, []string{"bazz", "foo"}, ls)
			})

			t.Run("LabelValues", func(t *testing.T) {
				vs, err := idx.LabelValues(context.Background(), "fake", 9, 10, "foo")
				require.Nil(t, err)
				sort.Strings(vs)
				require.Equal(t, []string{"bar", "bard"}, vs)
			})

			t.Run("LabelValuesWithMatchers", func(t *testing.T) {
				vs, err := idx.LabelValues(context.Background(), "fake", 9, 10, "foo", labels.MustNewMatcher(labels.MatchEqual, "bazz", "buzz"))
				require.Nil(t, err)
				require.Equal(t, []string{"bar"}, vs)
			})

		})
	}

}

func BenchmarkCacheableTSDBIndex_GetChunkRefs(b *testing.B) {
	runTSDBIndexCache(b)
	defer sharedCacheClient.Stop()

	now := model.Now()
	queryFrom, queryThrough := now.Add(3*time.Hour).Add(time.Millisecond), now.Add(5*time.Hour).Add(-time.Millisecond)
	queryBounds := newBounds(queryFrom, queryThrough)
	numChunksToMatch := 0

	var chunkMetas []index.ChunkMeta
	// build a chunk for every second with randomized chunk length
	for from, through := now, now.Add(24*time.Hour); from <= through; from = from.Add(time.Second) {
		// randomize chunk length between 1-120 mins
		chunkLenMin := rand.Intn(120)
		if chunkLenMin == 0 {
			chunkLenMin = 1
		}
		chunkMeta := index.ChunkMeta{
			MinTime:  int64(from),
			MaxTime:  int64(from.Add(time.Duration(chunkLenMin) * time.Minute)),
			Checksum: uint32(from),
			Entries:  1,
		}
		chunkMetas = append(chunkMetas, chunkMeta)
		if Overlap(chunkMeta, queryBounds) {
			numChunksToMatch++
		}
	}

	tempDir := b.TempDir()
	tsdbIndex := BuildIndex(b, tempDir, []LoadableSeries{
		{
			Labels: mustParseLabels(`{foo="bar", fizz="buzz"}`),
			Chunks: chunkMetas,
		},
		{
			Labels: mustParseLabels(`{foo="bar", ping="pong"}`),
			Chunks: chunkMetas,
		},
		{
			Labels: mustParseLabels(`{foo1="bar1", ping="pong"}`),
			Chunks: chunkMetas,
		},
	}, IndexOpts{UsePostingsCache: true})

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		chkRefs := ChunkRefsPool.Get()
		chkRefs, _ = tsdbIndex.GetChunkRefs(context.Background(), "fake", queryFrom, queryThrough, chkRefs, nil, labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"))
		ChunkRefsPool.Put(chkRefs)
	}
}

func TestCacheableTSDBIndex_Stats(t *testing.T) {
	runTSDBIndexCache(t)
	defer sharedCacheClient.Stop()
	series := []LoadableSeries{
		{
			Labels: mustParseLabels(`{foo="bar", fizz="buzz"}`),
			Chunks: []index.ChunkMeta{
				{
					MinTime:  0,
					MaxTime:  10,
					Checksum: 1,
					Entries:  10,
					KB:       10,
				},
				{
					MinTime:  10,
					MaxTime:  20,
					Checksum: 2,
					Entries:  20,
					KB:       20,
				},
			},
		},
		{
			Labels: mustParseLabels(`{foo="bar", ping="pong"}`),
			Chunks: []index.ChunkMeta{
				{
					MinTime:  0,
					MaxTime:  10,
					Checksum: 3,
					Entries:  30,
					KB:       30,
				},
				{
					MinTime:  10,
					MaxTime:  20,
					Checksum: 4,
					Entries:  40,
					KB:       40,
				},
			},
		},
	}

	// Create the TSDB index
	tempDir := t.TempDir()

	// Create the test cases
	testCases := []struct {
		name        string
		from        model.Time
		through     model.Time
		expected    stats.Stats
		expectedErr error
	}{
		{
			name:    "from at the beginning of one chunk and through at the end of another chunk",
			from:    0,
			through: 20,
			expected: stats.Stats{
				Streams: 2,
				Chunks:  4,
				Bytes:   (10 + 20 + 30 + 40) * 1024,
				Entries: 10 + 20 + 30 + 40,
			},
		},
		{
			name:    "from inside one chunk and through inside another chunk",
			from:    5,
			through: 15,
			expected: stats.Stats{
				Streams: 2,
				Chunks:  4,
				Bytes:   (10*0.5 + 20*0.5 + 30*0.5 + 40*0.5) * 1024,
				Entries: 10*0.5 + 20*0.5 + 30*0.5 + 40*0.5,
			},
		},
		{
			name:    "from inside one chunk and through at the end of another chunk",
			from:    5,
			through: 20,
			expected: stats.Stats{
				Streams: 2,
				Chunks:  4,
				Bytes:   (10*0.5 + 20 + 30*0.5 + 40) * 1024,
				Entries: 10*0.5 + 20 + 30*0.5 + 40,
			},
		},
		{
			name:    "from at the beginning of one chunk and through inside another chunk",
			from:    0,
			through: 15,
			expected: stats.Stats{
				Streams: 2,
				Chunks:  4,
				Bytes:   (10 + 20*0.5 + 30 + 40*0.5) * 1024,
				Entries: 10 + 20*0.5 + 30 + 40*0.5,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tsdbIndex := BuildIndex(t, tempDir, series, IndexOpts{UsePostingsCache: true})
			acc := &stats.Stats{}
			err := tsdbIndex.Stats(context.Background(), "fake", tc.from, tc.through, acc, nil, nil, labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"))
			require.Equal(t, tc.expectedErr, err)
			require.Equal(t, tc.expected, *acc)
		})
	}
}

func BenchmarkSeriesRepetitive(b *testing.B) {
	runTSDBIndexCache(b)
	defer sharedCacheClient.Stop()
	series := []LoadableSeries{
		{
			Labels: mustParseLabels(`{foo="bar", fizz="buzz"}`),
			Chunks: []index.ChunkMeta{
				{
					MinTime:  0,
					MaxTime:  10,
					Checksum: 1,
					Entries:  10,
					KB:       10,
				},
				{
					MinTime:  10,
					MaxTime:  20,
					Checksum: 2,
					Entries:  20,
					KB:       20,
				},
			},
		},
		{
			Labels: mustParseLabels(`{foo="bar", ping="pong"}`),
			Chunks: []index.ChunkMeta{
				{
					MinTime:  0,
					MaxTime:  10,
					Checksum: 3,
					Entries:  30,
					KB:       30,
				},
				{
					MinTime:  10,
					MaxTime:  20,
					Checksum: 4,
					Entries:  40,
					KB:       40,
				},
			},
		},
	}
	tempDir := b.TempDir()
	tsdbIndex := BuildIndex(b, tempDir, series, IndexOpts{UsePostingsCache: true})
	acc := &stats.Stats{}

	for i := 0; i < b.N; i++ {
		tsdbIndex.Stats(context.Background(), "fake", 5, 15, acc, nil, nil, labels.MustNewMatcher(labels.MatchEqual, "foo", "bar")) //nolint:errcheck
	}
}
