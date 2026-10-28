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
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	spb "cloud.google.com/go/bigtable/apiv2/bigtablepb"
	btmetrics "cloud.google.com/go/bigtable/internal/metrics"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

const peerInfoHeaderKey = "bigtable-peer-info"

// Start opens the session by sending OpenSessionRequest, then launches the
// read and heartbeat loops. ctx governs the loops; cancelling it forces the
// session closed. Unblocking the underlying Recv requires the caller to also
// cancel the stream's context.
func (s *Session) Start(ctx context.Context, req *spb.OpenSessionRequest) error {
	if prev, ok := s.transitionTo(StateStarting, isState(StateNew)); !ok {
		return fmt.Errorf("session already started or closed (state: %v)", prev)
	}

	openReq := &spb.SessionRequest{
		Payload: &spb.SessionRequest_OpenSession{OpenSession: req},
	}
	if err := s.Send(openReq); err != nil {
		s.ForceClose(&spb.CloseSessionRequest{
			Reason:      spb.CloseSessionRequest_CLOSE_SESSION_REASON_ERROR,
			Description: "failed to send open session request",
		})
		// Wrap as codes.Unavailable so retry plumbing (status.Code +
		// RetryingVRpc's idempotency-independent retry for pre-wire
		// failures) treats a failed OpenSession send the same as any
		// other transport-side loss. errors.Is still resolves the
		// underlying send error via sessionErr.Unwrap.
		//
		// TODO(sushanb): distinguish Unimplemented from generic
		// transport failure so the client can fall back to the unary
		// (non-session) path when the server rejects OpenSession
		// with codes.Unimplemented. Today every Send failure — network
		// drop, backpressure, Unimplemented — is folded into
		// Unavailable, which retries indefinitely against a server
		// that will never accept the RPC. Follow-up when the fallback
		// path lands.
		return unavailable(err, "session OpenSession request failed: %v", err)
	}

	// Fire onStart BEFORE spawning readLoop/heartbeatLoop so hook
	// ordering (SESSION_SPEC #4) is enforced by construction: on a fast
	// handshake, readLoop can Recv the OpenSessionResponse and fire
	// onActive before Start returns; if we spawned the loops first, the
	// race would let onActive land before onStart. OnStart is defined as
	// a pure notification with no dependency on readLoop, so the reorder
	// is safe.
	s.hooks.onStart(ctx)

	// Track readLoop + heartbeatLoop so WaitGoroutines can block until
	// their callback chains (notifyClosed → recordClose, etc.) have
	// unwound. Owners (SessionPoolImpl.Close) call WaitGoroutines during
	// teardown so no session-owned goroutine outlives the pool.
	s.loops.Add(2)
	go func() { defer s.loops.Done(); s.readLoop(ctx) }()
	go func() { defer s.loops.Done(); s.heartbeatLoop(ctx) }()
	return nil
}

// WaitGoroutines blocks until readLoop and heartbeatLoop have fully
// returned (including their notifyClosed / recordClose callback
// chains). No-op if Start was never called.
func (s *Session) WaitGoroutines() {
	s.loops.Wait()
}

// ForceClose immediately transitions the session to StateClosed and cancels
// every in-flight RPC. It is safe to call multiple times; only the first call
// fires the tracer.recordClose and hooks.onClose callbacks.
func (s *Session) ForceClose(req *spb.CloseSessionRequest) {
	prev, ok := s.transitionTo(StateClosed, notState(StateClosed))
	if !ok {
		return
	}
	s.prevStateAtClose.Store(int32(prev))
	if prev == StateNew {
		// Force-closing a NEW session means the pool decided to tear us
		// down before Start ran — a bookkeeping oddity worth flagging.
		recordDebugTag(tagSessionForceCloseNeverStarted)
	}

	// ForceClose skips the Ready → Closing transition (goes straight to
	// Closed), so this is our one chance to fire onClosing before onClose.
	s.notifyClosing()

	s.setCloseReason(closeReasonLabel(req))
	desc := "session force closed"
	if req != nil && req.Description != "" {
		desc = "session force closed: " + req.Description
	}
	s.cancelActiveRPCs(unavailable(closeReasonToCause(req), "%s", desc))
	s.signalQuiescent()
	s.notifyClosed(nil)
}

