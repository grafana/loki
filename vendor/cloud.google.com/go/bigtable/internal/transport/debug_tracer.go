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

// debug_tracer.go — counter for "this branch shouldn't be reached"
// sites in the session pool, session, and configuration manager. One
// atomic add plus one OTel Int64Counter increment per emission, so it's
// cheap enough to sprinkle on cold paths. The metric name matches
// java-bigtable's ClientDebugTagCount so cross-language dashboards can
// join on the tag column.
//
// # Entry points
//
//   - recordDebugTag(name)              — Warn-level observation.
//   - recordDebugTagAt(level, name)     — same, non-Warn level.
//   - assertDebugTag(expr, name)        — invariant check, no format args.
//   - assertDebugTagf(expr, name, fmt…) — invariant check with log context.
//
// The assert forms return `expr` and never panic; use as `if !assert…`
// so the site records + logs + bails in one line.
//
// # Rules
//
//   - `name` is always a constant from the catalog below — never a
//     format string. Dynamic context belongs in the log message.
//   - Emission is additive: keep whatever log/err/return the branch
//     already does.
//   - Default to recordDebugTag (Warn). Reserve Error for assert
//     failures and the rare "this is really wrong" observation.

package internal

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	btopt "cloud.google.com/go/bigtable/internal/option"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// debugLevel gates whether an emission is admitted. Values match
// Java's TelemetryConfiguration.Level so future wire-config plumbing
// is a straight cast.
type debugLevel int32

// lvl namespaces the two levels so call sites read as `lvl.Warn` /
// `lvl.Error`. Named `lvl` (not `tag`) so it doesn't collide visually
// with the `tag…` catalog constants at call sites.
var lvl = struct {
	Warn  debugLevel
	Error debugLevel
}{
	Warn:  1,
	Error: 2,
}

// debugTagCounterName is the OTel instrument name. Kept short because
// the Cloud Monitoring exporter prepends
// `bigtable.googleapis.com/internal/client/` itself; a fully-qualified
// name here would double-prefix and Cloud Monitoring would reject it.
const debugTagCounterName = "debug_tags"

// debugTagAttrKey is the sole OTel attribute carrying the tag string.
// Cardinality is bounded by the catalog below.
const debugTagAttrKey = "tag"

// Debug-tag catalog. Every emission site passes one of these constants;
// inline literals are never used. Wire names are snake_case (matching
// java-bigtable); Go identifiers are camelCase. Grep either form to
// jump between the definition and its emission sites.
const (
	// Session-lifecycle observations.
	tagSessionUnknownResponse        = "session_unknown_response"
	tagSessionOpenWrongState         = "session_open_wrong_state"
	tagSessionGoawayAfterClose       = "session_goaway_after_close"
	tagSessionGoawayBeforeStart      = "session_goaway_before_start"
	tagSessionAbnormalClose          = "session_abnormal_close"
	tagSessionHeartbeatMissed        = "session_heartbeat_missed"
	tagSessionForceCloseNeverStarted = "session_force_close_never_started"
	tagSessionCloseNoReason          = "session_close_no_reason"

	// vRPC dispatch observations.
	tagSessionVRPCNil                = "session_vrpc_nil"
	tagSessionVRPCErrorNil           = "session_vrpc_error_nil"
	tagSessionVRPCIDMismatch         = "session_vrpc_id_mismatch"
	tagSessionVRPCResponseWrongState = "session_vrpc_response_wrong_state"
	tagSessionVRPCDuplicateResult    = "session_vrpc_duplicate_result"

	// Pool-scoped anomalies.
	tagSessionPoolStuckSessionSwept = "session_pool_stuck_session_swept"
	tagSessionPoolDrainTimeout      = "session_pool_drain_timeout"
	tagSessionPoolCreateFailed      = "session_pool_create_failed"
	tagSessionPoolPickLostRace      = "session_pool_pick_lost_race"

	// Client configuration polling.
	tagClientConfigPollFailed     = "client_config_poll_failed"
	tagClientConfigPollCtxExpired = "client_config_poll_ctx_expired"
)

var (
	// debugTagCounter is the OTel Int64Counter registered by
	// registerDebugTagCounter. Held in an atomic.Value so the
	// register-once write and every-emission reads don't race on the
	// two-word interface value. Load returns nil until initialization
	// runs, so the tracer is safe to call before InitializeSessionMetrics
	// or in tests that don't wire OTel.
	debugTagCounter atomic.Value

	// debugTagLevelFloor drops any emission with level < floor before it
	// touches the counter or the in-memory map. Defaults to Warn.
	debugTagLevelFloor atomic.Int32

	// debugTagCountsMu guards debugTagStats. Contention is negligible:
	// emissions are cold-path and reads (tests, /debugtagsz/) are rare.
	debugTagCountsMu sync.RWMutex
	// debugTagStats is the in-process view of every tag seen since
	// process start. Kept alongside the OTel counter so tests and
	// /debugtagsz/ can read state without an exporter wired up.
	debugTagStats = map[string]*tagStat{}
)

// tagStat holds one tag's counters. Fields are atomic so the emission
// path stays lock-free after the map entry exists (RLock the map, then
// bump atomics).
type tagStat struct {
	count     atomic.Int64 // total emissions since process start
	firstSeen atomic.Int64 // unix-nano of first emission; write-once
	lastSeen  atomic.Int64 // unix-nano of most-recent emission
}

