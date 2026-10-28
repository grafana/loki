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
	"fmt"
	"sync/atomic"
	"time"

	spb "cloud.google.com/go/bigtable/apiv2/bigtablepb"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// activeVRPC returns the currently in-flight vRPC, or nil if the slot is
// empty. Snapshot read under slotMu — the field's docstring covers the
// multiplex=1 invariant and the slot lifecycle.
func (s *Session) activeVRPC() *vrpcImpl {
	s.slotMu.Lock()
	rpc := s.activeRPC
	s.slotMu.Unlock()
	return rpc
}

// claimSlot assigns rpc to the empty slot. Returns false if the slot still
// holds a prior vRPC — concurrent claims are rejected as UNCOMMITTED at
// the call site.
func (s *Session) claimSlot(rpc *vrpcImpl) bool {
	s.slotMu.Lock()
	defer s.slotMu.Unlock()
	if s.activeRPC != nil {
		return false
	}
	s.activeRPC = rpc
	return true
}

// markCancelled records ctx.Done cancellation of rpc without freeing the
// slot — the caller returns, but activeRPC stays until the server response
// arrives to drain it. First-cancel-wins; a racing drain that clears
// activeRPC makes this a no-op. Returns whether rpc was still the active
// slot occupant, so callers that also want the "still in flight?" signal
// don't have to re-acquire slotMu with a separate activeVRPC() call.
func (s *Session) markCancelled(rpc *vrpcImpl, res vrpcResult) (stillActive bool) {
	s.slotMu.Lock()
	defer s.slotMu.Unlock()
	if s.activeRPC != rpc {
		return false
	}
	if s.currentCancel == nil {
		s.currentCancel = &res
	}
	return true
}

// drainSlot atomically clears the (activeRPC, currentCancel) pair iff
// activeRPC == expect, returning what was there. Used by response handlers
// (after id-match) and by cancelActiveRPCs (session teardown); ok=false
// means a racing drain got here first.
func (s *Session) drainSlot(expect *vrpcImpl) (rpc *vrpcImpl, cancel *vrpcResult, ok bool) {
	s.slotMu.Lock()
	defer s.slotMu.Unlock()
	if s.activeRPC != expect {
		return nil, nil, false
	}
	rpc = s.activeRPC
	cancel = s.currentCancel
	s.activeRPC = nil
	s.currentCancel = nil
	return rpc, cancel, true
}

