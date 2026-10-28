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

// SessionPoolImpl core: struct, ctor, CheckoutSession/Invoke hot path,
// Stats, UpdateConfig. Observability ring buffers → session_pool_debug.go;
// hooks + Close + heartbeat → session_pool_lifecycle.go; scaling driver
// + createSession → session_pool_scaling.go.

package internal

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	spb "cloud.google.com/go/bigtable/apiv2/bigtablepb"
	btopt "cloud.google.com/go/bigtable/internal/option"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// ErrNoSessionsAvailable is returned by CheckoutSession when a parked
// waiter is unblocked by ctx cancellation or deadline. Wraps ctx.Err()
// so errors.Is against either the sentinel or the ctx cause holds.
var ErrNoSessionsAvailable = errors.New("bigtable: no sessions available")

// ErrConsecutiveFailures is returned by CheckoutSession when the pool's
// consecutive-abnormal-close circuit breaker trips. All parked waiters
// at trip time are woken with an error that either equals this sentinel
// or wraps it via *consecutiveFailureError (which also carries the last
// abnormal-close cause and its gRPC status code).
var ErrConsecutiveFailures = errors.New("bigtable: session pool tripped consecutive-failure threshold")

// consecutiveFailureError decorates ErrConsecutiveFailures with the last
// abnormal-close error captured by the pool. It preserves both:
//   - errors.Is(err, ErrConsecutiveFailures) — so retry/observability
//     paths that already match the sentinel keep working.
//   - status.Code(err) — inherited from the last abnormal-close error
//     when that error is a gRPC status (e.g. FailedPrecondition when
//     the server rejected OpenSession because the resource is still
//     being created). Falls back to codes.Unknown otherwise.
//
// Only constructed via SessionPoolImpl.consecutiveFailureErr, which
// guarantees inner != nil.
type consecutiveFailureError struct {
	inner error
}

func (e *consecutiveFailureError) Error() string {
	return fmt.Sprintf("%s (last error: %v)", ErrConsecutiveFailures.Error(), e.inner)
}

func (e *consecutiveFailureError) Is(target error) bool {
	return target == ErrConsecutiveFailures
}

func (e *consecutiveFailureError) Unwrap() error { return e.inner }

// GRPCStatus lets status.Code / status.FromError extract the gRPC code
// from the underlying cause. Without this shim, callers doing
// status.Code(err) == codes.FailedPrecondition would see Unknown.
// status.FromError always returns a non-nil *Status (Unknown on ok=false),
// so no fallback needed.
func (e *consecutiveFailureError) GRPCStatus() *status.Status {
	st, _ := status.FromError(e.inner)
	return st
}

// ErrPoolClosed is returned to any CheckoutSession caller parked on
// the waiter queue when Close() runs. Distinct from
// ErrNoSessionsAvailable (ctx-cancel during cold-start) and
// ErrConsecutiveFailures (circuit trip) so operators can distinguish
// "pool is going away" from "your ctx expired while waiting."
var ErrPoolClosed = errors.New("bigtable: session pool is closed")

// waiter is one parked CheckoutSession caller. Close-exactly-once is
// guarded by waitersMu + w.elem: enqueued while elem != nil; both wake
// paths hold waitersMu when they nil elem out.
type waiter struct {
	ready chan struct{}
	elem  *list.Element // non-nil while enqueued; nil after dequeue
	// err, when set before close(ready), fails the waiter with a
	// specific error instead of prompting a re-pick (used by the
	// consecutive-failure trip).
	err error
}

