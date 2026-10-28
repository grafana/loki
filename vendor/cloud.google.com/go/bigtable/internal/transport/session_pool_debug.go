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

// Debug / observability ring buffers and histograms for SessionPoolImpl.
// Feeds sessionz / loadz / metric exporters; none of the pool's
// operational logic depends on it.

package internal

import (
	"math/bits"
	"sync"
	"sync/atomic"
	"time"

	btopt "cloud.google.com/go/bigtable/internal/option"
)

// poolMetrics owns every observability-only field for SessionPoolImpl:
// counters, ring buffers, and histograms. Embedded by value on the pool
// and accessed as p.m.<field>.
type poolMetrics struct {
	// Lifecycle counters, lock-free.
	sessionsOpened atomic.Int64
	sessionsClosed atomic.Int64
	listenerFires  atomic.Int64
	closesByReason sync.Map // close-reason label → *atomic.Int64

	// Scaling history ring buffer.
	scalingHistoryMu sync.Mutex
	scalingHistory   []ScalingEvent

	// Slow-vRPC log ring buffer.
	slowVRpcsMu sync.Mutex
	slowVRpcs   []SlowVRpcEvent

	// Time-series sparkline ring buffer + rate-computation state.
	timeSeriesMu    sync.Mutex
	timeSeries      []TimeSeriesSample
	tsLastOkRpcs    int64
	tsLastErrorRpcs int64

	// Session-lifetime ring buffer.
	lifetimesMu sync.Mutex
	lifetimes   []time.Duration

	// Picker-decision ring buffer + per-AFE counters.
	pickHistoryMu   sync.Mutex
	pickHistory     []PickHistoryEvent
	pickHistoryHead int
	afePickCounts   map[AfeID]int64

	// Pool-wide latency histograms — lifetime-of-pool, survive session
	// churn (per-session ring buffers cap at 256 samples each).
	backendLatencyHist   latencyHist
	totalLatencyHist     latencyHist
	transportLatencyHist latencyHist
}

// SlowVRpcEvent is one row in the pool's slow-vRPC log.
type SlowVRpcEvent struct {
	At      time.Time
	Method  string
	Latency time.Duration
	Session string // log name of the session that handled the call
	Success bool
	ErrCode string // grpc status code on failure, empty on success
	// PoolWait is how long the caller spent inside CheckoutSession —
	// where saturation queue wait lives.
	PoolWait time.Duration
	// BackendLatency is server-reported processing time; zero if the
	// server didn't populate Stats.
	BackendLatency time.Duration
	// TransportLatency = (stream Send→Recv) − BackendLatency — wire +
	// AFE + client-decode overhead outside server processing. Zero
	// when BackendLatency is missing or the call errored pre-Recv.
	TransportLatency time.Duration
	// RPCIDOnSession is the per-session 1-indexed RPC id; small values
	// indicate a freshly-opened session.
	RPCIDOnSession int64
	// SessionAge is time since the session entered StateReady; zero if
	// the session never reached Ready.
	SessionAge time.Duration
	// Peer is the AFE/GFE the session was bound to. Empty on very young
	// sessions where the bidi header hasn't been parsed yet.
	Peer PeerInfoSnapshot
	// RemoteAddr is the AFE socket address ("ip:port") from gRPC peer
	// info; sessionz links it into tcpz for TCP_INFO lookup.
	RemoteAddr string
}

const (
	maxSlowVRpcs         = 100
	defaultSlowThreshold = 10 * time.Millisecond
	// maxTimeSeries: 5 min at the 1-Hz heartbeat.
	maxTimeSeries = 300
	maxLifetimes  = 512
)

// LifetimeBuckets are the sessionz lifetime buckets, smallest-first;
// spans sub-minute churn through multi-hour long-lived sessions.
var LifetimeBuckets = []struct {
	Label string
	Max   time.Duration
}{
	{"<10s", 10 * time.Second},
	{"<1m", time.Minute},
	{"<5m", 5 * time.Minute},
	{"<30m", 30 * time.Minute},
	{"<1h", time.Hour},
	{"<6h", 6 * time.Hour},
	{"<24h", 24 * time.Hour},
	{"≥24h", time.Duration(1<<62 - 1)},
}

// recordLifetime appends a completed session lifetime to the ring buffer.
// Called from each pool removal site that has a known createdAt for the
// session being retired.
func (p *SessionPoolImpl) recordLifetime(d time.Duration) {
	if !p.debugEnabled {
		return
	}
	if d <= 0 {
		return
	}
	p.m.lifetimesMu.Lock()
	defer p.m.lifetimesMu.Unlock()
	if len(p.m.lifetimes) >= maxLifetimes {
		copy(p.m.lifetimes, p.m.lifetimes[1:])
		p.m.lifetimes = p.m.lifetimes[:len(p.m.lifetimes)-1]
	}
	p.m.lifetimes = append(p.m.lifetimes, d)
}

func (p *SessionPoolImpl) snapshotLifetimes() []time.Duration {
	p.m.lifetimesMu.Lock()
	out := make([]time.Duration, len(p.m.lifetimes))
	copy(out, p.m.lifetimes)
	p.m.lifetimesMu.Unlock()
	return out
}

