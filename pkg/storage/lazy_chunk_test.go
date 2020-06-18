package storage

import (
	"context"
	"testing"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

func TestIsBlockOverlapping(t *testing.T) {
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
			lazyChunkWithBounds(model.TimeFromUnixNano(0), model.TimeFromUnixNano(10)),
			blockWithBounds(0, 10),
			true,
		},
		{
			"equal backward",
			logproto.BACKWARD,
			lazyChunkWithBounds(model.TimeFromUnixNano(0), model.TimeFromUnixNano(10)),
			blockWithBounds(0, 10),
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, IsBlockOverlapping(tt.b, tt.with, tt.direction))
		})
	}
}

func lazyChunkWithBounds(from, through model.Time) *LazyChunk {
	return &LazyChunk{
		Chunk: chunk.Chunk{
			From:    from,
			Through: through,
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
func (fakeBlock) Iterator(context.Context, logql.LineFilter) iter.EntryIterator {
	return nil
}

func blockWithBounds(mint, maxt int64) chunkenc.Block {
	return &fakeBlock{
		maxt: maxt,
		mint: mint,
	}
}
