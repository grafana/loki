package storage

import (
	"context"
	"errors"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
)

// LazyChunk loads the chunk when it is accessed.
type LazyChunk struct {
	Chunk   chunk.Chunk
	IsValid bool
	Fetcher *chunk.Fetcher

	// cache of overlapping block.
	// We use the offset of the block as key since it's unique per chunk.
	overlappingBlocks       map[int]*cachedIterator
	overlappingSampleBlocks map[int]*cachedSampleIterator
}

// Iterator returns an entry iterator.
// The iterator returned will cache overlapping block's entries with the next chunk if passed.
// This way when we re-use them for ordering across batches we don't re-decompress the data again.
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

	lokiChunk := c.Chunk.Data.(*chunkenc.Facade).LokiChunk()
	blocks := lokiChunk.Blocks(from, through)
	if len(blocks) == 0 {
		return iter.NoopIterator, nil
	}
	its := make([]iter.EntryIterator, 0, len(blocks))

	for _, b := range blocks {
		// if we have already processed and cache block let's use it.
		if cache, ok := c.overlappingBlocks[b.Offset()]; ok {
			clone := *cache
			clone.reset()
			its = append(its, &clone)
			continue
		}
		// if the block is overlapping cache it with the next chunk boundaries.
		if nextChunk != nil && IsBlockOverlapping(b, nextChunk, direction) {
			it := newCachedIterator(b.Iterator(ctx, filter), b.Entries())
			its = append(its, it)
			if c.overlappingBlocks == nil {
				c.overlappingBlocks = make(map[int]*cachedIterator)
			}
			c.overlappingBlocks[b.Offset()] = it
			continue
		}
		if nextChunk != nil {
			delete(c.overlappingBlocks, b.Offset())
		}
		// non-overlapping block with the next chunk are not cached.
		its = append(its, b.Iterator(ctx, filter))
	}

	// build the final iterator bound to the requested time range.
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

// SampleIterator returns an sample iterator.
// The iterator returned will cache overlapping block's entries with the next chunk if passed.
// This way when we re-use them for ordering across batches we don't re-decompress the data again.
func (c *LazyChunk) SampleIterator(
	ctx context.Context,
	from, through time.Time,
	filter logql.LineFilter,
	extractor logql.SampleExtractor,
	nextChunk *LazyChunk,
) (iter.SampleIterator, error) {

	// If the chunk is not already loaded, then error out.
	if c.Chunk.Data == nil {
		return nil, errors.New("chunk is not loaded")
	}

	lokiChunk := c.Chunk.Data.(*chunkenc.Facade).LokiChunk()
	blocks := lokiChunk.Blocks(from, through)
	if len(blocks) == 0 {
		return iter.NoopIterator, nil
	}
	its := make([]iter.SampleIterator, 0, len(blocks))

	for _, b := range blocks {
		// if we have already processed and cache block let's use it.
		if cache, ok := c.overlappingSampleBlocks[b.Offset()]; ok {
			clone := *cache
			clone.reset()
			its = append(its, &clone)
			continue
		}
		// if the block is overlapping cache it with the next chunk boundaries.
		if nextChunk != nil && IsBlockOverlapping(b, nextChunk, logproto.FORWARD) {
			it := newCachedSampleIterator(b.SampleIterator(ctx, filter, extractor), b.Entries())
			its = append(its, it)
			if c.overlappingSampleBlocks == nil {
				c.overlappingSampleBlocks = make(map[int]*cachedSampleIterator)
			}
			c.overlappingSampleBlocks[b.Offset()] = it
			continue
		}
		if nextChunk != nil {
			delete(c.overlappingSampleBlocks, b.Offset())
		}
		// non-overlapping block with the next chunk are not cached.
		its = append(its, b.SampleIterator(ctx, filter, extractor))
	}

	// build the final iterator bound to the requested time range.
	return iter.NewTimeRangedSampleIterator(
		iter.NewNonOverlappingSampleIterator(its, ""),
		from.UnixNano(),
		through.UnixNano(),
	), nil
}

func IsBlockOverlapping(b chunkenc.Block, with *LazyChunk, direction logproto.Direction) bool {
	if direction == logproto.BACKWARD {
		through := int64(with.Chunk.Through) * int64(time.Millisecond)
		if b.MinTime() <= through {
			return true
		}
	} else {
		from := int64(with.Chunk.From) * int64(time.Millisecond)
		if b.MaxTime() >= from {
			return true
		}
	}
	return false
}

func (c *LazyChunk) IsOverlapping(with *LazyChunk, direction logproto.Direction) bool {
	if direction == logproto.BACKWARD {
		if c.Chunk.From.Before(with.Chunk.Through) || c.Chunk.From == with.Chunk.Through {
			return true
		}
	} else {
		if !c.Chunk.Through.Before(with.Chunk.From) {
			return true
		}
	}
	return false
}

// lazyChunks is a slice of lazy chunks that can ordered by chunk boundaries
// in ascending or descending depending on the direction
type lazyChunks struct {
	chunks    []*LazyChunk
	direction logproto.Direction
}

func (l lazyChunks) Len() int         { return len(l.chunks) }
func (l lazyChunks) Swap(i, j int)    { l.chunks[i], l.chunks[j] = l.chunks[j], l.chunks[i] }
func (l lazyChunks) Peek() *LazyChunk { return l.chunks[0] }
func (l lazyChunks) Less(i, j int) bool {
	if l.direction == logproto.FORWARD {
		t1, t2 := l.chunks[i].Chunk.From, l.chunks[j].Chunk.From
		if !t1.Equal(t2) {
			return t1.Before(t2)
		}
		return l.chunks[i].Chunk.Fingerprint < l.chunks[j].Chunk.Fingerprint
	}
	t1, t2 := l.chunks[i].Chunk.Through, l.chunks[j].Chunk.Through
	if !t1.Equal(t2) {
		return t1.After(t2)
	}
	return l.chunks[i].Chunk.Fingerprint > l.chunks[j].Chunk.Fingerprint
}

// pop returns the top `count` lazychunks, the original slice is splitted an copied
// to avoid retaining chunks in the slice backing array.
func (l *lazyChunks) pop(count int) []*LazyChunk {
	if len(l.chunks) <= count {
		old := l.chunks
		l.chunks = nil
		return old
	}
	// split slices into two new ones and copy parts to each so we don't keep old reference
	res := make([]*LazyChunk, count)
	copy(res, l.chunks[0:count])
	new := make([]*LazyChunk, len(l.chunks)-count)
	copy(new, l.chunks[count:len(l.chunks)])
	l.chunks = new
	return res
}
