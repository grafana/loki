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

// Scaling for SessionPoolImpl: the Tick driver, createSession worker,
// scaling-history ring buffer, reason helper, and the deadline-stripping
// context wrapper used for long-lived dials.
//
// Only scale-up is actioned; negative deltas are advisory and the pool
// shrinks passively via OnClose's replace-on-death gate.

package internal

import (
	"context"
	"fmt"
	"log"
	"math/rand/v2"
	"runtime/debug"
	"sync/atomic"
	"time"

	btopt "cloud.google.com/go/bigtable/internal/option"
	"google.golang.org/grpc/metadata"
)

// ScalingEvent is one row in a pool's scaling history. Requested is
// the scale-up delta the sizer asked for (always > 0 in the ring —
// scale-down deltas short-circuit before recordScaling). Actual pool
// growth lags this event: handshakes complete asynchronously.
type ScalingEvent struct {
	At        time.Time
	Before    int
	Requested int
	Launched  int // reserved for future per-action outcome; unused today
	Reason    string

	// Decision is the full sizer trace that produced Requested — every
	// input, intermediate, and the branch taken.
	Decision ScaleDecision
}

// maxScalingHistory ≈ 16 min of history at the 1-sec heartbeat.
const maxScalingHistory = 1024

// recordScaling appends an event to the ring buffer, dropping the oldest
// entry when full.
func (p *SessionPoolImpl) recordScaling(ev ScalingEvent) {
	if !p.debugEnabled {
		return
	}
	p.m.scalingHistoryMu.Lock()
	defer p.m.scalingHistoryMu.Unlock()
	if len(p.m.scalingHistory) >= maxScalingHistory {
		copy(p.m.scalingHistory, p.m.scalingHistory[1:])
		p.m.scalingHistory = p.m.scalingHistory[:len(p.m.scalingHistory)-1]
	}
	p.m.scalingHistory = append(p.m.scalingHistory, ev)
}

// snapshotScalingHistory returns a copy of the ring buffer, oldest first.
func (p *SessionPoolImpl) snapshotScalingHistory() []ScalingEvent {
	p.m.scalingHistoryMu.Lock()
	defer p.m.scalingHistoryMu.Unlock()
	out := make([]ScalingEvent, len(p.m.scalingHistory))
	copy(out, p.m.scalingHistory)
	return out
}

// Tick is the heartbeat/checkout-triggered driver that samples state
// and launches new-session goroutines when the sizer requests growth.
// Negative deltas are logged, not actioned. Stuck-session sweeping is
// on its own loop (startSweepStuckSessionsLoop) at a coarser cadence.
func (p *SessionPoolImpl) Tick(ctx context.Context) {
	p.mu.Lock()
	if p.closed || p.scalingInProgress {
		p.mu.Unlock()
		return
	}
	p.scalingInProgress = true
	p.mu.Unlock()

	defer func() {
		p.mu.Lock()
		p.scalingInProgress = false
		p.mu.Unlock()
	}()

	decision := p.sizer.Decide()
	stats := &PoolStats{
		ReadyCount:    decision.ReadyCount,
		StartingCount: decision.StartingCount,
		InUseCount:    decision.InUseCount,
		PendingCount:  decision.PendingCount,
	}
	delta := decision.Delta

	currentSessions := p.sl.ReadyCount()

	if delta <= 0 {
		return
	}

	reason := scalingReason(stats, delta, decision.MinSessions)
	// Record Requested = delta up-front; per-session outcomes complete
	// asynchronously. Sessions signal readiness via OnActive →
	// signalFree; failures bump tagSessionPoolCreateFailed.
	p.recordScaling(ScalingEvent{
		At:        time.Now(),
		Before:    currentSessions,
		Requested: delta,
		Reason:    reason,
		Decision:  decision,
	})

	// Reserve pendingStarts + register spawns BEFORE launching. Held
	// under p.mu with a re-check of p.closed so Close's Phase 1
	// synchronizes-with p.spawns.Wait — no Add races Wait.
	// pendingStarts guards the sizer double-count; spawns guards
	// teardown so no createSession goroutine outlives Close.
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.pendingStarts += delta
	p.spawns.Add(delta)
	p.mu.Unlock()

	// Fire and forget; readiness signals via OnActive → signalFree.
	// Per-goroutine recover: tickOnce's recover fires BEFORE this
	// goroutine runs (Tick spawns and returns), so an unhandled panic
	// inside third-party code (NewSession / streamFactory / hook wiring)
	// would crash the process. Distinct tag from create_failed so ops
	// can grep panics (client bug) apart from err returns (transient).
	// createSession's `reserved` defer balances pendingStarts on panic.
	for i := 0; i < delta; i++ {
		go func() {
			defer p.spawns.Done()
			defer func() {
				if r := recover(); r != nil {
					recordDebugTag(tagSessionPoolCreatePanic)
					btopt.Debugf(nil, "POOL %s createSession panic recovered: %v\n%s", p.poolName, r, debug.Stack())
				}
			}()
			if err := p.createSession(ctx); err != nil {
				recordDebugTag(tagSessionPoolCreateFailed)
				btopt.Debugf(nil, "POOL %s Tick createSession failed: %v", p.poolName, err)
			}
		}()
	}
}

