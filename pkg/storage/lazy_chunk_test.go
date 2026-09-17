package storage

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/util"
)

func TestLazyChunkIterator(t *testing.T) {
	periodConfigs := []config.PeriodConfig{
		{
			From:   config.DayTime{Time: 0},
			Schema: "v11",
		},
		{
			From:   config.DayTime{Time: 0},
			Schema: "v12",
		},
		{
			From:   config.DayTime{Time: 0},
			Schema: "v13",
		},
	}

	for _, periodConfig := range periodConfigs {
		chunkfmt, headfmt, err := periodConfig.ChunkFormat()
		require.NoError(t, err)

		for i, tc := range []struct {
			chunk    *LazyChunk
			expected []logproto.Stream
		}{
			// TODO: Add tests for metadata labels.
			{
				newLazyChunk(chunkfmt, headfmt, logproto.Stream{
					Labels: fooLabelsWithName.String(),
					Hash:   labels.StableHash(fooLabelsWithName),
					Entries: []logproto.Entry{
						{
							Timestamp:          from,
							Line:               "1",
							Parsed:             logproto.EmptyLabelAdapters(),
							StructuredMetadata: logproto.EmptyLabelAdapters(),
						},
					},
				}),
				[]logproto.Stream{
					{
						Labels: fooLabels.String(),
						Hash:   labels.StableHash(fooLabels),
						Entries: []logproto.Entry{
							{
								Timestamp:          from,
								Line:               "1",
								Parsed:             logproto.EmptyLabelAdapters(),
								StructuredMetadata: logproto.EmptyLabelAdapters(),
							},
						},
					},
				},
			},
		} {
			t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
				it, err := tc.chunk.Iterator(context.Background(), time.Unix(0, 0), time.Unix(1000, 0), logproto.FORWARD, log.NewNoopPipeline().ForStream(labels.New(labels.Label{Name: "foo", Value: "bar"})), nil, queryTimeRanges{})
				require.Nil(t, err)
				streams, _, err := iter.ReadBatch(it, 1000)
				require.Nil(t, err)
				_ = it.Close()
				require.Equal(t, tc.expected, streams.Streams)
			})
		}
	}
}

func TestHintRangeIterators(t *testing.T) {
	base := time.Unix(100, 0)
	allOffsets := []time.Duration{0, time.Millisecond, 2 * time.Millisecond, 3 * time.Millisecond, 4 * time.Millisecond, 5 * time.Millisecond}

	tests := []struct {
		name     string
		hints    []logproto.HintTimeRange
		start    time.Time
		end      time.Time
		expected []time.Duration
	}{
		{
			name:     "nil hints preserve current behavior",
			start:    base,
			end:      base.Add(6 * time.Millisecond),
			expected: allOffsets,
		},
		{
			name:     "empty hints preserve current behavior",
			hints:    []logproto.HintTimeRange{},
			start:    base,
			end:      base.Add(6 * time.Millisecond),
			expected: allOffsets,
		},
		{
			name: "disjoint hints return nothing",
			hints: []logproto.HintTimeRange{{
				Start: base.Add(10 * time.Millisecond),
				End:   base.Add(11 * time.Millisecond),
			}},
			start: base,
			end:   base.Add(6 * time.Millisecond),
		},
		{
			name: "ranges are clipped and half open",
			hints: []logproto.HintTimeRange{
				{Start: base.Add(-time.Millisecond), End: base.Add(2 * time.Millisecond)},
				{Start: base.Add(5 * time.Millisecond), End: base.Add(8 * time.Millisecond)},
			},
			start:    base.Add(time.Millisecond),
			end:      base.Add(5 * time.Millisecond),
			expected: []time.Duration{time.Millisecond},
		},
		{
			name: "multiple ranges form a union",
			hints: []logproto.HintTimeRange{
				{Start: base.Add(4 * time.Millisecond), End: base.Add(6 * time.Millisecond)},
				{Start: base.Add(time.Millisecond), End: base.Add(2 * time.Millisecond)},
				{Start: base.Add(3 * time.Millisecond), End: base.Add(5 * time.Millisecond)},
			},
			start: base,
			end:   base.Add(6 * time.Millisecond),
			expected: []time.Duration{
				time.Millisecond,
				3 * time.Millisecond,
				4 * time.Millisecond,
				5 * time.Millisecond,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ranges := newQueryTimeRanges(tc.hints, tc.start, tc.end)

			entries := make([]logproto.Entry, 0, len(allOffsets))
			samples := make([]logproto.Sample, 0, len(allOffsets))
			for _, offset := range allOffsets {
				entries = append(entries, logproto.Entry{
					Timestamp: base.Add(offset),
					Line:      offset.String(),
				})
				samples = append(samples, logproto.Sample{
					Timestamp: base.Add(offset).UnixNano(),
					Value:     float64(offset),
				})
			}

			entryIt := newHintEntryIterator(iter.NewStreamIterator(logproto.Stream{
				Labels:  `{foo="bar"}`,
				Hash:    123,
				Entries: entries,
			}), ranges)
			var gotEntries []time.Duration
			for entryIt.Next() {
				gotEntries = append(gotEntries, entryIt.At().Timestamp.Sub(base))
			}
			require.NoError(t, entryIt.Err())
			if len(tc.expected) > 0 {
				require.Equal(t, tc.expected[len(tc.expected)-1], entryIt.At().Timestamp.Sub(base), "At must retain the last accepted entry after exhaustion")
			}
			require.NoError(t, entryIt.Close())

			sampleIt := newHintSampleIterator(iter.NewSeriesIterator(logproto.Series{
				Labels:     `{foo="bar"}`,
				StreamHash: 123,
				Samples:    samples,
			}), ranges)
			var gotSamples []time.Duration
			for sampleIt.Next() {
				gotSamples = append(gotSamples, time.Unix(0, sampleIt.At().Timestamp).Sub(base))
			}
			require.NoError(t, sampleIt.Err())
			if len(tc.expected) > 0 {
				require.Equal(t, tc.expected[len(tc.expected)-1], time.Unix(0, sampleIt.At().Timestamp).Sub(base), "At must retain the last accepted sample after exhaustion")
			}
			require.NoError(t, sampleIt.Close())

			require.Equal(t, tc.expected, gotEntries)
			require.Equal(t, tc.expected, gotSamples)
		})
	}
}

