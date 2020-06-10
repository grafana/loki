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
	IsValid bool
	Fetcher *chunk.Fetcher

	lastBlock       *CachedIterator
	lastblockOffset int
}

// Iterator returns an entry iterator.
func (c *LazyChunk) Iterator(ctx context.Context, from, through time.Time, direction logproto.Direction, filter logql.LineFilter) (iter.EntryIterator, error) {
	// If the chunk is already loaded, then use that.
	if c.Chunk.Data != nil {
		// log.Print("chunk iterator requested")
		// log.Print(c.Chunk.ExternalKey())
		lokiChunk := c.Chunk.Data.(*Facade).LokiChunk()
		blocks := lokiChunk.BlocksIterator(ctx, from, through, direction, filter)
		if len(blocks) == 0 {
			return iter.NoopIterator, nil
		}
		its := make([]iter.EntryIterator, 0, len(blocks))

		for i, b := range blocks {
			if c.lastBlock != nil && b.Offset == c.lastblockOffset {
				fcop := *c.lastBlock
				its = append(its, &fcop)
				continue
			}
			if i == len(blocks)-1 {
				// set the last block
				f := blocks[len(blocks)-1]
				c.lastBlock = NewCachedIterator(f.Iterator())
				c.lastblockOffset = f.Offset
				its = append(its, c.lastBlock)
				continue
			}
			its = append(its, b.Iterator())
		}

		iterForward := iter.NewTimeRangedIterator(
			iter.NewNonOverlappingIterator(its, ""),
			from,
			through,
		)

		if direction == logproto.FORWARD {
			return iterForward, nil
		}

		return iter.NewEntryReversedIter(iterForward)

	}

	return nil, errors.New("chunk is not loaded")
}

// CachedIterator is an iterator that caches iteration to be replayed.
type CachedIterator struct {
	cache []*logproto.Entry
	base  iter.EntryIterator

	labels string
	curr   int
}

// NewCachedIterator creates an iterator that cache iteration result and can be iterated again
// after closing it without re-using the underlaying iterator `it`.
func NewCachedIterator(it iter.EntryIterator) *CachedIterator {
	return &CachedIterator{
		base:  it,
		cache: make([]*logproto.Entry, 0, 1024),
		curr:  -1,
	}
}

func (it *CachedIterator) reset() {
	it.curr = -1
}

func (it *CachedIterator) load() {
	if it.base != nil {
		defer func() {
			it.base.Close()
			it.base = nil
		}()
		// set labels using the first entry
		if !it.base.Next() {
			return
		}
		it.labels = it.base.Labels()

		// add all entries until the base iterator is exhausted
		for {
			e := it.base.Entry()
			it.cache = append(it.cache, &e)
			if !it.base.Next() {
				break
			}
		}

	}
}

func (it *CachedIterator) Next() bool {
	it.load()
	if len(it.cache) == 0 {
		it.cache = nil
		return false
	}
	it.curr++
	return it.curr < len(it.cache)
}

func (it *CachedIterator) Entry() logproto.Entry {
	return *it.cache[it.curr]
}

func (it *CachedIterator) Labels() string {
	return it.labels
}

func (it *CachedIterator) Error() error { return nil }

func (it *CachedIterator) Close() error {
	it.reset()
	if it.base != nil {
		return it.base.Close()
	}
	return nil
}
