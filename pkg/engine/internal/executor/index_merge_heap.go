package executor

import (
	"context"

	"github.com/grafana/loki/v3/pkg/util/loser"
)

// pileSequence[R] is one pile cursor for the K-way merge. It satisfies
// loser.Sequence (Next() bool) and exposes the current value via Value().
// Implementations must store enough state to:
//   - report the most recently read record via Value()
//   - report any error that ended iteration via Err() (nil on natural EOF)
//   - close their underlying reader via Close()
//   - report the pile's index via PileIdx()
//
// Records must be emitted in non-decreasing order according to the comparator
// passed to mergeHeap.
type pileSequence[R any] interface {
	// Next advances the cursor. Returns false on exhaustion (natural EOF or
	// any error). Subsequent calls to Next continue to return false.
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

// heapVal wraps a record with an "exhausted" flag so we can express "+infinity"
// to the loser tree without requiring callers to construct a max-value R.
type heapVal[R any] struct {
	rec       R
	pileIdx   int
	exhausted bool
}

// seqWrapper wraps a pileWithExhausted to satisfy the loser.Sequence interface.
// The loser tree expects sequences to have a Next() bool method.
type seqWrapper[R any] struct {
	seq       pileSequence[R]
	exhausted bool
}

// Next implements the loser.Sequence interface by delegating to the underlying
// pileSequence and tracking exhaustion state.
func (sw *seqWrapper[R]) Next() bool {
	if sw.exhausted {
		return false
	}
	if !sw.seq.Next() {
		sw.exhausted = true
		return false
	}
	return true
}

// mergeHeap returns a yield-style iterator over the sorted K-way merge of the
// provided piles. If reduce is non-nil, runs of equal-key records (per cmp)
// are reduced into a single record before being yielded.
//
// mergeHeap takes ownership of the piles: on return (success or error) every
// pile's Close is called.
func mergeHeap[R any](
	ctx context.Context,
	piles []pileSequence[R],
	cmp func(a, b R) int,
	reduce func(acc, next R) R,
) func(yield func(R) bool) error {
	return func(yield func(R) bool) error {
		// Wrap piles with seqWrapper to satisfy loser.Sequence interface.
		seqs := make([]*seqWrapper[R], len(piles))
		for i, p := range piles {
			seqs[i] = &seqWrapper[R]{seq: p, exhausted: false}
		}

		// maxVal represents exhaustion (treated as +infinity by the loser tree).
		maxVal := heapVal[R]{exhausted: true}

		// at returns the current value of a pile, or maxVal if exhausted/errored.
		at := func(s *seqWrapper[R]) heapVal[R] {
			if s.exhausted || s.seq.Err() != nil {
				return maxVal
			}
			return heapVal[R]{rec: s.seq.Value(), pileIdx: s.seq.PileIdx(), exhausted: false}
		}

		// less defines the loser tree ordering. Exhausted sequences sort as +infinity
		// (never "less than" anything). For non-exhausted sequences, compare by the
		// provided comparator; on equality, lower pile index wins (stable tiebreak).
		less := func(a, b heapVal[R]) bool {
			if a.exhausted {
				return false
			}
			if b.exhausted {
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
		closeSeq := func(s *seqWrapper[R]) {
			_ = s.seq.Close()
		}

		tree := loser.New(seqs, maxVal, at, less, closeSeq)

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
			rec := winner.seq.Value()

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
		for _, s := range seqs {
			if s.seq.Err() != nil {
				return s.seq.Err()
			}
		}

		return nil
	}
}
