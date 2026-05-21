package executor

import (
	"context"
	"errors"
	"io"

	"github.com/grafana/loki/v3/pkg/util/loser"
)

// pileReader is one pile cursor; records must be emitted in non-decreasing
// order according to the comparator passed to mergeHeap.
type pileReader[R any] interface {
	Next(ctx context.Context) (R, error) // returns io.EOF when exhausted
	Close() error
}

// pileSeq adapts a pileReader to loser.Sequence. It buffers one record at a
// time (the current value) plus an optional error from the underlying reader.
// When exhausted (io.EOF or any other error), Next() returns false and the
// loser tree treats this sequence as +infinity via the value-wrapper below.
type pileSeq[R any] struct {
	ctx       context.Context
	reader    pileReader[R]
	pileIdx   int
	cur       R
	err       error
	exhausted bool
}

func (s *pileSeq[R]) Next() bool {
	if s.exhausted {
		return false
	}
	rec, err := s.reader.Next(s.ctx)
	if errors.Is(err, io.EOF) {
		s.exhausted = true
		return false
	}
	if err != nil {
		s.err = err
		s.exhausted = true
		return false
	}
	s.cur = rec
	return true
}

// heapVal wraps a record with an "exhausted" flag so we can express "+infinity"
// to the loser tree without requiring callers to construct a max-value R.
type heapVal[R any] struct {
	rec       R
	pileIdx   int
	exhausted bool
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
		// Initialize sequences by wrapping each pileReader.
		seqs := make([]*pileSeq[R], len(piles))
		for i, p := range piles {
			seqs[i] = &pileSeq[R]{ctx: ctx, reader: p, pileIdx: i}
		}

		// maxVal represents exhaustion (treated as +infinity by the loser tree).
		maxVal := heapVal[R]{exhausted: true}

		// at returns the current value of a sequence.
		at := func(s *pileSeq[R]) heapVal[R] {
			if s.exhausted {
				return maxVal
			}
			return heapVal[R]{rec: s.cur, pileIdx: s.pileIdx, exhausted: false}
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
		closeSeq := func(s *pileSeq[R]) {
			_ = s.reader.Close()
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
			rec := winner.cur

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
			if s.err != nil {
				return s.err
			}
		}

		return nil
	}
}
