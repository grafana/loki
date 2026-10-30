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

// SessionPoolImpl lifecycle: session hooks (onStart/onActive/onClosing/
// onClose), Close/teardown, close-reason ledger, and the background
// maintenance ticker that drives uptime sampling and stuck-session sweeping.

package internal

import (
	"context"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	spb "cloud.google.com/go/bigtable/apiv2/bigtablepb"
	btopt "cloud.google.com/go/bigtable/internal/option"
)

// waitServerCloseGrace bounds StateWaitServerClose before the pool
// force-closes the session. 5 min is long enough that a slow-but-alive
// server still draining in-flight state after acknowledging
// CloseSession is not force-closed as collateral damage (typical
// server-side drain is bounded by CloseSession's own reply timeout,
// well under a minute); a session still stuck past 5 min is almost
// certainly a hung server or lost stream and needs reclamation.
const waitServerCloseGrace = 5 * time.Minute

// sweepStuckSessionsInterval is the cadence for the WSC-stuck sweeper.
// Decoupled from waitServerCloseGrace so worst-case detection is
// grace + interval (~5m30s) instead of 2×grace (10 min) — no reason
// to make the loop wake up as slowly as the grace itself, since the
// per-iteration work is just a stateless walk of sl.AllHandles.
const sweepStuckSessionsInterval = 30 * time.Second

// sampleActiveUptimes records each Ready session's current age into the
// session.uptime histogram. Runs without the pool lock so tracer work
// never blocks CheckoutSession / OnClose.
func (p *SessionPoolImpl) sampleActiveUptimes(ctx context.Context) {
	handles := p.sl.AllHandles()
	for _, sh := range handles {
		if sh == nil || sh.session == nil {
			continue
		}
		if State(sh.session.state.Load()) != StateReady {
			continue
		}
		sh.session.SampleUptime(ctx)
	}
}

// sweepStuckSessions force-closes sessions parked in StateWaitServerClose
// beyond waitServerCloseGrace. Driven by startSweepStuckSessionsLoop;
// ForceClose calls fire outside the pool lock.
func (p *SessionPoolImpl) sweepStuckSessions() {
	type victim struct {
		sess     *Session
		stuckFor time.Duration
	}
	var victims []victim

	for _, sh := range p.sl.AllHandles() {
		if sh == nil || sh.session == nil {
			continue
		}
		stuck := State(sh.session.state.Load()) == StateWaitServerClose
		since := time.Since(time.Unix(0, sh.session.lastStateChangeNano.Load()))
		if stuck && since > waitServerCloseGrace {
			victims = append(victims, victim{sess: sh.session, stuckFor: since})
		}
	}

	for _, v := range victims {
		// One tag per swept victim — the count is the "stuck sessions
		// per minute" gauge.
		recordDebugTag(tagSessionPoolStuckSessionSwept)
		btopt.Debugf(nil, "POOL %s sweepStuckSessions: force-closing %s stuck in WaitServerClose for %v",
			p.poolName, v.sess.LogName(), v.stuckFor.Round(time.Second))
		v.sess.ForceClose(&spb.CloseSessionRequest{
			Reason:      spb.CloseSessionRequest_CLOSE_SESSION_REASON_ERROR,
			Description: "stuck in WaitServerClose past grace",
		})
	}
}

// bumpCloseReason atomically increments the close-reason counter; the map
// is keyed by label so the set of reasons can grow without struct churn.
// Load-first, LoadOrStore only on miss — avoids allocating a fresh
// atomic.Int64 on every hit-path call (close-reason bumps are frequent).
func (p *SessionPoolImpl) bumpCloseReason(label string) {
	if label == "" {
		label = "Unspecified"
	}
	c, ok := p.m.closesByReason.Load(label)
	if !ok {
		c, _ = p.m.closesByReason.LoadOrStore(label, new(atomic.Int64))
	}
	c.(*atomic.Int64).Add(1)
}

// recordSessionClose marks a session as retired exactly once and bumps
// sessionsClosed + the close-reason histogram. Once-flag lives on
// Session so it dedupes across every removal site. fallbackReason is
// used when the session hasn't recorded its own reason yet.
func (p *SessionPoolImpl) recordSessionClose(s *Session, fallbackReason string) {
	if s == nil {
		return
	}
	if !s.poolCloseRecorded.CompareAndSwap(false, true) {
		return
	}
	reason := s.CloseReason()
	if reason == "" {
		reason = fallbackReason
	}
	p.m.sessionsClosed.Add(1)
	p.bumpCloseReason(reason)
}