// SessionPoolImpl implements a thread-safe session pool.
type SessionPoolImpl struct {
	mu     sync.Mutex
	sizer  *PoolSizer
	picker AfePicker
	// sl owns the AFE-aware idle-session queues, per-AFE PeakEwma
	// trackers, and the canonical set of active SessionHandles. sl has
	// its own lock; sl methods never call back into SessionPoolImpl, so
	// "never take p.mu while holding sl.mu" holds by construction.
	sl     *sessionList
	budget SessionThrottler
	// startingSessions holds handles dialed via createSession that have
	// not yet reached onActive; cleared on promotion or failed-start.
	startingSessions map[*SessionHandle]struct{}
	// pendingStarts counts createSession goroutines that haven't yet
	// reached streamFactory success. Prevents back-to-back Ticks in
	// the streamFactory window from re-requesting the same delta.
	pendingStarts int
	// spawns tracks pool-spawned goroutines (today: createSession
	// workers) so Close can wait them out. Session-owned goroutines
	// are tracked separately on Session.loops.
	spawns             sync.WaitGroup
	closed             bool
	scalingInProgress  bool
	streamFactory      func(ctx context.Context) (Stream, error)
	openSessionRequest *spb.OpenSessionRequest
	metadata           metadata.MD
	sessionType        SessionType
	poolName           string
	startOnce          sync.Once
	// tickPending debounces tickOnce so a burst of empty-pool kicks
	// coalesces to at most one active Tick.
	tickPending atomic.Bool
	// poolID is baked into every session log name for the
	// channelz → sessionz reverse link.
	poolID uint64

	// waitersCount is the live count of CheckoutSession callers parked
	// at the pool boundary — the "pending vRPCs" signal for the sizer.
	waitersCount atomic.Int32

	// consecutiveFailures counts abnormal session closes since the last
	// successful session-open (cleared in onActive). Crossing the
	// threshold trips the pool: parked waiters are woken with
	// ErrConsecutiveFailures.
	consecutiveFailures         atomic.Int32
	consecutiveFailureThreshold atomic.Int32

	// lastAbnormalCloseErr holds the most recent abnormal-close raw error,
	// captured in noteAbnormalCloseIfAny. When the breaker trips we wrap
	// this into the ErrConsecutiveFailures poison so waiters see WHY
	// (e.g. FailedPrecondition ... still being created) instead of only
	// the generic sentinel.
	lastAbnormalCloseErr atomic.Pointer[error]

	// m holds observability-only state (counters, ring buffers,
	// histograms) — see poolMetrics in session_pool_debug.go.
	m poolMetrics

	// waiters is a FIFO queue of parked CheckoutSession callers. Each
	// free-session event wakes exactly one waiter, in insertion order.
	// Cancellation removes the waiter so no wake-up token is dropped.
	waitersMu sync.Mutex
	waiters   *list.List // *waiter

	// poolCtx scopes the pool's lifetime; wrapped (deadline stripped,
	// cancellation preserved) into streamFactory, budget.Acquire, and
	// Session.Start so teardown propagates.
	poolCtx    context.Context
	poolCancel context.CancelFunc

	// maintCtx scopes the background maintenance loops started by Start
	// (AFE prune, WaitServerClose sweep, slow metrics). Child of poolCtx
	// so it can never outlive the pool, but cancelled separately in
	// Close's Phase 1 — ahead of poolCancel — so no maintenance tick runs
	// against a pool that's mid-teardown. Each loop closure captures p,
	// so a loop that survives Close pins the whole pool on the heap;
	// that's what leaked under session.TableCache TTL eviction, which
	// Closes pools continuously over a long-lived process's life.
	maintCtx    context.Context
	maintCancel context.CancelFunc

	// debugEnabled mirrors session.Config.EnableDebug via
	// NewSessionPoolImpl. Immutable after construction (no atomic
	// needed). When false, every allocating debug recorder in
	// session_pool_debug.go / session_debug.go / session_pool_scaling.go
	// early-returns before touching a slice / map / ring buffer, and
	// the picker skips the PickDecision.Candidates slice construction.
	// Pure atomic-counter bumps (msgsSent, retries, etc.) are NOT
	// gated — a branch check costs more than the atomic.
	debugEnabled bool
}

// noteVRpcOutcome forwards the outcome to the AFE's PeakEwma trackers
// (OK-gated). The consecutive-failure counter is NOT reset here —
// only a successful session-open (onActive) clears it. Resetting on
// per-vRPC OK would let one long-lived healthy session mask a run
// of failed opens and keep the breaker from tripping.
func (p *SessionPoolImpl) noteVRpcOutcome(sh *SessionHandle, e2e, backend time.Duration, ok bool) {
	p.sl.RecordVRpcOutcome(sh, e2e, backend, ok)
}

