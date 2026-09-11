package retry

import (
	"context"
	"math"
	"sync/atomic"
	"time"
)

type state [2]time.Duration

type fibonacciBackoff struct {
	state atomic.Pointer[state]
}

// Fibonacci is a wrapper around Retry that uses a Fibonacci backoff. See
// NewFibonacci.
func Fibonacci(ctx context.Context, base time.Duration, f RetryFunc) error {
	return Do(ctx, NewFibonacci(base), f)
}

// NewFibonacci creates a new Fibonacci backoff using the starting value of
// base. The wait time is the sum of the previous two wait times on each failed
// attempt (1, 1, 2, 3, 5, 8, 13...).
//
// Once it overflows, the function constantly returns the maximum time.Duration
// for a 64-bit integer.
//
// It panics if the given base is less than zero.
func NewFibonacci(base time.Duration) Backoff {
	if base <= 0 {
		panic("base must be greater than 0")
	}

	b := new(fibonacciBackoff)
	b.state.Store(&state{0, base})
	return b
}

// Next implements Backoff. It is safe for concurrent use.
func (b *fibonacciBackoff) Next() (time.Duration, bool) {
	for {
		curr := b.state.Load()
		next := curr[0] + curr[1]

		if next <= 0 {
			return math.MaxInt64, false
		}

		if b.state.CompareAndSwap(curr, &state{curr[1], next}) {
			return next, false
		}
	}
}
