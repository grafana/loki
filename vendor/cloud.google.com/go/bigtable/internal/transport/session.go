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
	"errors"
	"sync"
	"sync/atomic"
	"time"

	spb "cloud.google.com/go/bigtable/apiv2/bigtablepb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Raising multiPlexingLimit requires a negotiated server-side change.
const multiPlexingLimit = 1

const (
	// defaultHeartbeatInterval is the fallback cadence when no server-provided
	// SessionParametersResponse has landed yet. Once negotiated, this is
	// overwritten by the server-supplied interval.
	defaultHeartbeatInterval = 100 * time.Millisecond
	// initialHeartbeatGrace is the deadline used from OpenSession until
	// SessionParametersResponse arrives with the real cadence; kept in
	// lock-step with defaultHeartbeatInterval so a session that never
	// receives SessionParameters trips within one interval.
	initialHeartbeatGrace = 100 * time.Millisecond
)

// Session-level errors, wrapped in codes.Unavailable so retry plumbing works
// via status.Code while errors.Is distinguishes the cause.
var (
	ErrSessionNotActive           = errors.New("bigtable: session not active")
	ErrUnavailableHeartBeatMissed = errors.New("bigtable: session unavailable: server heartbeat missed")
	ErrUnavailableGoAway          = errors.New("bigtable: session unavailable: server sent GOAWAY")
	ErrUnavailableSessionError    = errors.New("bigtable: session unavailable: server reported session error")
)

// Stream is the bidirectional gRPC stream a Session multiplexes over.
type Stream interface {
	Send(*spb.SessionRequest) error
	Recv() (*spb.SessionResponse, error)
	Header() (metadata.MD, error)
	Context() context.Context
}

// SessionHooks holds optional lifecycle callbacks. Nil fields are skipped.
// Hooks must not block.
//
// OnSlotDrained fires on every successful drainSlot on the wire (normal
// deliver, cancelled-drain, Send-failure Invoke branch). It is the sole
// "session became free" signal — consumers use it to re-enqueue the
// session in its AFE idle queue and wake one parked Checkout waiter.
// cancelActiveRPCs (session teardown) intentionally does NOT fire it —
// OnClosing/OnClose handle removal from routing structures on that path.
type SessionHooks struct {
	OnStart       func(ctx context.Context)
	OnActive      func(s *Session)
	OnSlotDrained func()
	OnClosing     func(s *Session)
	OnClose       func(s *Session, err error)
}

func (h SessionHooks) onStart(ctx context.Context) {
	if h.OnStart != nil {
		h.OnStart(ctx)
	}
}

func (h SessionHooks) onActive(s *Session) {
	if h.OnActive != nil {
		h.OnActive(s)
	}
}

func (h SessionHooks) onSlotDrained() {
	if h.OnSlotDrained != nil {
		h.OnSlotDrained()
	}
}

func (h SessionHooks) onClosing(s *Session) {
	if h.OnClosing != nil {
		h.OnClosing(s)
	}
}

func (h SessionHooks) onClose(s *Session, err error) {
	if h.OnClose != nil {
		h.OnClose(s, err)
	}
}

// vrpcResult is the value delivered to Invoke on resultChan. Exactly one of
// resp, errResp, err is set.
type vrpcResult struct {
	resp    *spb.VirtualRpcResponse
	errResp *spb.ErrorResponse
	err     error
}

// ClusterInfo returns whichever server frame's ClusterInformation is set, or
// nil on a transport-side err.
func (r vrpcResult) ClusterInfo() *spb.ClusterInformation {
	if r.resp != nil {
		return r.resp.ClusterInfo
	}
	if r.errResp != nil {
		return r.errResp.ClusterInfo
	}
	return nil
}

// vrpcImpl tracks an in-flight virtual RPC. Publication point is the
// activeRPC assignment under slotMu.
type vrpcImpl struct {
	id         int64
	method     string
	resultChan chan vrpcResult
}