// notifyClosing fires hooks.onClosing exactly once over the lifetime of a
// Session. Called from every path that first transitions the session out of
// Ready — handleGoAway, Close, ForceClose, handleClose. notifyClosed also
// invokes it as a safety net so onClosing is guaranteed to precede
// onClose even if some future path forgets to call it explicitly.
//
// Ordering caveat for downstream consumers: onActive is NOT guaranteed to
// precede onClosing. If the session tears down while still in Starting
// (Close or ForceClose fires before OpenSession completes), onActive
// never runs but onClosing/onClose still do. SESSION_SPEC #4 orders the
// hooks only among those that fire — if a hook doesn't fire, its
// ordering slot is skipped, not deferred.
func (s *Session) notifyClosing() {
	s.closingOnce.Do(func() {
		s.hooks.onClosing(s)
	})
}

// notifyClosed fires tracer.recordClose and hooks.onClose exactly once over
// the lifetime of a Session. Ensures hooks.onClosing fires first (via the
// closingOnce safety net) so the ordering contract onClosing → onClose
// always holds.
func (s *Session) notifyClosed(streamErr error) {
	s.closeOnce.Do(func() {
		s.notifyClosing()
		s.tracer.recordClose(
			context.Background(),
			s.peerInfo.Load(),
			s.CloseReason(),
			streamErr,
			s.HasOkRpcs(),
			s.HasErrorRpcs(),
		)
		s.hooks.onClose(s, streamErr)
	})
}

// Close requests a graceful shutdown:
//  1. Transitions to StateClosing (no-op if already Closing — callers like
//     handleGoAway invoke this after the state was advanced upstream).
//  2. Waits for in-flight RPCs to drain (or for ctx to fire).
//  3. Sends CloseSessionRequest to the server.
//  4. Transitions to StateWaitServerClose.
//  5. The server's EOF eventually drives handleClose → StateClosed.
//
// A pool-side monitor (see SessionPoolImpl.startStuckSessionMonitor)
// force-closes sessions stuck in StateWaitServerClose past a grace period
// so an unresponsive server can't leak Closing sessions indefinitely.
func (s *Session) Close(ctx context.Context, req *spb.CloseSessionRequest) error {
	// Allow being called from Closing too — handleGoAway already
	// transitioned and now wants the drain + send + WaitServerClose dance.
	if _, ok := s.transitionTo(StateClosing, isState(StateNew, StateStarting, StateReady)); ok {
		// We drove the state change; fire onClosing on our goroutine.
		// If handleGoAway got here first, it already fired — closingOnce
		// makes this a no-op.
		s.notifyClosing()
	}
	st := s.State()
	if st != StateClosing {
		// Already past Closing (WaitServerClose / Closed); nothing to do.
		return nil
	}
	// Record the intended reason now — the eventual handleClose (driven by
	// the server's EOF) would otherwise stamp "StreamEnd" over it and the
	// downstream OnClose hook would see the wrong label. setCloseReason is
	// CompareAndSwap-once so callers like handleGoAway that stamped earlier
	// win over this one.
	s.setCloseReason(closeReasonLabel(req))

	// Wait for the in-flight RPC (if any) to drain. The quiescent channel
	// is closed by the RPC's cleanup defer when it sees state==Closing on
	// its way out, or by ForceClose if it races us. Order matters: we
	// transitioned to Closing above BEFORE observing activeRPC — so the
	// defer's "check state after clearing" side always signals if we miss
	// the empty snapshot here.
	empty := s.activeVRPC() == nil
	if empty {
		s.signalQuiescent()
	} else {
		select {
		case <-s.quiescent:
		case <-ctx.Done():
			s.ForceClose(&spb.CloseSessionRequest{
				Reason:      spb.CloseSessionRequest_CLOSE_SESSION_REASON_USER,
				Description: fmt.Sprintf("close context done during drain: %v", ctx.Err()),
			})
			return ctx.Err()
		}
	}

	// If ForceClose raced us during the drain, our work is done.
	if s.State() == StateClosed {
		return nil
	}

	closeReq := &spb.SessionRequest{
		Payload: &spb.SessionRequest_CloseSession{CloseSession: req},
	}
	if err := s.Send(closeReq); err != nil {
		s.ForceClose(&spb.CloseSessionRequest{
			Reason:      spb.CloseSessionRequest_CLOSE_SESSION_REASON_ERROR,
			Description: "failed to send close session request",
		})
		return fmt.Errorf("send close session request: %w", err)
	}
	// Advance to WaitServerClose so the pool monitor can see we're waiting
	// on the server. handleClose accepts StateWaitServerClose → Closed
	// and will capture WSC as prevStateAtClose — that's the signal
	// noteAbnormalCloseIfAny reads to skip the abnormal-close counter.
	s.transitionTo(StateWaitServerClose, isState(StateClosing))
	return nil
}