// bumpStartingClose records the close for sessions that died before
// reaching Ready — they never fire onActive, so onClose's starting
// branch is the only close signal.
func (p *SessionPoolImpl) bumpStartingClose(s *Session) {
	p.recordSessionClose(s, "FailedToStart")
}

// snapshotCloseReasons returns the per-reason counts as a flat map.
func (p *SessionPoolImpl) snapshotCloseReasons() map[string]int64 {
	out := map[string]int64{}
	p.m.closesByReason.Range(func(k, v interface{}) bool {
		out[k.(string)] = v.(*atomic.Int64).Load()
		return true
	})
	return out
}

// Close gracefully closes all active sessions in the pool, bounded by
// a 30s timeout. Sessions close concurrently; Close blocks until every
// per-session graceful Close returns (or the bounded ctx fires). Only
// after the WaitGroup completes do we cancel poolCtx, which tears down
// any remaining session goroutines.
//
// Close is the ONLY teardown entry point. When it returns, every
// goroutine the pool owns has been cancelled and the pool is
// unreachable — sessions and createSession workers are joined outright,
// maintenance loops are cancelled and unwind on their own. That is what
// lets session.TableCache evict pools without leaking them.
func (p *SessionPoolImpl) Close() error {
	// Phase 1: mark closed so no new sessions are admitted, and stop the
	// background maintenance loops. maintCancel runs here rather than
	// riding on Phase 4's poolCancel so no prune / sweep / metrics tick
	// fires against a pool that's mid-teardown.
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	p.mu.Unlock()
	p.maintCancel()

	// Drain parked waiters with ErrPoolClosed. Without this, long-poll
	// callers with context.Background hang past Close: only ctx-cancel
	// or an onSlotDrained wake unblocks them, and cancelActiveRPCs
	// intentionally skips onSlotDrained on teardown paths.
	p.drainWaitersWithErr(ErrPoolClosed)

	// Snapshot AFTER marking closed so any onActive races either see
	// p.closed (and ForceClose without registering) or land in sl in
	// time to be caught here.
	snapshot := p.sl.AllHandles()

	// Pre-flip closingRecorded / closeRecorded via CAS so a mid-flight
	// onClosing (GOAWAY / heartbeat trip) that landed after AllHandles
	// but before this loop can't double-count via its own CAS at
	// onClosing. closingRecorded is load-bearing (recordLifetime has
	// no inner guard); closeRecorded is symmetric — recordSessionClose
	// already dedupes internally, but keeping the CAS reads uniform.
	// Also drops handles from sl so a picker can't hand back a retired
	// session before Phase-2 tears them down.
	for _, sh := range snapshot {
		if sh == nil || sh.session == nil {
			continue
		}
		if sh.closingRecorded.CompareAndSwap(false, true) && !sh.createdAt.IsZero() {
			p.recordLifetime(time.Since(sh.createdAt))
		}
		if sh.closeRecorded.CompareAndSwap(false, true) {
			p.recordSessionClose(sh.session, "PoolClose")
		}
		p.sl.OnSessionClosed(sh)
	}

	// Phase 2: kick off graceful Close on every session under a bounded
	// ctx derived from p.poolCtx (not context.Background — this pool
	// scopes its lifetime to poolCtx). Safe because Phase 4's
	// poolCancel runs AFTER Phase 3's wg.Wait returns, so Phase 2's
	// session Close goroutines finish before poolCtx is cancelled.
	closeCtx, cancel := context.WithTimeout(p.poolCtx, 30*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for _, sh := range snapshot {
		if sh.session == nil {
			continue
		}
		wg.Add(1)
		go func(s *Session) {
			defer wg.Done()
			s.Close(closeCtx, &spb.CloseSessionRequest{
				Reason:      spb.CloseSessionRequest_CLOSE_SESSION_REASON_USER,
				Description: "graceful pool teardown",
			})
		}(sh.session)
	}

	// Phase 3: wait for graceful closes. Session.Close selects on its
	// ctx and ForceCloses on expiry, so the WaitGroup unblocks either way.
	wg.Wait()
	if closeCtx.Err() != nil {
		recordDebugTag(tagSessionPoolDrainTimeout)
	}

	// Phase 4: cancel poolCtx to bring down any lingering session
	// goroutines (readLoop / heartbeatLoop supervisors).
	if p.poolCancel != nil {
		p.poolCancel()
	}

	// Phase 5: wait for every createSession worker (Tick-spawned). Their
	// error paths touch package-level metric state, so without this wait
	// they can outlive Close and race the next test's metrics init.
	p.spawns.Wait()

	// The maintenance loops cancelled in Phase 1 are deliberately NOT
	// waited on. They park on a select, so cancel releases the pool
	// immediately; joining them would only close a microsecond-wide
	// window in which an already-running tick body finishes after Close
	// returns. Not worth blocking for — Close runs inline on the
	// TableCache sweeper (sweepOnce closes evicted handles serially), so
	// a wedged tick body would stall eviction for every table.

	// Phase 6: wait for every Session's goroutines to unwind. Re-snapshot
	// AFTER spawns.Wait to catch sessions that reached sl during the tail
	// of Phase 5. Sessions still in startingSessions never entered sl and
	// are already accounted for via Phase 5.
	for _, sh := range p.sl.AllHandles() {
		if sh != nil && sh.session != nil {
			sh.session.WaitGoroutines()
		}
	}
	return nil
}

// onStart is a no-op — retained for SessionHooks shape symmetry.
func (p *SessionPoolImpl) onStart(ctx context.Context) {}

// onActive publishes a newly-started SessionHandle into sl and clears
// its starting-set entry. The activated CAS makes this safe to invoke
// twice; production wires each closure exactly once per Session.
func (p *SessionPoolImpl) onActive(sh *SessionHandle) {
	if !sh.activated.CompareAndSwap(false, true) {
		return
	}
	// Keep work here allocation-only / atomic-only. Anything blocking
	// would deadlock the read loop and heartbeat scheduler that contend
	// for p.mu. (SessionHooks: "hooks must not block.")
	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.startingSessions, sh)

	if p.closed {
		// Dispatch async so we release p.mu before the onClose chain
		// re-acquires it. Race window: onActive can land ~30s after
		// Close set p.closed=true. Track on p.spawns so Phase-5 catches
		// this goroutine; safe to Add(1) after p.closed=true because
		// createSession's own spawns entry for this session is still
		// pending in WaitGoroutines — the counter stays > 0.
		p.spawns.Add(1)
		go func() {
			defer p.spawns.Done()
			sh.session.ForceClose(&spb.CloseSessionRequest{
				Reason:      spb.CloseSessionRequest_CLOSE_SESSION_REASON_ERROR,
				Description: "pool closed before session became active",
			})
		}()
		return
	}

	p.m.sessionsOpened.Add(1)

	// A successful session-open signals sustained transport health;
	// clear the consecutive-failure counter. Reset lives here (not on
	// per-vRPC OK) so a long-lived healthy session can't mask a run
	// of failing opens. Under sustained failures no session reaches
	// this point, so the counter grows unimpeded toward the trip.
	p.consecutiveFailures.Store(0)
	p.lastAbnormalCloseErr.Store(nil)

	// PeerInfo is guaranteed populated: handleOpenSession parses it
	// synchronously before firing onActive.
	p.sl.OnSessionStarted(sh)

	// New session is immediately idle — wake any parked waiter.
	p.signalFree()
}

