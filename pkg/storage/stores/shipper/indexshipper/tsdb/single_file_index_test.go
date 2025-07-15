package tsdb

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/seriesvolume"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/stats"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
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

func TestTSDBIndex_Volume(t *testing.T) {
	now := time.Now()
	t1 := now.Add(-time.Hour)
	t2 := now.Add(-time.Minute)

	series := []LoadableSeries{
		{
			Labels: mustParseLabels(`{foo="bar", fizz="buzz", us="them", __loki_tenant__="fake"}`),

			Chunks: []index.ChunkMeta{
				{
					MinTime:  t1.UnixMilli(),
					MaxTime:  t1.Add(30 * time.Minute).UnixMilli(),
					Checksum: 1,
					Entries:  10,
					KB:       10,
				},
				{
					MinTime:  t1.Add(30 * time.Minute).UnixMilli(),
					MaxTime:  t2.UnixMilli(),
					Checksum: 2,
					Entries:  20,
					KB:       20,
				},
			},
		},
		{
			Labels: mustParseLabels(`{foo="bar", fizz="fizz", in="out", __loki_tenant__="fake"}`),
			Chunks: []index.ChunkMeta{
				{
					MinTime:  t1.UnixMilli(),
					MaxTime:  t1.Add(30 * time.Minute).UnixMilli(),
					Checksum: 3,
					Entries:  30,
					KB:       30,
				},
				{
					MinTime:  t1.Add(30 * time.Minute).UnixMilli(),
					MaxTime:  t2.UnixMilli(),
					Checksum: 4,
					Entries:  40,
					KB:       40,
				},
			},
		},
		{
			Labels: mustParseLabels(`{foo="baz", __loki_tenant__="fake"}`),
			Chunks: []index.ChunkMeta{
				{
					MinTime:  t1.UnixMilli(),
					MaxTime:  t1.Add(30 * time.Minute).UnixMilli(),
					Checksum: 5,
					Entries:  50,
					KB:       50,
				},
				{
					MinTime:  t1.Add(30 * time.Minute).UnixMilli(),
					MaxTime:  t2.UnixMilli(),
					Checksum: 6,
					Entries:  60,
					KB:       60,
				},
			},
		},
	}

	// Create the TSDB index
	tempDir := t.TempDir()
	tsdbIndex := BuildIndex(t, tempDir, series)

	from := model.TimeFromUnixNano(t1.UnixNano())
	through := model.TimeFromUnixNano(t2.UnixNano())

	t.Run("aggregate by series", func(t *testing.T) {
		t.Run("it matches all the series when the match all matcher is passed", func(t *testing.T) {
			matcher := labels.MustNewMatcher(labels.MatchEqual, "", "")
			acc := seriesvolume.NewAccumulator(10, 10)
			err := tsdbIndex.Volume(context.Background(), "fake", from, through, acc, nil, nil, nil, seriesvolume.Series, matcher)
			require.NoError(t, err)
			require.Equal(t, &logproto.VolumeResponse{
				Volumes: []logproto.Volume{
					{Name: `{foo="baz"}`, Volume: (50 + 60) * 1024},
					{Name: `{fizz="fizz", foo="bar", in="out"}`, Volume: (30 + 40) * 1024},
					{Name: `{fizz="buzz", foo="bar", us="them"}`, Volume: (10 + 20) * 1024},
				},
				Limit: 10,
			}, acc.Volumes())
		})

		t.Run("it ignores the tenant label matcher", func(t *testing.T) {
			matcher := []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchRegexp, "fizz", ".+"),
				labels.MustNewMatcher(labels.MatchRegexp, "foo", ".+"),
			}
			acc := seriesvolume.NewAccumulator(10, 10)
			err := tsdbIndex.Volume(context.Background(), "fake", from, through, acc, nil, nil, nil, seriesvolume.Series, withTenantLabelMatcher("fake", matcher)...)
			require.NoError(t, err)
			require.Equal(t, &logproto.VolumeResponse{
				Volumes: []logproto.Volume{
					{Name: `{fizz="fizz", foo="bar"}`, Volume: (30 + 40) * 1024},
					{Name: `{fizz="buzz", foo="bar"}`, Volume: (10 + 20) * 1024},
				},
				Limit: 10,
			}, acc.Volumes())
		})

		t.Run("it matches none of the series", func(t *testing.T) {
			matcher := labels.MustNewMatcher(labels.MatchEqual, "foo", "boo")
			acc := seriesvolume.NewAccumulator(10, 10)
			err := tsdbIndex.Volume(context.Background(), "fake", from, through, acc, nil, nil, nil, seriesvolume.Series, matcher)
			require.NoError(t, err)
			require.Equal(t, &logproto.VolumeResponse{
				Volumes: []logproto.Volume{},
				Limit:   10,
			}, acc.Volumes())
		})

		t.Run("it only returns results for the labels in the matcher", func(t *testing.T) {
			matcher := labels.MustNewMatcher(labels.MatchEqual, "foo", "bar")
			acc := seriesvolume.NewAccumulator(10, 10)
			err := tsdbIndex.Volume(context.Background(), "fake", from, through, acc, nil, nil, nil, seriesvolume.Series, matcher)
			require.NoError(t, err)
			require.Equal(t, &logproto.VolumeResponse{
				Volumes: []logproto.Volume{
					{Name: `{foo="bar"}`, Volume: (10 + 20 + 30 + 40) * 1024},
				},
				Limit: 10,
			}, acc.Volumes())
		})

		t.Run("it returns results for label names in matchers", func(t *testing.T) {
			matchers := []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"),
				labels.MustNewMatcher(labels.MatchRegexp, "fizz", ".+"),
			}
			acc := seriesvolume.NewAccumulator(10, 10)
			err := tsdbIndex.Volume(context.Background(), "fake", from, through, acc, nil, nil, nil, seriesvolume.Series, matchers...)
			require.NoError(t, err)
			require.Equal(t, &logproto.VolumeResponse{
				Volumes: []logproto.Volume{
					{Name: `{fizz="fizz", foo="bar"}`, Volume: (30 + 40) * 1024},
					{Name: `{fizz="buzz", foo="bar"}`, Volume: (10 + 20) * 1024},
				},
				Limit: 10,
			}, acc.Volumes())
		})

		t.Run("it can filter chunks", func(t *testing.T) {
			tsdbIndex.SetChunkFilterer(&filterAll{})
			defer tsdbIndex.SetChunkFilterer(nil)

			matcher := labels.MustNewMatcher(labels.MatchEqual, "", "")
			acc := seriesvolume.NewAccumulator(10, 10)
			err := tsdbIndex.Volume(context.Background(), "fake", from, through, acc, nil, nil, nil, seriesvolume.Series, matcher)

			require.NoError(t, err)
			require.Equal(t, &logproto.VolumeResponse{
				Volumes: []logproto.Volume{},
				Limit:   10,
			}, acc.Volumes())
		})

		t.Run("only gets factor of stream size within time bounds", func(t *testing.T) {
			matchers := []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"),
				labels.MustNewMatcher(labels.MatchRegexp, "fizz", ".+"),
			}
			acc := seriesvolume.NewAccumulator(10, 10)
			err := tsdbIndex.Volume(context.Background(), "fake", from, through.Add(-30*time.Minute), acc, nil, nil, nil, seriesvolume.Series, matchers...)
			require.NoError(t, err)
			require.Equal(t, &logproto.VolumeResponse{
				Volumes: []logproto.Volume{
					{Name: `{fizz="fizz", foo="bar"}`, Volume: (29) * 1024},
					{Name: `{fizz="buzz", foo="bar"}`, Volume: (9) * 1024},
				},
				Limit: 10,
			}, acc.Volumes())
		})

		t.Run("when targetLabels provided, it aggregates by those labels only", func(t *testing.T) {
			t.Run("all targetLabels are added to matchers", func(t *testing.T) {
				matcher := labels.MustNewMatcher(labels.MatchEqual, "", "")
				acc := seriesvolume.NewAccumulator(10, 10)
				err := tsdbIndex.Volume(context.Background(), "fake", from, through, acc, nil, nil, []string{"fizz"}, seriesvolume.Series, matcher)
				require.NoError(t, err)
				require.Equal(t, &logproto.VolumeResponse{
					Volumes: []logproto.Volume{
						{Name: `{fizz="fizz"}`, Volume: (30 + 40) * 1024},
						{Name: `{fizz="buzz"}`, Volume: (10 + 20) * 1024},
					},
					Limit: 10,
				}, acc.Volumes())
			})

			t.Run("with a specific equals matcher", func(t *testing.T) {
				matcher := labels.MustNewMatcher(labels.MatchEqual, "foo", "bar")
				acc := seriesvolume.NewAccumulator(10, 10)
				err := tsdbIndex.Volume(context.Background(), "fake", from, through, acc, nil, nil, []string{"fizz"}, seriesvolume.Series, matcher)
				require.NoError(t, err)
				require.Equal(t, &logproto.VolumeResponse{
					Volumes: []logproto.Volume{
						{Name: `{fizz="fizz"}`, Volume: (30 + 40) * 1024},
						{Name: `{fizz="buzz"}`, Volume: (10 + 20) * 1024},
					},
					Limit: 10,
				}, acc.Volumes())
			})

			t.Run("with a specific regexp matcher", func(t *testing.T) {
				matcher := labels.MustNewMatcher(labels.MatchRegexp, "fizz", ".+")
				acc := seriesvolume.NewAccumulator(10, 10)
				err := tsdbIndex.Volume(context.Background(), "fake", from, through, acc, nil, nil, []string{"foo"}, seriesvolume.Series, matcher)
				require.NoError(t, err)
				require.Equal(t, &logproto.VolumeResponse{
					Volumes: []logproto.Volume{
						{Name: `{foo="bar"}`, Volume: (100) * 1024},
					},
					Limit: 10,
				}, acc.Volumes())
			})
		})
	})

	t.Run("aggregate by labels", func(t *testing.T) {
		t.Run("it matches all the series when the match all matcher is passed", func(t *testing.T) {
			matcher := labels.MustNewMatcher(labels.MatchEqual, "", "")
			acc := seriesvolume.NewAccumulator(10, 10)
			err := tsdbIndex.Volume(context.Background(), "fake", from, through, acc, nil, nil, nil, seriesvolume.Labels, matcher)
			require.NoError(t, err)
			require.Equal(t, &logproto.VolumeResponse{
				Volumes: []logproto.Volume{
					{Name: `foo`, Volume: (10 + 20 + 30 + 40 + 50 + 60) * 1024},
					{Name: `fizz`, Volume: (10 + 20 + 30 + 40) * 1024},
					{Name: `in`, Volume: (30 + 40) * 1024},
					{Name: `us`, Volume: (10 + 20) * 1024},
				},
				Limit: 10,
			}, acc.Volumes())
		})

		t.Run("it ignores the tenant label matcher", func(t *testing.T) {
			matcher := []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchRegexp, "fizz", ".+"),
				labels.MustNewMatcher(labels.MatchRegexp, "foo", ".+"),
			}
			acc := seriesvolume.NewAccumulator(10, 10)
			err := tsdbIndex.Volume(context.Background(), "fake", from, through, acc, nil, nil, nil, seriesvolume.Labels, withTenantLabelMatcher("fake", matcher)...)
			require.NoError(t, err)
			require.Equal(t, &logproto.VolumeResponse{
				Volumes: []logproto.Volume{
					{Name: `fizz`, Volume: (10 + 20 + 30 + 40) * 1024},
					{Name: `foo`, Volume: (10 + 20 + 30 + 40) * 1024},
					{Name: `in`, Volume: (30 + 40) * 1024},
					{Name: `us`, Volume: (10 + 20) * 1024},
				},
				Limit: 10,
			}, acc.Volumes())
		})

		t.Run("it matches none of the series", func(t *testing.T) {
			matcher := labels.MustNewMatcher(labels.MatchEqual, "foo", "boo")
			acc := seriesvolume.NewAccumulator(10, 10)
			err := tsdbIndex.Volume(context.Background(), "fake", from, through, acc, nil, nil, nil, seriesvolume.Labels, matcher)
			require.NoError(t, err)
			require.Equal(t, &logproto.VolumeResponse{
				Volumes: []logproto.Volume{},
				Limit:   10,
			}, acc.Volumes())
		})

		t.Run("it only returns labels that exist on series intersecting with the matcher ", func(t *testing.T) {
			matcher := labels.MustNewMatcher(labels.MatchEqual, "us", "them")
			acc := seriesvolume.NewAccumulator(10, 10)
			err := tsdbIndex.Volume(context.Background(), "fake", from, through, acc, nil, nil, nil, seriesvolume.Labels, matcher)
			require.NoError(t, err)
			require.Equal(t, &logproto.VolumeResponse{
				Volumes: []logproto.Volume{
					{Name: `fizz`, Volume: (10 + 20) * 1024},
					{Name: `foo`, Volume: (10 + 20) * 1024},
					{Name: `us`, Volume: (10 + 20) * 1024},
				},
				Limit: 10,
			}, acc.Volumes())
		})

		t.Run("it returns results for label names in matchers", func(t *testing.T) {
			matchers := []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"),
				labels.MustNewMatcher(labels.MatchRegexp, "fizz", ".+"),
			}
			acc := seriesvolume.NewAccumulator(10, 10)
			err := tsdbIndex.Volume(context.Background(), "fake", from, through, acc, nil, nil, nil, seriesvolume.Labels, matchers...)
			require.NoError(t, err)
			require.Equal(t, &logproto.VolumeResponse{
				Volumes: []logproto.Volume{
					{Name: `fizz`, Volume: (10 + 20 + 30 + 40) * 1024},
					{Name: `foo`, Volume: (10 + 20 + 30 + 40) * 1024},
					{Name: `in`, Volume: (30 + 40) * 1024},
					{Name: `us`, Volume: (10 + 20) * 1024},
				},
				Limit: 10,
			}, acc.Volumes())
		})

		t.Run("it can filter chunks", func(t *testing.T) {
			tsdbIndex.SetChunkFilterer(&filterAll{})
			defer tsdbIndex.SetChunkFilterer(nil)

			matcher := labels.MustNewMatcher(labels.MatchEqual, "", "")
			acc := seriesvolume.NewAccumulator(10, 10)
			err := tsdbIndex.Volume(context.Background(), "fake", from, through, acc, nil, nil, nil, seriesvolume.Labels, matcher)

			require.NoError(t, err)
			require.Equal(t, &logproto.VolumeResponse{
				Volumes: []logproto.Volume{},
				Limit:   10,
			}, acc.Volumes())
		})

		t.Run("only gets factor of stream size within time bounds", func(t *testing.T) {
			matcher := labels.MustNewMatcher(labels.MatchEqual, "", "")
			acc := seriesvolume.NewAccumulator(10, 10)
			err := tsdbIndex.Volume(context.Background(), "fake", from, through.Add(-30*time.Minute), acc, nil, nil, nil, seriesvolume.Labels, matcher)
			require.NoError(t, err)
			require.Equal(t, &logproto.VolumeResponse{
				Volumes: []logproto.Volume{
					{Name: `foo`, Volume: (29 + 9 + 48) * 1024},
					{Name: `fizz`, Volume: (29 + 9) * 1024},
					{Name: `in`, Volume: (29) * 1024},
					{Name: `us`, Volume: (9) * 1024},
				},
				Limit: 10,
			}, acc.Volumes())
		})

		t.Run("when targetLabels provided, it aggregates by those labels only", func(t *testing.T) {
			t.Run("all targetLabels are added to matchers", func(t *testing.T) {
				matcher := labels.MustNewMatcher(labels.MatchEqual, "", "")
				acc := seriesvolume.NewAccumulator(10, 10)
				err := tsdbIndex.Volume(context.Background(), "fake", from, through, acc, nil, nil, []string{"fizz"}, seriesvolume.Labels, matcher)
				require.NoError(t, err)
				require.Equal(t, &logproto.VolumeResponse{
					Volumes: []logproto.Volume{
						{Name: `fizz`, Volume: (10 + 20 + 30 + 40) * 1024},
					},
					Limit: 10,
				}, acc.Volumes())
			})

			t.Run("with a specific equals matcher", func(t *testing.T) {
				matcher := labels.MustNewMatcher(labels.MatchEqual, "foo", "bar")
				acc := seriesvolume.NewAccumulator(10, 10)
				err := tsdbIndex.Volume(context.Background(), "fake", from, through, acc, nil, nil, []string{"fizz"}, seriesvolume.Labels, matcher)
				require.NoError(t, err)
				require.Equal(t, &logproto.VolumeResponse{
					Volumes: []logproto.Volume{
						{Name: `fizz`, Volume: (10 + 20 + 30 + 40) * 1024},
					},
					Limit: 10,
				}, acc.Volumes())
			})

			t.Run("with a specific regexp matcher", func(t *testing.T) {
				matcher := labels.MustNewMatcher(labels.MatchRegexp, "fizz", ".+")
				acc := seriesvolume.NewAccumulator(10, 10)
				err := tsdbIndex.Volume(context.Background(), "fake", from, through, acc, nil, nil, []string{"foo"}, seriesvolume.Labels, matcher)
				require.NoError(t, err)
				require.Equal(t, &logproto.VolumeResponse{
					Volumes: []logproto.Volume{
						{Name: `foo`, Volume: (100) * 1024},
					},
					Limit: 10,
				}, acc.Volumes())
			})
			// todo(cyriltovena): tests with chunk filterer
		})
	})
}