// NewSessionPoolImpl creates a new SessionPoolImpl. id is baked into
// every session log name so channelz/sessionz can reverse-link back to
// the pool that owns each session.
//
// The sizer/picker/budget/threshold are bootstrapped from the default
// ClientConfiguration proto (default_client_config.go). Production
// callers register via ClientConfigurationManager, which fires
// UpdateConfig synchronously and replaces these with server-driven
// values before the pool serves traffic. Callers that want custom
// bounds without going through a manager (tests) should call
// pool.UpdateConfig(...) right after construction; a pool that
// never sees an UpdateConfig will serve traffic with the
// defaultPoolConfig() values (currently 5/400).
func NewSessionPoolImpl(id uint64, poolName string, streamFactory func(ctx context.Context) (Stream, error), openSessionRequest *spb.OpenSessionRequest, md metadata.MD, sessionType SessionType, debugEnabled bool) *SessionPoolImpl {
	poolCtx, poolCancel := context.WithCancel(context.Background())
	maintCtx, maintCancel := context.WithCancel(poolCtx)
	pool := &SessionPoolImpl{
		poolName:           poolName,
		poolID:             id,
		streamFactory:      streamFactory,
		openSessionRequest: openSessionRequest,
		metadata:           md,
		startingSessions:   make(map[*SessionHandle]struct{}),
		sessionType:        sessionType,
		waiters:            list.New(),
		poolCtx:            poolCtx,
		poolCancel:         poolCancel,
		maintCtx:           maintCtx,
		maintCancel:        maintCancel,
		sl:                 newSessionList(),
		debugEnabled:       debugEnabled,
	}
	pool.m.afePickCounts = make(map[AfeID]int64)

	defaultCfg := defaultPoolConfig()
	fetcher := func() *PoolStats { return pool.Stats() }
	pool.sizer = NewPoolSizer(fetcher, int(defaultCfg.GetMinSessionCount()), int(defaultCfg.GetMaxSessionCount()), float64(defaultCfg.GetHeadroom()))
	pool.picker = pickerFromLoadBalancing(defaultCfg.GetLoadBalancingOptions(), pool.debugEnabled)
	pool.budget = NewAdaptiveSessionThrottler(
		int(defaultCfg.GetNewSessionCreationBudget()),
		defaultCfg.GetNewSessionCreationPenalty().AsDuration(),
	)
	pool.consecutiveFailureThreshold.Store(defaultCfg.GetConsecutiveSessionFailureThreshold())

	return pool
}

// CheckoutSession returns a session ready to serve one vRPC. With
// multiPlexingLimit=1 the pool only hands out a session whose outstanding
// count is 0; if all are busy, the caller parks on the FIFO waiter queue
// until a drainSlot wake fires.
func (p *SessionPoolImpl) CheckoutSession(ctx context.Context) (*SessionHandle, error) {
	// One-shot kick if the pool is empty. Cheap check; Tick
	// gates on its own in-progress flag so a duplicate goroutine here
	// exits immediately.
	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()
	if !closed && p.sl.ReadyCount() == 0 {
		p.spawnTickOnce(p.poolCtx)
	}

	for {
		// Snapshot closed + picker under p.mu; everything after runs
		// outside the lock so concurrent CheckoutSession calls parallelize.
		p.mu.Lock()
		if p.closed {
			p.mu.Unlock()
			return nil, ErrPoolClosed
		}
		picker := p.picker
		p.mu.Unlock()

		// Two-tier pick: picker chooses an AFE from the ready snapshot,
		// then sessionList dequeues one idle session in that AFE.
		ready := p.sl.ReadyAfes()
		pickerName := picker.Name()
		afeID, picked, decision := picker.PickAfe(ready)
		p.recordPickDecision(decision, pickerName)
		if picked {
			if idle := p.sl.Checkout(afeID); idle != nil {
				idle.IncOutstanding()
				idle.IncPicks()
				return idle, nil
			}
		}

		// Slow path: picker returned nil or 2 checkoutSession raced and returned the same AFE id. Dying sessions leave sl.readyCount
		// the instant they transition out of Ready (OnClosing), so the
		// maxSessions gate reflects only live-or-starting sessions —
		// a miss just means all live sessions are busy.
		if p.sl.ReadyCount() < p.sizer.MaxSessions() {
			p.spawnTickOnce(p.poolCtx)
		}

		// Park in the FIFO waiter queue. Each free-session event wakes
		// exactly one waiter (queue head). Bracket with waitersCount so
		// the sizer (via Stats()) sees real queue depth.
		w := &waiter{ready: make(chan struct{})}
		p.waitersMu.Lock()
		w.elem = p.waiters.PushBack(w)
		p.waitersMu.Unlock()

		p.waitersCount.Add(1)
		select {
		case <-ctx.Done():
			p.waitersCount.Add(-1)
			// Remove from queue so a subsequent free-session wake
			// doesn't burn on a caller that's already given up.
			p.removeWaiter(w)
			return nil, fmt.Errorf("%w: %w", ErrNoSessionsAvailable, ctx.Err())
		case <-w.ready:
			p.waitersCount.Add(-1)
			// A poisoned wake (drainWaitersWithErr) sets w.err before
			// closing w.ready. Normal signalFree leaves it nil so the
			// caller loops back to re-pick.
			if w.err != nil {
				return nil, w.err
			}
			// Woken by signalFree. Loop back to re-pick.
		}
	}
}

