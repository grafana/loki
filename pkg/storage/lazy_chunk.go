package storage

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/go-kit/log/level"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/chunkenc"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/chunk/fetcher"
	"github.com/grafana/loki/v3/pkg/util"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

// LazyChunk loads the chunk when it is accessed.
type LazyChunk struct {
	Chunk   chunk.Chunk
	IsValid bool
	Fetcher *fetcher.Fetcher

	// cache of overlapping block.
	// We use the offset of the block as key since it's unique per chunk.
	overlappingBlocks       map[int]iter.CacheEntryIterator
	overlappingSampleBlocks map[int]iter.CacheSampleIterator
}

// Iterator returns an entry iterator.
// The iterator returned will cache overlapping block's entries with the next chunk if passed.
// This way when we re-use them for ordering across batches we don't re-decompress the data again.
func (c *LazyChunk) Iterator(
	ctx context.Context,
	from, through time.Time,
	direction logproto.Direction,
	pipeline log.StreamPipeline,
	nextChunk *LazyChunk,
	hintRanges queryTimeRanges,
) (iter.EntryIterator, error) {
	// If the chunk is not already loaded, then error out.
	if c.Chunk.Data == nil {
		return nil, errors.New("chunk is not loaded")
	}

	lokiChunk := c.Chunk.Data.(*chunkenc.Facade).LokiChunk()
	blocks := lokiChunk.Blocks(from, through)
	blocks = filterBlocksByHintRanges(blocks, hintRanges)
	if len(blocks) == 0 {
		return iter.NoopEntryIterator, nil
	}
	its := make([]iter.EntryIterator, 0, len(blocks))

	for _, b := range blocks {
		// if we have already processed and cache block let's use it.
		if cache, ok := c.overlappingBlocks[b.Offset()]; ok {
			cache.Reset()
			its = append(its, cache)
			continue
		}
		// if the block is overlapping cache it with the next chunk boundaries.
		if nextChunk != nil && IsBlockOverlapping(b, nextChunk, direction) {
			// todo(cyriltovena) we can avoid to drop the metric name for each chunks since many chunks have the same metric/labelset.
			it := iter.NewCachedIterator(b.Iterator(ctx, pipeline), b.Entries())
			its = append(its, it)
			if c.overlappingBlocks == nil {
				c.overlappingBlocks = make(map[int]iter.CacheEntryIterator)
			}
			c.overlappingBlocks[b.Offset()] = it
			continue
		}
		if nextChunk != nil {
			if cache, ok := c.overlappingBlocks[b.Offset()]; ok {
				delete(c.overlappingBlocks, b.Offset())
				if err := cache.Wrapped().Close(); err != nil {
					level.Warn(util_log.Logger).Log(
						"msg", "failed to close cache block iterator",
						"err", err,
					)
				}
			}
		}
		// non-overlapping block with the next chunk are not cached.
		its = append(its, b.Iterator(ctx, pipeline))
	}

	if direction == logproto.FORWARD {
		result := iter.NewTimeRangedIterator(
			iter.NewNonOverlappingIterator(its),
			from,
			through,
		)
		if hintRanges.enabled {
			result = newHintEntryIterator(result, hintRanges)
		}
		return result, nil
	}
	for i, it := range its {
		r, err := iter.NewEntryReversedIter(
			iter.NewTimeRangedIterator(it,
				from,
				through,
			))
		if err != nil {
			return nil, err
		}
		its[i] = r
	}

	for i, j := 0, len(its)-1; i < j; i, j = i+1, j-1 {
		its[i], its[j] = its[j], its[i]
	}

	result := iter.NewNonOverlappingIterator(its)
	if hintRanges.enabled {
		return newHintEntryIterator(result, hintRanges), nil
	}
	return result, nil
}

// SampleIterator returns an sample iterator.
// The iterator returned will cache overlapping block's entries with the next chunk if passed.
// This way when we re-use them for ordering across batches we don't re-decompress the data again.
func (c *LazyChunk) SampleIterator(
	ctx context.Context,
	from, through time.Time,
	nextChunk *LazyChunk,
	extractor log.StreamSampleExtractor,
	hintRanges queryTimeRanges,
) (iter.SampleIterator, error) {
	// If the chunk is not already loaded, then error out.
	if c.Chunk.Data == nil {
		return nil, errors.New("chunk is not loaded")
	}

	lokiChunk := c.Chunk.Data.(*chunkenc.Facade).LokiChunk()
	blocks := lokiChunk.Blocks(from, through)
	blocks = filterBlocksByHintRanges(blocks, hintRanges)
	if len(blocks) == 0 {
		return iter.NoopSampleIterator, nil
	}
	its := make([]iter.SampleIterator, 0, len(blocks))

	for _, b := range blocks {
		// if we have already processed and cache block let's use it.
		if cache, ok := c.overlappingSampleBlocks[b.Offset()]; ok {
			cache.Reset()
			its = append(its, cache)
			continue
		}
		// if the block is overlapping cache it with the next chunk boundaries.
		if nextChunk != nil && IsBlockOverlapping(b, nextChunk, logproto.FORWARD) {
			// todo(cyriltovena) we can avoid to drop the metric name for each chunks since many chunks have the same metric/labelset.
			it := iter.NewCachedSampleIterator(b.SampleIterator(ctx, extractor), b.Entries())
			its = append(its, it)
			if c.overlappingSampleBlocks == nil {
				c.overlappingSampleBlocks = make(map[int]iter.CacheSampleIterator)
			}
			c.overlappingSampleBlocks[b.Offset()] = it
			continue
		}
		if nextChunk != nil {
			if cache, ok := c.overlappingSampleBlocks[b.Offset()]; ok {
				delete(c.overlappingSampleBlocks, b.Offset())
				if err := cache.Wrapped().Close(); err != nil {
					level.Warn(util_log.Logger).Log(
						"msg", "failed to close cache block sample iterator",
						"err", err,
					)
				}
			}
		}
		// non-overlapping block with the next chunk are not cached.
		its = append(its, b.SampleIterator(ctx, extractor))
	}

	// build the final iterator bound to the requested time range.
	result := iter.NewTimeRangedSampleIterator(
		iter.NewNonOverlappingSampleIterator(its),
		from.UnixNano(),
		through.UnixNano(),
	)
	if hintRanges.enabled {
		result = newHintSampleIterator(result, hintRanges)
	}
	return result, nil
}

