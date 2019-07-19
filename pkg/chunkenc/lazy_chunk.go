package chunkenc

import (
	"context"
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

func (c *LazyChunk) getChunk(ctx context.Context) (Chunk, error) {
	chunks, err := c.Fetcher.FetchChunks(ctx, []chunk.Chunk{c.Chunk}, []string{c.Chunk.ExternalKey()})
	if err != nil {
		return nil, err
	}
	return chunks[0].Data.(*Facade).LokiChunk(), nil
}

// Iterator returns an entry iterator.
func (c *LazyChunk) Iterator(ctx context.Context, from, through time.Time, direction logproto.Direction, filter logql.Filter) (iter.EntryIterator, error) {
	// If the chunk is already loaded, then use that.
	if c.Chunk.Data != nil {
		lokiChunk := c.Chunk.Data.(*Facade).LokiChunk()
		return lokiChunk.Iterator(from, through, direction, filter)
	}

	return &lazyIterator{
		chunk:  c,
		filter: filter,

		from:      from,
		through:   through,
		direction: direction,
		context:   ctx,
	}, nil
}

type lazyIterator struct {
	iter.EntryIterator

	chunk *LazyChunk
	err   error

	from, through time.Time
	direction     logproto.Direction
	context       context.Context
	filter        logql.Filter

	closed bool
}

func (it *lazyIterator) Next() bool {
	if it.err != nil {
		return false
	}

	if it.closed {
		return false
	}

	if it.EntryIterator != nil {
		next := it.EntryIterator.Next()
		if !next {
			it.Close()
		}
		return next
	}

	chk, err := it.chunk.getChunk(it.context)
	if err != nil {
		it.err = err
		return false
	}
	it.EntryIterator, it.err = chk.Iterator(it.from, it.through, it.direction, it.filter)
	return it.Next()
}

func (it *lazyIterator) Labels() string {
	return it.chunk.Chunk.Metric.String()
}

func (it *lazyIterator) Error() error {
	if it.err != nil {
		return it.err
	}
	if it.EntryIterator != nil {
		return it.EntryIterator.Error()
	}
	return nil
}

func (it *lazyIterator) Close() error {
	if it.EntryIterator != nil {
		it.closed = true
		err := it.EntryIterator.Close()
		it.EntryIterator = nil
		return err
	}
	return nil
}