func TestFilterBlocksByHintRanges(t *testing.T) {
	base := time.Unix(100, 0)
	blocks := []chunkenc.Block{
		blockWithBounds(base.UnixNano(), base.Add(time.Millisecond-time.Nanosecond).UnixNano()),
		blockWithBounds(base.Add(time.Millisecond).UnixNano(), base.Add(2*time.Millisecond-time.Nanosecond).UnixNano()),
		blockWithBounds(base.Add(2*time.Millisecond).UnixNano(), base.Add(3*time.Millisecond).UnixNano()),
	}
	ranges := newQueryTimeRanges(
		[]logproto.HintTimeRange{{Start: base.Add(time.Millisecond), End: base.Add(2 * time.Millisecond)}},
		base,
		base.Add(3*time.Millisecond),
	)

	filtered := filterBlocksByHintRanges(blocks, ranges)

	require.Len(t, filtered, 1)
	require.Same(t, blocks[1], filtered[0])
}

func TestFilterChunksByHintRanges(t *testing.T) {
	base := time.Unix(100, 0)
	chunks := []chunk.Chunk{
		{ChunkRef: logproto.ChunkRef{From: model.Time(base.UnixMilli()), Through: model.Time(base.UnixMilli())}},
		{ChunkRef: logproto.ChunkRef{From: model.Time(base.Add(time.Millisecond).UnixMilli()), Through: model.Time(base.Add(time.Millisecond).UnixMilli())}},
		{ChunkRef: logproto.ChunkRef{From: model.Time(base.Add(2 * time.Millisecond).UnixMilli()), Through: model.Time(base.Add(3 * time.Millisecond).UnixMilli())}},
	}
	ranges := newQueryTimeRanges(
		[]logproto.HintTimeRange{{Start: base.Add(time.Millisecond), End: base.Add(2 * time.Millisecond)}},
		base,
		base.Add(3*time.Millisecond),
	)

	filtered := filterChunksByHintRanges(chunks, ranges)

	require.Len(t, filtered, 1)
	require.Equal(t, chunks[1].ChunkRef, filtered[0].ChunkRef)
}

