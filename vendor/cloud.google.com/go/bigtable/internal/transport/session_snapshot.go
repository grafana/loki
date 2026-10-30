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
	"reflect"
	"sort"
	"time"

	spb "cloud.google.com/go/bigtable/apiv2/bigtablepb"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// openRequestMarshaler formats decoded OpenSessionRequest payloads for the
// debug UI: multiline JSON, snake_case names, omit empties.
var openRequestMarshaler = protojson.MarshalOptions{
	Multiline:       true,
	Indent:          "  ",
	UseProtoNames:   true,
	EmitUnpopulated: false,
}

// buildOpenRequestSnapshot decodes the OpenSessionRequest's Payload into
// the message type indicated by sessionType and renders it (plus the
// feature-flags wrapper) as protojson for the debug UI. Returns nil when
// the pool has no template request (rare — only happens in tests that
// inject a session directly).
func buildOpenRequestSnapshot(req *spb.OpenSessionRequest, sessionType SessionType) *OpenRequestSnapshot {
	if req == nil {
		return nil
	}
	out := &OpenRequestSnapshot{ProtocolVersion: req.ProtocolVersion}

	var inner proto.Message
	switch sessionType {
	case SessionTypeTable:
		out.PayloadType = "OpenTableRequest"
		inner = &spb.OpenTableRequest{}
	case SessionTypeAuthorizedView:
		out.PayloadType = "OpenAuthorizedViewRequest"
		inner = &spb.OpenAuthorizedViewRequest{}
	case SessionTypeMaterializedView:
		out.PayloadType = "OpenMaterializedViewRequest"
		inner = &spb.OpenMaterializedViewRequest{}
	default:
		out.PayloadType = "unknown"
	}

	if inner != nil && len(req.Payload) > 0 {
		if err := proto.Unmarshal(req.Payload, inner); err == nil {
			if b, mErr := openRequestMarshaler.Marshal(inner); mErr == nil {
				out.PayloadJSON = string(b)
			}
		}
	}
	if req.Flags != nil {
		if b, err := openRequestMarshaler.Marshal(req.Flags); err == nil {
			out.FlagsJSON = string(b)
		}
	}
	return out
}

// SessionSnapshot is an immutable, allocation-friendly snapshot of one
// Session's debugging state. All fields are value types so the snapshot can
// be marshaled (JSON / template) without re-acquiring any session lock.
type SessionSnapshot struct {
	LogName         string
	State           string
	SessionType     string
	LastStateChange time.Time
	OkRpcs          int64
	ErrorRpcs       int64
	Retries         int64
	MsgsSent        int64
	MsgsRecv        int64
	// MsgsSentByType / MsgsRecvByType break the totals above down by the
	// SessionRequest / SessionResponse oneof payload type. Keys come from the
	// reqMsgType / respMsgType String() methods (e.g. "VirtualRpc", "Heartbeat").
	// Buckets with a zero count are omitted to keep the rendered cell short.
	MsgsSentByType map[string]int64
	MsgsRecvByType map[string]int64
	ActiveRpcs     int
	CloseReason    string
	// LatencyP50/95/99 are computed from the server-reported BackendLatency
	// values cached on the session (last 256 samples). Zero when the session
	// hasn't seen any responses with Stats populated yet.
	LatencyP50 time.Duration
	LatencyP95 time.Duration
	LatencyP99 time.Duration
	LatencyN   int // number of samples in the window
	// ClusterCounts is the per-ClusterInformation.ClusterId response tally
	// (e.g. {"cluster-c1": 412, "cluster-c2": 198}). Empty until the server
	// has attached ClusterInformation to at least one response.
	ClusterCounts map[string]int64
	// ChannelIndex is the BigtableChannelPool connEntry index the session's
	// bidi stream landed on, or -1 when unknown (non-Bigtable channel pool).
	ChannelIndex      int
	HeartbeatInterval time.Duration
	NextHeartbeat     time.Time
	HasRefreshConfig  bool
	Peer              PeerInfoSnapshot
	Handle            SessionHandleSnapshot
	// RecentEvents is the per-session debug-event ring buffer (close,
	// heartbeat-missed, heartbeat-alive while in-flight, ctx-done). Capped
	// at maxSessionEvents and ordered oldest-first. Empty for healthy
	// long-lived sessions.
	RecentEvents []SessionEvent
}

