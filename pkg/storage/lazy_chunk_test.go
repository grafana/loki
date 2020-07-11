package storage

import (
	"context"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/util"
)

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

func (fakeBlock) Entries() int                                                  { return 0 }
func (fakeBlock) Offset() int                                                   { return 0 }
func (f fakeBlock) MinTime() int64                                              { return f.mint }
func (f fakeBlock) MaxTime() int64                                              { return f.maxt }
func (fakeBlock) Iterator(context.Context, logql.LineFilter) iter.EntryIterator { return nil }
func (fakeBlock) SampleIterator(context.Context, logql.LineFilter, logql.SampleExtractor) iter.SampleIterator {
	return nil
}

func blockWithBounds(mint, maxt int64) chunkenc.Block {
	return &fakeBlock{
		maxt: maxt,
		mint: mint,
	}
}