// Invoke executes a single virtual RPC on this session and returns every
// observable output of the roundtrip — decoded response, cluster info,
// server-reported Stats, and the local SentAt timestamp — so callers can
// populate metrics (client_blocking_latency, server_backend_latency) and
// respect server-supplied retry hints without losing data on the way out of
// the transport.
//
// The caller MUST have exclusive access to this Session for the duration
// of the call — the pool guarantees this via CheckoutSession's per-session
// idle-slot gate. The single-in-flight invariant is enforced by claimSlot
// under slotMu; a losing claim returns StateUncommitted so the retry
// oracle can steer to another session. The slot is released only by
// handleVRPCResponse / handleVRPCErrorResponse (successful drain),
// cancelActiveRPCs (session teardown), or the Send-failure path below —
// caller ctx.Done leaves the slot claimed and marks it via markCancelled.
func (s *Session) Invoke(ctx context.Context, desc VRpcDescriptor, req interface{}) (result InvokeResult, err error) {
	startTime := time.Now()

	if st := State(s.state.Load()); st != StateReady {
		return result, tagErr(StateUncommitted,
			unavailable(ErrSessionNotActive, "session is not active (state: %v)", st))
	}

	reqBytes, err := desc.Encode(req)
	if err != nil {
		return result, tagErr(StateUncommitted, fmt.Errorf("encode vRPC request: %w", err))
	}

	rpcID := s.nextRPCID.Add(1)
	result.RPCIDOnSession = rpcID
	rpc := &vrpcImpl{
		id:         rpcID,
		method:     desc.Method(),
		resultChan: make(chan vrpcResult, 1),
	}

	// Claim the single in-flight slot. A losing claim means a prior vRPC
	// on this session has not drained on the wire yet — return Uncommitted
	// so the retryer picks another session.
	if !s.claimSlot(rpc) {
		return result, tagErr(StateUncommitted,
			unavailable(ErrSessionNotActive,
				"session %q busy: prior vRPC has not drained on the wire", s.logName))
	}

	// Reset the heartbeat deadline whenever we send an outbound frame: the
	// server's keepalive clock is implicitly reset by our activity.
	s.resetHeartbeatDeadline()

	attempt := currentAttempt(ctx)
	if attempt > 1 {
		s.noteRetryAttempt(ctx, desc.Method(), attempt)
	}
	sessionReq := buildInvokeRequest(ctx, rpcID, reqBytes, attempt, startTime)

	// Capture SentAt immediately before the frame is handed to Send so
	// downstream metrics can compute client-side blocking latency as
	// (SentAt - attemptStart) without double-counting encode/setup overhead.
	result.SentAt = time.Now()
	if sendErr := s.Send(sessionReq); sendErr != nil {
		// Synchronous Send failed: no server response is ever coming,
		// so this call must free the slot itself. Other paths don't need
		// this branch — sendMessage there is async and failures come
		// back through the response observer — but Go's Send is
		// synchronous, so an early drain is on the Invoke path.
		s.drainSlot(rpc)
		// Drain is the sole "session became free" signal. Fire the pool
		// wake here as well — ReleaseToPool's inExpectedCount guard
		// (session_list.go) drops the re-enqueue when OnSessionClosing
		// already dropped this handle in the teardown that the Send
		// failure typically kicks off, so an unguarded emit here is safe
		// against the concurrent-teardown race gemini flagged.
		s.hooks.onSlotDrained()
		if State(s.state.Load()) == StateClosing {
			s.signalQuiescent()
		}
		return result, tagErr(StateTransportFailure, fmt.Errorf("send vRPC request: %w", sendErr))
	}

	err = s.awaitInvokeResult(ctx, rpc, desc, &result)
	return result, err
}

// currentAttempt reads the retry-interceptor's attempt tag from ctx. Calls
// that bypass RetryingVRpc have no tag; treat them as attempt 1.
func currentAttempt(ctx context.Context) int64 {
	if a := int64(VRpcAttempt(ctx)); a > 0 {
		return a
	}
	return 1
}

// noteRetryAttempt bumps the per-session retries counter and — if the
// retry interceptor stashed the prior attempt's error on ctx — emits a
// debug + sessionz event tagged with the prior gRPC code so operators
// can trace WHY the retry fired without cross-referencing logs.
func (s *Session) noteRetryAttempt(ctx context.Context, method string, attempt int64) {
	s.retries.Add(1)
	prev := PrevAttemptErr(ctx)
	if prev == nil {
		return
	}
	prevCode := status.Code(prev).String()
	s.debugf("retry attempt=%d method=%s prev_code=%s prev_err=%v",
		attempt, method, prevCode, prev)
	s.recordEvent(SessionEventRetry, "attempt=%d method=%s prev_code=%s prev_err=%v",
		attempt, method, prevCode, prev)
}

// buildInvokeRequest constructs the wire-level SessionRequest envelope
// for a single vRPC attempt. Pure — no I/O, no side effects. The
// VirtualRpc.Deadline field carries the remaining budget as a duration
// so the server measures from receive time rather than an absolute wall
// clock. Omitted when ctx has no deadline or the budget is already
// non-positive (the client-side ctx.Done branch will fire immediately).
func buildInvokeRequest(ctx context.Context, rpcID int64, reqBytes []byte, attempt int64, startTime time.Time) *spb.SessionRequest {
	virtRPC := &spb.VirtualRpcRequest{
		RpcId:   rpcID,
		Payload: reqBytes,
		Metadata: &spb.VirtualRpcRequest_Metadata{
			AttemptNumber: attempt,
			AttemptStart:  timestamppb.New(startTime),
		},
	}
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining > 0 {
			virtRPC.Deadline = durationpb.New(remaining)
		}
	}
	return &spb.SessionRequest{
		Payload: &spb.SessionRequest_VirtualRpc{VirtualRpc: virtRPC},
	}
}