// removeWaiter pulls w out of the waiter queue if still present. Safe
// to call from the ctx-cancel path even when signalFree has already
// removed the waiter (checks w.elem — nil means already dequeued by
// signalFree, which will have closed w.ready).
func (p *SessionPoolImpl) removeWaiter(w *waiter) {
	p.waitersMu.Lock()
	if w.elem != nil {
		p.waiters.Remove(w.elem)
		w.elem = nil
	}
	p.waitersMu.Unlock()
}

// signalFree wakes exactly one parked CheckoutSession waiter (the FIFO
// queue head). No-op when the queue is empty. Called from OnActive and
// from every drainSlot success via SessionHandle.onSlotDrained
// (SESSION_SPEC.md #2). Never blocks: wake channel is per-waiter.
func (p *SessionPoolImpl) signalFree() {
	p.waitersMu.Lock()
	if e := p.waiters.Front(); e != nil {
		w := e.Value.(*waiter)
		p.waiters.Remove(e)
		w.elem = nil
		close(w.ready)
	}
	p.waitersMu.Unlock()
}

// drainWaitersWithErr wakes every parked CheckoutSession caller with
// the given error. Returns the number of waiters woken. Safe on empty.
func (p *SessionPoolImpl) drainWaitersWithErr(err error) int {
	p.waitersMu.Lock()
	defer p.waitersMu.Unlock()
	woken := 0
	for {
		e := p.waiters.Front()
		if e == nil {
			return woken
		}
		w := e.Value.(*waiter)
		p.waiters.Remove(e)
		w.elem = nil
		w.err = err
		close(w.ready)
		woken++
	}
}

// Stats snapshots ready/inUse/starting/pending counts.
//
// The AllHandles walk and the startingSessions read now happen under
// disjoint locks, so a handle can transition starting → ready between
// the two reads and be missed by both — StartingCount + ReadyCount is
// therefore a best-effort snapshot, not a coherent sum. Sizer.Decide
// is the sole consumer and self-corrects on the next Tick.
func (p *SessionPoolImpl) Stats() *PoolStats {
	// AllHandles takes its own snapshot under sl.mu — walk it OUTSIDE
	// p.mu so per-session Load()s can't back up under the pool lock and
	// stall CheckoutSession. State/outstanding reads are all atomic.
	handles := p.sl.AllHandles()
	ready := 0
	inUse := 0
	for _, sh := range handles {
		// Same nil-guard shape as sampleActiveUptimes and sweepStuckSessions
		// in session_pool_lifecycle.go — sl.AllHandles is a snapshot and
		// the underlying handle can be torn down mid-iteration by a
		// concurrent teardown that beat this walk to sl.mu.
		if sh == nil || sh.session == nil {
			continue
		}
		if sh.session.State() == StateReady {
			ready++
		}
		if sh.outstanding.Load() > 0 {
			inUse++
		}
	}

	// p.mu bracket is now just around the startingSessions map read +
	// pendingStarts int read — both plain non-atomic fields the pool
	// mutates from Tick / createSession under p.mu.
	p.mu.Lock()
	startingCount := len(p.startingSessions) + p.pendingStarts
	p.mu.Unlock()

	return &PoolStats{
		ReadyCount: ready,
		InUseCount: inUse,
		// StartingCount = sessions past streamFactory (in startingSessions)
		// PLUS goroutines Tick has spawned but that haven't yet reached
		// the transfer point. Both count as "in-flight scale-up capacity"
		// so the sizer doesn't request duplicate delta in a burst.
		StartingCount: startingCount,
		// PendingCount is the true pool-boundary queue depth —
		// callers parked inside CheckoutSession waiting on
		// freeSignal. The pending-rpc count is the input
		// to the sizer.
		PendingCount: int(p.waitersCount.Load()),
	}
}