func TestLazyChunksPop(t *testing.T) {
	for i, tc := range []struct {
		initial    int
		n          int
		expectedLn int
		rem        int
	}{
		{1, 1, 1, 0},
		{2, 1, 1, 1},
		{3, 4, 3, 0},
	} {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			lc := &lazyChunks{}
			for i := 0; i < tc.initial; i++ {
				lc.chunks = append(lc.chunks, &LazyChunk{})
			}
			out := lc.pop(tc.n)

			for i := 0; i < tc.expectedLn; i++ {
				require.NotNil(t, out[i])
			}

			for i := 0; i < tc.rem; i++ {
				require.NotNil(t, lc.chunks[i])
			}
		})
	}
}

func TestIsOverlapping(t *testing.T) {
	tests := []struct {
		name      string
		direction logproto.Direction
		with      *LazyChunk
		b         chunkenc.Block
		want      bool
	}{
		{
			"equal forward",
			logproto.FORWARD,
			lazyChunkWithBounds(time.Unix(0, 0), time.Unix(0, int64(time.Millisecond*5))),
			blockWithBounds(0, int64(time.Millisecond*5)),
			true,
		},
		{
			"equal backward",
			logproto.BACKWARD,
			lazyChunkWithBounds(time.Unix(0, 0), time.Unix(0, int64(time.Millisecond*5))),
			blockWithBounds(0, int64(time.Millisecond*5)),
			true,
		},
		{
			"equal through backward",
			logproto.BACKWARD,
			lazyChunkWithBounds(time.Unix(0, int64(time.Millisecond*5)), time.Unix(0, int64(time.Millisecond*10))),
			blockWithBounds(0, int64(time.Millisecond*10)),
			true,
		},
		{
			"< through backward",
			logproto.BACKWARD,
			lazyChunkWithBounds(time.Unix(0, int64(time.Millisecond*5)), time.Unix(0, int64(time.Millisecond*10))),
			blockWithBounds(0, int64(time.Millisecond*5)),
			true,
		},
		{
			"from > forward",
			logproto.FORWARD,
			lazyChunkWithBounds(time.Unix(0, int64(time.Millisecond*4)), time.Unix(0, int64(time.Millisecond*10))),
			blockWithBounds(int64(time.Millisecond*3), int64(time.Millisecond*5)),
			true,
		},
		{
			"from < forward",
			logproto.FORWARD,
			lazyChunkWithBounds(time.Unix(0, int64(time.Millisecond*5)), time.Unix(0, int64(time.Millisecond*10))),
			blockWithBounds(int64(time.Millisecond*3), int64(time.Millisecond*4)),
			false,
		},
		{
			"from = forward",
			logproto.FORWARD,
			lazyChunkWithBounds(time.Unix(0, int64(time.Millisecond*5)), time.Unix(0, int64(time.Millisecond*10))),
			blockWithBounds(int64(time.Millisecond*3), int64(time.Millisecond*5)),
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// testing the block one
			require.Equal(t, tt.want, IsBlockOverlapping(tt.b, tt.with, tt.direction))
			// testing the chunk one
			l := lazyChunkWithBounds(time.Unix(0, tt.b.MinTime()), time.Unix(0, tt.b.MaxTime()))
			require.Equal(t, tt.want, l.IsOverlapping(tt.with, tt.direction))
		})
	}
}

func lazyChunkWithBounds(from, through time.Time) *LazyChunk {
	// In loki chunks are rounded when flushed fro nanoseconds to milliseconds.
	fromM, throughM := util.RoundToMilliseconds(from, through)
	return &LazyChunk{
		Chunk: chunk.Chunk{
			ChunkRef: logproto.ChunkRef{
				From:    fromM,
				Through: throughM,
			},
		},
	}
}

type fakeBlock struct {
	mint, maxt int64
	// it is the SampleIterator to hand back; nil unless a test sets it.
	it iter.SampleIterator
}

func (fakeBlock) Entries() int     { return 0 }
func (fakeBlock) Offset() int      { return 0 }
func (f fakeBlock) MinTime() int64 { return f.mint }
func (f fakeBlock) MaxTime() int64 { return f.maxt }
func (fakeBlock) Iterator(context.Context, log.StreamPipeline) iter.EntryIterator {
	return nil
}

func (f fakeBlock) SampleIterator(_ context.Context, _ log.StreamSampleExtractor) iter.SampleIterator {
	return f.it
}

func blockWithBounds(mint, maxt int64) chunkenc.Block {
	return &fakeBlock{
		maxt: maxt,
		mint: mint,
	}
}