// onClosing fires on the FIRST transition out of Ready (handleGoAway,
// Close, ForceClose, or handleClose). Drops the session from the
// picker's idle queue and the scale-up gate so replacement can start
// before teardown completes; refCount keeps in-flight vRPCs alive
// until onClose finalizes.
func (p *SessionPoolImpl) onClosing(sh *SessionHandle) {
	// Still-starting sessions never reached sl; they exit via onClose's
	// bumpStartingClose path.
	p.mu.Lock()
	_, starting := p.startingSessions[sh]
	p.mu.Unlock()
	if starting {
		return
	}
	if !sh.closingRecorded.CompareAndSwap(false, true) {
		return // Pool.Close Phase 1 already recorded.
	}
	if !sh.createdAt.IsZero() {
		p.recordLifetime(time.Since(sh.createdAt))
	}

	p.sl.OnSessionClosing(sh)

	if p.sl.ReadyCount() < p.sizer.MaxSessions() {
		p.spawnTickOnce(p.poolCtx)
	}
}

// onClose fires when the stream has actually closed. onClosing has
// already dropped the session from sl.readyCount and the picker's idle
// queue; this callback finalizes by dropping the AFE handle and
// recording the close reason. Pool.Close's Phase 1 pre-flips
// sh.closeRecorded so pool-driven closes short-circuit here.
func (p *SessionPoolImpl) onClose(sh *SessionHandle, err error) {
	p.mu.Lock()
	if _, starting := p.startingSessions[sh]; starting {
		delete(p.startingSessions, sh)
		p.mu.Unlock()
		p.bumpStartingClose(sh.session)
		p.noteAbnormalCloseIfAny(sh)
		// Re-drive creation after a session died before ever reaching
		// Ready. This is the load-bearing fix for the deadline-less
		// CheckoutSession hang: a session that dies in Starting fires
		// neither onClosing's replace-on-death kick (it early-returns for
		// starting handles) nor any other of the sizer's event-driven wake
		// sites, so without this kick a cold-start cohort that all fails in
		// Starting stalls — the consecutive-failure counter freezes below
		// ConsecutiveSessionFailureThreshold (a MinSessionCount cohort is
		// smaller than the threshold), the breaker never trips, and a caller
		// parked on a deadline-less context waits forever. Kicking here lets
		// the pool keep working toward its floor: each re-driven cohort
		// fails again, the counter climbs to the threshold, and the breaker
		// trips and drains waiters with ErrConsecutiveFailures carrying the
		// underlying cause (e.g. Unimplemented). Paced by the throttler's
		// failure penalty so this is not a tight retry loop. Gate mirrors
		// onClosing; spawnTickOnce re-checks p.closed under p.mu.
		if p.sl.ReadyCount() < p.sizer.MaxSessions() {
			p.spawnTickOnce(p.poolCtx)
		}
		return
	}
	p.mu.Unlock()

	if !sh.closeRecorded.CompareAndSwap(false, true) {
		// Pool.Close Phase 1 already recorded. Still safe to bump the
		// consecutive-failure counter — Session.OnClose is once-guarded,
		// so this method fires at most once per session.
		p.noteAbnormalCloseIfAny(sh)
		return
	}
	p.sl.OnSessionClosed(sh)
	p.recordSessionClose(sh.session, "")
	p.noteAbnormalCloseIfAny(sh)
}