// readLoop drives the inbound side of the stream until Recv returns an error.
func (s *Session) readLoop(ctx context.Context) {
	// Panic guard: if handleSessionResponse ever panics on a malformed
	// frame, tear the session down cleanly instead of losing the goroutine
	// silently. Debug tag makes the cause traceable via sessionz.
	defer func() {
		if r := recover(); r != nil {
			recordDebugTag(tagSessionReadLoopPanic)
			s.debugf("readLoop panic: %v", r)
			s.ForceClose(&spb.CloseSessionRequest{
				Reason:      spb.CloseSessionRequest_CLOSE_SESSION_REASON_ERROR,
				Description: fmt.Sprintf("readLoop panic: %v", r),
			})
		}
	}()

	// Supervisor: if ctx is cancelled, mark the session closed so callers
	// observe state immediately. Unblocking Recv() requires the underlying
	// stream's context to be cancelled by the caller.
	readLoopDone := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			s.ForceClose(&spb.CloseSessionRequest{
				Reason:      spb.CloseSessionRequest_CLOSE_SESSION_REASON_USER,
				Description: "client context cancelled",
			})
		case <-readLoopDone:
		}
	}()
	defer close(readLoopDone)

	for {
		resp, err := s.stream.Recv()
		if err != nil {
			s.handleClose(err)
			return
		}
		s.msgsRecv.Add(1)
		s.msgsRecvByType[classifyResp(resp)].Add(1)
		s.handleSessionResponse(resp)
	}
}

// handleSessionResponse dispatches every SessionResponse oneof variant.
// Receiving any recognized frame resets the heartbeat watchdog; unknown
// frames do NOT, so a misbehaving server cannot keep the watchdog
// satisfied with junk payloads and a rogue future oneof variant can't
// mask a broken stream. The watchdog is only armed while a vRPC is
// in-flight anyway — during that window the server MUST be sending
// heartbeats, so a new-variant frame arriving instead of a heartbeat
// within the interval is itself a signal worth surfacing.
func (s *Session) handleSessionResponse(resp *spb.SessionResponse) {
	switch p := resp.GetPayload().(type) {
	case *spb.SessionResponse_OpenSession:
		s.handleOpenSession(p.OpenSession)
	case *spb.SessionResponse_VirtualRpc:
		s.handleVRPCResponse(p.VirtualRpc)
	case *spb.SessionResponse_Error:
		s.handleErrorResponse(p.Error)
	case *spb.SessionResponse_SessionParameters:
		s.handleSessionParameters(p.SessionParameters)
	case *spb.SessionResponse_Heartbeat:
		// Server emits Heartbeats during long-running VRPCs; the deadline
		// reset below is what keeps the watchdog from firing on them.
	case *spb.SessionResponse_GoAway:
		s.handleGoAway(p.GoAway)
	case *spb.SessionResponse_SessionRefreshConfig:
		s.handleSessionRefreshConfig(p.SessionRefreshConfig)
	default:
		recordDebugTag(tagSessionUnknownResponse)
		s.debugf("received SessionResponse with unknown payload type %T", p)
		return
	}
	s.resetHeartbeatDeadline()
}

// handleOpenSession transitions Starting -> Ready and fires hooks.onActive.
// Peer info (from the bigtable-peer-info header) and the TCP remote addr are
// captured synchronously here — gRPC guarantees the header frame precedes
// any bidi message, so by the time we've received the OpenSession response
// the header is already buffered and stream.Header() returns without
// blocking. Ordering: extract first, then fire onActive, so every observer
// sees a session whose PeerInfo (and therefore AfeID) is populated.
//
// The response body itself is intentionally not consumed. Everything the
// client needs (PeerInfo) comes from the stream Header; the reply payload
// has no actionable fields today. If a future server-side change starts
// populating OpenSessionResponse fields, plumb them here.
func (s *Session) handleOpenSession(_ *spb.OpenSessionResponse) {
	if prev, ok := s.transitionTo(StateReady, isState(StateStarting)); !ok {
		// Server confirmed OpenSession while we were in a state that
		// couldn't accept it (already Ready, Closing, or Closed).
		// Either the server sent a duplicate confirmation or we raced
		// with a local teardown — both are bookkeeping oddities.
		recordDebugTag(tagSessionOpenWrongState)
		s.debugf("handleOpenSession in wrong state %s (want Starting)", prev)
		return
	}
	if md, err := s.stream.Header(); err == nil {
		s.extractPeerInfo(md.Get(peerInfoHeaderKey))
	} else {
		s.debugf("stream Header() failed: %v", err)
	}
	if p, ok := peer.FromContext(s.stream.Context()); ok && p.Addr != nil {
		addr := p.Addr.String()
		s.remoteAddr.Store(&addr)
	}
	s.tracer.recordOpen(context.Background(), s.peerInfo.Load(), nil)
	s.hooks.onActive(s)
}