// awaitInvokeResult blocks until either ctx cancels or the server
// delivers a result on rpc.resultChan. Populates the tail of result
// (TransportLatency, ClusterInfo, Stats, Response) and returns a
// fully-tagged error on failure. res.err from resultChan is already
// tagged at the source (cancelActiveRPCs → StateTransportFailure);
// server error frames arrive as res.errResp and are unpacked here into
// a StateServerResult-tagged error.
//
// ctx.Done branch: check resultChan non-blockingly first so a server
// response that landed in the same tick as the ctx cancellation isn't
// dropped. Otherwise mark the slot as cancelled — activeRPC stays set
// so a concurrent Invoke fails claimSlot with Uncommitted rather than
// racing to id-mismatch the abandoned response.
func (s *Session) awaitInvokeResult(ctx context.Context, rpc *vrpcImpl, desc VRpcDescriptor, result *InvokeResult) error {
	select {
	case <-ctx.Done():
		select {
		case res := <-rpc.resultChan:
			return s.processResult(desc, result, res)
		default:
		}
		cancelErr := tagErr(StateTransportFailure, ctx.Err())
		stillActive := s.markCancelled(rpc, vrpcResult{err: cancelErr})
		s.recordCtxDone(ctx, rpc, desc.Method(), result.SentAt, stillActive)
		return cancelErr
	case res := <-rpc.resultChan:
		return s.processResult(desc, result, res)
	}
}

// processResult unpacks a resultChan payload into result and returns
// the tagged error (nil on success). Extracted so awaitInvokeResult's
// ctx.Done race-guard can share the same success path when a response
// beat the cancellation into resultChan by a tick.
//
// The res.resp.RpcId == rpc.id check that used to live here is gone:
// under slotMu, handleVRPCResponse gates the id match BEFORE drainSlot,
// so deliver can only ever put a matching-id response into resultChan.
func (s *Session) processResult(desc VRpcDescriptor, result *InvokeResult, res vrpcResult) error {
	wire := time.Since(result.SentAt)
	ci := res.ClusterInfo()
	result.ClusterInfo = ci
	if ci != nil {
		s.recordCluster(ci.GetClusterId())
	}
	if res.err != nil {
		return res.err
	}
	if res.errResp != nil {
		return tagErr(StateServerResult, errorResponseToErr(res.errResp))
	}
	respMsg, decodeErr := desc.Decode(res.resp.Payload)
	if decodeErr != nil {
		// StateServerResult per SESSION_SPEC #9: the server sent a
		// response, so the request likely COMMITTED even if we can't
		// parse the reply. Retrying an idempotent-only-under-strict-ID
		// mutation could double-apply. Callers with a genuine idempotency
		// token can override at the retry-oracle level.
		return tagErr(StateServerResult, fmt.Errorf("decode vRPC response: %w", decodeErr))
	}
	result.Response = respMsg
	result.Stats = res.resp.Stats
	if res.resp.Stats != nil && res.resp.Stats.BackendLatency != nil {
		backend := res.resp.Stats.BackendLatency.AsDuration()
		s.recordLatency(backend)
		result.TransportLatency = wire - backend
	}
	return nil
}

// recordCtxDone emits the debug + sessionz event for a ctx cancellation
// or deadline fire while a vRPC was in flight. stillActive comes from
// markCancelled's return so no second slotMu take is needed here — it
// reports whether rpc still held the slot at cancel time, useful for
// spotting races between our cancel and a late server response.
func (s *Session) recordCtxDone(ctx context.Context, rpc *vrpcImpl, method string, sentAt time.Time, stillActive bool) {
	sessState := State(s.state.Load())
	waited := time.Since(sentAt)
	peer := s.peerInfoSummary()
	s.debugf("vRPC %s rpc_id=%d ctx.Done waited=%v err=%v session_state=%v still_in_flight=%v %s",
		method, rpc.id, waited, ctx.Err(), sessState, stillActive, peer)
	s.recordEvent(SessionEventCtxDone, "method=%s rpc_id=%d waited=%v err=%v session_state=%v still_in_flight=%v %s",
		method, rpc.id, waited, ctx.Err(), sessState, stillActive, peer)
}

