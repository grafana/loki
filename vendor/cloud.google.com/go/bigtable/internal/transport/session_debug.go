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
	"log"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	btmetrics "cloud.google.com/go/bigtable/internal/metrics"
	btopt "cloud.google.com/go/bigtable/internal/option"
)

// sessionDebug groups the Session's observability state — counters,
// per-session event ring, latency histogram, tracer/logger handles, and
// the metric-attribution plumbing (close reason, pool-close gate,
// channel index). Embedded into Session so existing call sites keep
// using bare field access (s.okRpcs, s.recordEvent, s.tracer, ...).
// The split exists so session.go stays focused on the protocol state
// machine and vRPC dispatch; new observability hooks land here.
//
// Writer sites for the counter/close-reason/remote-addr fields land in
// the follow-up session_vrpc.go and session_lifecycle.go PRs; today
// most read-side accessors defined below return zero-values.
type sessionDebug struct {
	logger *log.Logger
	tracer *sessionTracer

	// lastStateChangeNano stamps each successful transitionTo with
	// time.Now().UnixNano(). Approximate ordering across transitions is
	// enough for the debug UI.
	lastStateChangeNano atomic.Int64

	// remoteAddr is the TCP remote (AFE) socket address in "ip:port" form,
	// captured from grpc peer.FromContext once the stream Header returns.
	// atomic.Pointer so the vRPC hot path can read it lock-free to stamp
	// slow-vRPC rows for the sessionz→tcpz cross-link.
	remoteAddr atomic.Pointer[string]

	// okRpcs / errorRpcs are bumped lock-free from the vRPC dispatch paths
	// so the debug UI can read a numeric total without locking.
	okRpcs    atomic.Int64
	errorRpcs atomic.Int64

	// msgsSent / msgsRecv count every Send / Recv frame on the bidi stream
	// — every session-level frame type (OpenSession, vRPC, CloseSession,
	// Heartbeat), not just successful vRPCs.
	msgsSent atomic.Int64
	msgsRecv atomic.Int64

	// Per-frame-type breakdown of msgsSent / msgsRecv. Indexed by
	// reqMsgType / respMsgType (see session_msgtype.go).
	msgsSentByType [numReqMsgTypes]atomic.Int64
	msgsRecvByType [numRespMsgTypes]atomic.Int64

	// retries counts vRPCs that arrived with VRpcAttempt>1 — i.e. the
	// retry interceptor re-issued them on this session.
	retries atomic.Int64

	// closeReason is set exactly once via setCloseReason() at session
	// teardown so SessionPoolImpl.OnClose can attribute the close to a
	// category (Heartbeat / GoAway / Error / User).
	closeReason atomic.Pointer[string]

	// poolCloseRecorded is the once-flag SessionPoolImpl consults so its
	// sessionsClosed / CloseReasons counters bump exactly once per session
	// regardless of which removal path arrives first (proactive prune,
	// CheckoutSession dead-detect, or hooks.OnClose).
	poolCloseRecorded atomic.Bool

	// latencyMu guards latencySamples — a tiny ring buffer of the last
	// latencyWindow server-reported BackendLatency values, used to compute
	// p50/p95/p99 in the debug UI.
	latencyMu      sync.Mutex
	latencySamples []time.Duration
	latencyNext    int

	// clusterCounts tallies per-ClusterInformation.ClusterId responses.
	// Values are *atomic.Int64.
	clusterCounts sync.Map

	// channelIndex is set once at construction to the BigtableChannelPool
	// connEntry index the session's bidi stream was placed on. -1 when
	// the underlying pool isn't a BigtableChannelPool (e.g. test setups
	// using option.WithGRPCConn) or when the pick wasn't observed.
	channelIndex atomic.Int32

	// eventsMu guards the per-session debug-event ring surfaced in
	// sessionz. maxSessionEvents caps the size; eventsNext is the write
	// cursor once the buffer wraps.
	eventsMu   sync.Mutex
	events     []SessionEvent
	eventsNext int
}

