package tsdb

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline"
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
					_, _ = head.Append(x.Labels, labels.StableHash(x.Labels), x.Chunks)
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

				expected := []logproto.ChunkRefWithSizingInfo{
					{
						ChunkRef: logproto.ChunkRef{
							UserID:      "fake",
							Fingerprint: labels.StableHash(mustParseLabels(`{foo="bar"}`)),
							From:        0,
							Through:     3,
							Checksum:    0,
						},
					},
					{
						ChunkRef: logproto.ChunkRef{
							UserID:      "fake",
							Fingerprint: labels.StableHash(mustParseLabels(`{foo="bar"}`)),
							From:        1,
							Through:     4,
							Checksum:    1,
						},
					},
					{
						ChunkRef: logproto.ChunkRef{
							UserID:      "fake",
							Fingerprint: labels.StableHash(mustParseLabels(`{foo="bar"}`)),
							From:        2,
							Through:     5,
							Checksum:    2,
						},
					},
					{
						ChunkRef: logproto.ChunkRef{
							UserID:      "fake",
							Fingerprint: labels.StableHash(mustParseLabels(`{foo="bar", bazz="buzz"}`)),
							From:        1,
							Through:     10,
							Checksum:    3,
						},
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

				require.Equal(t, []logproto.ChunkRefWithSizingInfo{{
					ChunkRef: logproto.ChunkRef{
						UserID:      "fake",
						Fingerprint: labels.StableHash(mustParseLabels(`{foo="bar", bazz="buzz"}`)),
						From:        1,
						Through:     10,
						Checksum:    3,
					},
				}}, shardedRefs)
			})

			t.Run("Series", func(t *testing.T) {
				xs, err := idx.Series(context.Background(), "fake", 8, 9, nil, nil, labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"))
				require.Nil(t, err)

				expected := []Series{
					{
						Labels:      mustParseLabels(`{foo="bar", bazz="buzz"}`),
						Fingerprint: model.Fingerprint(labels.StableHash(mustParseLabels(`{foo="bar", bazz="buzz"}`))),
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
						Fingerprint: model.Fingerprint(labels.StableHash(mustParseLabels(`{foo="bar"}`))),
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

// TestTSDBIndex_ForSeriesLabelsOutliveIndex checks that ForSeries hands out labels which aren't reused across
// series and survive the index being closed, so the compactor can retain them without copying. With stringlabels
// it catches a reused label buffer; with slicelabels it also catches label strings aliasing the unmapped file and
// the head reusing the caller's label slice.
func TestTSDBIndex_ForSeriesLabelsOutliveIndex(t *testing.T) {
	var streams []stream
	for i := range 100 {
		streams = append(streams, buildStream(labels.FromStrings("foo", fmt.Sprintf("bar-%d", i), "pod", fmt.Sprintf("pod-%d", i)), buildChunkMetas(0, 1), ""))
	}

	for _, tc := range []struct {
		name string
		// open returns the index and a func that closes it and releases what backs it.
		open func(t *testing.T) (*TSDBIndex, func())
	}{
		{
			name: "file",
			open: func(t *testing.T) (*TSDBIndex, func()) {
				path := setupPerTenantIndex(t, index.FormatV3, streams, t.TempDir(), time.Unix(0, 0))
				idx, err := OpenShippableTSDB(path, index.MmapOptions{})
				require.NoError(t, err)
				return idx.(*TSDBFile).Index.(*TSDBIndex), func() {
					require.NoError(t, idx.Close())
					require.NoError(t, os.Remove(path))
				}
			},
		},
		{
			name: "head",
			open: func(_ *testing.T) (*TSDBIndex, func()) {
				head := NewHead("fake", NewMetrics(nil), log.NewNopLogger())
				for _, s := range streams {
					_, _ = head.Append(s.labels, uint64(s.fp), s.chunks)
				}
				return NewTSDBIndex(head.Index()), func() {}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			idx, closeIdx := tc.open(t)

			var retained []labels.Labels
			err := idx.ForSeries(context.Background(), "", nil, 0, math.MaxInt64, func(ls labels.Labels, _ model.Fingerprint, _ []index.ChunkMeta) (stop bool) {
				retained = append(retained, ls)
				return false
			}, labels.MustNewMatcher(labels.MatchEqual, "", ""))
			closeIdx()
			require.NoError(t, err)

			expected := make([]string, 0, len(streams))
			for _, s := range streams {
				expected = append(expected, s.labels.String())
			}
			actual := make([]string, 0, len(retained))
			for _, ls := range retained {
				actual = append(actual, ls.String())
			}
			require.ElementsMatch(t, expected, actual)
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
		ChunkRefsPool.Put(chkRefs)
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

func TestTSDBIndex_StatsWithHints(t *testing.T) {
	series := []LoadableSeries{
		{
			Labels: mustParseLabels(`{foo="bar", fizz="buzz"}`),
			Chunks: []index.ChunkMeta{
				{MinTime: 0, MaxTime: 9, Checksum: 1, Entries: 10, KB: 10},
				{MinTime: 10, MaxTime: 19, Checksum: 2, Entries: 20, KB: 20},
			},
		},
		{
			Labels: mustParseLabels(`{foo="bar", ping="pong"}`),
			Chunks: []index.ChunkMeta{
				{MinTime: 30, MaxTime: 39, Checksum: 3, Entries: 30, KB: 30},
				{MinTime: 40, MaxTime: 49, Checksum: 4, Entries: 40, KB: 40},
			},
		},
	}
	variants := map[string]func() Index{
		"file": func() Index { return BuildIndex(t, t.TempDir(), series) },
		"head": func() Index {
			head := NewHead("fake", NewMetrics(nil), log.NewNopLogger())
			for _, s := range series {
				_, _ = head.Append(s.Labels, labels.StableHash(s.Labels), s.Chunks)
			}
			return NewTSDBIndex(head.Index())
		},
	}

	hint := func(startMs, endMs int64) *logproto.HintTimeRanges {
		return &logproto.HintTimeRanges{Ranges: []logproto.HintTimeRange{{Start: time.UnixMilli(startMs), End: time.UnixMilli(endMs)}}}
	}
	entries := func(streams, chunks, entries uint64) stats.Stats {
		return stats.Stats{Streams: streams, Chunks: chunks, Entries: entries, Bytes: entries * 1024}
	}

	for _, tc := range []struct {
		name     string
		hints    *logproto.HintTimeRanges
		expected stats.Stats
	}{
		{name: "no hints counts every chunk", hints: nil, expected: entries(2, 4, 100)},
		{name: "empty hints count nothing", hints: &logproto.HintTimeRanges{}, expected: stats.Stats{}},
		{name: "hint inside a chunk", hints: hint(5, 6), expected: entries(1, 1, 10)},
		{name: "hint across both series", hints: hint(15, 35), expected: entries(2, 2, 50)},
		{name: "series without a hinted chunk is not a stream", hints: hint(40, 41), expected: entries(1, 1, 40)},
		{name: "hint starting at a chunk's last millisecond", hints: hint(19, 20), expected: entries(1, 1, 20)},
		{name: "hint ending at a chunk's first millisecond", hints: hint(25, 30), expected: stats.Stats{}},
	} {
		for variant, build := range variants {
			t.Run(fmt.Sprintf("%s/%s", variant, tc.name), func(t *testing.T) {
				acc := &stats.Stats{}
				filter := newHintChunkFilter(tc.hints, 0, 50)
				err := build().Stats(context.Background(), "fake", 0, 50, acc, nil, filter, labels.MustNewMatcher(labels.MatchEqual, "foo", "bar"))
				require.NoError(t, err)
				require.Equal(t, tc.expected, *acc)
			})
		}
	}
}

func TestTSDBIndex_StatsWithDocumentShardHints(t *testing.T) {
	// Every series has a chunk in [0, 10) and one in [10, 20). The hints cover
	// [0, 10) for document shard 1 of 2 and [10, 20) for every stream.
	chunks := index.ChunkMetas{
		{MinTime: 0, MaxTime: 9, Checksum: 1, Entries: 1, KB: 1},
		{MinTime: 10, MaxTime: 19, Checksum: 2, Entries: 1, KB: 1},
	}
	series := []LoadableSeries{
		{Labels: mustParseLabels(`{foo="bar", shard="0"}`), Chunks: chunks, Fingerprint: 1 << 62},
		{Labels: mustParseLabels(`{foo="bar", shard="1"}`), Chunks: chunks, Fingerprint: 1<<63 | 1<<62},
		// The ingester may have remapped this fingerprint, so its document shard is unknown.
		{Labels: mustParseLabels(`{foo="bar", shard="remapped"}`), Chunks: chunks, Fingerprint: logline.MaxMappedFingerprint},
	}
	hints := &logproto.HintTimeRanges{Ranges: []logproto.HintTimeRange{
		{Start: time.UnixMilli(0), End: time.UnixMilli(10), DocumentShard: 1, DocumentShardBits: 1},
		{Start: time.UnixMilli(10), End: time.UnixMilli(20)},
	}}
	variants := map[string]func() Index{
		"file": func() Index { return BuildIndex(t, t.TempDir(), series) },
		"head": func() Index {
			head := NewHead("fake", NewMetrics(nil), log.NewNopLogger())
			for _, s := range series {
				_, _ = head.Append(s.Labels, uint64(s.Fingerprint), s.Chunks)
			}
			return NewTSDBIndex(head.Index())
		},
	}

	for variant, build := range variants {
		idx := build()
		for shard, wantChunks := range map[string]uint64{"0": 1, "1": 2, "remapped": 2} {
			t.Run(fmt.Sprintf("%s/%s", variant, shard), func(t *testing.T) {
				var got stats.Stats
				filter := newHintChunkFilter(hints, 0, 20)
				require.NoError(t, idx.Stats(context.Background(), "fake", 0, 20, &got, nil, filter, labels.MustNewMatcher(labels.MatchEqual, "shard", shard)))
				require.Equal(t, wantChunks, got.Chunks)
			})
		}
	}
}

func TestTSDBIndex_StatsWithHintsAcrossChunkPages(t *testing.T) {
	// The series has enough chunks for the index to write chunk pages, so the
	// page-level shortcuts run.
	const numChunks = 300
	metas := make([]index.ChunkMeta, 0, numChunks)
	for i := range numChunks {
		metas = append(metas, index.ChunkMeta{MinTime: int64(i) * 10, MaxTime: int64(i)*10 + 9, Checksum: uint32(i), Entries: 1, KB: 1})
	}
	through := model.Time(numChunks * 10)

	hints := func(msRanges ...[2]int64) *logproto.HintTimeRanges {
		h := &logproto.HintTimeRanges{}
		for _, r := range msRanges {
			h.Ranges = append(h.Ranges, logproto.HintTimeRange{Start: time.UnixMilli(r[0]), End: time.UnixMilli(r[1])})
		}
		return h
	}
	cases := map[string]*logproto.HintTimeRanges{
		"empty":                hints(),
		"everything":           hints([2]int64{0, int64(through)}),
		"page aligned":         hints([2]int64{640, 1280}),
		"across page bounds":   hints([2]int64{635, 1285}),
		"one chunk":            hints([2]int64{1000, 1001}),
		"several ranges":       hints([2]int64{5, 6}, [2]int64{700, 2000}, [2]int64{2995, 3000}),
		"sub-millisecond edge": {Ranges: []logproto.HintTimeRange{{Start: time.UnixMilli(1999).Add(time.Microsecond), End: time.UnixMilli(2000)}}},
		"document shards": {Ranges: []logproto.HintTimeRange{
			{Start: time.UnixMilli(700), End: time.UnixMilli(2000), DocumentShard: 1, DocumentShardBits: 1},
			{Start: time.UnixMilli(0), End: through.Time(), DocumentShard: 0, DocumentShardBits: 1},
		}},
	}
	// The first fingerprint is in document shard 1 of 2. The ingester may have
	// remapped the second, so every document shard's ranges cover it.
	for _, fp := range []uint64{1 << 63, logline.MaxMappedFingerprint} {
		idx := BuildIndex(t, t.TempDir(), []LoadableSeries{{Labels: mustParseLabels(`{foo="bar"}`), Chunks: metas, Fingerprint: model.Fingerprint(fp)}})
		for name, h := range cases {
			t.Run(fmt.Sprintf("fp=%d/%s", fp, name), func(t *testing.T) {
				filter := newHintChunkFilter(h, 0, through)
				var want stats.Stats
				for _, m := range metas {
					if filter.Overlaps(fp, m.MinTime, m.MaxTime) {
						want.Chunks++
						want.Entries++
						want.Bytes += 1024
					}
				}
				if want.Chunks > 0 {
					want.Streams = 1
				}

				var got stats.Stats
				require.NoError(t, idx.Stats(context.Background(), "fake", 0, through, &got, nil, filter, labels.MustNewMatcher(labels.MatchEqual, "foo", "bar")))
				require.Equal(t, want, got)
			})
		}
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

func (f *filterAllFilterer) PushableLabelMatchers() []*labels.Matcher {
	// Pushing down matchers is not supported.
	return nil
}
