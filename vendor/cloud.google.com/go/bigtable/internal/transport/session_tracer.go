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
	"sync"
	"sync/atomic"
	"time"

	spb "cloud.google.com/go/bigtable/apiv2/bigtablepb"
	metricsinternal "cloud.google.com/go/bigtable/internal/metrics"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"google.golang.org/grpc/status"
)

// Session-scoped histograms. They are registered once at process startup via
// InitializeSessionMetrics and shared across every session.
var (
	sessionDurations     metric.Float64Histogram
	sessionOpenLatencies metric.Float64Histogram
	sessionUptime        metric.Float64Histogram
	// transportLatencies is per-vRPC (not per-session-lifecycle), but shares
	// the same meter + registration path so all session-adjacent metrics
	// initialize together — matches java-bigtable's MetricRegistry layout.
	transportLatencies metric.Float64Histogram

	sessionMetricsOnce sync.Once
	sessionMetricsErr  error
)

// FineGrainLatencyBounds matches java-bigtable's
// AGGREGATION_WITH_MILLIS_HISTOGRAM: fine sub-ms + coarse tail. Shared
// by transport_latencies and attempt_latencies2.
var FineGrainLatencyBounds = []float64{
	// Linear 0 → 3ms by 0.1ms (31 boundaries): fine-grained sub-ms.
	0.0, 0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9,
	1.0, 1.1, 1.2, 1.3, 1.4, 1.5, 1.6, 1.7, 1.8, 1.9,
	2.0, 2.1, 2.2, 2.3, 2.4, 2.5, 2.6, 2.7, 2.8, 2.9, 3.0,
	// Coarse 4ms → 80ms.
	4.0, 5.0, 6.0, 8.0, 10.0, 13.0, 16.0, 20.0, 25.0, 30.0, 40.0, 50.0, 65.0, 80.0,
	// Coarse 100ms → 900ms.
	100.0, 130.0, 160.0, 200.0, 250.0, 300.0, 400.0, 500.0, 650.0, 800.0, 900.0,
	// Coarse 1s → 50s.
	1000.0, 2000.0, 3000.0, 4000.0, 5000.0, 6000.0, 10000.0, 20000.0, 50000.0,
	// Long tail: 100s → 5000s (~83 min).
	100000.0, 200000.0, 500000.0, 1000000.0, 2000000.0, 5000000.0,
}

// InitializeSessionMetrics registers the session histograms against the given
// meter provider. It runs at most once for the lifetime of the process;
// subsequent calls (with any provider, including nil) return the result of
// the first call. Passing nil on the first call leaves the histograms unset
// and returns nil.
func InitializeSessionMetrics(meterProvider metric.MeterProvider) error {
	sessionMetricsOnce.Do(func() {
		if meterProvider == nil {
			return
		}
		meter := meterProvider.Meter(clientMeterName)

		var err error
		if sessionDurations, err = meter.Float64Histogram(
			"session.durations",
			metric.WithDescription("Duration a session was alive (startTime → close)"),
			metric.WithUnit("ms"),
		); err != nil {
			sessionMetricsErr = fmt.Errorf("create session.durations histogram: %w", err)
			return
		}
		if sessionOpenLatencies, err = meter.Float64Histogram(
			"session.open_latencies",
			metric.WithDescription("Latency to open a session"),
			metric.WithUnit("ms"),
		); err != nil {
			sessionMetricsErr = fmt.Errorf("create session.open_latencies histogram: %w", err)
			return
		}
		if sessionUptime, err = meter.Float64Histogram(
			"session.uptime",
			metric.WithDescription("Age of currently-active sessions, sampled periodically"),
			metric.WithUnit("ms"),
		); err != nil {
			sessionMetricsErr = fmt.Errorf("create session.uptime histogram: %w", err)
			return
		}
		if transportLatencies, err = meter.Float64Histogram(
			"transport_latencies",
			metric.WithDescription("The latency measured from e2e latencies minus node latencies."),
			metric.WithUnit("ms"),
			metric.WithExplicitBucketBoundaries(FineGrainLatencyBounds...),
		); err != nil {
			sessionMetricsErr = fmt.Errorf("create transport_latencies histogram: %w", err)
			return
		}
		if err = registerDebugTagCounter(meter); err != nil {
			sessionMetricsErr = err
			return
		}
	})
	return sessionMetricsErr
}