// init sets the non-zero defaults for a freshly-embedded sessionDebug.
// Called once from NewSession. Takes an already-constructed tracer so
// the sessionType parameter (redundant with Session.sessionType) doesn't
// have to be threaded through.
func (d *sessionDebug) init(tracer *sessionTracer) {
	d.tracer = tracer
	d.lastStateChangeNano.Store(time.Now().UnixNano())
	d.channelIndex.Store(-1)
}

// SessionEventKind is the closed set of debug-event categories emitted
// into the per-session ring buffer. A typed const (rather than a bare
// string) keeps writers honest as they land in the follow-up vRPC and
// lifecycle PRs.
type SessionEventKind string

// Enumerated SessionEventKind values.
const (
	// SessionEventClose fires from handleClose on stream tear-down.
	// Message carries reason, age, in-flight count, last rpc id, raw err.
	SessionEventClose SessionEventKind = "close"
	// SessionEventHBMissed fires from the heartbeat watchdog when it force-
	// closes. Message carries in-flight count and last-frame age.
	SessionEventHBMissed SessionEventKind = "hb-missed"
	// SessionEventHBAlive fires when the heartbeat tick observes in-flight
	// RPC(s) while a recent frame already pushed the deadline out — useful
	// for spotting "server kept stream alive but lost specific vRPC
	// response" stalls. Suppressed unless lastFrameAge is at least one
	// heartbeat interval.
	SessionEventHBAlive SessionEventKind = "hb-alive"
	// SessionEventCtxDone fires when Session.Invoke's per-attempt wait is
	// killed by the caller's context (deadline or cancel). Message carries
	// method, rpc id, time waited, ctx err, session state. Sole vRPC-drop-
	// adjacent event kind — drainSlot serializes exactly one caller per
	// resultChan, so a two-writers race on resultChan is unreachable.
	SessionEventCtxDone SessionEventKind = "ctx-done"
	// SessionEventRetry fires from noteRetryAttempt when Invoke sees
	// VRpcAttempt>1 on the ctx. Message carries attempt, method, and the
	// prior attempt's gRPC code + err (if the retry interceptor stashed
	// one on ctx).
	SessionEventRetry SessionEventKind = "retry"
	// SessionEventProtocolError fires when routeVRPCFrame observes a
	// state/frame combination that violates the client-server contract:
	// a frame arrived in a state readLoop shouldn't be running in
	// (New/Starting/Closed) OR the server's rpc_id didn't match our
	// active vRPC. Escalated to session teardown via ForceClose so the
	// pool's OnClosing/OnClose hooks fire and the session leaves the
	// AFE routing set. Kept separate from SessionEventLateFrame so
	// operators filtering sessionz for genuine desyncs aren't swamped
	// by benign late-after-cancel drops.
	SessionEventProtocolError SessionEventKind = "protocol-error"
	// SessionEventLateFrame fires when routeVRPCFrame drops a frame
	// because there's no active vRPC on this session (activeVRPC()==nil).
	// This is a documented race — the caller ctx.Done'd and cancelled
	// the slot, but the server's response arrived before the cancel
	// landed on the wire. Not a protocol violation; the session stays
	// healthy and the frame is dropped.
	SessionEventLateFrame SessionEventKind = "late-frame"
)

// SessionEvent is one entry in a session's per-session debug ring buffer.
// Surfaced through SessionSnapshot.RecentEvents and merged across all
// sessions into PoolSnapshot.RecentEvents for the sessionz UI.
type SessionEvent struct {
	At      time.Time
	Kind    SessionEventKind
	Message string
}

const maxSessionEvents = 64

// recordEvent appends a SessionEvent to the per-session ring buffer.
// Safe to call from any goroutine (readLoop, heartbeatLoop, etc.). Uses
// the same wrap-index scheme as latencySamples so an at-cap append is
// O(1), not an O(N) shift.
func (s *Session) recordEvent(kind SessionEventKind, format string, args ...interface{}) {
	if !s.debugEnabled {
		return
	}
	ev := SessionEvent{
		At:      time.Now(),
		Kind:    kind,
		Message: fmt.Sprintf(format, args...),
	}
	s.eventsMu.Lock()
	if len(s.events) < maxSessionEvents {
		s.events = append(s.events, ev)
	} else {
		s.events[s.eventsNext] = ev
		s.eventsNext = (s.eventsNext + 1) % maxSessionEvents
	}
	s.eventsMu.Unlock()
}