// handleVRPCResponse delivers a server VirtualRpcResponse to the waiting
// Invoke caller, or drains a cancelled slot if the caller already
// abandoned the RPC via ctx.Done (bookkeeping-only in that branch).
func (s *Session) handleVRPCResponse(resp *spb.VirtualRpcResponse) {
	s.routeVRPCFrame(resp.RpcId, "VirtualRpcResponse", tagSessionVRPCNil,
		&s.okRpcs, vrpcResult{resp: resp})
}

// handleVRPCErrorResponse routes per-vRPC errors to the waiting caller.
// Mirrors handleVRPCResponse's drain path — a cancelled slot drains
// without deliver.
func (s *Session) handleVRPCErrorResponse(errResp *spb.ErrorResponse) {
	s.routeVRPCFrame(errResp.RpcId, "ErrorResponse", tagSessionVRPCErrorNil,
		&s.errorRpcs, vrpcResult{errResp: errResp})
}

// routeVRPCFrame is the shared skeleton for handleVRPCResponse /
// handleVRPCErrorResponse. All frame gating (state check, active-vRPC
// nil guard, id-match, drain-vs-cancel branching, OnSlotDrained hook
// on every drain, quiescence signalling in Closing) lives here so the
// two call sites can't drift.
//
// Frame gating (per-scenario handling, per mutianf review on #20213 —
// silent drops are dangerous; escalate genuine protocol violations to
// a full session teardown so the pool removes the session from routing
// and the caller isn't left waiting on a session the server has
// desynced from):
//
//   - State ∈ {Ready, Closing, WaitServerClose}: expected. The drain
//     window keeps accepting frames until the server's EOF.
//   - State ∈ {New, Starting, Closed}: unrecoverable state-tracking
//     inconsistency. Emit SessionEventProtocolError + ForceClose to
//     get the OnClosing/OnClose hooks to remove the session from the
//     pool's AFE routing (cancelActiveRPCs alone leaves a Ready
//     session orphaned from the AFE queue). In practice this branch
//     is unreachable in production — readLoop only runs post-Start
//     (state ≥ Ready) and ForceClose no-ops on Closed via the
//     transitionTo CAS. Kept as belt-and-suspenders: cheap, and the
//     id-mismatch branch below IS live under wire corruption.
//   - Nil active vRPC: documented ctx.Done race — the caller cancelled
//     the slot and the server's response landed after. NOT a protocol
//     violation. Emit SessionEventLateFrame (separate kind so operators
//     grep-ing "protocol-error" don't see benign late-frame drops),
//     drop the frame, keep the session healthy.
//   - RPC id mismatch: genuine protocol desync (server sent a response
//     for a different id than our active vRPC). Emit
//     SessionEventProtocolError + ForceClose so the caller gets a
//     terminal error AND the session leaves the pool cleanly.
//
// The two call sites differ only in:
//   - frameName / nilTag / counter — labels + which atomic bumps
//   - result — vrpcResult{resp:} vs vrpcResult{errResp:} handed to deliver
//
// drainSlot returning !ok means cancelActiveRPCs (session teardown)
// won the slot between the id-match above and our drainSlot call. Its
// deliver already fired the terminal error; nothing to do.
func (s *Session) routeVRPCFrame(rpcID int64, frameName, nilTag string, counter *atomic.Int64, result vrpcResult) {
	st := s.State()
	if st != StateReady && st != StateClosing && st != StateWaitServerClose {
		recordDebugTagAt(lvl.Error, tagSessionVRPCResponseWrongState)
		s.recordEvent(SessionEventProtocolError,
			"%s for rpc_id=%d arrived in state %s — force-closing session",
			frameName, rpcID, st)
		s.ForceClose(&spb.CloseSessionRequest{
			Reason:      spb.CloseSessionRequest_CLOSE_SESSION_REASON_ERROR,
			Description: fmt.Sprintf("protocol error: %s for rpc_id=%d in state %s", frameName, rpcID, st),
		})
		return
	}
	rpc := s.activeVRPC()
	if rpc == nil {
		recordDebugTag(nilTag)
		s.recordEvent(SessionEventLateFrame,
			"%s for rpc_id=%d dropped — no in-flight RPC tracked (late frame after ctx.Done cancel)",
			frameName, rpcID)
		return
	}
	if rpc.id != rpcID {
		recordDebugTag(tagSessionVRPCIDMismatch)
		s.recordEvent(SessionEventProtocolError,
			"%s rpc_id=%d != in-flight rpc_id=%d — server desync, force-closing session",
			frameName, rpcID, rpc.id)
		s.ForceClose(&spb.CloseSessionRequest{
			Reason:      spb.CloseSessionRequest_CLOSE_SESSION_REASON_ERROR,
			Description: fmt.Sprintf("protocol error: %s rpc_id=%d != active rpc_id=%d", frameName, rpcID, rpc.id),
		})
		return
	}
	drained, cancel, ok := s.drainSlot(rpc)
	if !ok {
		return
	}
	counter.Add(1)
	if cancel != nil {
		// Caller already returned via ctx.Done — no one is waiting on
		// resultChan. Just count the drain for observability.
		recordDebugTag(tagSessionVRPCCancelledDrained)
	} else {
		// resultChan is cap-1 and drainSlot serialized this write; the
		// send never blocks.
		drained.resultChan <- result
	}
	// v3: drainSlot success is the sole "session became free" signal.
	// Fires on every drain (not just the cancelled branch) so the pool
	// re-enqueues the session in its AFE idle queue and wakes one
	// parked Checkout waiter. Invoke's return path in the pool no
	// longer performs the re-enqueue or the wake.
	s.hooks.onSlotDrained()
	if State(s.state.Load()) == StateClosing {
		s.signalQuiescent()
	}
}

