package chunkenc

import (
	"context"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
)

type LazyChunk struct {
	Chunk   chunk.Chunk
	Fetcher *chunk.Fetcher
}

func (c *LazyChunk) getChunk(ctx context.Context) (Chunk, error) {
	chunks, err := c.Fetcher.FetchChunks(context.TODO(), []chunk.Chunk{c.Chunk}, []string{c.Chunk.ExternalKey()})
	if err != nil {
		return nil, err
	}

	return chunks[0].Data.(*Facade).LokiChunk(), nil
}

func (c LazyChunk) Iterator(from, through time.Time, direction logproto.Direction) (iter.EntryIterator, error) {
	return &lazyIterator{
		chunk: c,

		from:      from,
		through:   through,
		direction: direction,
	}, nil
}

type lazyIterator struct {
	iter.EntryIterator

	chunk LazyChunk
	err   error

	from, through time.Time
	direction     logproto.Direction
}

func (it *lazyIterator) Next() bool {
	if it.err != nil {
		return false
	}

	if it.EntryIterator != nil {
		return it.EntryIterator.Next()
	}

	chk, err := it.chunk.getChunk(context.TODO())
	if err != nil {
		it.err = err
		return false
	}

	it.EntryIterator, it.err = chk.Iterator(it.from, it.through, it.direction)

	return it.Next()
}

func (it *lazyIterator) Error() error {
	if it.err != nil {
		return it.err
	}

	return it.EntryIterator.Error()
}