// snapshotEvents returns a copy of the session's debug-event ring buffer,
// oldest first. Unwraps around eventsNext when the buffer has wrapped.
func (s *Session) snapshotEvents() []SessionEvent {
	s.eventsMu.Lock()
	out := make([]SessionEvent, len(s.events))
	if len(s.events) < maxSessionEvents {
		copy(out, s.events)
	} else {
		n := copy(out, s.events[s.eventsNext:])
		copy(out[n:], s.events[:s.eventsNext])
	}
	s.eventsMu.Unlock()
	return out
}

// peerInfoSummary renders the session's PeerInfo as a compact single line
// suitable for inclusion in log messages and debug events. Returns
// "peer=unknown" when the bidi stream header hasn't been parsed yet.
func (s *Session) peerInfoSummary() string {
	p := s.peerInfo.Load()
	if p == nil {
		return "peer=unknown"
	}
	return fmt.Sprintf("peer={afe=%d/%s/%s gfe=%d transport=%s}",
		p.GetApplicationFrontendId(),
		p.GetApplicationFrontendRegion(),
		p.GetApplicationFrontendSubzone(),
		p.GetGoogleFrontendId(),
		btmetrics.TransportTypeName(p.GetTransportType()))
}

const latencyWindow = 256

// recordLatency appends a server-reported BackendLatency sample to the
// ring buffer. Called from Invoke whenever the response includes Stats.
func (s *Session) recordLatency(d time.Duration) {
	if !s.debugEnabled {
		return
	}
	if d <= 0 {
		return
	}
	s.latencyMu.Lock()
	if len(s.latencySamples) < latencyWindow {
		s.latencySamples = append(s.latencySamples, d)
	} else {
		s.latencySamples[s.latencyNext] = d
		s.latencyNext = (s.latencyNext + 1) % latencyWindow
	}
	s.latencyMu.Unlock()
}

// snapshotLatencies returns a sorted copy of the current samples so
// callers can compute percentiles without holding the lock.
func (s *Session) snapshotLatencies() []time.Duration {
	s.latencyMu.Lock()
	out := make([]time.Duration, len(s.latencySamples))
	copy(out, s.latencySamples)
	s.latencyMu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// percentile returns the p-th percentile (0-100) of the sorted slice.
// Uses nearest-rank — small N, linear interpolation isn't worth the
// complexity.
func percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	if p >= 100 {
		return sorted[len(sorted)-1]
	}
	idx := int(float64(len(sorted)-1) * p / 100)
	return sorted[idx]
}

// recordCluster increments the per-cluster response counter.
func (s *Session) recordCluster(id string) {
	if !s.debugEnabled {
		return
	}
	if id == "" {
		return
	}
	c, _ := s.clusterCounts.LoadOrStore(id, new(atomic.Int64))
	c.(*atomic.Int64).Add(1)
}

// snapshotClusters returns the per-cluster response counts as a flat map.
func (s *Session) snapshotClusters() map[string]int64 {
	out := map[string]int64{}
	s.clusterCounts.Range(func(k, v interface{}) bool {
		out[k.(string)] = v.(*atomic.Int64).Load()
		return true
	})
	return out
}

// setChannelIndex records the BigtableChannelPool connEntry index the
// session's stream landed on. Package-private — the only writer is the
// pool wiring in the follow-up PRs.
func (s *Session) setChannelIndex(idx int32) {
	s.channelIndex.Store(idx)
}

// ChannelIndex returns the per-pool channel index, or -1 if unset.
func (s *Session) ChannelIndex() int32 {
	return s.channelIndex.Load()
}

// setCloseReason records the reason for the session's terminal close.
// Only the first call sticks; subsequent calls (racing teardown paths)
// are ignored so the OnClose callback sees a stable value.
func (s *Session) setCloseReason(reason string) {
	if reason == "" {
		// Every teardown path is supposed to name a reason so the
		// close-reasons dashboard bucket makes sense; an empty call
		// means someone forgot to plumb a label.
		recordDebugTag(tagSessionCloseNoReason)
		return
	}
	s.closeReason.CompareAndSwap(nil, &reason)
}