// PeerInfoSnapshot is a JSON-friendly mirror of the relevant fields of
// spb.PeerInfo. Empty fields indicate the server has not yet sent peer info
// (which arrives asynchronously via the response header).
type PeerInfoSnapshot struct {
	TransportType              string
	GoogleFrontendID           int64
	ApplicationFrontendID      int64
	ApplicationFrontendRegion  string
	ApplicationFrontendSubzone string
}

// SessionHandleSnapshot captures the per-handle pool bookkeeping.
// Per-session PeakEwma was moved to the per-AFE tracker on afeHandle
// as part of the AFE-grouping refactor — surface those via the pool's
// afez view instead.
type SessionHandleSnapshot struct {
	Outstanding  int64
	LastActivity time.Time
	Picks        int64
}

// PoolSnapshot is a snapshot of one SessionPoolImpl, including every session
// currently in the pool. Sessions are listed in their pool order; callers may
// re-sort as they wish.
type PoolSnapshot struct {
	Name          string
	SessionType   string
	MinSessions   int
	MaxSessions   int
	PickerType    string
	ReadyCount    int
	StartingCount int
	InUseCount    int
	PendingCount  int
	TotalSessions int
	Sessions      []SessionSnapshot
	CapturedAt    time.Time
	// Lifecycle aggregates surfaced via PoolSnapshot.
	SessionsOpened int64
	SessionsClosed int64
	CloseReasons   map[string]int64
	ListenerFires  int64
	Throttler      ThrottlerSnapshot
	ScalingHistory []ScalingEvent
	// OpenRequest captures the OpenSessionRequest template used to handshake
	// every session in this pool — protocol version, feature flags, and the
	// decoded inner payload (OpenTable / OpenAuthorizedView /
	// OpenMaterializedView). All sessions in a given pool share this exact
	// request, so it's surfaced per-pool rather than per-session.
	OpenRequest *OpenRequestSnapshot
	// Pool-level aggregates derived from the per-session snapshots and
	// non-session pool state.
	ClusterCounts map[string]int64
	// StateCounts is the per-state population summary across every session
	// currently in the pool: e.g. {"Ready": 5, "Closing": 1,
	// "WaitServerClose": 2}. Lets the debug UI render "how many are
	// healthy right now" without rescanning each row. Keys come from
	// State.String() so they line up with the per-session State column.
	StateCounts map[string]int
	// LatencyP50/95/99/N: server-reported BackendLatency aggregated
	// across all sessions in the pool.
	LatencyP50 time.Duration
	LatencyP95 time.Duration
	LatencyP99 time.Duration
	LatencyN   int
	// TotalLatencyP50/95/99/N: end-to-end wall-clock latency as
	// observed by the caller of SessionPoolImpl.Invoke — includes
	// pool-boundary checkout wait + network + decode + BackendLatency.
	// The gap between Total and Backend percentiles is the client-side
	// overhead (mostly CheckoutSession wait when the pool is hot).
	TotalLatencyP50 time.Duration
	TotalLatencyP95 time.Duration
	TotalLatencyP99 time.Duration
	TotalLatencyN   int
	// TransportLatencyP50/95/99/N: ClientTransportLatency
	// = (stream Send→Recv) − server-reported BackendLatency. Isolates
	// wire + AFE + client-decode overhead outside server processing.
	// Samples are excluded when either half is missing (pre-Recv
	// failure, no server Stats) or the delta is non-positive (clock
	// skew) so p50 isn't dragged toward 0.
	TransportLatencyP50 time.Duration
	TransportLatencyP95 time.Duration
	TransportLatencyP99 time.Duration
	TransportLatencyN   int
	SlowVRpcs           []SlowVRpcEvent
	// RecentEvents is the pool-wide merge of every session's RecentEvents,
	// sorted newest-first and capped at maxPoolRecentEvents. Lets the
	// sessionz UI render a single timeline of session-lifecycle anomalies
	// (closes, missed heartbeats, ctx-done stalls) without scrolling
	// through every session row.
	RecentEvents []PoolSessionEvent
	TimeSeries   []TimeSeriesSample
	// Session-lifetime distribution (built from the pool's lifetime ring
	// buffer of completed sessions). LifetimeHistogram is the bucket-label
	// → count list in the order defined by LifetimeBuckets; percentile
	// fields are computed over the same window.
	LifetimeHistogram []LifetimeBucketCount
	LifetimeP50       time.Duration
	LifetimeP95       time.Duration
	LifetimeP99       time.Duration
	LifetimeN         int
	// AFEs is the per-AFE view of the pool's sessionList — the picker's
	// primary bucketing unit. One entry per AFE the pool has ever seen
	// (empty buckets aged past afePruneMaxIdle are GC'd). Populated from
	// sessionList.Snapshot so afez / sessionz can render the fanout
	// without reaching into the internal type.
	AFEs []AfeSnapshotRow
}

