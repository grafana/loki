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
	"math"
	"sync"

	spb "cloud.google.com/go/bigtable/apiv2/bigtablepb"
)

// PoolStats is a snapshot of a session pool's current state, sufficient
// to drive one PoolSizer decision. Values are integers taken at a single
// instant — no windowing, no averaging.
type PoolStats struct {
	ReadyCount    int // sessions handshake-complete and available for checkout
	StartingCount int // sessions mid-open (OpenSession in flight)
	InUseCount    int // sessions with an active vRPC
	PendingCount  int // callers parked at the pool boundary waiting for a session
}

// StatsFetcher returns the current PoolStats. Returning nil signals
// "pool not started yet" and PoolSizer.Decide short-circuits to the
// no-stats branch (no decision made).
type StatsFetcher func() *PoolStats

// PoolSizer calculates the optimal session pool size from a snapshot
// of workload metrics. Decisions are stateless — no windowed peak
// tracking, no scale-down cooldown, no historical state.
//
// The client only ever GROWS the pool proactively: growth fires when
// CheckoutSession misses. Shrinkage is passive — sessions die when the
// server closes them (GoAway, maxAge, error) and the pool's OnClose
// decides whether to replace based on the sizer's current delta. When
// the pool is overprovisioned the sizer returns delta < 0, OnClose
// declines the replacement, and the pool shrinks by exactly one.
// Convergence rate is bounded by server-driven session churn.
//
// This design cannot oscillate: the client never proactively kills a
// healthy session, so a burst-then-lull traffic wave whose trough sits
// outside any peak-window cannot trigger a mass prune followed by a
// cold-start scale-up on the next crest.
type PoolSizer struct {
	mu              sync.Mutex
	fetcher         StatsFetcher
	minSessions     int
	maxSessions     int
	headroomPct     float64 // Idle headroom as a fraction of sessions in use (e.g., 0.10 = 10%)
	newSessionQLen  int     // server-driven per-session pending queue length; divides PendingCount
	minIdleSessions int     // floor on the idle cushion so headroom never collapses to 0
}

// defaultMinIdleSessions is the only knob without a matching field on
// the SessionPoolConfiguration proto — a purely client-side cushion
// floor that prevents ceil(0 * HeadroomPct)==0 from starving the
// warmup path. All other fallbacks read from defaultClientConfig so
// there's one place to update when a proto default moves.
const defaultMinIdleSessions = 1

// defaultPoolConfig returns the server-shipped pool configuration
// defaults (headroom, min/max session count, per-session queue length,
// etc.) from defaultClientConfig. Kept as a helper so both NewPoolSizer
// and UpdateConfig read the same table when the server hasn't yet
// supplied a value.
func defaultPoolConfig() *spb.SessionClientConfiguration_SessionPoolConfiguration {
	return defaultClientConfig.GetSessionConfiguration().GetSessionPoolConfiguration()
}

// NewPoolSizer creates a new PoolSizer. headroomPct <= 0 falls back to
// the server-shipped default carried in defaultClientConfig. The
// per-session queue length and MinIdleSessions use the same source /
// built-in floor; UpdateConfig can override any of these once the
// server config lands.
func NewPoolSizer(fetcher StatsFetcher, minSessions, maxSessions int, headroomPct float64) *PoolSizer {
	if headroomPct <= 0 {
		headroomPct = float64(defaultPoolConfig().GetHeadroom())
	}
	return &PoolSizer{
		fetcher:         fetcher,
		minSessions:     minSessions,
		maxSessions:     maxSessions,
		headroomPct:     headroomPct,
		newSessionQLen:  int(defaultPoolConfig().GetNewSessionQueueLength()),
		minIdleSessions: defaultMinIdleSessions,
	}
}

// UpdateConfig dynamically adjusts the sizer's capacity bounds,
// headroom cushion, and per-session queue length at runtime. Called
// from the server-config listener path; safe against concurrent
// Decide calls via the sizer's own mutex. A nil config is a no-op —
// the listener path is defensive about intermediate zero-valued
// updates during startup.
func (s *PoolSizer) UpdateConfig(config *spb.SessionClientConfiguration_SessionPoolConfiguration) {
	if config == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	s.minSessions = int(config.MinSessionCount)
	s.maxSessions = int(config.MaxSessionCount)
	// Mirror the constructor guard: a zero or negative headroom from
	// the server would render as HeadroomPct=0 on the loadz trace and
	// collapse IdleHeadroom to the MinIdleSessions floor — the pool
	// still works but operators would see a confusing decision trace.
	// Fall back to the same default the constructor uses.
	if hp := float64(config.Headroom); hp > 0 {
		s.headroomPct = hp
	} else {
		s.headroomPct = float64(defaultPoolConfig().GetHeadroom())
	}
	if nsql := int(config.GetNewSessionQueueLength()); nsql > 0 {
		s.newSessionQLen = nsql
	}
}