// Session manages the lifecycle of a Bigtable Session and routes vRPCs over
// its bidirectional Stream.
type Session struct {
	nextRPCID atomic.Int64

	// sendMu serializes concurrent Send calls — grpc.ClientStream.Send is
	// not safe for concurrent use.
	sendMu sync.Mutex

	logName     string
	stream      Stream
	hooks       SessionHooks
	sessionType SessionType

	// debugEnabled mirrors the owning SessionPoolImpl.debugEnabled. Set
	// once at construction via WithSessionDebugEnabled, immutable
	// thereafter. When false, session-scope recorders in session_debug.go
	// (recordEvent / recordLatency / recordCluster) short-circuit before
	// touching any ring buffer or map. See session.Config.EnableDebug
	// for the top-of-thread contract.
	debugEnabled bool

	// state is the lifecycle position; read via State(), mutate via
	// transitionTo. lastStateChangeNano is stamped inside transitionTo on
	// each successful swap; it lives on the embedded sessionDebug (below)
	// so the observability field set stays co-located with the rest of the
	// debug/metric plumbing.

	state atomic.Int32

	// prevStateAtClose is the state the session was in immediately
	// before its final transition to StateClosed — captured as the
	// prev return of transitionTo(StateClosed, ...) at the two
	// transition sites (ForceClose, handleClose). Set-once by
	// construction (transitionTo(StateClosed) applies at most once),
	// then read from hooks.OnClose consumers. Lets the pool
	// distinguish a client-initiated clean-close (prev == WSC) from a
	// server-initiated / transport-error close without carrying a
	// side-channel bool.
	prevStateAtClose atomic.Int32

	// closingOnce serializes hooks.OnClosing so it fires exactly once
	// across the four transition sites that can drive a session out of
	// Ready (Close, ForceClose, handleGoAway, handleClose).
	closingOnce sync.Once
	closeOnce   sync.Once

	// slotMu serializes the (activeRPC, currentCancel) pair for the
	// one-in-flight slot. Innermost lock; held only across pointer
	// assignments. Accessors land with Invoke in a follow-up PR.
	slotMu        sync.Mutex
	activeRPC     *vrpcImpl
	currentCancel *vrpcResult

	// heartbeat*Nano: interval is server-negotiated (SessionParameters);
	// deadline is extended by every inbound/outbound frame.
	heartbeatIntervalNano     atomic.Int64
	nextHeartbeatDeadlineNano atomic.Int64

	// heartbeatWake nudges the heartbeat loop to re-evaluate its Timer
	// after the atomic deadline moves. Cap-1 non-blocking channel so a
	// burst of resets coalesces into a single wake and hot-path frame
	// handling stays allocation-free.
	heartbeatWake chan struct{}

	// quiescent closes when the in-flight vRPC drains after StateClosing,
	// or when ForceClose runs.
	quiescent     chan struct{}
	quiescentOnce sync.Once

	// peerInfo is set once, synchronously in handleOpenSession before
	// hooks.onActive fires — reads stay lock-free.
	peerInfo atomic.Pointer[spb.PeerInfo]
	// refreshConfig is set once when the server sends SessionRefreshConfig.
	refreshConfig atomic.Pointer[spb.SessionRefreshConfig]

	// sessionDebug bundles observability: per-session counters, event
	// ring, latency histogram, tracer/logger, close-reason attribution.
	// Embedded so bare field access (s.tracer, s.okRpcs, s.recordEvent,
	// ...) continues to compile once vRPC / lifecycle land.
	sessionDebug

	// loops tracks readLoop + heartbeatLoop so a supervising owner
	// (SessionPoolImpl.Close) can wait for them to fully unwind — through
	// their notifyClosing / notifyClosed callback chains — before it
	// returns. Prevents readLoop's recordClose from racing package-level
	// metric var writes across test boundaries.
	loops sync.WaitGroup

	// closeErr preserves the raw Recv error handed to handleClose. The
	// pool surfaces this on consecutive-failure breaker trips so operators
	// see the underlying server rejection instead of only the sentinel.
	closeErr atomic.Pointer[error]
}

// SessionOption configures a Session at construction time.
type SessionOption func(*Session)

// WithSessionDebugEnabled toggles the session's debug-recorder gate.
// When false (the zero-value default when the option is omitted),
// session-scope recorders in session_debug.go short-circuit before
// allocating any ring-buffer or map entry.
//
// Every production caller MUST pass this option — the owning
// SessionPoolImpl wires it from its own debugEnabled at createSession
// time (session_pool_scaling.go), so the flag is uniform across every
// session in a pool. Tests that construct a Session directly and then
// assert on the session's debug ring buffers (events, latencies,
// clusters) also MUST pass WithSessionDebugEnabled(true) — omitting
// it silently disables recorders and leaves the assertions puzzling.
// The test helper newTestSession in session_test.go enables debug by
// default for exactly this reason.
func WithSessionDebugEnabled(enabled bool) SessionOption {
	return func(s *Session) { s.debugEnabled = enabled }
}