// Bucket i covers [2^i, 2^(i+1)) ns, spanning ~1ns .. ~1000s.
const latencyHistBuckets = 40

// latencyHist is a lock-free log2-bucket histogram sized for pool-wide
// latency percentiles over the pool's full lifetime — constant memory,
// atomic-add on record, ≤2× worst-case tail interpolation error.
type latencyHist struct {
	buckets [latencyHistBuckets]atomic.Uint64
}

// record adds one observation; non-positive durations are ignored.
func (h *latencyHist) record(d time.Duration) {
	if d <= 0 {
		return
	}
	// floor(log2(ns)); bits.Len64(x) is one more than that.
	ns := uint64(d)
	b := bits.Len64(ns) - 1
	if b < 0 {
		b = 0
	}
	if b >= latencyHistBuckets {
		b = latencyHistBuckets - 1
	}
	h.buckets[b].Add(1)
}

// snapshot returns p50/p95/p99 and the total sample count. Lock-free;
// concurrent record() may skew results by at most the write rate over
// the snapshot window.
func (h *latencyHist) snapshot() (p50, p95, p99 time.Duration, n uint64) {
	var counts [latencyHistBuckets]uint64
	for i := range h.buckets {
		counts[i] = h.buckets[i].Load()
		n += counts[i]
	}
	if n == 0 {
		return
	}
	p50 = interpLatencyPercentile(counts[:], n, 50)
	p95 = interpLatencyPercentile(counts[:], n, 95)
	p99 = interpLatencyPercentile(counts[:], n, 99)
	return
}

// interpLatencyPercentile walks the bucket counts and linearly
// interpolates the target position inside the containing bucket. Caller
// guarantees n > 0.
func interpLatencyPercentile(counts []uint64, n uint64, pct float64) time.Duration {
	target := uint64(float64(n) * pct / 100)
	if target == 0 {
		target = 1
	}
	var cum uint64
	for i, c := range counts {
		if c == 0 {
			continue
		}
		if cum+c >= target {
			lo := uint64(1) << i
			hi := uint64(1) << (i + 1)
			frac := float64(target-cum) / float64(c)
			return time.Duration(lo + uint64(float64(hi-lo)*frac))
		}
		cum += c
	}
	// Only reachable on numerical edge cases (target rounded above
	// total); fall back to the last non-empty bucket's upper bound.
	for i := len(counts) - 1; i >= 0; i-- {
		if counts[i] > 0 {
			return time.Duration(uint64(1) << (i + 1))
		}
	}
	return 0
}

// TimeSeriesSample is one point in a pool's sparkline ring buffer.
// All counters are deltas since the previous sample so the chart shows
// rate-per-second rather than running totals.
type TimeSeriesSample struct {
	At        time.Time
	Sessions  int
	OkPerSec  float64
	ErrPerSec float64
	InUse     int
	Pending   int
}

func (p *SessionPoolImpl) recordTimeSeries() {
	if !p.debugEnabled {
		return
	}
	handles := p.sl.AllHandles()
	totalSessions := len(handles)
	inUse := 0
	var okTotal, errTotal int64
	for _, sh := range handles {
		if sh == nil || sh.session == nil {
			continue
		}
		if sh.outstanding.Load() > 0 {
			inUse++
		}
		okTotal += sh.session.okRpcs.Load()
		errTotal += sh.session.errorRpcs.Load()
	}
	// Pending = pool-boundary queue depth, same as Stats().PendingCount.
	pending := int(p.waitersCount.Load())

	now := time.Now()
	p.m.timeSeriesMu.Lock()
	defer p.m.timeSeriesMu.Unlock()

	var okRate, errRate float64
	if len(p.m.timeSeries) > 0 {
		prev := p.m.timeSeries[len(p.m.timeSeries)-1]
		dt := now.Sub(prev.At).Seconds()
		if dt > 0 {
			okRate = float64(okTotal-p.m.tsLastOkRpcs) / dt
			errRate = float64(errTotal-p.m.tsLastErrorRpcs) / dt
		}
	}
	p.m.tsLastOkRpcs = okTotal
	p.m.tsLastErrorRpcs = errTotal

	sample := TimeSeriesSample{
		At:        now,
		Sessions:  totalSessions,
		OkPerSec:  okRate,
		ErrPerSec: errRate,
		InUse:     inUse,
		Pending:   pending,
	}
	if len(p.m.timeSeries) >= maxTimeSeries {
		copy(p.m.timeSeries, p.m.timeSeries[1:])
		p.m.timeSeries = p.m.timeSeries[:len(p.m.timeSeries)-1]
	}
	p.m.timeSeries = append(p.m.timeSeries, sample)
}

func (p *SessionPoolImpl) snapshotTimeSeries() []TimeSeriesSample {
	p.m.timeSeriesMu.Lock()
	defer p.m.timeSeriesMu.Unlock()
	out := make([]TimeSeriesSample, len(p.m.timeSeries))
	copy(out, p.m.timeSeries)
	return out
}