type queryTimeRange struct {
	start int64
	end   int64
}

// queryTimeRanges distinguishes an absent hint list from a supplied list whose
// ranges do not overlap the query. An absent or empty list is passthrough; a
// supplied list with no effective ranges matches nothing.
type queryTimeRanges struct {
	enabled bool
	ranges  []queryTimeRange
}

func (r queryTimeRanges) contains(ts int64) bool {
	if !r.enabled {
		return true
	}

	i := sort.Search(len(r.ranges), func(i int) bool {
		return r.ranges[i].end > ts
	})
	return i < len(r.ranges) && r.ranges[i].start <= ts
}

// overlapsClosed reports whether the closed data bounds [from, through]
// intersect any half-open hint range.
func (r queryTimeRanges) overlapsClosed(from, through int64) bool {
	if !r.enabled {
		return true
	}

	i := sort.Search(len(r.ranges), func(i int) bool {
		return r.ranges[i].end > from
	})
	return i < len(r.ranges) && r.ranges[i].start <= through
}

func (r queryTimeRanges) modelBounds() (model.Time, model.Time) {
	from := time.Unix(0, r.ranges[0].start)
	through := time.Unix(0, r.ranges[len(r.ranges)-1].end)
	return util.RoundToMilliseconds(from, through)
}

func filterBlocksByHintRanges(blocks []chunkenc.Block, hintRanges queryTimeRanges) []chunkenc.Block {
	if !hintRanges.enabled {
		return blocks
	}

	filtered := make([]chunkenc.Block, 0, len(blocks))
	for _, block := range blocks {
		if hintRanges.overlapsClosed(block.MinTime(), block.MaxTime()) {
			filtered = append(filtered, block)
		}
	}
	return filtered
}

type hintEntryIterator struct {
	iter.EntryIterator
	hintRanges queryTimeRanges

	entry      logproto.Entry
	labels     string
	streamHash uint64
}

func newHintEntryIterator(it iter.EntryIterator, hintRanges queryTimeRanges) iter.EntryIterator {
	return &hintEntryIterator{
		EntryIterator: it,
		hintRanges:    hintRanges,
	}
}

func (i *hintEntryIterator) Next() bool {
	for i.EntryIterator.Next() {
		entry := i.EntryIterator.At()
		if !i.hintRanges.contains(entry.Timestamp.UnixNano()) {
			continue
		}

		i.entry = entry
		i.labels = i.EntryIterator.Labels()
		i.streamHash = i.EntryIterator.StreamHash()
		return true
	}
	return false
}

func (i *hintEntryIterator) At() logproto.Entry {
	return i.entry
}

func (i *hintEntryIterator) Labels() string {
	return i.labels
}

func (i *hintEntryIterator) StreamHash() uint64 {
	return i.streamHash
}

type hintSampleIterator struct {
	iter.SampleIterator
	hintRanges queryTimeRanges

	sample     logproto.Sample
	labels     string
	streamHash uint64
}

func newHintSampleIterator(it iter.SampleIterator, hintRanges queryTimeRanges) iter.SampleIterator {
	return &hintSampleIterator{
		SampleIterator: it,
		hintRanges:     hintRanges,
	}
}

func (i *hintSampleIterator) Next() bool {
	for i.SampleIterator.Next() {
		sample := i.SampleIterator.At()
		if !i.hintRanges.contains(sample.Timestamp) {
			continue
		}

		i.sample = sample
		i.labels = i.SampleIterator.Labels()
		i.streamHash = i.SampleIterator.StreamHash()
		return true
	}
	return false
}

func (i *hintSampleIterator) At() logproto.Sample {
	return i.sample
}

func (i *hintSampleIterator) Labels() string {
	return i.labels
}

func (i *hintSampleIterator) StreamHash() uint64 {
	return i.streamHash
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
	newChks := make([]*LazyChunk, len(l.chunks)-count)
	copy(newChks, l.chunks[count:len(l.chunks)])
	l.chunks = newChks
	return res
}