// handleErrorResponse delivers a server ErrorResponse to the in-flight vRPC.
func (s *Session) handleErrorResponse(errResp *spb.ErrorResponse) {
	s.handleVRPCErrorResponse(errResp)
}

// handleSessionParameters updates the heartbeat interval negotiated by the
// server and immediately recomputes the watchdog deadline against the new
// interval.
func (s *Session) handleSessionParameters(params *spb.SessionParametersResponse) {
	if params.KeepAlive == nil {
		return
	}
	interval := params.KeepAlive.AsDuration()
	if interval <= 0 {
		return
	}
	s.heartbeatIntervalNano.Store(int64(interval))
	s.nextHeartbeatDeadlineNano.Store(time.Now().Add(interval).UnixNano())
	// Wake the watchdog: this is the sole path that changes the interval
	// itself (not just the deadline), so the Timer must re-evaluate
	// against the new interval instead of the initialHeartbeatGrace
	// bootstrap it was armed to at NewSession.
	s.wakeHeartbeatLoop()
}

// handleSessionRefreshConfig stores the server-provided refresh configuration
// for later use by reconnection logic.
func (s *Session) handleSessionRefreshConfig(cfg *spb.SessionRefreshConfig) {
	s.refreshConfig.Store(cfg)
	s.debugf("stored SessionRefreshConfig (optimized_open=%t, metadata_entries=%d)",
		cfg.GetOptimizedOpenRequest() != nil, len(cfg.GetMetadata()))
}

// handleGoAway processes a server-initiated GoAway:
//  1. Transitions to StateClosing so pool.CheckoutSession stops handing
//     this session out for new work.
//  2. Stamps "GoAway" as the close reason so it survives the eventual
//     handleClose stamp.
//  3. Does NOT cancel the in-flight RPC. If the server sends the response
//     before dropping the stream, the RPC completes successfully. Only
//     when the stream actually terminates does the RPC get failed with
//     TRANSPORT_FAILURE (via handleClose → cancelActiveRPCs). This grace
//     period matters most for non-idempotent Apply on server graceful
//     drains — a fail-fast on GoAway would surface as spurious errors when
//     the response was already on the wire.
//  4. Spawns a goroutine that drives the session through Closing →
//     WaitServerClose → Closed via s.Close. s.Close already drains the
//     in-flight RPC (waits on s.quiescent, or ForceCloses after its 30s
//     ctx) before sending CloseSession, so the RPC gets its chance
//     without needing extra scheduling here.
//
// A late-arriving GOAWAY from an already terminal session is ignored.
func (s *Session) handleGoAway(goAway *spb.GoAwayResponse) {
	// Sanity-check state BEFORE attempting the transition: GOAWAY landing
	// on a NEW session is a protocol oddity (server shouldn't send GOAWAY
	// before the session is Started). The current predicate below would
	// silently accept it; the assert makes it observable.
	preState := s.State()
	if !assertDebugTagf(preState >= StateStarting, tagSessionGoawayBeforeStart,
		"GOAWAY arrived while session was in %s (want >= Starting)", preState) {
		return
	}
	if _, ok := s.transitionTo(StateClosing, notState(StateClosing, StateWaitServerClose, StateClosed)); !ok {
		// Session is already terminal (Closing, WaitServerClose, or
		// Closed). A late GOAWAY here just races our local teardown —
		// observable but harmless.
		recordDebugTag(tagSessionGoawayAfterClose)
		return
	}
	// Fire onClosing immediately so the pool can pull this session out of
	// routing structures. Firing here (not just from Close's transitionTo
	// below) matters because handleGoAway is the earliest point we know
	// the session is dying — up to waitServerCloseGrace seconds before
	// the actual stream close.
	s.notifyClosing()
	s.setCloseReason("GoAway")

	s.debugf("received GOAWAY reason=%q description=%q",
		goAway.GetReason(), goAway.GetDescription())

	// Drive the lifecycle to completion off the readLoop. s.Close
	// sends CloseSession, transitions to WaitServerClose, and then the
	// pool's stuck-session monitor or the server's EOF moves us to Closed.
	//
	// Derive the drain ctx from the stream's ctx so a pool teardown that
	// cancels the stream also unblocks this drain wait cleanly, instead of
	// hanging up to 30s on a detached background ctx.
	go func() {
		ctx, cancel := context.WithTimeout(s.stream.Context(), 30*time.Second)
		defer cancel()
		_ = s.Close(ctx, &spb.CloseSessionRequest{
			Reason:      spb.CloseSessionRequest_CLOSE_SESSION_REASON_GOAWAY,
			Description: "session closing after server GOAWAY",
		})
	}()
}

