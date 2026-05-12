// Package limitedreader provides a shared byte budget for concurrent
// decompressed request bodies.
//
// [Pool.Reserve] pre-acquires the maximum expected size (blocking up to the
// caller's context deadline), then lets callers call [Reservation.AdjustToActual]
// once the true size is known and [Reservation.Release] when done.
package limitedreader

import (
	"context"
	"sync"

	"golang.org/x/sync/semaphore"
)

// Pool tracks the shared byte budget. Construct with [NewPool].
type Pool struct {
	sem    *semaphore.Weighted
	onHeld func(int64) // called with +n on Reserve, −delta on AdjustToActual/Release; may be nil
}

// NewPool returns a Pool with the given byte limit. limit must be > 0.
// onHeld is an optional callback invoked whenever the held-byte count changes.
func NewPool(limit int64, onHeld func(int64)) *Pool {
	return &Pool{sem: semaphore.NewWeighted(limit), onHeld: onHeld}
}

// Reservation holds bytes pre-reserved from a [Pool].
// Use [Pool.Reserve] or [NewNoopReservation] to create one.
// Its methods are safe to call concurrently.
type Reservation struct {
	mu   sync.Mutex
	pool *Pool
	held int64
}

// NewNoopReservation returns a Reservation that does not interact with any Pool.
// All methods are no-ops.
func NewNoopReservation() *Reservation {
	return &Reservation{}
}

// Reserve pre-acquires n bytes, blocking until space is available or ctx expires.
// The caller must eventually call [Reservation.Release].
func (p *Pool) Reserve(ctx context.Context, n int64) (*Reservation, error) {
	if err := p.sem.Acquire(ctx, n); err != nil {
		return nil, err
	}
	if p.onHeld != nil {
		p.onHeld(n)
	}
	return &Reservation{pool: p, held: n}, nil
}

// AdjustToActual releases the over-estimate (held − actual) back to the Pool
// once the true decompressed size is known. No-op if actual ≥ held.
func (r *Reservation) AdjustToActual(actual int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	overage := r.held - actual
	if overage <= 0 {
		return
	}
	if r.pool != nil {
		r.pool.sem.Release(overage)
		if r.pool.onHeld != nil {
			r.pool.onHeld(-overage)
		}
	}
	r.held = actual
}

// Release returns all remaining held bytes to the Pool.
// Safe to call multiple times and without a prior [AdjustToActual].
func (r *Reservation) Release() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.held == 0 {
		return
	}
	if r.pool != nil {
		r.pool.sem.Release(r.held)
		if r.pool.onHeld != nil {
			r.pool.onHeld(-r.held)
		}
	}
	r.held = 0
}