// sessionTracer tracks and records metrics for a Session's lifecycle and
// individual operations.
//
// Field lifecycle (all fields are single-writer-at-construction except
// `opened`; readers are lock-free):
//
//   - startTime / sessionType — set once in newSessionTracer; immutable.
//   - poolName — set once via setPoolName, called from WithSessionPoolName
//     during NewSession before Session.Start spawns any goroutine.
//     Pool-scoped name matches java-bigtable's SessionPoolInfo name
//     semantics (bounded cardinality, one per pool per process) — NOT
//     the per-session logName (unbounded).
//   - opened — atomic.Bool flipped once by recordOpen; read by
//     recordClose / sampleUptime for the `ready` label.
//
// peerInfo is NOT stored on the tracer. It lives on Session as an
// atomic.Pointer (single source of truth per SESSION_SPEC #3 — set
// once, synchronously, in handleOpenSession) and is passed in at every
// record*/sample* call. Duplicating it on the tracer would be shadow
// state with a separate synchronization mechanism.
//
// startTime is the single wall-clock anchor for every emitted metric —
// mirrors java-bigtable's SessionTracerImpl.uptime stopwatch, started
// once in onStart() and driving session.open_latencies (start → open),
// session.durations (start → close), and session.uptime (start →
// sample). `opened` fills the same role as Java's State enum for the
// ready label.
type sessionTracer struct {
	startTime   time.Time
	sessionType SessionType
	poolName    string
	opened      atomic.Bool
}

// newSessionTracer starts the "open" timer.
func newSessionTracer(sessionType SessionType) *sessionTracer {
	return &sessionTracer{
		startTime:   time.Now(),
		sessionType: sessionType,
	}
}

// setPoolName stamps the pool-scoped name used for the session_name label
// on every emitted metric. Called from WithSessionPoolName during
// NewSession — before Session.Start spawns any goroutine, so no
// synchronization is required.
func (t *sessionTracer) setPoolName(name string) {
	t.poolName = name
}

// StartedAt returns the wall-clock time the session was constructed —
// i.e. the anchor for every session-scoped metric. Immutable after
// newSessionTracer, so read lock-free.
func (t *sessionTracer) StartedAt() time.Time {
	return t.startTime
}

// peerInfoLabels derives the (transport_type, afe_location) metric-label
// pair from a PeerInfo. Nil peerInfo yields ("unknown", "") — matching
// pre-handshake state. Callers pass Session.PeerInfo() (an
// atomic.Pointer load — SESSION_SPEC #3 makes the pointer set-once, so
// the returned strings are stable for the session's lifetime).
func peerInfoLabels(pi *spb.PeerInfo) (transportType, afeLocation string) {
	if pi == nil {
		return "unknown", ""
	}
	return metricsinternal.TransportTypeName(pi.GetTransportType()), pi.GetApplicationFrontendSubzone()
}

// recordOpen records the latency to open the session and flips the
// `opened` flag so subsequent recordClose / sampleUptime label the
// session as ready. peerInfo is the caller-supplied Session.PeerInfo()
// at the moment of the OpenSession completion; may be nil if the
// handshake failed before headers arrived.
func (t *sessionTracer) recordOpen(ctx context.Context, peerInfo *spb.PeerInfo, err error) {
	t.opened.Store(true)

	if sessionOpenLatencies == nil {
		return
	}
	transportType, afeLocation := peerInfoLabels(peerInfo)
	statusStr := "OK"
	if err != nil {
		statusStr = status.Code(err).String()
	}
	sessionOpenLatencies.Record(ctx, msSince(t.startTime), metric.WithAttributes(
		attribute.String("transport_type", transportType),
		attribute.String("status", statusStr),
		attribute.String("session_type", t.sessionType.String()),
		attribute.String("afe_location", afeLocation),
		attribute.String("session_name", t.poolName),
	))
}