// DebugTagSnapshot is one row of DebugTags output — a tag's count plus
// its first- and last-seen timestamps. Exported for /debugtagsz/.
type DebugTagSnapshot struct {
	Name      string
	Count     int64
	FirstSeen time.Time
	LastSeen  time.Time
}

func init() {
	debugTagLevelFloor.Store(int32(lvl.Warn))
}

// registerDebugTagCounter is called once from InitializeSessionMetrics
// after the meter provider is validated non-nil.
func registerDebugTagCounter(meter metric.Meter) error {
	c, err := meter.Int64Counter(
		debugTagCounterName,
		metric.WithDescription("Count of unexpected events tagged by call site — the Go client's parity with java-bigtable's ClientDebugTagCount."),
	)
	if err != nil {
		return fmt.Errorf("create debug_tags counter: %w", err)
	}
	debugTagCounter.Store(c)
	return nil
}

// setDebugTagLevelFloor sets the emission floor. Intended for future
// wiring from TelemetryConfiguration.debug_tag_level.
func setDebugTagLevelFloor(l debugLevel) {
	debugTagLevelFloor.Store(int32(l))
}

// recordDebugTag increments the debug_tags counter for `name` at Warn.
// Safe to call before InitializeSessionMetrics — only the in-memory
// map increments until the OTel counter is registered.
func recordDebugTag(name string) {
	recordDebugTagAt(lvl.Warn, name)
}

// recordDebugTagAt is the level-explicit form. Prefer recordDebugTag
// for ordinary observations; use this only when a site needs a
// non-Warn level outside an assertDebugTag.
func recordDebugTagAt(level debugLevel, name string) {
	if int32(level) < debugTagLevelFloor.Load() {
		return
	}
	bumpDebugTagCount(name)
	if c, ok := debugTagCounter.Load().(metric.Int64Counter); ok && c != nil {
		c.Add(context.Background(), 1,
			metric.WithAttributes(attribute.String(debugTagAttrKey, name)))
	}
}

// assertDebugTag returns `expr`. On false it records an Error tag and
// logs "debug-tag assertion failed [name]". Never panics — the caller
// decides whether to bail, drop, or continue.
func assertDebugTag(expr bool, name string) bool {
	if expr {
		return true
	}
	recordDebugTagAt(lvl.Error, name)
	btopt.Debugf(nil, "bigtable: debug-tag assertion failed [%s]", name)
	return false
}

// assertDebugTagf is the format-string form of assertDebugTag. Use it
// when the site has diagnostic context (state, ids, timing) worth
// putting in the log line; the counter increment is identical.
func assertDebugTagf(expr bool, name, format string, args ...interface{}) bool {
	if expr {
		return true
	}
	recordDebugTagAt(lvl.Error, name)
	btopt.Debugf(nil, "bigtable: debug-tag assertion failed [%s]: "+format, append([]interface{}{name}, args...)...)
	return false
}

// bumpDebugTagCount bumps the in-memory count for `name` and stamps
// its emission timestamps. The first emission creates the entry under
// the write lock; subsequent ones take the RLock and touch atomics.
// The function handles its own locking; the name deliberately avoids
// the "…Locked" suffix (which by convention means the caller holds
// the lock).
func bumpDebugTagCount(name string) {
	now := time.Now().UnixNano()
	debugTagCountsMu.RLock()
	s, ok := debugTagStats[name]
	debugTagCountsMu.RUnlock()
	if ok {
		s.count.Add(1)
		s.lastSeen.Store(now)
		return
	}
	debugTagCountsMu.Lock()
	if s, ok = debugTagStats[name]; !ok {
		s = &tagStat{}
		s.firstSeen.Store(now)
		debugTagStats[name] = s
	}
	s.count.Add(1)
	s.lastSeen.Store(now)
	debugTagCountsMu.Unlock()
}

// DebugTags returns every tag emitted since process start, sorted by
// LastSeen descending. The catalog is small (bounded by the const
// block above) so callers can render the whole slice without paging.
func DebugTags() []DebugTagSnapshot {
	debugTagCountsMu.RLock()
	out := make([]DebugTagSnapshot, 0, len(debugTagStats))
	for name, s := range debugTagStats {
		out = append(out, DebugTagSnapshot{
			Name:      name,
			Count:     s.count.Load(),
			FirstSeen: time.Unix(0, s.firstSeen.Load()),
			LastSeen:  time.Unix(0, s.lastSeen.Load()),
		})
	}
	debugTagCountsMu.RUnlock()
	// Sort after releasing the lock — `out` is a local slice of value
	// copies, so the sort touches no shared state.
	sort.Slice(out, func(i, j int) bool {
		return out[i].LastSeen.After(out[j].LastSeen)
	})
	return out
}

// snapshotDebugTagCounts returns a bare name→count map for tests that
// only care about counts. New callers should prefer DebugTags.
func snapshotDebugTagCounts() map[string]int64 {
	debugTagCountsMu.RLock()
	defer debugTagCountsMu.RUnlock()
	out := make(map[string]int64, len(debugTagStats))
	for name, s := range debugTagStats {
		out[name] = s.count.Load()
	}
	return out
}

// resetDebugTagCountsForTest wipes the map so a test can assert on a
// specific tag's count without cross-test contamination. Test-only.
func resetDebugTagCountsForTest() {
	debugTagCountsMu.Lock()
	debugTagStats = map[string]*tagStat{}
	debugTagCountsMu.Unlock()
}