// noteAbnormalCloseIfAny bumps the consecutive-failure counter when a
// session's terminal transition did NOT come through StateWaitServerClose.
// Classification is state-based via Session.prevStateAtClose, captured
// at the two transitionTo(StateClosed, …) call sites — no reason-string
// whitelist to keep in lockstep with new close reasons; the
// state-transition history is the source of truth. A clean shutdown
// (Close() → WSC → server ack → Closed) skips the counter; a
// server-initiated GoAway / heartbeat trip / stream error on a Ready
// session counts. Also emits `tagSessionAbnormalClose` on the counted
// path so operators can see per-abnormal-close volume in debug-tag
// counters. Crossing the threshold drains every parked waiter with
// ErrConsecutiveFailures and resets. CAS on reset guards against two
// goroutines double-draining.
func (p *SessionPoolImpl) noteAbnormalCloseIfAny(sh *SessionHandle) {
	// Defensive nil-guard: production callers always pass a live sh
	// with sh.session backfilled (createSession sets it before wiring
	// the hook closures at session_pool_scaling.go:248). Kept so a
	// future test double / injection path can't NPE the whole close
	// path — the counter simply doesn't advance on a nil handle.
	if sh == nil || sh.session == nil {
		return
	}
	// State-history gate: skip the trip counter only when the session's
	// state immediately before Closed was StateWaitServerClose — i.e.,
	// the client-initiated clean-close path (Close() sent CloseSession,
	// server acked, handleClose completed WSC → Closed). Every other
	// terminal transition counts:
	//   - never activated → open failure (prev = Starting)
	//   - activated then server GoAway / heartbeat miss / stream error
	//     → prev = Closing (transport failure worth surfacing)
	//   - sweep of stuck WSC session via ForceClose → prev = WSC
	//     already, so those stay exempt.
	if State(sh.session.prevStateAtClose.Load()) == StateWaitServerClose {
		return
	}
	recordDebugTag(tagSessionAbnormalClose)
	s := sh.session
	if e := s.closeError(); e != nil {
		p.lastAbnormalCloseErr.Store(&e)
	}
	n := p.consecutiveFailures.Add(1)
	threshold := p.consecutiveFailureThreshold.Load()
	if threshold <= 0 || n < threshold {
		return
	}
	// Snapshot the poison BEFORE the CAS-reset so a concurrent abnormal
	// close arriving in the reset→drain gap can't swap the "last error"
	// out from under the waiter drain — trip attribution stays pinned
	// to the close that actually crossed the threshold.
	tripErr := p.consecutiveFailureErr()
	// TODO: reconsider the CAS-reset. Alternative semantics: leave the
	// counter elevated until a fresh session-open (onActive) clears it,
	// so a burst of abnormal closes that keeps arriving after a trip
	// re-drains any newly parked waiters instead of building up a
	// second budget.
	if !p.consecutiveFailures.CompareAndSwap(n, 0) {
		return
	}
	woken := p.drainWaitersWithErr(tripErr)
	if woken > 0 {
		recordDebugTag(tagSessionPoolConsecutiveFailuresTripped)
		// Unimplemented-driven trips are handled by the routing layer:
		// TableShim reads status.Code on consecutiveFailureError
		// (GRPCStatus inherits the underlying code) and flips its
		// sticky per-resource breaker. Pool only surfaces the cause.
	}
}