// recordClose records the session's total elapsed time on close.
//   - peerInfo: Session.PeerInfo() at close time (may be nil if the
//     session died pre-handshake).
//   - closingReason: the terminal Session.CloseReason(), always
//     non-empty — Session synthesizes a fallback if the close path
//     forgot to set one, mirroring Java's SessionImpl.notifyTerminalClose
//     guard (SessionImpl.java:782-793).
//   - streamErr: the terminal stream error; nil means clean close ("OK").
//   - hadOk / hadErr: whether the session served any OK / error vRPCs.
//
// Duration is always startTime→now — matches java-bigtable
// SessionTracerImpl.onClose (`uptime.elapsed()`, where uptime is anchored
// at onStart). The `ready` label distinguishes sessions that reached Ready
// from those that died pre-open.
func (t *sessionTracer) recordClose(ctx context.Context, peerInfo *spb.PeerInfo, closingReason string, streamErr error, hadOk, hadErr bool) {
	if sessionDurations == nil {
		return
	}
	var elapsed float64
	if !t.startTime.IsZero() {
		elapsed = msSince(t.startTime)
	}
	transportType, afeLocation := peerInfoLabels(peerInfo)

	statusStr := "OK"
	if streamErr != nil {
		statusStr = status.Code(streamErr).String()
	}

	sessionDurations.Record(ctx, elapsed, metric.WithAttributes(
		attribute.String("transport_type", transportType),
		attribute.String("status", statusStr),
		attribute.String("session_type", t.sessionType.String()),
		attribute.String("closing_reason", closingReason),
		attribute.String("vrpcs", vrpcCloseState(hadOk, hadErr)),
		attribute.Bool("ready", t.opened.Load()),
		attribute.String("afe_location", afeLocation),
		attribute.String("session_name", t.poolName),
	))
}

// vrpcCloseState mirrors java-bigtable's SessionCloseVRpcState.find —
// (hadOk, hadErr) → {none, all_ok, all_error, some_ok}. The labels match
// Java exactly so cross-language dashboards work.
func vrpcCloseState(hadOk, hadErr bool) string {
	switch {
	case hadOk && hadErr:
		return "some_ok"
	case hadOk:
		return "all_ok"
	case hadErr:
		return "all_error"
	default:
		return "none"
	}
}

// sampleUptime records the current alive time (startTime → now) of a
// still-active session into sessionUptime, with a `ready` label sourced
// from the tracer's `opened` flag. Called periodically from the pool
// heartbeat so the histogram represents the distribution of ages across
// currently-active sessions. Java parity: SessionTracerImpl.recordAsyncMetrics
// records uptime.elapsed() with `state == Ready` as the label.
func (t *sessionTracer) sampleUptime(ctx context.Context, peerInfo *spb.PeerInfo) {
	if sessionUptime == nil {
		return
	}
	if t.startTime.IsZero() {
		return
	}
	transportType, afeLocation := peerInfoLabels(peerInfo)
	sessionUptime.Record(ctx, msSince(t.startTime), metric.WithAttributes(
		attribute.String("transport_type", transportType),
		attribute.String("session_type", t.sessionType.String()),
		attribute.Bool("ready", t.opened.Load()),
		attribute.String("afe_location", afeLocation),
		attribute.String("session_name", t.poolName),
	))
}

func msSince(t time.Time) float64 {
	return float64(time.Since(t)) / float64(time.Millisecond)
}

// recordTransportOverhead emits a per-vRPC (stream − backend) sample into
// the transport_latencies histogram. Java's ClientTransportLatency defines
// this metric as "e2e latencies minus node latencies" — the wire + AFE
// overhead outside server processing. Caller has already validated the
// delta is positive; this is a no-op if the metric isn't registered.
// Method is the vRPC method name (e.g. "ExecuteVRpc/Read").
func (t *sessionTracer) recordTransportOverhead(ctx context.Context, peerInfo *spb.PeerInfo, method string, overhead time.Duration) {
	if transportLatencies == nil || overhead <= 0 {
		return
	}
	transportType, afeLocation := peerInfoLabels(peerInfo)
	transportLatencies.Record(ctx, float64(overhead)/float64(time.Millisecond), metric.WithAttributes(
		attribute.String("transport_type", transportType),
		attribute.String("session_type", t.sessionType.String()),
		attribute.String("afe_location", afeLocation),
		attribute.String("session_name", t.poolName),
		attribute.String("method", method),
	))
}
