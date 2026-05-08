// Package limitedreader provides an io.Reader wrapper that enforces a
// shared, global byte budget across many concurrent readers.
//
// It is intended for guarding decompressed request bodies: every byte
// returned to the consumer is held against a single Pool budget until the
// reader is closed. Readers that would push the global total over the
// budget terminate with [ErrBudgetExceeded] instead of blocking.
package limitedreader

import (
	"errors"
	"io"

	"golang.org/x/sync/semaphore"
)

// ErrBudgetExceeded is returned by [Reader.Read] when returning the bytes
// just read from the underlying source would push the Pool's combined
// in-flight bytes over its limit.
var ErrBudgetExceeded = errors.New("limitedreader: global budget exceeded")

// Pool tracks the shared byte budget for a set of [Reader]s. A zero Pool
// is unusable; construct one with [NewPool].
type Pool struct {
	sem *semaphore.Weighted
}

// NewPool returns a Pool that allows up to limit bytes to be in flight
// across all Readers it creates. limit must be > 0.
func NewPool(limit int64) *Pool {
	return &Pool{sem: semaphore.NewWeighted(limit)}
}

// NewReader wraps r so every byte successfully Read is charged against
// the Pool. The caller MUST Close the returned Reader to release its
// charged bytes back to the Pool — otherwise the budget leaks.
func (p *Pool) NewReader(r io.Reader) *Reader {
	return &Reader{pool: p, src: r}
}

// Reader is an io.ReadCloser that charges the bytes it returns against
// a shared [Pool]. It is not safe for concurrent use by multiple
// goroutines, but many Readers from the same Pool may run in parallel.
type Reader struct {
	pool *Pool
	src  io.Reader
	held int64
	err  error
}

// Read reads from the underlying source and charges the bytes returned
// against the Pool. If the Pool has no room for the bytes just read,
// Read discards them, puts the Reader into a permanent error state, and
// returns (0, [ErrBudgetExceeded]).
func (r *Reader) Read(p []byte) (int, error) {
	if r.err != nil {
		return 0, r.err
	}
	n, err := r.src.Read(p)
	if n > 0 {
		if !r.pool.sem.TryAcquire(int64(n)) {
			r.err = ErrBudgetExceeded
			return 0, r.err
		}
		r.held += int64(n)
	}
	if err != nil {
		r.err = err
	}
	return n, err
}

// Close releases all bytes this Reader has charged back to the Pool.
// It is safe to call multiple times.
func (r *Reader) Close() error {
	if r.held > 0 {
		r.pool.sem.Release(r.held)
		r.held = 0
	}
	return nil
}
