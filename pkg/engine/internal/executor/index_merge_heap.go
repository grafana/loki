package executor

import (
	"container/heap"
	"context"
	"io"
)

// pileReader is one pile cursor; records must be emitted in non-decreasing
// order according to the comparator passed to mergeHeap.
type pileReader[R any] interface {
	Next(ctx context.Context) (R, error)  // returns io.EOF when exhausted
	Close() error
}

// mergeEntry represents one entry in the merge heap, tracking the record and
// which pile it came from.
type mergeEntry[R any] struct {
	rec  R
	pile int
}

// mergeHeapImpl is the internal heap implementation for the K-way merge.
type mergeHeapImpl[R any] struct {
	entries []mergeEntry[R]
	cmp     func(a, b R) int
}

// Len returns the number of entries in the heap.
func (h *mergeHeapImpl[R]) Len() int { return len(h.entries) }

// Less determines heap ordering. Entries are ordered by the comparator;
// on exact equality, lower pile index wins (stable tie-breaking).
func (h *mergeHeapImpl[R]) Less(i, j int) bool {
	cmpResult := h.cmp(h.entries[i].rec, h.entries[j].rec)
	if cmpResult != 0 {
		return cmpResult < 0
	}
	// Tie-break: lower pile index wins
	return h.entries[i].pile < h.entries[j].pile
}

// Swap exchanges two entries in the heap.
func (h *mergeHeapImpl[R]) Swap(i, j int) {
	h.entries[i], h.entries[j] = h.entries[j], h.entries[i]
}

// Push adds a new entry to the heap (used by heap.Push).
func (h *mergeHeapImpl[R]) Push(x any) {
	h.entries = append(h.entries, x.(mergeEntry[R]))
}

// Pop removes and returns the minimum entry from the heap (used by heap.Pop).
func (h *mergeHeapImpl[R]) Pop() any {
	old := h.entries
	n := len(old)
	x := old[n-1]
	h.entries = old[0 : n-1]
	return x
}

// mergeHeap returns a yield-style iterator over the sorted K-way merge of the
// provided piles. If reduce is non-nil, runs of equal-key records (per cmp)
// are reduced into a single record before being yielded.
//
// mergeHeap takes ownership of the piles: on return (success or error) every
// pile's Close is called.
func mergeHeap[R any](
	ctx context.Context,
	piles []pileReader[R],
	cmp func(a, b R) int,
	reduce func(acc, next R) R,
) func(yield func(R) bool) error {
	return func(yield func(R) bool) error {
		// Check context early
		if err := ctx.Err(); err != nil {
			// Close all piles
			for _, p := range piles {
				_ = p.Close()
			}
			return err
		}

		// Initialize the heap by reading the first record from each pile
		h := &mergeHeapImpl[R]{
			entries: make([]mergeEntry[R], 0, len(piles)),
			cmp:     cmp,
		}

		for i, p := range piles {
			rec, err := p.Next(ctx)
			if err == io.EOF {
				// Pile is empty, skip it
				continue
			}
			if err != nil {
				// Error reading from pile, close all and return error
				for _, pile := range piles {
					_ = pile.Close()
				}
				return err
			}
			h.entries = append(h.entries, mergeEntry[R]{rec: rec, pile: i})
		}

		heap.Init(h)

		// Main merge loop
		for h.Len() > 0 {
			// Check context on each iteration
			if err := ctx.Err(); err != nil {
				for _, p := range piles {
					_ = p.Close()
				}
				return err
			}

			// Pop minimum entry from heap
			entry := heap.Pop(h).(mergeEntry[R])
			currentRec := entry.rec
			currentPile := entry.pile

			// If reduce is set, accumulate all equal-key records
			if reduce != nil {
				for h.Len() > 0 {
					// Peek at the next entry without popping
					nextEntry := h.entries[0]
					if cmp(currentRec, nextEntry.rec) != 0 {
						// Different key, stop reducing
						break
					}

					// Pop and reduce
					heap.Pop(h)
					currentRec = reduce(currentRec, nextEntry.rec)

					// Read next record from the pile we just consumed
					rec, err := piles[nextEntry.pile].Next(ctx)
					if err == io.EOF {
						// Pile exhausted, don't push anything back
						continue
					}
					if err != nil {
						// Error reading, close all and return error
						for _, p := range piles {
							_ = p.Close()
						}
						return err
					}
					// Push the new record back onto the heap
					heap.Push(h, mergeEntry[R]{rec: rec, pile: nextEntry.pile})
				}
			}

			// Yield the current record
			if !yield(currentRec) {
				// Caller stopped iteration
				for _, p := range piles {
					_ = p.Close()
				}
				return nil
			}

			// Read the next record from the pile we just consumed
			rec, err := piles[currentPile].Next(ctx)
			if err == io.EOF {
				// Pile exhausted, don't push anything back
				continue
			}
			if err != nil {
				// Error reading, close all and return error
				for _, p := range piles {
					_ = p.Close()
				}
				return err
			}

			// Push the new record onto the heap
			heap.Push(h, mergeEntry[R]{rec: rec, pile: currentPile})
		}

		// Close all piles
		for _, p := range piles {
			_ = p.Close()
		}

		return nil
	}
}