// LoadBalancingSnapshot is the pool's view of its picker state, decision
// history, and per-AFE fanout — the input to the loadz debug page. Not
// embedded in PoolSnapshot because it's a heavier surface (ring buffer,
// cumulative counters) and only the loadz page consumes it; sessionz /
// afez use the lighter AFEs slice on PoolSnapshot instead.
type LoadBalancingSnapshot struct {
	// PoolName / PickerName echo the pool identity + current picker so
	// the loadz page can render self-contained rows per pool.
	PoolName   string
	PickerName string

	// AFEs mirrors PoolSnapshot.AFEs so loadz can render the per-AFE
	// table without cross-referencing another snapshot.
	AFEs []AfeSnapshotRow

	// PickCounts is the cumulative per-AFE pick tally over the pool's
	// lifetime. Loadz computes actual-share as PickCounts[afe] /
	// sum(PickCounts).
	PickCounts map[int64]int64

	// Recent is the ring buffer of the last N pick decisions,
	// newest-last. Powers the "recent picks" table.
	Recent []PickHistoryEvent

	// CapturedAt is the snapshot wall-clock.
	CapturedAt time.Time
}

// LifetimeBucketCount is one bar in the session-lifetime histogram.
type LifetimeBucketCount struct {
	Label string
	Count int
}

// PoolSessionEvent is a SessionEvent tagged with the originating session's
// LogName so the pool-level merged timeline in sessionz can attribute each
// entry without nesting.
type PoolSessionEvent struct {
	At      time.Time
	Kind    string
	Session string
	Message string
}

// maxPoolRecentEvents caps the merged pool-level event timeline so the
// sessionz render stays bounded under high churn. Sized to comfortably
// hold a multi-minute incident across a busy pool (≈8× the per-session
// cap × a handful of misbehaving sessions).
const maxPoolRecentEvents = 500

// OpenRequestSnapshot is the JSON-friendly form of the OpenSessionRequest.
// PayloadType names the inner-message kind ("OpenTable",
// "OpenAuthorizedView", "OpenMaterializedView", or "unknown"); PayloadJSON
// is the inner message rendered via protojson — that's the field that
// answers "what was this session opened for?". FlagsJSON renders the
// FeatureFlags proto so operators can see what the client asked for.
type OpenRequestSnapshot struct {
	ProtocolVersion int64
	PayloadType     string
	PayloadJSON     string
	FlagsJSON       string
}

