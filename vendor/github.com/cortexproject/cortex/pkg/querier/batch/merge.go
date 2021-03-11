package batch

import (
	"container/heap"
	"sort"

	promchunk "github.com/cortexproject/cortex/pkg/chunk/encoding"
)

type mergeIterator struct {
	its []*nonOverlappingIterator
	h   iteratorHeap

	// Store the current sorted batchStream
	batches batchStream

	// Buffers to merge in.
	batchesBuf   batchStream
	nextBatchBuf [1]promchunk.Batch

	currErr error
}

func newMergeIterator(cs []GenericChunk) *mergeIterator {
	css := partitionChunks(cs)
	its := make([]*nonOverlappingIterator, 0, len(css))
	for _, cs := range css {
		its = append(its, newNonOverlappingIterator(cs))
	}

	c := &mergeIterator{
		its:        its,
		h:          make(iteratorHeap, 0, len(its)),
		batches:    make(batchStream, 0, len(its)*2*promchunk.BatchSize),
		batchesBuf: make(batchStream, len(its)*2*promchunk.BatchSize),
	}

	for _, iter := range c.its {
		if iter.Next(1) {
			c.h = append(c.h, iter)
			continue
		}

		if err := iter.Err(); err != nil {
			c.currErr = err
		}
	}

	heap.Init(&c.h)
	return c
}

func (c *mergeIterator) Seek(t int64, size int) bool {

	// Optimisation to see if the seek is within our current caches batches.
found:
	for len(c.batches) > 0 {
		batch := &c.batches[0]
		if t >= batch.Timestamps[0] && t <= batch.Timestamps[batch.Length-1] {
			batch.Index = 0
			for batch.Index < batch.Length && t > batch.Timestamps[batch.Index] {
				batch.Index++
			}
			break found
		}
		copy(c.batches, c.batches[1:])
		c.batches = c.batches[:len(c.batches)-1]
	}

	// If we didn't find anything in the current set of batches, reset the heap
	// and seek.
	if len(c.batches) == 0 {
		c.h = c.h[:0]
		c.batches = c.batches[:0]

		for _, iter := range c.its {
			if iter.Seek(t, size) {
				c.h = append(c.h, iter)
				continue
			}

			if err := iter.Err(); err != nil {
				c.currErr = err
				return false
			}
		}

		heap.Init(&c.h)
	}

	return c.buildNextBatch(size)
}

func (c *mergeIterator) Next(size int) bool {
	// Pop the last built batch in a way that doesn't extend the slice.
	if len(c.batches) > 0 {
		copy(c.batches, c.batches[1:])
		c.batches = c.batches[:len(c.batches)-1]
	}

	return c.buildNextBatch(size)
}

func (c *mergeIterator) nextBatchEndTime() int64 {
	batch := &c.batches[0]
	return batch.Timestamps[batch.Length-1]
}

func (c *mergeIterator) buildNextBatch(size int) bool {
	// All we need to do is get enough batches that our first batch's last entry
	// is before all iterators next entry.
	for len(c.h) > 0 && (len(c.batches) == 0 || c.nextBatchEndTime() >= c.h[0].AtTime()) {
		c.nextBatchBuf[0] = c.h[0].Batch()
		c.batchesBuf = mergeStreams(c.batches, c.nextBatchBuf[:], c.batchesBuf, size)
		copy(c.batches[:len(c.batchesBuf)], c.batchesBuf)
		c.batches = c.batches[:len(c.batchesBuf)]

		if c.h[0].Next(size) {
			heap.Fix(&c.h, 0)
		} else {
			heap.Pop(&c.h)
		}
	}

	return len(c.batches) > 0
}

func (c *mergeIterator) AtTime() int64 {
	return c.batches[0].Timestamps[0]
}

func (c *mergeIterator) Batch() promchunk.Batch {
	return c.batches[0]
}

func (c *mergeIterator) Err() error {
	return c.currErr
}

type iteratorHeap []iterator

func (h *iteratorHeap) Len() int      { return len(*h) }
func (h *iteratorHeap) Swap(i, j int) { (*h)[i], (*h)[j] = (*h)[j], (*h)[i] }

func (h *iteratorHeap) Less(i, j int) bool {
	iT := (*h)[i].AtTime()
	jT := (*h)[j].AtTime()
	return iT < jT
}

func (h *iteratorHeap) Push(x interface{}) {
	*h = append(*h, x.(iterator))
}

func (h *iteratorHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

// Build a list of lists of non-overlapping chunks.
func partitionChunks(cs []GenericChunk) [][]GenericChunk {
	sort.Sort(byMinTime(cs))

	css := [][]GenericChunk{}
outer:
	for _, c := range cs {
		for i, cs := range css {
			if cs[len(cs)-1].MaxTime < c.MinTime {
				css[i] = append(css[i], c)
				continue outer
			}
		}
		cs := make([]GenericChunk, 0, len(cs)/(len(css)+1))
		cs = append(cs, c)
		css = append(css, cs)
	}

	return css
}

type byMinTime []GenericChunk

func (b byMinTime) Len() int           { return len(b) }
func (b byMinTime) Swap(i, j int)      { b[i], b[j] = b[j], b[i] }
func (b byMinTime) Less(i, j int) bool { return b[i].MinTime < b[j].MinTime }