// consecutiveFailureErr builds the error handed to parked waiters on a
// breaker trip. When a last abnormal-close error is captured, returns a
// *consecutiveFailureError so that (a) errors.Is against
// ErrConsecutiveFailures still holds and (b) status.Code inherits the
// underlying cause's gRPC code (e.g. FailedPrecondition when the server
// rejected OpenSession because the resource is still being created).
// Falls back to the bare sentinel when no cause is available.
func (p *SessionPoolImpl) consecutiveFailureErr() error {
	last := p.lastAbnormalCloseErr.Load()
	if last == nil {
		return ErrConsecutiveFailures
	}
	return &consecutiveFailureError{inner: *last}
}

// slowMetricsInterval is the cadence for periodic metric-recorder
// bodies whose per-second firing was wasteful: sampleActiveUptimes
// (session.uptime OTel histogram) and recordTimeSeries (in-memory
// series for sessionz). Matches java-bigtable's
// scheduleAtFixedRate(recordAsyncSessionMetrics, 1, 1, MINUTES) at
// MetricsImpl.java:193 — session.uptime is a periodic snapshot
// metric, not a per-second stream.
const slowMetricsInterval = 1 * time.Minute

// Start brings the pool up: fires a pre-start Tick to seed min-sessions,
// then runs the AFE prune loop, WaitServerClose sweep loop, and 1-min
// slow-metrics recorder. Non-blocking; idempotent via startOnce.
//
// There is deliberately no periodic Tick watchdog — sizing decisions
// are driven by events at the four sites that meaningfully change
// pool state:
//
//   - onActive → signalFree                    (session became Ready)
//   - onClosing → spawnTickOnce                (Ready→closing transition)
//   - CheckoutSession → spawnTickOnce          (waiter park on empty)
//   - UpdateConfig → spawnTickOnce             (server-driven min/max bump)
//
// The pre-start Tick above (line: p.spawnTickOnce) covers the
// cold-start MinSessions seed; everything after is event-driven.
// Matches java-bigtable's fully event-driven poolSizer (no periodic
// timer for sizing).
//
// Start takes no ctx on purpose. Everything it launches is scoped to
// the pool's own poolCtx / maintCtx, so Close alone is sufficient to
// unwind it. Threading a caller ctx here previously outlived Close
// (which cancels poolCtx, not the caller's) and leaked the loops.
func (p *SessionPoolImpl) Start() {
	p.startOnce.Do(func() {
		p.spawnTickOnce(p.poolCtx)
		p.startAfePruneLoop()
		p.startSweepStuckSessionsLoop()
		p.startSlowMetricsLoop()
	})
}