func BenchmarkTSDBIndex_Volume(b *testing.B) {
	var series []LoadableSeries
	for i := 0; i < 1000; i++ {
		series = append(series, LoadableSeries{
			Labels: mustParseLabels(fmt.Sprintf(`{foo="bar", fizz="fizz%d", buzz="buzz%d",bar="bar%d", bozz="bozz%d"}`, i, i, i, i)),
			Chunks: []index.ChunkMeta{
				{
					MinTime:  0,
					MaxTime:  10,
					Checksum: uint32(i),
					KB:       10,
					Entries:  10,
				},
				{
					MinTime:  10,
					MaxTime:  20,
					Checksum: uint32(i),
					KB:       10,
					Entries:  10,
				},
			},
		})
	}
	ctx := context.Background()
	from := model.Earliest
	through := model.Latest
	// Create the TSDB index
	tempDir := b.TempDir()
	tsdbIndex := BuildIndex(b, tempDir, series)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		acc := seriesvolume.NewAccumulator(10, 10)
		err := tsdbIndex.Volume(ctx, "fake", from, through, acc, nil, nil, nil, seriesvolume.Series, labels.MustNewMatcher(labels.MatchRegexp, "foo", ".+"))
		require.NoError(b, err)
	}
}

type filterAll struct{}

func (f *filterAll) ForRequest(_ context.Context) chunk.Filterer {
	return &filterAllFilterer{}
}

type filterAllFilterer struct{}

func (f *filterAllFilterer) ShouldFilter(_ labels.Labels) bool {
	return true
}

func (f *filterAllFilterer) RequiredLabelNames() []string {
	return nil
}