func (p *SessionPoolImpl) createSession(ctx context.Context) error {
	// Pool-scoped ctx with deadline stripped: a Bidi stream must not
	// inherit a per-request timeout; cancellation still propagates.
	dialCtx := noDeadlineButCancellableContext{Context: p.poolCtx}

	// This goroutine owns one pendingStarts reservation minted by Tick.
	// Any failure before the transfer below releases it; the transfer
	// consumes it atomically with the startingSessions insert.
	reserved := true
	defer func() {
		if reserved {
			p.mu.Lock()
			p.pendingStarts--
			p.mu.Unlock()
		}
	}()

	if err := p.budget.Acquire(dialCtx); err != nil {
		// Distinct debug tag from create_failed so ops can grep the
		// throttled path (budget ceiling exhausted / poolCtx cancel /
		// penalty window elapsed) apart from stream-open errors.
		recordDebugTag(tagSessionPoolNoBudget)
		return fmt.Errorf("failed to acquire session creation budget: %w", err)
	}

	// budgetReleased is the sole gate: the success path calls
	// budget.Release(true) explicitly and sets budgetReleased=true, so
	// any deferred fallback only fires on failure. Passing false is
	// correct because the only way we reach the defer with
	// budgetReleased=false is when streamFactory / cap-gate /
	// Session.Start returned an error.
	budgetReleased := false
	defer func() {
		if !budgetReleased {
			p.budget.Release(false)
		}
	}()

	dialCtxOut := metadata.NewOutgoingContext(dialCtx, p.metadata)
	// pickedChannel receives the channel-pool's connEntry hint; -1
	// means the underlying pool didn't publish one.
	var pickedChannel atomic.Int32
	pickedChannel.Store(-1)
	stream, err := p.streamFactory(ChannelPickHintInto(dialCtxOut, &pickedChannel))
	if err != nil {
		return err
	}

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return fmt.Errorf("session pool is closed")
	}
	if p.sl.ReadyCount() >= p.sizer.MaxSessions() {
		p.mu.Unlock()
		return fmt.Errorf("session pool limit reached")
	}
	// Session log name = {poolID}-{uniqueHex}. Random 32-bit hex tail
	// (not a counter) so long-running pools never overflow; collision
	// odds at N live sessions ≈ N²/2^33 (~1 in 8k at N=1000).
	sessionName := fmt.Sprintf("%d-%08x", p.poolID, rand.Uint32())
	p.mu.Unlock()

	// Mint the SessionHandle BEFORE NewSession so per-session hook
	// closures capture it directly — no Session→SessionHandle back-ref.
	// sh.session / createdAt are backfilled below; closures don't fire
	// until Session.Start runs.
	sh := &SessionHandle{}
	hooks := SessionHooks{
		OnStart:  p.onStart,
		OnActive: func(_ *Session) { p.onActive(sh) },
		OnSlotDrained: func() {
			p.sl.ReleaseToPool(sh)
			p.signalFree()
		},
		OnClosing: func(_ *Session) { p.onClosing(sh) },
		OnClose:   func(_ *Session, err error) { p.onClose(sh, err) },
	}
	s := NewSession(sessionName, stream, hooks, p.sessionType,
		WithSessionPoolName(p.poolName), WithSessionLogger(log.Default()),
		WithSessionDebugEnabled(p.debugEnabled))
	if hint := pickedChannel.Load(); hint >= 0 {
		s.setChannelIndex(hint)
	}

	sh.session = s
	sh.createdAt = time.Now()

	// Transfer pendingStarts → startingSessions in one lock so a
	// concurrent Decide() never sees a gap (both zero) or a double.
	p.mu.Lock()
	p.pendingStarts--
	p.startingSessions[sh] = struct{}{}
	p.mu.Unlock()
	reserved = false

	if err := s.Start(dialCtx, p.openSessionRequest); err != nil {
		p.mu.Lock()
		delete(p.startingSessions, sh)
		p.mu.Unlock()
		btopt.Debugf(nil, "POOL %p createSession Start failed for %s: %v", p, sessionName, err)
		return fmt.Errorf("failed to start session: %w", err)
	}

	// Release budget now so the next scale-up isn't blocked by this
	// session's lifetime (the deferred fallback would otherwise fire
	// only after WaitGoroutines returns, i.e. after the session dies).
	p.budget.Release(true)
	budgetReleased = true

	// Block on WaitGoroutines so this createSession goroutine stays on
	// p.spawns for the session's entire lifetime — Close's Phase-5 then
	// waits for every Session's readLoop / heartbeatLoop to exit before
	// returning.
	s.WaitGoroutines()
	return nil
}

// scalingReason summarizes why the sizer requested a scale delta.
// Pure helper — same text the operator would derive from the log.
func scalingReason(stats *PoolStats, delta int, minSessions int) string {
	if delta > 0 {
		switch {
		case stats == nil:
			return "scale up (no stats)"
		case stats.PendingCount > 0:
			return fmt.Sprintf("pending=%d", stats.PendingCount)
		case stats.ReadyCount+stats.StartingCount < minSessions:
			// Floor-driven (session churn), distinct from load-driven.
			return fmt.Sprintf("below min sessions (ready=%d starting=%d < min=%d)",
				stats.ReadyCount, stats.StartingCount, minSessions)
		case stats.InUseCount > 0 && stats.ReadyCount-stats.InUseCount <= 0:
			return fmt.Sprintf("ready=%d in_use=%d (headroom exhausted)", stats.ReadyCount, stats.InUseCount)
		default:
			return fmt.Sprintf("ready=%d in_use=%d (load>headroom)", stats.ReadyCount, stats.InUseCount)
		}
	}
	if stats == nil {
		return "scale down (no stats)"
	}
	return fmt.Sprintf("scale down: ready=%d in_use=%d", stats.ReadyCount, stats.InUseCount)
}

// noDeadlineButCancellableContext strips deadline (long-lived Bidi
// stream must not inherit a per-request timeout) while preserving
// cancellation, errors, and values from the parent. Built on poolCtx
// so pool teardown unblocks dial / budget / Session.Start loops.
type noDeadlineButCancellableContext struct {
	context.Context
}

func (noDeadlineButCancellableContext) Deadline() (deadline time.Time, ok bool) {
	return time.Time{}, false
}