// UpdateConfig dynamically adjusts the pool size constraints and budget governor limits at runtime.
// Callers are serialized by ClientConfigurationManager, so we don't
// bracket min/max as an atomic pair here.
func (p *SessionPoolImpl) UpdateConfig(config *spb.SessionClientConfiguration_SessionPoolConfiguration) {
	p.m.listenerFires.Add(1)
	// Defensive: ClientConfigurationManager only fires listeners on
	// successful GetClientConfiguration, so config should never be nil
	// in practice. Log-and-bail (rather than silent-return or panic) so
	// a broken caller shows up in operator logs the same day it lands,
	// but a bad configuration source doesn't take down the pool.
	if config == nil {
		log.Printf("bigtable_session_pool: UpdateConfig received nil config; ignoring (ClientConfigurationManager contract violation)")
		return
	}
	// p.mu only brackets the picker swap — it's the sole non-atomic
	// field UpdateConfig mutates that a concurrent CheckoutSession
	// reads. sizer.UpdateConfig / budget.UpdateConfig each take their
	// own internal lock; the consecutive-failure threshold is a raw
	// atomic. Keeping them out of p.mu means UpdateConfig can't stall
	// a hot-path CheckoutSession behind the sizer/budget config writes.
	p.mu.Lock()
	if config.LoadBalancingOptions != nil {
		p.picker = pickerFromLoadBalancing(config.LoadBalancingOptions, p.debugEnabled)
	}
	p.mu.Unlock()

	// sizer.UpdateConfig re-stores min/max/headroom/qlen atomically
	// under the sizer's own mu — pool no longer duplicates min/max into
	// its own atomics.
	p.sizer.UpdateConfig(config)

	// Budget was bootstrapped with a placeholder; UpdateConfig on
	// registration replaces it with the server-driven ceiling + penalty.
	if budget := int(config.GetNewSessionCreationBudget()); budget > 0 {
		penalty := config.GetNewSessionCreationPenalty().AsDuration()
		p.budget.UpdateConfig(budget, penalty)
	}

	// Consecutive-failure threshold: server-driven cap on how many
	// back-to-back abnormal session closes the pool tolerates before
	// failing all parked waiters. Zero/negative preserves the bootstrap
	// default.
	if thr := config.GetConsecutiveSessionFailureThreshold(); thr > 0 {
		p.consecutiveFailureThreshold.Store(thr)
	}

	// Server-driven config change (min/max bump most commonly) may
	// require a scale-up. Sizing is otherwise fully event-driven via
	// onActive / onClosing / CheckoutSession's empty-pool kick; this
	// explicit kick covers the one path where none of those events
	// fire — a config poll that raises MinSessionCount on an idle
	// pool. spawnTickOnce is CAS-guarded so a burst of listener fires
	// coalesces to one Tick body.
	p.spawnTickOnce(p.poolCtx)
}

// pickerFromLoadBalancing builds an AfePicker from server-driven
// LoadBalancingOptions. A nil lbo (or unknown strategy) returns the
// default LeastInFlight picker with K=defaultAfeRandomSubsetSize, so
// bootstrap paths and tests that skip the config wiring still work.
// Sole LBO → picker mapping; every picker with a K knob reads its
// RandomSubsetSize from the corresponding oneof.
func pickerFromLoadBalancing(lbo *spb.LoadBalancingOptions, recordCandidates bool) AfePicker {
	if lbo == nil {
		return NewLeastInFlightAfePicker(defaultAfeRandomSubsetSize, recordCandidates)
	}
	switch opt := lbo.LoadBalancingStrategy.(type) {
	case *spb.LoadBalancingOptions_Random_:
		return NewSimpleAfePicker(recordCandidates)
	case *spb.LoadBalancingOptions_LeastInFlight_:
		k := defaultAfeRandomSubsetSize
		if opt.LeastInFlight != nil && opt.LeastInFlight.RandomSubsetSize > 0 {
			k = int(opt.LeastInFlight.RandomSubsetSize)
		}
		return NewLeastInFlightAfePicker(k, recordCandidates)
	case *spb.LoadBalancingOptions_PeakEwma_:
		k := defaultAfeRandomSubsetSize
		if opt.PeakEwma != nil && opt.PeakEwma.RandomSubsetSize > 0 {
			k = int(opt.PeakEwma.RandomSubsetSize)
		}
		return NewLeastLatencyAfePicker(k, recordCandidates)
	default:
		return NewLeastInFlightAfePicker(defaultAfeRandomSubsetSize, recordCandidates)
	}
}

