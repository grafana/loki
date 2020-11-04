package storage

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/util"
)

func TestLazyChunkIterator(t *testing.T) {
	for i, tc := range []struct {
		chunk    *LazyChunk
		expected []logproto.Stream
	}{
		{
			newLazyChunk(logproto.Stream{
				Labels: fooLabelsWithName,
				Entries: []logproto.Entry{
					{
						Timestamp: from,
						Line:      "1",
					},
				},
			}),
			[]logproto.Stream{
				{
					Labels: fooLabels,
					Entries: []logproto.Entry{
						{
							Timestamp: from,
							Line:      "1",
						},
					},
				},
			},
		},
	} {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			it, err := tc.chunk.Iterator(context.Background(), time.Unix(0, 0), time.Unix(1000, 0), logproto.FORWARD, logql.NoopPipeline, nil)
			require.Nil(t, err)
			streams, _, err := iter.ReadBatch(it, 1000)
			require.Nil(t, err)
			_ = it.Close()
			require.Equal(t, tc.expected, streams.Streams)
		})
	}
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
			From:    fromM,
			Through: throughM,
		},
	}
}

type fakeBlock struct {
	mint, maxt int64
}

func (fakeBlock) Entries() int     { return 0 }
func (fakeBlock) Offset() int      { return 0 }
func (f fakeBlock) MinTime() int64 { return f.mint }
func (f fakeBlock) MaxTime() int64 { return f.maxt }
func (fakeBlock) Iterator(context.Context, labels.Labels, logql.Pipeline) iter.EntryIterator {
	return nil
}
func (fakeBlock) SampleIterator(context.Context, labels.Labels, logql.SampleExtractor) iter.SampleIterator {
	return nil
}

func blockWithBounds(mint, maxt int64) chunkenc.Block {
	return &fakeBlock{
		maxt: maxt,
		mint: mint,
	}
}
