// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package internal

import (
	"context"
	"sync"
	"time"
)

// SessionThrottler manages the pacing and rate-limiting for creating new sessions.
type SessionThrottler interface {
	// Acquire blocks until a session creation token is available or ctx is done.
	Acquire(ctx context.Context) error
	// Release returns a token back to the throttler, registering a penalty duration hold-off on failure.
	Release(success bool)
	// UpdateConfig swaps the concurrency ceiling and failure-penalty
	// duration at runtime (driven by ClientConfigurationManager polls).
	// New penalty applies to failures observed after the call; already-
	// scheduled hold-offs keep their original expiry.
	UpdateConfig(maxConcurrent int, penalty time.Duration)
	// Snapshot exposes the throttler's current state for the debug UI:
	// InUse is the number of tokens currently held (in-flight creations
	// plus not-yet-expired failure hold-offs), Capacity is the ceiling,
	// PenaltyDuration is the hold-off applied to a failed-creation token.
	Snapshot() ThrottlerSnapshot
}

// ThrottlerSnapshot is the budget state surfaced to the debug UI.
type ThrottlerSnapshot struct {
	InUse           int
	Capacity        int
	PenaltyDuration time.Duration
}

// AdaptiveSessionThrottler is a concurrency governor with adaptive
// failure penalties: a failed OpenSession keeps its slot reserved for
// penaltyDuration before returning it to the pool, so repeated
// failures throttle further attempts. Unlike a chan-based semaphore,
// the counter+slice representation lets UpdateConfig raise or lower
// the ceiling without leaking in-flight callers or orphaning a
// channel.
type AdaptiveSessionThrottler struct {
	mu              sync.Mutex
	cond            *sync.Cond
	maxConcurrent   int
	penaltyDuration time.Duration
	inUse           int         // active creations holding a token
	penalties       []time.Time // sorted-ish expirations of failure hold-offs

	nowFn func() time.Time // injectable for tests
}

// NewAdaptiveSessionThrottler creates a new SessionThrottler implemented by AdaptiveSessionThrottler.
func NewAdaptiveSessionThrottler(maxConcurrent int, penaltyDuration time.Duration) SessionThrottler {
	t := &AdaptiveSessionThrottler{
		maxConcurrent:   maxConcurrent,
		penaltyDuration: penaltyDuration,
		nowFn:           time.Now,
	}
	t.cond = sync.NewCond(&t.mu)
	return t
}

// Acquire blocks until a token is available or ctx is done. Fairness is
// FIFO under sync.Cond broadcast, which is close enough for a creation
// path that fires at most a few times per second.
func (b *AdaptiveSessionThrottler) Acquire(ctx context.Context) error {
	// Fast path + ctx wake: run the wait on a helper goroutine so ctx
	// cancellation can interrupt it via cond.Broadcast. We only spawn
	// the watcher if we actually have to wait.
	b.mu.Lock()
	if b.tryAcquireLocked() {
		b.mu.Unlock()
		return nil
	}

	// Set up ctx-driven wake before dropping the lock so the watcher
	// can't race ahead of our Wait.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			b.mu.Lock()
			b.cond.Broadcast()
			b.mu.Unlock()
		case <-done:
		}
	}()

	for {
		if ctx.Err() != nil {
			b.mu.Unlock()
			return ctx.Err()
		}
		if b.tryAcquireLocked() {
			b.mu.Unlock()
			return nil
		}
		b.cond.Wait()
	}
}

// tryAcquireLocked drains expired penalties, then reserves a slot if
// one is free. Caller holds b.mu.
func (b *AdaptiveSessionThrottler) tryAcquireLocked() bool {
	b.drainPenaltiesLocked()
	if b.inUse+len(b.penalties) >= b.maxConcurrent {
		return false
	}
	b.inUse++
	return true
}

// Release returns a token. On success the slot is freed immediately
// and any parked waiters are woken. On failure the slot is held for
// penaltyDuration (as of Release time), then reclaimed on the next
// Acquire — a time.AfterFunc schedules a broadcast at expiry so a
// waiter parked on cond.Wait is woken exactly when the slot frees,
// not on the next unrelated Release / UpdateConfig event.
func (b *AdaptiveSessionThrottler) Release(success bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.inUse > 0 {
		b.inUse--
	}
	if !success && b.penaltyDuration > 0 {
		b.penalties = append(b.penalties, b.nowFn().Add(b.penaltyDuration))
		// No slot freed yet — schedule the wake for when the penalty
		// expires. Broadcast without holding b.mu (sync.Cond permits it)
		// so the callback doesn't fight the Acquire loop for the lock.
		time.AfterFunc(b.penaltyDuration, b.cond.Broadcast)
		return
	}
	// Slot freed immediately (success) or ceiling grew (UpdateConfig
	// broadcasts on its own path); either way, wake everyone and let
	// the loop re-check under the lock.
	b.cond.Broadcast()
}

// UpdateConfig hot-swaps the ceiling and penalty. Called from
// SessionPoolImpl.UpdateConfig on every config-manager fire.
func (b *AdaptiveSessionThrottler) UpdateConfig(maxConcurrent int, penalty time.Duration) {
	if maxConcurrent <= 0 {
		return
	}
	b.mu.Lock()
	grew := maxConcurrent > b.maxConcurrent
	b.maxConcurrent = maxConcurrent
	if penalty >= 0 {
		b.penaltyDuration = penalty
	}
	if grew {
		b.cond.Broadcast()
	}
	b.mu.Unlock()
}

// Snapshot returns the current state. InUse counts both live creations
// and unexpired failure hold-offs, so the debug page can distinguish
// "budget fully consumed by real work" from "budget burned by penalty
// tokens" by comparing to the live inUse counter (surfaced via
// SessionPoolImpl.Stats().StartingCount, which is the same population
// as inUse minus penalties).
func (b *AdaptiveSessionThrottler) Snapshot() ThrottlerSnapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.drainPenaltiesLocked()
	return ThrottlerSnapshot{
		InUse:           b.inUse + len(b.penalties),
		Capacity:        b.maxConcurrent,
		PenaltyDuration: b.penaltyDuration,
	}
}

func (b *AdaptiveSessionThrottler) drainPenaltiesLocked() {
	if len(b.penalties) == 0 {
		return
	}
	now := b.nowFn()
	kept := b.penalties[:0]
	for _, t := range b.penalties {
		if t.After(now) {
			kept = append(kept, t)
		}
	}
	b.penalties = kept
}