// errorResponseToErr converts a server ErrorResponse frame into a Go
// error carrying the gRPC status code and any RetryInfo the server
// attached. RetryInfo is packed into the status details so downstream
// consumers (notably RetryingVRpc) can recover it via status.FromError.
// WithDetails returns a fresh *Status on success; on the rare failure
// (e.g. anypb marshal) we fall back to the bare status so the error path
// still propagates the server's code.
func errorResponseToErr(errResp *spb.ErrorResponse) error {
	if errResp.Status == nil {
		return fmt.Errorf("unknown vRPC error (rpc_id=%d)", errResp.RpcId)
	}
	st := status.FromProto(errResp.Status)
	if errResp.RetryInfo != nil {
		if withDetails, derr := st.WithDetails(errResp.RetryInfo); derr == nil {
			st = withDetails
		}
	}
	return st.Err()
}

// cancelActiveRPCs cancels the in-flight vRPC (if any) with the given
// error. With multiPlexingLimit=1 there is at most one such vRPC.
// Called from session teardown paths (Close, ForceClose, handleGoAway,
// handleClose, heartbeat miss) — the "server will never respond" cases
// where the slot must be freed unilaterally. If the caller already
// abandoned via ctx.Done, the drain returns a cancel handle and we
// skip the deliver (no reader on resultChan).
func (s *Session) cancelActiveRPCs(err error) {
	rpc := s.activeVRPC()
	if rpc == nil {
		return
	}
	drained, cancel, ok := s.drainSlot(rpc)
	if !ok {
		// Concurrent handleVRPCResponse / handleVRPCErrorResponse
		// cleared the slot; that caller already delivered.
		return
	}
	if cancel != nil {
		// Caller already returned via ctx.Done; no one to deliver to.
		return
	}
	// Session-side cancellation: session died / GoAway / heartbeat missed
	// / benign shutdown while an RPC was in-flight. Server may or may not
	// have processed — TransportFailure classification lets idempotent ops
	// retry and prevents non-idempotent ones from double-applying.
	drained.resultChan <- vrpcResult{err: tagErr(StateTransportFailure, err)}
}