// ScaleDecision is the full trace from one Decide() evaluation. Every
// input, every intermediate, plus the final delta and the branch it
// landed in — surfaced in ScalingEvent so operators can answer "WHY
// did the sizer choose this?" without re-running the math by hand.
type ScaleDecision struct {
	// Inputs (copy of PoolStats at the moment of decision)
	ReadyCount    int
	StartingCount int
	InUseCount    int
	PendingCount  int // pool-boundary waiters

	// Sizer config snapshot
	MinSessions     int
	MaxSessions     int
	HeadroomPct     float64
	NewSessionQLen  int
	MinIdleSessions int

	// Intermediates
	EffectivePending  int // ceil(PendingCount / NewSessionQLen)
	SessionsInUse     int // InUseCount + EffectivePending
	IdleHeadroom      int // max(MinIdleSessions, ceil(SessionsInUse * HeadroomPct))
	DesiredRaw        int // SessionsInUse + IdleHeadroom (pre-clamp)
	DesiredCapacity   int // clamped to [MinSessions, MaxSessions]
	ImmediateCapacity int // ReadyCount
	EventualCapacity  int // ReadyCount + StartingCount

	// Final decision
	Delta  int    // desired − eventual (scale-up) or desired − immediate (scale-down); 0 otherwise
	Branch string // "scale-up" | "scale-down" | "dead-band" | "no-stats"
}

// GetScaleDelta is a thin wrapper around Decide() that returns only
// the final delta — for callers that don't need the trace.
func (s *PoolSizer) GetScaleDelta() int {
	return s.Decide().Delta
}

// Decide computes a full ScaleDecision from the current snapshot.
//
// Branch semantics:
//   - scale-up   (Delta > 0): desired capacity exceeds what will be
//     available once starting sessions finish. Caller SHOULD create
//     Delta new sessions.
//   - scale-down (Delta < 0): desired capacity is below what's already
//     ready. Caller MUST NOT proactively kill sessions to close the
//     gap — that's the whole point of the passive-shrink design. This
//     branch is advisory: OnClose reads the delta and lets the pool
//     shrink by one (per closed session) when the sign is negative.
//   - dead-band  (Delta == 0): desired sits between immediate and
//     eventual capacity — do nothing; in-flight starts will absorb any
//     pending demand.
//   - no-stats:  the StatsFetcher returned nil (pool not started yet).
func (s *PoolSizer) Decide() ScaleDecision {
	// Call the fetcher BEFORE acquiring s.mu — the fetcher runs
	// pool-owned code that may take the pool's own mutex, and holding
	// two mutexes across a callback is a lock-inversion trap. Fetcher
	// is read-only after construction so this races nothing. A nil
	// fetcher short-circuits to no-stats (defensive; production
	// always passes one).
	var stats *PoolStats
	if s.fetcher != nil {
		stats = s.fetcher()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	d := ScaleDecision{
		MinSessions:     s.minSessions,
		MaxSessions:     s.maxSessions,
		HeadroomPct:     s.headroomPct,
		NewSessionQLen:  s.newSessionQLen,
		MinIdleSessions: s.minIdleSessions,
	}

	if stats == nil {
		d.Branch = "no-stats"
		return d
	}
	d.ReadyCount = stats.ReadyCount
	d.StartingCount = stats.StartingCount
	d.InUseCount = stats.InUseCount
	d.PendingCount = stats.PendingCount

	// effectivePending = ceil(PendingCount / NewSessionQueueLength) via
	// integer arithmetic: (a + b - 1) / b for non-negative a. Waiters
	// at the pool boundary become the number of *sessions* we'd need
	// to open to drain them (each new session can absorb up to
	// NewSessionQLen concurrent vRPCs).
	divisor := s.newSessionQLen
	if divisor <= 0 {
		divisor = int(defaultPoolConfig().GetNewSessionQueueLength())
	}
	d.EffectivePending = (stats.PendingCount + divisor - 1) / divisor
	d.SessionsInUse = stats.InUseCount + d.EffectivePending

	// Idle headroom as a fraction of in-use, floored so a brief in-use
	// dip can't collapse the cushion to zero. Kept as math.Ceil on a
	// float multiply — HeadroomPct is a fraction (e.g. 0.10), not a
	// discrete count, so the integer-ceil trick doesn't apply cleanly.
	d.IdleHeadroom = int(math.Ceil(float64(d.SessionsInUse) * s.headroomPct))
	if d.IdleHeadroom < s.minIdleSessions {
		d.IdleHeadroom = s.minIdleSessions
	}
	d.DesiredRaw = d.SessionsInUse + d.IdleHeadroom
	d.DesiredCapacity = clamp(d.DesiredRaw, s.minSessions, s.maxSessions)

	d.ImmediateCapacity = stats.ReadyCount
	d.EventualCapacity = stats.ReadyCount + stats.StartingCount

	switch {
	case d.DesiredCapacity > d.EventualCapacity:
		d.Delta = d.DesiredCapacity - d.EventualCapacity
		d.Branch = "scale-up"
	case d.DesiredCapacity < d.ImmediateCapacity:
		d.Delta = d.DesiredCapacity - d.ImmediateCapacity // negative; ADVISORY
		d.Branch = "scale-down"
	default:
		d.Branch = "dead-band"
	}
	return d
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
