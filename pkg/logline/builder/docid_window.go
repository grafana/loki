package builder

import (
	"fmt"
	"math"
	"time"

	"github.com/grafana/loki/v3/pkg/kafka"
)

// The docID space: docID is an epoch tick — absDocBucket - epochBucket, a
// uint32 count of document buckets from the fixed docIDEpoch. Everything
// that reasons about that space — the postings buffer's base bucket
// (epochBucket), Validate's window-coverage rule, the ingest out-of-window
// panic — derives from the helpers here so the math cannot drift apart.

// docIDEpoch is the fixed epoch of the docID window:
// 2026-01-01T00:00:00Z.
//
// The epoch is FIXED, not derived from "now", because Grafana Adaptive Logs
// Archive/Replay can replay logs older than a year; the earliest archived
// data in our environments is 2026-01-01, so the epoch must never move past
// it. (A per-cycle epoch of now − 365d was tried and reverted: it would push
// replayed archive data behind the epoch as calendar time passes.)
//
// At the default 100ms document_interval the uint32 window runs from the
// epoch to 2039-08-12 UTC (epoch + 2^32 × 100ms) — asserted by
// TestDocIDWindowEndAtDefaultInterval. Timestamps outside the window PANIC at
// ingest (processStream); Validate rejects intervals whose window ends less
// than minDocIDFutureRunway from now, so the panic is reserved for genuinely
// anomalous timestamps, not window exhaustion.
//
// Midnight-UTC alignment keeps the epoch bucket an exact multiple of
// ticksPerDay (the interval divides 24h evenly, per Validate), which the
// merge's day/date math relies on: writerForDay's `dayStartAbs - baseBucket`
// must not underflow, and formatDate (ingest) must agree with dateOfDay
// (merge) on every representable tick.
var docIDEpoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// minDocIDFutureRunway is the minimum headroom the docID window must extend
// past "now" at config-validation time: room for client clock skew and
// deliberately-future timestamps, and a floor that rejects the config well
// before live traffic starts panicking as the fixed-epoch window fills up.
const minDocIDFutureRunway = 365 * 24 * time.Hour

// epochBucket returns the epoch's absolute document bucket at the given
// interval — the base subtracted from every absolute bucket to form a docID.
func epochBucket(epoch time.Time, intervalNanos int64) uint64 {
	return uint64(epoch.UnixNano()) / uint64(intervalNanos)
}

// docIDWindowLength returns the span of the uint32 docID window for
// the given document interval: 2^32 ticks. Timestamps at or past the window's
// end (epoch + this length) panic at ingest, so Validate requires the end to
// be at least minDocIDFutureRunway away.
func docIDWindowLength(interval time.Duration) time.Duration {
	const windowTicks = 1 << 32
	if interval > math.MaxInt64/windowTicks {
		// 2^32 ticks overflow time.Duration (~292y); the window is effectively
		// unbounded for any realistic interval.
		return math.MaxInt64
	}
	return windowTicks * interval
}

// docIDWindowEnd returns the exclusive end of the docID window for the
// given document interval: docIDEpoch + 2^32 ticks. At 100ms this is
// 2039-08-12 UTC.
func docIDWindowEnd(interval time.Duration) time.Time {
	return docIDEpoch.Add(docIDWindowLength(interval))
}

// recordRef identifies the Kafka record a stream was decoded from. It exists
// solely so the out-of-window panic can name the poison record — the operator
// remediating a crash loop needs the partition and offset to advance past it.
// Threaded through processStream by value and read only on the panic path, so
// it adds zero per-line cost. The zero value (tests, benches, callers with no
// Kafka context) formats as "partition=? offset=? tenant=?".
type recordRef struct {
	valid     bool
	partition kafka.PartitionID
	offset    kafka.Offset
	tenantID  string
}

func (r recordRef) String() string {
	if !r.valid {
		return "partition=? offset=? tenant=?"
	}
	return fmt.Sprintf("partition=%d offset=%d tenant=%q", r.partition, r.offset, r.tenantID)
}

// panicOutOfWindow fails the process on a timestamp outside the docID
// window. DELIBERATE never-panic override (CLAUDE.md invariant #7): Adaptive
// Logs Archive/Replay can legitimately replay very old logs, and dropping an
// out-of-window line while the zero-file flush path commits Kafka offsets
// would be a permanent, silent data skip. A loud crash forces the anomaly
// (pre-epoch archive data, absurd future timestamp, misconfigured interval)
// to be dealt with instead of quietly leaving data unindexed. ref attributes
// the crash to the Kafka record that carried the timestamp, making the panic
// actionable: remediation is advancing the offset past that record (or fixing
// the interval/epoch config).
func panicOutOfWindow(ts time.Time, interval time.Duration, ref recordRef) {
	panic(fmt.Sprintf(
		"logline builder: log timestamp %s (record %s) is outside the docID window [%s, %s) (fixed epoch 2026-01-01 + 2^32 × %v document_interval); "+
			"out-of-window data must fail loudly rather than be silently unindexed (Adaptive Logs Archive/Replay can replay pre-window logs)",
		ts.UTC().Format(time.RFC3339Nano),
		ref,
		docIDEpoch.Format(time.RFC3339),
		docIDWindowEnd(interval).UTC().Format(time.RFC3339),
		interval,
	))
}
