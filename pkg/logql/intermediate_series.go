package logql

import (
	"context"
	"errors"

	"go.uber.org/atomic"
)

// errIntermediateSeriesLimit is returned by Reserve when a reservation would take the counter
// over its limit.
var errIntermediateSeriesLimit = errors.New("intermediate series limit exceeded")

// intermediateSeriesCounter counts the label sets the range iterators of one job hold while
// they evaluate. Every iterator in the job's evaluator tree shares the same counter, so both
// legs of a binary operation add up. A nil counter counts nothing and never fails.
type intermediateSeriesCounter struct {
	// limit is zero when the counter is only measuring.
	limit   int64
	current atomic.Int64
	peak    atomic.Int64
}

// newIntermediateSeriesCounter returns a counter that fails a reservation taking it over limit.
// A limit of zero means no limit.
func newIntermediateSeriesCounter(limit int) *intermediateSeriesCounter {
	return &intermediateSeriesCounter{limit: int64(limit)}
}

// Reserve adds n label sets to the count and returns errIntermediateSeriesLimit, leaving the
// count unchanged, when that would take it over the limit. The limit is checked before the count
// moves, so a failed reservation is never visible to other goroutines. Callers reserve before
// allocating, so a failed reservation has allocated nothing.
func (c *intermediateSeriesCounter) Reserve(n int) error {
	if c == nil {
		return nil
	}
	var cur int64
	for {
		prev := c.current.Load()
		cur = prev + int64(n)
		if c.limit > 0 && cur > c.limit {
			return errIntermediateSeriesLimit
		}
		if c.current.CompareAndSwap(prev, cur) {
			break
		}
	}
	for {
		peak := c.peak.Load()
		if cur <= peak || c.peak.CompareAndSwap(peak, cur) {
			return nil
		}
	}
}

// Release removes n label sets from the count, because the iterator that held them discarded them.
func (c *intermediateSeriesCounter) Release(n int) {
	if c == nil {
		return
	}
	c.current.Add(-int64(n))
}

// Current returns the number of label sets held now.
func (c *intermediateSeriesCounter) Current() int64 {
	if c == nil {
		return 0
	}
	return c.current.Load()
}

// Peak returns the largest count Current has had.
func (c *intermediateSeriesCounter) Peak() int64 {
	if c == nil {
		return 0
	}
	return c.peak.Load()
}

type intermediateSeriesCounterKey struct{}

// withIntermediateSeriesCounter returns a context that carries c, so every iterator built for
// the job finds the same counter.
func withIntermediateSeriesCounter(ctx context.Context, c *intermediateSeriesCounter) context.Context {
	return context.WithValue(ctx, intermediateSeriesCounterKey{}, c)
}

// intermediateSeriesCounterFromContext returns the counter of the job ctx belongs to, or nil
// when it has none.
func intermediateSeriesCounterFromContext(ctx context.Context) *intermediateSeriesCounter {
	c, _ := ctx.Value(intermediateSeriesCounterKey{}).(*intermediateSeriesCounter)
	return c
}