// handleClose is invoked when Recv returns an error. It transitions to
// StateClosed (from any non-terminal state — including WaitServerClose
// when the server's EOF arrives after a CloseSession we sent) and cancels
// every remaining in-flight RPC.
//
// The close reason is stamped by upstream paths (Close / ForceClose /
// handleGoAway / heartbeat trip / handleErrorResponse) before handleClose
// runs, and setCloseReason is CompareAndSwap-once — so no reason
// classification is needed here.
func (s *Session) handleClose(err error) {
	prev, ok := s.transitionTo(StateClosed, notState(StateClosed))
	if !ok {
		return
	}
	s.prevStateAtClose.Store(int32(prev))
	// Ready → Closed can happen directly here (server EOFed without a
	// prior GoAway or CloseSession). Guarantee onClosing fires before the
	// notifyClosed below drives onClose. closingOnce makes this a no-op
	// when handleGoAway or Close already fired earlier.
	s.notifyClosing()
	// Fallback close-reason stamp for observability: paths that reach
	// handleClose with no prior stamp (transport-level EOF/Unavailable
	// without a GoAway or client Close) would otherwise fall into
	// sessionz's "Unspecified" bucket. setCloseReason is CAS-once, so
	// upstream stampers (GoAway, MissedHeartbeat, Error, User) still win.
	if err != nil {
		// Special-case io.EOF (graceful server-side shutdown): status.Code(io.EOF)
		// returns codes.Unknown, which would render as "StreamEnd:Unknown" and
		// hide a distinct-and-common signal. Ctx errors (Canceled,
		// DeadlineExceeded) are already mapped by grpc-go's status helpers so
		// the default branch covers them correctly.
		if errors.Is(err, io.EOF) {
			s.setCloseReason("StreamEnd:EOF")
		} else {
			s.setCloseReason("StreamEnd:" + status.Code(err).String())
		}
	}
	s.setCloseErr(err)
	inFlight := 0
	if s.activeVRPC() != nil {
		inFlight = 1
	}
	age := time.Since(s.StartedAt())
	lastRPC := s.nextRPCID.Load()
	peer := s.peerInfoSummary()
	s.recordEvent(SessionEventClose, "age=%v in_flight=%d last_rpc_id=%d %s raw_err=%v",
		age, inFlight, lastRPC, peer, err)
	s.cancelActiveRPCs(unavailable(err, "session closed: %v", err))
	s.signalQuiescent()
	s.notifyClosed(err)
}

