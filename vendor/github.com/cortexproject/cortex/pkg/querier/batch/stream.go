package batch

import (
	promchunk "github.com/cortexproject/cortex/pkg/chunk/encoding"
)

// batchStream deals with iteratoring through multiple, non-overlapping batches,
// and building new slices of non-overlapping batches.  Designed to be used
// without allocations.
type batchStream []promchunk.Batch

// reset, hasNext, next, atTime etc are all inlined in go1.11.

func (bs *batchStream) reset() {
	for i := range *bs {
		(*bs)[i].Index = 0
	}
}

func (bs *batchStream) hasNext() bool {
	return len(*bs) > 0
}

func (bs *batchStream) next() {
	(*bs)[0].Index++
	if (*bs)[0].Index >= (*bs)[0].Length {
		*bs = (*bs)[1:]
	}
}

func (bs *batchStream) atTime() int64 {
	return (*bs)[0].Timestamps[(*bs)[0].Index]
}

func (bs *batchStream) at() (int64, float64) {
	b := &(*bs)[0]
	return b.Timestamps[b.Index], b.Values[b.Index]
}

func mergeStreams(left, right batchStream, result batchStream, size int) batchStream {
	// Reset the Index and Length of existing batches.
	for i := range result {
		result[i].Index = 0
		result[i].Length = 0
	}
	resultLen := 1 // Number of batches in the final result.
	b := &result[0]

	// This function adds a new batch to the result
	// if the current batch being appended is full.
	checkForFullBatch := func() {
		if b.Index == size {
			// The batch reached it intended size.
			// Add another batch the the result
			// and use it for further appending.

			// The Index is the place at which new sample
			// has to be appended, hence it tells the length.
			b.Length = b.Index
			resultLen++
			if resultLen > len(result) {
				// It is possible that result can grow longer
				// then the one provided.
				result = append(result, promchunk.Batch{})
			}
			b = &result[resultLen-1]
		}
	}

	for left.hasNext() && right.hasNext() {
		checkForFullBatch()
		t1, t2 := left.atTime(), right.atTime()
		if t1 < t2 {
			b.Timestamps[b.Index], b.Values[b.Index] = left.at()
			left.next()
		} else if t1 > t2 {
			b.Timestamps[b.Index], b.Values[b.Index] = right.at()
			right.next()
		} else {
			b.Timestamps[b.Index], b.Values[b.Index] = left.at()
			left.next()
			right.next()
		}
		b.Index++
	}

	// This function adds all the samples from the provided
	// batchStream into the result in the same order.
	addToResult := func(bs batchStream) {
		for ; bs.hasNext(); bs.next() {
			checkForFullBatch()
			b.Timestamps[b.Index], b.Values[b.Index] = bs.at()
			b.Index++
			b.Length++
		}
	}

	addToResult(left)
	addToResult(right)

	// The Index is the place at which new sample
	// has to be appended, hence it tells the length.
	b.Length = b.Index

	// The provided 'result' slice might be bigger
	// than the actual result, hence return the subslice.
	result = result[:resultLen]
	result.reset()
	return result
}
