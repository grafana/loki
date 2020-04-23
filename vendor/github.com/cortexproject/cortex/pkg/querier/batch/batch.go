package batch

import (
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/storage"

	"github.com/cortexproject/cortex/pkg/chunk"
	promchunk "github.com/cortexproject/cortex/pkg/chunk/encoding"
)

// iterator iterates over batches.
type iterator interface {
	// Seek to the batch at (or after) time t.
	Seek(t int64, size int) bool

	// Next moves to the next batch.
	Next(size int) bool

	// AtTime returns the start time of the next batch.  Must only be called after
	// Seek or Next have returned true.
	AtTime() int64

	// Batch returns the current batch.  Must only be called after Seek or Next
	// have returned true.
	Batch() promchunk.Batch

	Err() error
}

// NewChunkMergeIterator returns a storage.SeriesIterator that merges chunks together.
func NewChunkMergeIterator(chunks []chunk.Chunk, _, _ model.Time) storage.SeriesIterator {
	iter := newMergeIterator(chunks)
	return newIteratorAdapter(iter)
}

// iteratorAdapter turns a batchIterator into a storage.SeriesIterator.
// It fetches ever increasing batchSizes (up to promchunk.BatchSize) on each
// call to Next; on calls to Seek, resets batch size to 1.
type iteratorAdapter struct {
	batchSize  int
	curr       promchunk.Batch
	underlying iterator
}

func newIteratorAdapter(underlying iterator) storage.SeriesIterator {
	return &iteratorAdapter{
		batchSize:  1,
		underlying: underlying,
	}
}

// Seek implements storage.SeriesIterator.
func (a *iteratorAdapter) Seek(t int64) bool {
	// Optimisation: see if the seek is within the current batch.
	if a.curr.Length > 0 && t >= a.curr.Timestamps[0] && t <= a.curr.Timestamps[a.curr.Length-1] {
		a.curr.Index = 0
		for a.curr.Index < a.curr.Length && t > a.curr.Timestamps[a.curr.Index] {
			a.curr.Index++
		}
		return true
	}

	a.curr.Length = -1
	a.batchSize = 1
	if a.underlying.Seek(t, a.batchSize) {
		a.curr = a.underlying.Batch()
		return a.curr.Index < a.curr.Length
	}
	return false
}

// Next implements storage.SeriesIterator.
func (a *iteratorAdapter) Next() bool {
	a.curr.Index++
	for a.curr.Index >= a.curr.Length && a.underlying.Next(a.batchSize) {
		a.curr = a.underlying.Batch()
		a.batchSize = a.batchSize * 2
		if a.batchSize > promchunk.BatchSize {
			a.batchSize = promchunk.BatchSize
		}
	}
	return a.curr.Index < a.curr.Length
}

// At implements storage.SeriesIterator.
func (a *iteratorAdapter) At() (int64, float64) {
	return a.curr.Timestamps[a.curr.Index], a.curr.Values[a.curr.Index]
}

// Err implements storage.SeriesIterator.
func (a *iteratorAdapter) Err() error {
	return nil
}