// Snapshot returns a debug-friendly snapshot of the session. Reads every
// field lock-free via atomics; safe to call concurrently with Invoke and
// with any lifecycle transition. The individual field reads are not
// cross-consistent, but the sessionz UI treats each row as an approximate
// point-in-time picture, which is exactly what atomics give us.
func (s *Session) Snapshot() SessionSnapshot {
	state := State(s.state.Load())
	logName := s.logName
	lastChange := time.Unix(0, s.lastStateChangeNano.Load())
	hbInterval := time.Duration(s.heartbeatIntervalNano.Load())
	nextHb := time.Unix(0, s.nextHeartbeatDeadlineNano.Load())
	activeRpcs := 0
	if rpc := s.activeVRPC(); rpc != nil {
		activeRpcs = 1
	}
	peer := s.peerInfo.Load()
	hasRefresh := s.refreshConfig.Load() != nil
	sessionType := s.sessionType

	sortedLat := s.snapshotLatencies()
	return SessionSnapshot{
		LogName:           logName,
		State:             state.String(),
		SessionType:       sessionType.String(),
		LastStateChange:   lastChange,
		OkRpcs:            s.okRpcs.Load(),
		ErrorRpcs:         s.errorRpcs.Load(),
		Retries:           s.retries.Load(),
		MsgsSent:          s.msgsSent.Load(),
		MsgsRecv:          s.msgsRecv.Load(),
		MsgsSentByType:    sentByType(s),
		MsgsRecvByType:    recvByType(s),
		ActiveRpcs:        activeRpcs,
		CloseReason:       s.CloseReason(),
		ClusterCounts:     s.snapshotClusters(),
		ChannelIndex:      int(s.ChannelIndex()),
		LatencyP50:        percentile(sortedLat, 50),
		LatencyP95:        percentile(sortedLat, 95),
		LatencyP99:        percentile(sortedLat, 99),
		LatencyN:          len(sortedLat),
		HeartbeatInterval: hbInterval,
		NextHeartbeat:     nextHb,
		HasRefreshConfig:  hasRefresh,
		Peer:              peerInfoToSnapshot(peer),
		RecentEvents:      s.snapshotEvents(),
	}
}

func sentByType(s *Session) map[string]int64 {
	var out map[string]int64
	for i := reqMsgType(0); i < numReqMsgTypes; i++ {
		if v := s.msgsSentByType[i].Load(); v > 0 {
			if out == nil {
				out = make(map[string]int64, 2)
			}
			out[i.String()] = v
		}
	}
	return out
}

func recvByType(s *Session) map[string]int64 {
	var out map[string]int64
	for i := respMsgType(0); i < numRespMsgTypes; i++ {
		if v := s.msgsRecvByType[i].Load(); v > 0 {
			if out == nil {
				out = make(map[string]int64, 2)
			}
			out[i.String()] = v
		}
	}
	return out
}

func peerInfoToSnapshot(p *spb.PeerInfo) PeerInfoSnapshot {
	if p == nil {
		return PeerInfoSnapshot{}
	}
	return PeerInfoSnapshot{
		TransportType:              TransportTypeName(p.GetTransportType()),
		GoogleFrontendID:           p.GetGoogleFrontendId(),
		ApplicationFrontendID:      p.GetApplicationFrontendId(),
		ApplicationFrontendRegion:  p.GetApplicationFrontendRegion(),
		ApplicationFrontendSubzone: p.GetApplicationFrontendSubzone(),
	}
}

// TransportTypeName maps the PeerInfo transport type enum to the short
// label used in metric attributes and debug UIs (e.g. "cloudpath",
// "session_directpath"). Prefer this over .String(), which yields the
// verbose "TRANSPORT_TYPE_…" proto enum names. Exported so the outer
// bigtable package can share the same mapping (attempt_latencies2's
// transport_type attribute) without duplicating the switch.
func TransportTypeName(tt spb.PeerInfo_TransportType) string {
	switch tt {
	case spb.PeerInfo_TRANSPORT_TYPE_EXTERNAL:
		return "external"
	case spb.PeerInfo_TRANSPORT_TYPE_CLOUD_PATH:
		return "cloudpath"
	case spb.PeerInfo_TRANSPORT_TYPE_DIRECT_ACCESS:
		return "directpath"
	case spb.PeerInfo_TRANSPORT_TYPE_SESSION_EXTERNAL:
		return "session_external"
	case spb.PeerInfo_TRANSPORT_TYPE_SESSION_CLOUD_PATH:
		return "session_cloudpath"
	case spb.PeerInfo_TRANSPORT_TYPE_SESSION_DIRECT_ACCESS:
		return "session_directpath"
	case spb.PeerInfo_TRANSPORT_TYPE_SESSION_UNKNOWN:
		return "session_unknown"
	default:
		return "unknown"
	}
}

