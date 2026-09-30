package builder

import (
	"fmt"
	"math"
	"time"

	"github.com/grafana/loki/v3/pkg/kafka"
)

// The docID space: docID is an epoch cell — absCell - epochCell, a uint32
// count of document cells from the fixed docIDEpoch. A cell is one document
// interval of one document shard: absCell = absBucket × documentShards +
// documentShard, so time-only indexes (documentShards 1) have one cell per
// bucket. Everything that reasons about that space — the postings buffer's
// base cell (epochCell), Validate's window-coverage rule, the ingest
// out-of-window panic — derives from the helpers here so the math cannot
// drift apart.

// docIDEpoch is the fixed epoch of the docID window:
// 2026-01-01T00:00:00Z.
//
// The epoch is FIXED, not derived from "now", because Grafana Adaptive Logs
// Archive/Replay can replay logs older than a year; the earliest archived
// data in our environments is 2026-01-01, so the epoch must never move past
// it. (A per-cycle epoch of now − 365d was tried and reverted: it would push
// replayed archive data behind the epoch as calendar time passes.)
//
// The uint32 window spans 2^32 cells, which is (2^32 / documentShards) ×
// document_interval of time: at 100ms time-only it runs to 2039-08-12 UTC,
// and at the v5 default of 16s × 32 shards to about 2094 — asserted by
// TestDocIDWindowEnd. Timestamps outside the window PANIC at ingest
// (processStream); Validate rejects settings whose window ends less than
// minDocIDFutureRunway from now, so the panic is reserved for genuinely
// anomalous timestamps, not window exhaustion.
//
// Midnight-UTC alignment keeps the epoch cell an exact multiple of
// cellsPerDay (the interval divides 24h evenly, per Validate), which the
// merge's day/date math relies on: writerForDay's `dayStartAbs - baseCell`
// must not underflow, and formatDate (ingest) must agree with dateOfDay
// (merge) on every representable cell.
var docIDEpoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// minDocIDFutureRunway is the minimum headroom the docID window must extend
// past "now" at config-validation time: room for client clock skew and
// deliberately-future timestamps, and a floor that rejects the config well
// before live traffic starts panicking as the fixed-epoch window fills up.
const minDocIDFutureRunway = 365 * 24 * time.Hour

// epochCell returns the epoch's absolute document cell at the given interval
// and document shard count — the base subtracted from every absolute cell to
// form a docID.
func epochCell(epoch time.Time, intervalNanos int64, documentShards uint64) uint64 {
	return uint64(epoch.UnixNano()) / uint64(intervalNanos) * documentShards
}

// docIDWindowLength returns the span of time the uint32 docID window covers:
// 2^32 cells, or 2^32 / documentShards intervals. documentShards 0 means 1.
// Timestamps at or past the window's end (epoch + this length) panic at
// ingest, so Validate requires the end to be at least minDocIDFutureRunway
// away.
func docIDWindowLength(interval time.Duration, documentShards int) time.Duration {
	windowBuckets := int64(1<<32) / int64(max(documentShards, 1))
	if int64(interval) > math.MaxInt64/windowBuckets {
		// The window overflows time.Duration (~292y); it is effectively
		// unbounded for any realistic interval.
		return math.MaxInt64
	}
	return time.Duration(windowBuckets) * interval
}

// docIDWindowEnd returns the exclusive end of the docID window:
// docIDEpoch + docIDWindowLength. At 100ms time-only this is 2039-08-12 UTC.
func docIDWindowEnd(interval time.Duration, documentShards int) time.Time {
	return docIDEpoch.Add(docIDWindowLength(interval, documentShards))
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
func panicOutOfWindow(ts time.Time, interval time.Duration, documentShards int, ref recordRef) {
	panic(fmt.Sprintf(
		"logline builder: log timestamp %s (record %s) is outside the docID window [%s, %s) (fixed epoch 2026-01-01 + 2^32 cells of %v document_interval × %d document_shards); "+
			"out-of-window data must fail loudly rather than be silently unindexed (Adaptive Logs Archive/Replay can replay pre-window logs)",
		ts.UTC().Format(time.RFC3339Nano),
		ref,
		docIDEpoch.Format(time.RFC3339),
		docIDWindowEnd(interval, documentShards).UTC().Format(time.RFC3339),
		interval,
		max(documentShards, 1),
	))
}