// CloseReason returns the recorded close reason, or "" if none was set.
func (s *Session) CloseReason() string {
	if p := s.closeReason.Load(); p != nil {
		return *p
	}
	return ""
}

// SampleUptime records the session's current alive time into the
// session.uptime histogram. No-op if the session hasn't finished opening.
func (s *Session) SampleUptime(ctx context.Context) {
	s.tracer.sampleUptime(ctx, s.peerInfo.Load())
}

// RecordTransportOverhead emits a per-vRPC transport-overhead sample —
// (stream − backend) — to the transport_latencies histogram.
func (s *Session) RecordTransportOverhead(ctx context.Context, method string, overhead time.Duration) {
	s.tracer.recordTransportOverhead(ctx, s.peerInfo.Load(), method, overhead)
}

// Retries returns the number of vRPCs Invoke processed with AttemptNumber > 1.
func (s *Session) Retries() int64 { return s.retries.Load() }

// StartedAt returns the wall-clock time the session was constructed —
// the single anchor for every session-scoped metric and the ancestor of
// "session age" in debug views. Immutable after NewSession, safe to
// read lock-free.
func (s *Session) StartedAt() time.Time {
	return s.tracer.StartedAt()
}

// HasOkRpcs reports whether the session served at least one successful vRPC.
func (s *Session) HasOkRpcs() bool { return s.okRpcs.Load() > 0 }

// HasErrorRpcs reports whether the session served at least one failed vRPC.
func (s *Session) HasErrorRpcs() bool { return s.errorRpcs.Load() > 0 }

// OkRpcs returns the total number of successful vRPC responses delivered
// on this session.
func (s *Session) OkRpcs() int64 { return s.okRpcs.Load() }

// ErrorRpcs returns the total number of failed vRPC responses delivered
// on this session.
func (s *Session) ErrorRpcs() int64 { return s.errorRpcs.Load() }

// MsgsSent returns the total number of frames sent on this session's
// bidi stream (every Send, regardless of payload type).
func (s *Session) MsgsSent() int64 { return s.msgsSent.Load() }

// MsgsRecv returns the total number of frames received on this session's
// bidi stream (every successful Recv).
func (s *Session) MsgsRecv() int64 { return s.msgsRecv.Load() }

// RemoteAddr returns the TCP remote (AFE) socket address in "ip:port" form,
// or "" if the stream Header hasn't been observed yet or gRPC didn't
// populate peer info.
func (s *Session) RemoteAddr() string {
	if p := s.remoteAddr.Load(); p != nil {
		return *p
	}
	return ""
}

// debugf logs at debug level if a logger has been attached.
func (s *Session) debugf(format string, args ...interface{}) {
	if s.logger == nil {
		return
	}
	btopt.Debugf(s.logger, "bigtable_session %s: "+format, append([]interface{}{s.logName}, args...)...)
}

// WithSessionLogger attaches a logger for diagnostic output. Without it,
// the session's debugf calls no-op.
func WithSessionLogger(logger *log.Logger) SessionOption {
	return func(s *Session) { s.logger = logger }
}

// WithSessionPoolName stamps the pool-scoped name used for the
// session_name label on session lifecycle metrics — cardinality stays
// bounded by the number of pools per process, not per session.
func WithSessionPoolName(name string) SessionOption {
	return func(s *Session) { s.tracer.setPoolName(name) }
}

// setCloseErr records the raw error that ended the stream. First writer
// wins so a follow-up close path (e.g. cancelActiveRPCs) can't overwrite
// the original cause. Nil is treated as "no error" and ignored.
func (s *Session) setCloseErr(err error) {
	if err == nil {
		return
	}
	s.closeErr.CompareAndSwap(nil, &err)
}

// closeError returns the raw error that ended the stream, or nil if
// none was recorded. Consulted by the pool to surface the underlying
// server rejection on consecutive-failure trips.
func (s *Session) closeError() error {
	if p := s.closeErr.Load(); p != nil {
		return *p
	}
	return nil
}