// recordSlowVRpc appends to the slow-vRPC ring buffer. Only called from
// SessionPoolImpl.Invoke after a call exceeds the threshold.
func (p *SessionPoolImpl) recordSlowVRpc(ev SlowVRpcEvent) {
	if !p.debugEnabled {
		return
	}
	p.m.slowVRpcsMu.Lock()
	defer p.m.slowVRpcsMu.Unlock()
	if len(p.m.slowVRpcs) >= maxSlowVRpcs {
		copy(p.m.slowVRpcs, p.m.slowVRpcs[1:])
		p.m.slowVRpcs = p.m.slowVRpcs[:len(p.m.slowVRpcs)-1]
	}
	p.m.slowVRpcs = append(p.m.slowVRpcs, ev)
}

// recordCheckoutFailure feeds the pool latency histogram and, when the
// wait exceeded the slow threshold, appends a slow-vRPC row for a call
// that never got a session. Empty Session cell in the sessionz table
// marks "checkout never returned a handle".
func (p *SessionPoolImpl) recordCheckoutFailure(checkoutStart time.Time, desc VRpcDescriptor, err error) {
	poolWait := time.Since(checkoutStart)
	p.m.totalLatencyHist.record(poolWait)
	if poolWait <= defaultSlowThreshold {
		return
	}
	ev := SlowVRpcEvent{
		At:       checkoutStart,
		Method:   desc.Method(),
		Latency:  poolWait,
		PoolWait: poolWait,
	}
	ev.ErrCode = statusOf(err).Code().String()
	btopt.Debugf(nil, "POOL %s slow checkout failed method=%s pool_wait=%v code=%s raw_err=%v",
		p.poolName, ev.Method, poolWait, ev.ErrCode, err)
	p.recordSlowVRpc(ev)
}

func (p *SessionPoolImpl) snapshotSlowVRpcs() []SlowVRpcEvent {
	p.m.slowVRpcsMu.Lock()
	defer p.m.slowVRpcsMu.Unlock()
	out := make([]SlowVRpcEvent, len(p.m.slowVRpcs))
	copy(out, p.m.slowVRpcs)
	return out
}

// maxPickHistory sizes the pick-decision ring for ~1s of decisions at a
// few thousand picks/sec.
const maxPickHistory = 500

// PickHistoryEvent is one row in the pick-decision log. Populated even
// for no-candidate picks so loadz can show the fallback frequency.
type PickHistoryEvent struct {
	At       time.Time
	Decision PickDecision
	// PickerName is captured at record time so older entries retain
	// their picker attribution across UpdateConfig swaps.
	PickerName string
}

// recordPickDecision appends a picker outcome and increments the
// per-AFE pick counter for the winner. pickerName is a parameter to
// avoid a re-entrant p.mu acquisition (CheckoutSession already holds
// p.mu when it reads p.picker.Name()).
func (p *SessionPoolImpl) recordPickDecision(d PickDecision, pickerName string) {
	if !p.debugEnabled {
		return
	}
	ev := PickHistoryEvent{
		At:         time.Now(),
		Decision:   d,
		PickerName: pickerName,
	}
	p.m.pickHistoryMu.Lock()
	// Circular append: constant-time overwrite once at cap.
	if len(p.m.pickHistory) < maxPickHistory {
		p.m.pickHistory = append(p.m.pickHistory, ev)
	} else {
		p.m.pickHistory[p.m.pickHistoryHead] = ev
		p.m.pickHistoryHead++
		if p.m.pickHistoryHead == maxPickHistory {
			p.m.pickHistoryHead = 0
		}
	}
	if d.Winner != 0 {
		p.m.afePickCounts[d.Winner]++
	}
	p.m.pickHistoryMu.Unlock()
}

// snapshotPickHistory returns a copy of the pick-decision ring,
// oldest-first. Safe to call concurrently with recordPickDecision.
func (p *SessionPoolImpl) snapshotPickHistory() []PickHistoryEvent {
	p.m.pickHistoryMu.Lock()
	defer p.m.pickHistoryMu.Unlock()
	out := make([]PickHistoryEvent, len(p.m.pickHistory))
	if len(p.m.pickHistory) < maxPickHistory {
		copy(out, p.m.pickHistory)
	} else {
		// Full ring: oldest lives at pickHistoryHead. Copy the tail
		// then the head so output is oldest-first.
		n := copy(out, p.m.pickHistory[p.m.pickHistoryHead:])
		copy(out[n:], p.m.pickHistory[:p.m.pickHistoryHead])
	}
	return out
}

// snapshotAfePickCounts returns a copy of the per-AFE cumulative pick
// counter map. Used by loadz to compute actual-share vs. ideal-share.
func (p *SessionPoolImpl) snapshotAfePickCounts() map[AfeID]int64 {
	p.m.pickHistoryMu.Lock()
	defer p.m.pickHistoryMu.Unlock()
	out := make(map[AfeID]int64, len(p.m.afePickCounts))
	for k, v := range p.m.afePickCounts {
		out[k] = v
	}
	return out
}