// NewSession constructs a Session bound to stream. Zero-value SessionHooks is
// valid.
func NewSession(logName string, stream Stream, hooks SessionHooks, sessionType SessionType, opts ...SessionOption) *Session {
	s := &Session{
		logName:       logName,
		stream:        stream,
		hooks:         hooks,
		quiescent:     make(chan struct{}),
		sessionType:   sessionType,
		heartbeatWake: make(chan struct{}, 1),
	}
	s.state.Store(int32(StateNew))
	s.heartbeatIntervalNano.Store(int64(defaultHeartbeatInterval))
	s.nextHeartbeatDeadlineNano.Store(time.Now().Add(initialHeartbeatGrace).UnixNano())
	s.sessionDebug.init(newSessionTracer(sessionType))
	for _, o := range opts {
		o(s)
	}
	return s
}

// LogName returns the diagnostic identifier.
func (s *Session) LogName() string { return s.logName }

// State returns the current state.
func (s *Session) State() State { return State(s.state.Load()) }

// PeerInfo returns the peer info, or nil pre-Ready.
func (s *Session) PeerInfo() *spb.PeerInfo { return s.peerInfo.Load() }

// AfeID returns the AFE identifier, or 0 pre-Ready. Stable for the session's
// lifetime — PeerInfo is populated once at StateReady. AfeID type lives in
// session_list.go (same package) alongside the per-AFE sessionList
// bookkeeping that consumes it.
func (s *Session) AfeID() AfeID {
	if p := s.peerInfo.Load(); p != nil {
		return AfeID(p.GetApplicationFrontendId())
	}
	return 0
}

// RefreshConfig returns the server-provided refresh configuration, or nil.
func (s *Session) RefreshConfig() *spb.SessionRefreshConfig { return s.refreshConfig.Load() }

// signalQuiescent closes the quiescent channel exactly once.
func (s *Session) signalQuiescent() {
	s.quiescentOnce.Do(func() { close(s.quiescent) })
}

// sessionErr couples a gRPC Unavailable status with a sentinel cause so both
// status.Code and errors.Is work.
type sessionErr struct {
	st    *status.Status
	cause error
}

func (e *sessionErr) Error() string              { return e.st.Err().Error() }
func (e *sessionErr) Unwrap() error              { return e.cause }
func (e *sessionErr) GRPCStatus() *status.Status { return e.st }

// unavailable builds a sessionErr carrying codes.Unavailable.
func unavailable(cause error, format string, args ...interface{}) error {
	return &sessionErr{
		st:    status.Newf(codes.Unavailable, format, args...),
		cause: cause,
	}
}

// Send writes a SessionRequest under sendMu so concurrent producers don't
// corrupt the underlying stream. grpc.ClientStream.Send is not safe for
// concurrent use; sendMu is the only serialization point.
func (s *Session) Send(req *spb.SessionRequest) error {
	s.sendMu.Lock()
	err := s.stream.Send(req)
	s.sendMu.Unlock()
	if err == nil {
		s.msgsSent.Add(1)
		s.msgsSentByType[classifyReq(req)].Add(1)
	}
	return err
}

// resetHeartbeatDeadline pushes out the watchdog to (now + heartbeatInterval).
// One atomic load + one atomic store on the hot path, plus a non-blocking
// wake to the heartbeat loop so its Timer picks up the new deadline
// immediately (otherwise the initial bootstrap arm keeps the loop sleeping
// past atomic shortenings — SESSION_SPEC.md #7).
func (s *Session) resetHeartbeatDeadline() {
	s.nextHeartbeatDeadlineNano.Store(time.Now().Add(time.Duration(s.heartbeatIntervalNano.Load())).UnixNano())
	s.wakeHeartbeatLoop()
}

// wakeHeartbeatLoop signals the heartbeat loop to re-evaluate its Timer.
// Non-blocking send on a cap-1 channel: bursts coalesce into a single
// pending wake, so hot-path frame handlers stay allocation-free and
// contention-free even under high frame arrival rates.
func (s *Session) wakeHeartbeatLoop() {
	select {
	case s.heartbeatWake <- struct{}{}:
	default:
	}
}