// LoadBalancingSnapshot returns the pool's picker state + decision
// history + per-AFE fanout — the input to the loadz debug page. Takes
// p.mu briefly to read picker name + pool name, then hands off to the
// ring-buffer / sessionList accessors (each has its own lock).
func (p *SessionPoolImpl) LoadBalancingSnapshot() LoadBalancingSnapshot {
	p.mu.Lock()
	name := p.poolName
	pickerName := "unknown"
	if p.picker != nil {
		pickerName = p.picker.Name()
	}
	p.mu.Unlock()

	counts := p.snapshotAfePickCounts()
	countsInt64 := make(map[int64]int64, len(counts))
	for k, v := range counts {
		countsInt64[int64(k)] = v
	}
	return LoadBalancingSnapshot{
		PoolName:   name,
		PickerName: pickerName,
		AFEs:       p.sl.Snapshot(),
		PickCounts: countsInt64,
		Recent:     p.snapshotPickHistory(),
		CapturedAt: time.Now(),
	}
}

// Snapshot returns a snapshot of the per-handle pool bookkeeping using the
// existing atomics — no lock taken.
func (h *SessionHandle) Snapshot() SessionHandleSnapshot {
	return SessionHandleSnapshot{
		Outstanding:  h.outstanding.Load(),
		LastActivity: h.GetLastActivity(),
		Picks:        h.picks.Load(),
	}
}