// heartbeatLoop watches the session's heartbeat deadline using a single Timer
// that re-arms itself when a frame extends the deadline. The watchdog is
// only enforced while at least one VRPC is in flight: the server emits
// Heartbeats during long-running VRPCs, so an idle session legitimately
// receives no heartbeats and must not be torn down.
func (s *Session) heartbeatLoop(ctx context.Context) {
	timer := time.NewTimer(time.Until(time.Unix(0, s.nextHeartbeatDeadlineNano.Load())))
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			// Timer fired: fall through to re-evaluate.
		case <-s.heartbeatWake:
			// Atomic deadline moved (interval change, initial-grace
			// collapse on first server frame, etc). Drain the pending
			// Timer so the Reset below is safe per time.Timer docs.
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}

		if s.State() == StateClosed {
			return
		}
		interval := time.Duration(s.heartbeatIntervalNano.Load())
		if s.activeVRPC() == nil {
			// Idle session: no heartbeats are expected. Re-check after
			// one interval so a freshly-started VRPC is picked up.
			timer.Reset(interval)
			continue
		}
		remaining := time.Until(time.Unix(0, s.nextHeartbeatDeadlineNano.Load()))
		// last-frame age = interval - remaining (deadline is set to
		// now+interval on every inbound/outbound frame via
		// resetHeartbeatDeadline).
		lastFrameAge := interval - remaining

		if remaining > 0 {
			// Deadline was pushed out while we were sleeping; re-arm.
			// Only record when last_frame_age has crossed one interval —
			// otherwise every healthy session would spam the UI ring
			// buffer and drown out close/missed events.
			if lastFrameAge >= interval {
				s.recordEvent("hb-alive", "last_frame_age=%v remaining=%v interval=%v",
					lastFrameAge, remaining, interval)
			}
			timer.Reset(remaining)
			continue
		}
		// Deadline elapsed with a vRPC in flight — half-dead stream (no
		// frames arriving while we have in-flight work). Log before
		// ForceClose so we have a definitive marker even if downstream
		// cancel races.
		recordDebugTag(tagSessionHeartbeatMissed)
		s.debugf("heartbeat MISSED — forcing close last_frame_age=%v interval=%v",
			lastFrameAge, interval)
		s.recordEvent("hb-missed", "last_frame_age=%v interval=%v",
			lastFrameAge, interval)
		s.ForceClose(&spb.CloseSessionRequest{
			Reason:      spb.CloseSessionRequest_CLOSE_SESSION_REASON_MISSED_HEARTBEAT,
			Description: "client terminated session due to missed server heartbeats",
		})
		return
	}
}

// extractPeerInfo parses the base64-encoded peer info header and caches
// the decoded PeerInfo on the session. Server emits URL-safe base64;
// trailing '=' padding is stripped so a single RawURLEncoding decoder
// handles both shapes.
func (s *Session) extractPeerInfo(peerInfoData []string) {
	if len(peerInfoData) == 0 {
		return
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(peerInfoData[0], "="))
	if err != nil {
		s.debugf("decode base64 PeerInfo failed: %v", err)
		return
	}
	var peerInfo spb.PeerInfo
	if err := proto.Unmarshal(decoded, &peerInfo); err != nil {
		s.debugf("unmarshal PeerInfo proto failed: %v", err)
		return
	}
	s.peerInfo.Store(&peerInfo)
	s.debugf("parsed PeerInfo: transport_type=%s afe=%s",
		btmetrics.TransportTypeName(peerInfo.GetTransportType()), peerInfo.GetApplicationFrontendSubzone())
}

// closeReasonLabel maps a CloseSessionRequest reason to a short human-
// readable category string for the debug UI. Empty when req is nil.
func closeReasonLabel(req *spb.CloseSessionRequest) string {
	if req == nil {
		return ""
	}
	switch req.Reason {
	case spb.CloseSessionRequest_CLOSE_SESSION_REASON_MISSED_HEARTBEAT:
		return "MissedHeartbeat"
	case spb.CloseSessionRequest_CLOSE_SESSION_REASON_GOAWAY:
		return "GoAway"
	case spb.CloseSessionRequest_CLOSE_SESSION_REASON_ERROR:
		return "Error"
	case spb.CloseSessionRequest_CLOSE_SESSION_REASON_USER:
		return "User"
	default:
		// CLOSE_SESSION_REASON_DOWNSIZE folds here — no client code path
		// produces it anymore after passive-scaling migration (replace-
		// on-close replaced the periodic pruneSessions).
		return "Other"
	}
}

// closeReasonToCause maps a CloseSessionRequest reason to a sentinel error,
// or nil when the close is benign (user-initiated, downsize, unset). Callers
// of unavailable(nil, …) get a codes.Unavailable error without a wrapped
// sentinel, so errors.Is against the session sentinels returns false — which
// is the correct signal for "this was an expected shutdown."
func closeReasonToCause(req *spb.CloseSessionRequest) error {
	if req == nil {
		return nil
	}
	switch req.Reason {
	case spb.CloseSessionRequest_CLOSE_SESSION_REASON_MISSED_HEARTBEAT:
		return ErrUnavailableHeartBeatMissed
	case spb.CloseSessionRequest_CLOSE_SESSION_REASON_GOAWAY:
		return ErrUnavailableGoAway
	case spb.CloseSessionRequest_CLOSE_SESSION_REASON_ERROR:
		return ErrUnavailableSessionError
	default:
		return nil
	}
}
