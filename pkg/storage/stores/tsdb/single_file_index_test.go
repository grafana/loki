package tsdb

import (
	"context"
	"math/rand"
	"sort"
	"testing"
	"time"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/stores/index/labelvolume"
	"github.com/grafana/loki/pkg/storage/stores/index/stats"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
)

func TestSingleIdx(t *testing.T) {
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
				return BuildIndex(t, t.TempDir(), cases)
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
				return NewTSDBIndex(reader)
			},
		},
	} {
		t.Run(variant.desc, func(t *testing.T) {
			idx := variant.fn()
			t.Run("GetChunkRefs", func(t *testing.T) {
				refs, err := idx.GetChunkRefs(context.Background(), "fake", 1, 5, nil, nil, labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"))
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
				shardedRefs, err := idx.GetChunkRefs(context.Background(), "fake", 1, 5, nil, &shard, labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"))

				require.Nil(t, err)

				require.Equal(t, []ChunkRef{{
					User:        "fake",
					Fingerprint: model.Fingerprint(mustParseLabels(`{foo="bar", bazz="buzz"}`).Hash()),
					Start:       1,
					End:         10,
					Checksum:    3,
				}}, shardedRefs)

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

func BenchmarkTSDBIndex_GetChunkRefs(b *testing.B) {
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
	})

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		chkRefs, err := tsdbIndex.GetChunkRefs(context.Background(), "fake", queryFrom, queryThrough, nil, nil, labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"))
		require.NoError(b, err)
		require.Len(b, chkRefs, numChunksToMatch*2)
	}
}

func TestTSDBIndex_Stats(t *testing.T) {
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
	tsdbIndex := BuildIndex(t, tempDir, series)

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
			acc := &stats.Stats{}
			err := tsdbIndex.Stats(context.Background(), "fake", tc.from, tc.through, acc, nil, nil, labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"))
			require.Equal(t, tc.expectedErr, err)
			require.Equal(t, tc.expected, *acc)
		})
	}
}

func TestTSDBIndex_LabelVolume(t *testing.T) {
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
	tsdbIndex := BuildIndex(t, tempDir, series)

	t.Run("it matches all the series", func(t *testing.T) {
		matcher := labels.MustNewMatcher(labels.MatchEqual, "", "")
		acc := labelvolume.NewAccumulator(10)
		err := tsdbIndex.LabelVolume(context.Background(), "fake", 0, 20, acc, nil, nil, matcher)
		require.NoError(t, err)
		require.Equal(t, &logproto.LabelVolumeResponse{
			Volumes: []logproto.LabelVolume{
				{Name: "foo", Value: "bar", Volume: (10 + 20 + 30 + 40) * 1024, Timestamp: 20 * 1e6},
				{Name: "ping", Value: "pong", Volume: (30 + 40) * 1024, Timestamp: 20 * 1e6},
				{Name: "fizz", Value: "buzz", Volume: (10 + 20) * 1024, Timestamp: 20 * 1e6},
			},
			Limit: 10}, acc.Volumes())
	})

	t.Run("it matches none of the series", func(t *testing.T) {
		matcher := labels.MustNewMatcher(labels.MatchEqual, "foo", "baz")
		acc := labelvolume.NewAccumulator(10)
		err := tsdbIndex.LabelVolume(context.Background(), "fake", 0, 20, acc, nil, nil, matcher)
		require.NoError(t, err)
		require.Equal(t, &logproto.LabelVolumeResponse{
			Volumes: []logproto.LabelVolume{},
			Limit:   10}, acc.Volumes())
	})

	t.Run("it matches a subset", func(t *testing.T) {
		matcher := labels.MustNewMatcher(labels.MatchEqual, "ping", "pong")
		acc := labelvolume.NewAccumulator(10)
		err := tsdbIndex.LabelVolume(context.Background(), "fake", 0, 20, acc, nil, nil, matcher)
		require.NoError(t, err)
		require.Equal(t, &logproto.LabelVolumeResponse{
			Volumes: []logproto.LabelVolume{
				{Name: "foo", Value: "bar", Volume: (30 + 40) * 1024, Timestamp: 20 * 1e6},
				{Name: "ping", Value: "pong", Volume: (30 + 40) * 1024, Timestamp: 20 * 1e6},
			},
			Limit: 10}, acc.Volumes())
	})

	t.Run("it can filter chunks", func(t *testing.T) {
		tsdbIndex.SetChunkFilterer(&filterAll{})
		defer tsdbIndex.SetChunkFilterer(nil)

		matcher := labels.MustNewMatcher(labels.MatchEqual, "", "")
		acc := labelvolume.NewAccumulator(10)
		err := tsdbIndex.LabelVolume(context.Background(), "fake", 0, 20, acc, nil, nil, matcher)

		require.NoError(t, err)
		require.Equal(t, &logproto.LabelVolumeResponse{
			Volumes: []logproto.LabelVolume{},
			Limit:   10}, acc.Volumes())
	})
}

type filterAll struct{}

func (f *filterAll) ForRequest(_ context.Context) chunk.Filterer {
	return &filterAllFilterer{}
}

type filterAllFilterer struct{}

func (f *filterAllFilterer) ShouldFilter(_ labels.Labels) bool {
	return true
}
