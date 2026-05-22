package executor

import (
	"context"

	"github.com/grafana/loki/v3/pkg/util/loser"
)

// pileSequence[R] is one pile cursor for the K-way merge. It satisfies
// loser.Sequence (Next() bool) and exposes the current value via Value().
//
// Records must be emitted in sorted order
type pileSequence[R any] interface {
	// Next advances the cursor. Returns false on exhaustion (natural EOF or
	// any error).
	Next() bool
	// Value returns the current record. Undefined if Next has not been called
	// or if the last Next call returned false.
	Value() R
	// Err returns any error that caused iteration to end. nil on natural EOF
	// (io.EOF from the underlying reader).
	Err() error
	// Close releases any resources held by the sequence. Idempotent.
	Close() error
	// PileIdx returns the pile's index in the merge. Used for stable tiebreak ordering.
	PileIdx() int
}

// heapVal[R] is the loser-tree value type: a snapshot of a pile's
// current record plus its pile index for stable tiebreaks.
//
// isMax marks the +∞ sentinel.
type heapVal[R any] struct {
	rec     R
	pileIdx int
	isMax   bool
}

// mergeHeap returns a yield-style iterator over the sorted K-way merge of the
// provided piles. If reduce is non-nil, runs of equal-key records (per [cmp])
// are reduced into a single record before being yielded.
//
// on return (success or error) every pile's Close is called.
func mergeHeap[R any](
	ctx context.Context,
	piles []pileSequence[R],
	cmp func(a, b R) int,
	reduce func(acc, next R) R,
) func(yield func(R) bool) error {
	return func(yield func(R) bool) error {
		// maxVal is the loser-tree +∞ sentinel.
		maxVal := heapVal[R]{isMax: true}

		at := func(s pileSequence[R]) heapVal[R] {
			return heapVal[R]{rec: s.Value(), pileIdx: s.PileIdx()}
		}

		// less defines the loser tree ordering. The maxVal sentinel sorts after
		// every real record. For real records, order by the provided comparator;
		// break ties by pile index for a stable merge.
		less := func(a, b heapVal[R]) bool {
			if a.isMax {
				return false
			}
			if b.isMax {
				return true
			}
			cmpResult := cmp(a.rec, b.rec)
			if cmpResult != 0 {
				return cmpResult < 0
			}
			// Equal keys: lower pile index wins.
			return a.pileIdx < b.pileIdx
		}

		// closeSeq closes a sequence when the loser tree is done with it.
		closeSeq := func(s pileSequence[R]) {
			_ = s.Close()
		}

		tree := loser.New(piles, maxVal, at, less, closeSeq)
		defer tree.Close()

		var (
			havePending bool
			pending     R
		)

		for tree.Next() {
			if err := ctx.Err(); err != nil {
				return err
			}

			winner := tree.Winner()
			rec := winner.Value()

			if !havePending {
				pending = rec
				havePending = true
				continue
			}

			// If reduce is set and keys are equal, accumulate.
			if reduce != nil && cmp(pending, rec) == 0 {
				pending = reduce(pending, rec)
				continue
			}

			// Yield the pending record and start a new one.
			if !yield(pending) {
				return nil
			}
			pending = rec
			havePending = true
		}

		// Yield any remaining pending record.
		if havePending {
			_ = yield(pending)
		}

		// Check for errors that occurred during iteration.
		for _, p := range piles {
			if p.Err() != nil {
				return p.Err()
			}
		}

		return nil
	}
}