// Invoke runs one vRPC on a checked-out session. Server RetryInfo
// travels via gRPC status details on the returned error.
func (p *SessionPoolImpl) Invoke(ctx context.Context, desc VRpcDescriptor, req interface{}) (InvokeResult, error) {
	checkoutStart := time.Now()
	sh, err := p.CheckoutSession(ctx)
	if err != nil {
		// Record checkout failure so pool-exhaustion incidents show up
		// in sessionz's slow-vRPC table and latency histograms.
		p.recordCheckoutFailure(checkoutStart, desc, err)
		return InvokeResult{}, err
	}
	// poolWait is the queue-time spent inside CheckoutSession waiting
	// for an idle session — the Go-side observation of what OTel /
	// Cloud Monitoring surfaces as `client_blocking_latencies` (Java
	// internal name: throttling latencies). Wire it into the OTel
	// per-attempt path when the metric plumbing lands; right now it
	// only feeds the sessionz slow-vRPC row below.
	poolWait := time.Since(checkoutStart)
	// Anchor Latency at checkoutStart so recorded latency includes queue
	// wait (user-visible time).
	start := checkoutStart
	// Session release is driven by the session's OnSlotDrained hook
	// (installed at createSession); this defer only decrements the caller's
	// in-flight counter and feeds the per-AFE PeakEwma with the outcome.
	// The AFE-wake fires from the response handler BEFORE this defer runs,
	// so pickers may see pre-update EWMAs for one tick — accepted lag.
	//
	// noteVRpcOutcome feeds e2e latency (not including CheckoutSession).
	var (
		invokeErr  error
		backendDur time.Duration
		latency    time.Duration
	)
	defer func() {
		sh.DecOutstanding()
		p.noteVRpcOutcome(sh, latency-poolWait, backendDur, invokeErr == nil)
	}()

	var result InvokeResult
	result, invokeErr = sh.session.Invoke(ctx, desc, req)
	// PeerInfo is set once at session-open and never mutated; a shared
	// read here lets callers stamp per-attempt transport labels without
	// reaching back through the pool.
	result.PeerInfo = sh.session.PeerInfo()
	latency = time.Since(start)
	// Pool-level histograms feed the debug UI (per-session ring buffers
	// are too small). BackendLatency only records when the server
	// populated Stats; TotalLatency records for every call.
	// Gated on debugEnabled — histograms are debug-only surfaces.
	if p.debugEnabled {
		p.m.totalLatencyHist.record(latency)
	}
	if result.Stats != nil && result.Stats.BackendLatency != nil {
		backendDur = result.Stats.BackendLatency.AsDuration()
		if p.debugEnabled {
			p.m.backendLatencyHist.record(backendDur)
		}
	}
	// TransportLatency is computed at the source in
	// Session.processResult as (Send→Recv wall clock) − BackendLatency,
	// i.e. the AFE-attributable overhead only. Zero here means the
	// server didn't populate Stats, the call errored pre-Recv, or the
	// subtraction was non-positive (clock skew) — all cases we skip
	// from the per-AFE transport-overhead histograms so p50 isn't
	// dragged toward 0. RecordTransportOverhead feeds the OTel
	// transport_latencies metric — that is NOT debug-gated (it's a
	// customer-facing metric); only the debug histogram is gated.
	if result.TransportLatency > 0 {
		if p.debugEnabled {
			p.m.transportLatencyHist.record(result.TransportLatency)
		}
		sh.session.RecordTransportOverhead(ctx, desc.Method(), result.TransportLatency)
	}
	if latency > defaultSlowThreshold {
		ev := SlowVRpcEvent{
			At:               start,
			Method:           desc.Method(),
			Latency:          latency,
			Session:          sh.session.LogName(),
			Success:          invokeErr == nil,
			PoolWait:         poolWait,
			BackendLatency:   backendDur,
			TransportLatency: result.TransportLatency,
			RPCIDOnSession:   result.RPCIDOnSession,
		}
		ev.SessionAge = start.Sub(sh.session.StartedAt())
		// PeerInfo on the row makes cohort patterns (e.g. failures
		// clustered on one AFE) visible without a per-session cross-ref.
		ev.Peer = peerInfoToSnapshot(sh.session.PeerInfo())
		ev.RemoteAddr = sh.session.RemoteAddr()
		if invokeErr != nil {
			ev.ErrCode = statusOf(invokeErr).Code().String()
			btopt.Debugf(nil, "POOL %s slow vRPC failed method=%s session=%s rpc_id=%d code=%s latency=%v session_age=%v backend=%v raw_err=%v",
				p.poolName, ev.Method, ev.Session, ev.RPCIDOnSession, ev.ErrCode, ev.Latency, ev.SessionAge, ev.BackendLatency, invokeErr)
		}
		p.recordSlowVRpc(ev)
	}
	return result, invokeErr
}
