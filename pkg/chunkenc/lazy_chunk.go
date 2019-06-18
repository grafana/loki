package chunkenc

import (
	"context"
	"errors"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
)

// LazyChunk loads the chunk when it is accessed.
type LazyChunk struct {
	Chunk   chunk.Chunk
	Fetcher *chunk.Fetcher
}

// Iterator returns an entry iterator.
func (c *LazyChunk) Iterator(ctx context.Context, from, through time.Time, direction logproto.Direction, filter logql.Filter) (iter.EntryIterator, error) {
	// If the chunk is already loaded, then use that.
	if c.Chunk.Data != nil {
		lokiChunk := c.Chunk.Data.(*Facade).LokiChunk()
		return lokiChunk.Iterator(from, through, direction, filter)
	}

	return nil, errors.New("chunk is not loaded")
}