// PoolSnapshot returns a snapshot of the pool plus every session currently in
// it. Holds p.mu only long enough to copy out the slice header; per-session
// snapshots are taken after the pool lock is released so per-session locks
// cannot back up under p.mu.
func (p *SessionPoolImpl) PoolSnapshot() PoolSnapshot {
	// min/max are now sizer-owned and served via atomic accessors,
	// so read them outside p.mu — no p.mu bracket needed.
	min := p.sizer.MinSessions()
	max := p.sizer.MaxSessions()

	p.mu.Lock()
	name := p.poolName
	sessionType := p.sessionType
	startingCount := len(p.startingSessions)
	pickerType := "unknown"
	if p.picker != nil {
		pickerType = reflect.TypeOf(p.picker).Elem().Name()
	}
	p.mu.Unlock()
	handles := p.sl.AllHandles()

	var throttler ThrottlerSnapshot
	if p.budget != nil {
		throttler = p.budget.Snapshot()
	}
	snap := PoolSnapshot{
		Name:           name,
		SessionType:    sessionType.String(),
		MinSessions:    min,
		MaxSessions:    max,
		PickerType:     pickerType,
		StartingCount:  startingCount,
		TotalSessions:  len(handles),
		Sessions:       make([]SessionSnapshot, 0, len(handles)),
		CapturedAt:     time.Now(),
		SessionsOpened: p.m.sessionsOpened.Load(),
		SessionsClosed: p.m.sessionsClosed.Load(),
		CloseReasons:   p.snapshotCloseReasons(),
		ListenerFires:  p.m.listenerFires.Load(),
		Throttler:      throttler,
		ScalingHistory: p.snapshotScalingHistory(),
		OpenRequest:    buildOpenRequestSnapshot(p.openSessionRequest, sessionType),
		SlowVRpcs:      p.snapshotSlowVRpcs(),
		TimeSeries:     p.snapshotTimeSeries(),
		AFEs:           p.sl.Snapshot(),
		// True pool-boundary queue depth — same source as Stats().PendingCount.
		// Previous implementation summed per-session Outstanding, which under
		// multiPlexingLimit=1 equals InUseCount and made the debug UI show a
		// wrong "pending" number that never reflected real waiter backlog.
		PendingCount: int(p.waitersCount.Load()),
	}

	// Pool-level aggregates: combine cluster counts + latency samples
	// across every session. Pool-level latency percentiles come from the
	// pool's lifetime histograms (see latencyHist) so they reflect every
	// sample the pool has ever seen, not just the last 256 per active
	// session. Per-session ring buffers are still surfaced on each row of
	// the Sessions table.
	aggregatedClusters := map[string]int64{}
	stateCounts := map[string]int{}

	for _, sh := range handles {
		if sh == nil || sh.session == nil {
			continue
		}
		s := sh.session.Snapshot()
		s.Handle = sh.Snapshot()
		snap.Sessions = append(snap.Sessions, s)

		stateCounts[s.State]++
		if s.State == StateReady.String() {
			snap.ReadyCount++
		}
		if s.Handle.Outstanding > 0 {
			snap.InUseCount++
		}
		for k, v := range s.ClusterCounts {
			aggregatedClusters[k] += v
		}
		// Merge per-session debug events into the pool-wide timeline,
		// tagging each with the originating session log name so the UI
		// can attribute them without nesting per-session rows.
		for _, ev := range s.RecentEvents {
			snap.RecentEvents = append(snap.RecentEvents, PoolSessionEvent{
				At:      ev.At,
				Kind:    string(ev.Kind),
				Session: s.LogName,
				Message: ev.Message,
			})
		}
	}
	// Newest-first, then cap to maxPoolRecentEvents. Stable sort so events
	// recorded in the same instant keep their per-session ordering.
	sort.SliceStable(snap.RecentEvents, func(i, j int) bool {
		return snap.RecentEvents[i].At.After(snap.RecentEvents[j].At)
	})
	if len(snap.RecentEvents) > maxPoolRecentEvents {
		snap.RecentEvents = snap.RecentEvents[:maxPoolRecentEvents]
	}
	if len(stateCounts) > 0 {
		snap.StateCounts = stateCounts
	}
	if len(aggregatedClusters) > 0 {
		snap.ClusterCounts = aggregatedClusters
	}
	if p50, p95, p99, n := p.m.backendLatencyHist.snapshot(); n > 0 {
		snap.LatencyP50 = p50
		snap.LatencyP95 = p95
		snap.LatencyP99 = p99
		snap.LatencyN = int(n)
	}
	if p50, p95, p99, n := p.m.totalLatencyHist.snapshot(); n > 0 {
		snap.TotalLatencyP50 = p50
		snap.TotalLatencyP95 = p95
		snap.TotalLatencyP99 = p99
		snap.TotalLatencyN = int(n)
	}
	if p50, p95, p99, n := p.m.transportLatencyHist.snapshot(); n > 0 {
		snap.TransportLatencyP50 = p50
		snap.TransportLatencyP95 = p95
		snap.TransportLatencyP99 = p99
		snap.TransportLatencyN = int(n)
	}

	// Lifetime distribution: render whichever buckets actually have data
	// — but always emit them in canonical bucket order so the UI's bar
	// labels line up across pools.
	lifetimes := p.snapshotLifetimes()
	if len(lifetimes) > 0 {
		snap.LifetimeN = len(lifetimes)
		buckets := make([]LifetimeBucketCount, len(LifetimeBuckets))
		for i, b := range LifetimeBuckets {
			buckets[i].Label = b.Label
		}
		for _, d := range lifetimes {
			for i, b := range LifetimeBuckets {
				if d < b.Max {
					buckets[i].Count++
					break
				}
			}
		}
		snap.LifetimeHistogram = buckets
		sort.Slice(lifetimes, func(i, j int) bool { return lifetimes[i] < lifetimes[j] })
		snap.LifetimeP50 = percentile(lifetimes, 50)
		snap.LifetimeP95 = percentile(lifetimes, 95)
		snap.LifetimeP99 = percentile(lifetimes, 99)
	}
	return snap
}
