package chunkenc

import (
	"context"
	"errors"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
)

// LazyChunk loads the chunk when it is accessed.
type LazyChunk struct {
	Chunk   chunk.Chunk
	IsValid bool
	Fetcher *chunk.Fetcher

	overlappingBlocks map[int]*CachedIterator
}

// Iterator returns an entry iterator.
func (c *LazyChunk) Iterator(
	ctx context.Context,
	from, through time.Time,
	direction logproto.Direction,
	filter logql.LineFilter,
	nextChunk *LazyChunk,
) (iter.EntryIterator, error) {
	// If the chunk is not already loaded, then error out.
	if c.Chunk.Data == nil {
		return nil, errors.New("chunk is not loaded")
	}

	lokiChunk := c.Chunk.Data.(*Facade).LokiChunk()
	blocks := lokiChunk.BlocksIterator(ctx, from, through, direction, filter)
	if len(blocks) == 0 {
		return iter.NoopIterator, nil
	}
	its := make([]iter.EntryIterator, 0, len(blocks))

	for _, b := range blocks {
		// if we already processed that block let's use it.
		if cache, ok := c.overlappingBlocks[b.Offset]; ok {
			cache.reset()
			its = append(its, cache)
			continue
		}
		// if the block is overlapping cache it.
		if nextChunk != nil && b.IsOverlapping(nextChunk.Chunk.From, nextChunk.Chunk.Through, direction) {
			it := NewCachedIterator(b.Iterator(), b.NumEntries)
			its = append(its, it)
			if c.overlappingBlocks == nil {
				c.overlappingBlocks = make(map[int]*CachedIterator)
			}
			c.overlappingBlocks[b.Offset] = it
			continue
		}
		delete(c.overlappingBlocks, b.Offset)
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

func (b Block) IsOverlapping(from, through model.Time, direction logproto.Direction) bool {
	if direction == logproto.BACKWARD {
		if b.Mint < through.UnixNano() || b.Mint == through.UnixNano() {
			return true
		}
	} else {
		if !(b.Maxt < from.UnixNano()) {
			return true
		}
	}
	return false
}

func (c *LazyChunk) IsOverlapping(from, through model.Time, direction logproto.Direction) bool {
	if direction == logproto.BACKWARD {
		if c.Chunk.From.Before(through) || c.Chunk.From == through {
			return true
		}
	} else {
		if !c.Chunk.Through.Before(from) {
			return true
		}
	}
	return false
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
func NewCachedIterator(it iter.EntryIterator, maxEntries int) *CachedIterator {
	c := &CachedIterator{
		base:  it,
		cache: make([]*logproto.Entry, 0, maxEntries),
		curr:  -1,
	}
	c.load()
	return c
}

func (it *CachedIterator) reset() {
	it.curr = -1
}

func (it *CachedIterator) load() {
	if it.base != nil {
		defer func() {
			_ = it.base.Close()
			it.base = nil
			it.reset()
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
	if len(it.cache) == 0 {
		it.cache = nil
		return false
	}
	it.curr++
	return it.curr < len(it.cache)
}

func (it *CachedIterator) Entry() logproto.Entry {
	if it.curr < 0 {
		it.curr = 0
	}
	return *it.cache[it.curr]
}

func (it *CachedIterator) Labels() string {
	return it.labels
}

func (it *CachedIterator) Error() error { return nil }

func (it *CachedIterator) Close() error {
	it.reset()
	return nil
}