// startMaintenanceLoop starts one ticker-driven maintenance goroutine
// scoped to p.maintCtx, which Close cancels in Phase 1. No closed-check
// is needed: a Start racing Close either sees a live maintCtx and is
// cancelled a moment later, or sees a cancelled one and exits on the
// first select.
func (p *SessionPoolImpl) startMaintenanceLoop(interval time.Duration, fn func()) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-p.maintCtx.Done():
				return
			case <-ticker.C:
				fn()
			}
		}
	}()
}

// startSlowMetricsLoop runs recordTimeSeries + sampleActiveUptimes
// every slowMetricsInterval until the pool closes. Decoupled from Tick
// so Tick's per-second cadence doesn't drag ~60× extra OTel histogram
// writes onto sessionUptime.
func (p *SessionPoolImpl) startSlowMetricsLoop() {
	p.startMaintenanceLoop(slowMetricsInterval, func() {
		p.recordTimeSeries()
		p.sampleActiveUptimes(p.maintCtx)
	})
}

// tickOnce runs one Tick with panic recovery + a debounce gate. The
// tickPending CAS coalesces concurrent invocations to at most one
// active Tick body so bursts of empty-pool kicks don't repeatedly
// enter the sizer decision path before scalingInProgress rejects
// them.
func (p *SessionPoolImpl) tickOnce(ctx context.Context) {
	if !p.tickPending.CompareAndSwap(false, true) {
		return
	}
	defer p.tickPending.Store(false)
	defer func() {
		if r := recover(); r != nil {
			btopt.Debugf(nil, "POOL %s Tick panic recovered: %v\n%s", p.poolName, r, debug.Stack())
		}
	}()
	p.Tick(ctx)
}

// spawnTickOnce is the guarded `go p.tickOnce(ctx)` used at every kick
// site. Re-checks p.closed and bumps p.spawns under p.mu so an Add
// either lands before Close's Phase-1 Lock or is skipped — kicks
// spawned during Close can't leak past p.spawns.Wait.
func (p *SessionPoolImpl) spawnTickOnce(ctx context.Context) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.spawns.Add(1)
	p.mu.Unlock()
	go func() {
		defer p.spawns.Done()
		p.tickOnce(ctx)
	}()
}

// startAfePruneLoop runs sl.Prune on afePruneMaxIdle cadence until the
// pool closes — deliberately OFF the tickInterval so sl.mu held during
// the map walk can't contend with serving-path Checkouts.
func (p *SessionPoolImpl) startAfePruneLoop() {
	p.startMaintenanceLoop(afePruneMaxIdle, p.pruneOnce)
}

// pruneOnce runs one sl.Prune with panic recovery per iteration.
func (p *SessionPoolImpl) pruneOnce() {
	defer func() {
		if r := recover(); r != nil {
			btopt.Debugf(nil, "POOL %s AFE prune panic recovered: %v\n%s", p.poolName, r, debug.Stack())
		}
	}()
	p.sl.Prune(time.Now())
}

// startSweepStuckSessionsLoop runs sweepStuckSessions on
// sweepStuckSessionsInterval cadence until the pool closes.
// Deliberately OFF the 1s Tick — a stuck WaitServerClose session only
// becomes actionable after waitServerCloseGrace has elapsed, so
// checking more often just burns CPU walking sl.AllHandles. Worst-case
// detection is waitServerCloseGrace + sweepStuckSessionsInterval.
func (p *SessionPoolImpl) startSweepStuckSessionsLoop() {
	p.startMaintenanceLoop(sweepStuckSessionsInterval, p.sweepStuckSessionsOnce)
}

// sweepStuckSessionsOnce runs one sweepStuckSessions with panic
// recovery per iteration.
func (p *SessionPoolImpl) sweepStuckSessionsOnce() {
	defer func() {
		if r := recover(); r != nil {
			btopt.Debugf(nil, "POOL %s sweepStuckSessions panic recovered: %v\n%s", p.poolName, r, debug.Stack())
		}
	}()
	p.sweepStuckSessions()
}
