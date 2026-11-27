// Package failuredetector provides primitives for deciding when a Redis
// database is unhealthy enough that the multi-database client should trigger
// a failover.
package failuredetector

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9/internal/pool"
	"github.com/redis/go-redis/v9/internal/proto"
)

// txFailedErr mirrors redis.TxFailedErr (the root package cannot be imported
// from internal packages): the sentinel Exec returns when WATCH detected a
// concurrent write.
const txFailedErr = proto.RedisError("redis: transaction failed")

// FailureDetector decides when failover should be triggered based on a stream
// of command outcomes observed by the caller.
type FailureDetector interface {
	// RecordSuccess records a successful command outcome.
	RecordSuccess()
	// RecordFailure records a failed command outcome. Implementations may
	// ignore errors that are not health signals (for example client-side
	// context cancellation).
	RecordFailure(err error)
	// ShouldFailover returns true when the recent outcomes indicate that
	// failover should be triggered.
	ShouldFailover() bool
	// Reset discards all observed outcomes and starts fresh.
	Reset()
}

// CommandFailureDetectorConfig configures CommandFailureDetector. Every
// field has a documented default that NewCommandFailureDetector applies when
// the field is left at its zero value, so a zero-valued config is a valid
// way to ask for the recommended defaults.
type CommandFailureDetectorConfig struct {
	// MinNumFailures is the minimum number of failed commands that must be
	// observed within the detection window before failover is considered.
	// Ignored when IgnoreMinNumFailures is true.
	// Default: 1000.
	MinNumFailures uint64

	// IgnoreMinNumFailures disables the MinNumFailures check, so ShouldFailover
	// considers only FailureRateThreshold. Use this when the rate alone is the
	// signal you trust (typically combined with a small FailureRateThreshold).
	IgnoreMinNumFailures bool

	// FailureRateThreshold is the failure rate (0.0-1.0] that, together with
	// MinNumFailures, triggers failover. For example, 0.1 means failover when
	// 10% or more of the commands in the window fail.
	// Ignored when IgnoreFailureRateThreshold is true.
	// Default: 0.1. (A zero value means "use the default".)
	FailureRateThreshold float64

	// IgnoreFailureRateThreshold disables the FailureRateThreshold check, so
	// ShouldFailover considers only MinNumFailures. Use this when the absolute
	// number of failures is the signal you trust regardless of traffic volume.
	IgnoreFailureRateThreshold bool

	// FailureDetectionWindow is the sliding time window over which command
	// outcomes are considered. Outcomes older than FailureDetectionWindow
	// from now are no longer counted by ShouldFailover.
	// Default: 2 seconds.
	FailureDetectionWindow time.Duration

	// NumBuckets controls the time resolution of the sliding window. The
	// window is divided into NumBuckets sub-buckets, each of width
	// FailureDetectionWindow / NumBuckets, and outcomes age out one bucket
	// at a time. Larger values give finer-grained ageing at the cost of
	// O(NumBuckets) work per ShouldFailover call.
	// Default: 10.
	NumBuckets int
}

// DefaultCommandFailureDetectorConfig returns the default configuration.
// NewCommandFailureDetector applies the same defaults to any zero-valued
// field, so this is mainly useful as a starting point for tuning.
func DefaultCommandFailureDetectorConfig() CommandFailureDetectorConfig {
	return CommandFailureDetectorConfig{
		MinNumFailures:         defaultMinNumFailures,
		FailureRateThreshold:   defaultFailureRateThreshold,
		FailureDetectionWindow: defaultFailureDetectionWindow,
		NumBuckets:             defaultNumBuckets,
	}
}

const (
	defaultMinNumFailures         = 1000
	defaultFailureRateThreshold   = 0.1
	defaultFailureDetectionWindow = 2 * time.Second
	defaultNumBuckets             = 10
)

// applyDefaults fills zero-valued fields with their documented defaults so
// the rest of the detector can assume every threshold is set.
func (c *CommandFailureDetectorConfig) applyDefaults() {
	if c.MinNumFailures == 0 {
		c.MinNumFailures = defaultMinNumFailures
	}
	if c.FailureRateThreshold <= 0 {
		c.FailureRateThreshold = defaultFailureRateThreshold
	}
	if c.FailureDetectionWindow <= 0 {
		c.FailureDetectionWindow = defaultFailureDetectionWindow
	}
	if c.NumBuckets <= 0 {
		c.NumBuckets = defaultNumBuckets
	}
}

// bucket holds the outcomes recorded inside a single sub-bucket of the
// sliding window. All fields are accessed atomically so the detector is
// lock-free on the hot path.
//
// Each slot holds a pointer to an immutable-epoch bucketState. When the ring
// wraps around and a writer revisits a slot whose state belongs to a previous
// lap, the writer installs a fresh zeroed bucketState via CompareAndSwap on
// the pointer. Readers ignore any state whose epoch falls outside the current
// window.
type bucket struct {
	state atomic.Pointer[bucketState]
}

// bucketState is one lap of a ring slot: a fixed epoch plus the counters
// recorded during that lap. Lap transitions swap the whole state pointer, so
// a writer that obtained a previous lap's state can only increment that stale
// lap (which readers already ignore) — no increment is ever zeroed away, as
// could happen with the earlier claim-then-zero in-place design.
type bucketState struct {
	epochNano int64
	successes atomic.Uint64
	failures  atomic.Uint64
}

// CommandFailureDetector observes command outcomes inside a sliding time
// window and reports when failover should be triggered. The implementation
// uses a fixed-size ring of buckets and only sync/atomic operations on the
// hot path, so RecordSuccess and RecordFailure scale across many goroutines
// without contention.
type CommandFailureDetector struct {
	config          CommandFailureDetectorConfig
	buckets         []bucket
	bucketWidthNano int64
	windowNano      int64
	// nowNano is the detector's clock in nanoseconds. By default it is the
	// monotonic time since the detector was built, so a wall-clock step in
	// either direction neither forgets recent buckets (forward step) nor pins
	// stale ones (backward step). Injectable for tests, which may drive it
	// backwards; the future-epoch guards below stay for that.
	nowNano func() int64
}

// NewCommandFailureDetector creates a new sliding-window failure detector
// with the given configuration. Any zero-valued field in config is replaced
// with its documented default, so passing the zero value is equivalent to
// passing DefaultCommandFailureDetectorConfig().
func NewCommandFailureDetector(config CommandFailureDetectorConfig) *CommandFailureDetector {
	config.applyDefaults()
	// Clamp to at least one nanosecond so bucketFor never divides by zero
	// when a caller picks a window shorter than NumBuckets nanoseconds.
	bucketWidthNano := int64(config.FailureDetectionWindow) / int64(config.NumBuckets)
	if bucketWidthNano < 1 {
		bucketWidthNano = 1
	}
	epoch := time.Now()
	return &CommandFailureDetector{
		config:          config,
		buckets:         make([]bucket, config.NumBuckets),
		bucketWidthNano: bucketWidthNano,
		windowNano:      int64(config.FailureDetectionWindow),
		// time.Since reads the monotonic clock: bucket epochs and the window
		// cutoff are all relative to the detector's start, never to the wall
		// clock.
		nowNano: func() int64 { return int64(time.Since(epoch)) },
	}
}

// RecordSuccess records a successful command outcome.
func (d *CommandFailureDetector) RecordSuccess() {
	d.addSuccess()
}

// RecordFailure records a failed command outcome. A nil error is treated as
// a no-op (so callers that forward errors unconditionally do not accumulate
// phantom failures); context cancellation and deadline-exceeded errors are
// also ignored because they originate on the client side and are not a
// signal about the database's health.
func (d *CommandFailureDetector) RecordFailure(err error) {
	if err == nil {
		return
	}
	// Dial errors mean the TCP connection was never established — the
	// canonical unreachable-database signal. Checked BEFORE the context
	// filter below, mirroring the root retry classifier: a dial that expires
	// through DialTimeout surfaces as *net.OpError wrapping
	// context.DeadlineExceeded, and filtering it as a client-side context
	// error would keep the detector from ever tripping on a dead endpoint.
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Op == "dial" {
		d.addFailure()
		return
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return
	}
	if errors.Is(err, pool.ErrPoolTimeout) || errors.Is(err, pool.ErrPoolExhausted) {
		// Local pool saturation: the command never reached the database, so
		// this is no verdict on its health — an undersized pool on a busy
		// client must not drive failover.
		return
	}
	if errors.Is(err, proto.Nil) || errors.Is(err, txFailedErr) {
		// redis.Nil (key missing) and an optimistic-locking transaction
		// abort are well-formed server replies — proof of a healthy
		// database. Count them as successes so miss-heavy or contended
		// workloads cannot trip the failure rate.
		d.addSuccess()
		return
	}
	var reply redisReply
	if errors.As(err, &reply) && !isAvailabilityReply(reply.Error()) {
		// Any other well-formed server reply (WRONGTYPE, BUSYGROUP,
		// NOSCRIPT, ...) is an application-level error from a database that
		// processed the command: proof of health, not a failure — except
		// the availability replies (LOADING, CLUSTERDOWN, ...) that signal
		// a database unable to serve.
		d.addSuccess()
		return
	}
	d.addFailure()
}

// addSuccess and addFailure record one outcome in the current bucket, skipping
// it when bucketFor reports the caller's timestamp is a full ring lap stale
// (nil) — an already-expired outcome must not be counted in the live window.
func (d *CommandFailureDetector) addSuccess() {
	if b := d.bucketFor(d.nowNano()); b != nil {
		b.successes.Add(1)
	}
}

func (d *CommandFailureDetector) addFailure() {
	if b := d.bucketFor(d.nowNano()); b != nil {
		b.failures.Add(1)
	}
}

// redisReply matches any well-formed server error reply. The concrete
// proto.RedisError string only covers replies the reader does not recognize:
// known prefixes are parsed into typed structs (*proto.LoadingError,
// *proto.AuthError, *proto.MovedError, ...) that share just the RedisError()
// marker — matching the concrete string type alone would misclassify every
// typed reply as a transport failure.
type redisReply interface {
	error
	RedisError()
}

// isAvailabilityReply matches server replies that indicate the database
// cannot currently serve traffic (mirroring the root package's retryable
// reply classification, which cannot be imported from here).
func isAvailabilityReply(s string) bool {
	for _, prefix := range []string{
		"LOADING ", "READONLY ", "CLUSTERDOWN ", "TRYAGAIN ",
		"MASTERDOWN ", "NOREPLICAS ", "ERR max number of clients",
		// A MOVED/ASK that reaches this layer means the cluster client
		// exhausted its redirect budget and still could not route: the member
		// cannot serve, an availability failure — mirroring the root
		// classifier's isRedirectReply case. Without these, RecordFailure on a
		// surfaced redirect would fall through to the success branch below.
		"MOVED ", "ASK ",
	} {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	// A write script hitting a read-only replica embeds READONLY inside the
	// script error instead of at the prefix; mirror the root classifier's
	// substring match so EVAL-heavy workloads see the same verdict.
	return strings.Contains(s, "-READONLY You can't write against a read only replica")
}

// ShouldFailover returns true when the outcomes observed within the trailing
// FailureDetectionWindow indicate that failover should be triggered. A
// database is considered faulty when at least MinNumFailures commands have
// failed AND the observed failure rate is at least FailureRateThreshold.
// Either half of the check can be disabled by setting IgnoreMinNumFailures
// or IgnoreFailureRateThreshold; when both are disabled, any single failure
// in the window triggers failover.
// At least one failure must have been observed for failover to be considered.
func (d *CommandFailureDetector) ShouldFailover() bool {
	successes, failures := d.snapshot()

	if failures == 0 {
		return false
	}
	if !d.config.IgnoreMinNumFailures && failures < d.config.MinNumFailures {
		return false
	}
	if d.config.IgnoreFailureRateThreshold {
		return true
	}

	total := successes + failures
	failureRate := float64(failures) / float64(total)
	return failureRate >= d.config.FailureRateThreshold
}

// Reset discards all recorded outcomes. Concurrent recorders may race with
// Reset; in the worst case a small number of in-flight increments survive
// the reset, which is acceptable for a failure detector.
func (d *CommandFailureDetector) Reset() {
	for i := range d.buckets {
		d.buckets[i].state.Store(nil)
	}
}

// Stats returns a read-only snapshot of the outcomes observed within the
// current sliding window. The returned counts are aggregated across the
// bucket ring and reflect the same view of state used by ShouldFailover.
func (d *CommandFailureDetector) Stats() (successes, failures uint64) {
	return d.snapshot()
}

// bucketFor returns the bucket that owns the supplied nanosecond timestamp,
// initialising it (resetting counters and stamping the new epoch) if a
// previous lap of the ring left stale data in that slot. It returns nil when
// the caller's timestamp is at least a full ring lap stale — the outcome has
// already aged out of the window and must be dropped, not recorded.
func (d *CommandFailureDetector) bucketFor(nowNano int64) *bucketState {
	bucketStart := nowNano - (nowNano % d.bucketWidthNano)
	idx := (bucketStart / d.bucketWidthNano) % int64(len(d.buckets))
	b := &d.buckets[idx]

	for {
		st := b.state.Load()
		if st != nil && st.epochNano == bucketStart {
			return st
		}
		// A slot holding a LATER epoch than ours is ambiguous. Either a
		// concurrent writer advanced the ring a full lap while this caller was
		// descheduled between reading its timestamp and here — the epoch is real
		// (<= the current clock) — or a backward wall-clock step left a future
		// epoch (> the current clock) that describes no live bucket. Re-read the
		// clock to tell them apart.
		//
		// In the concurrent-writer case, drop the outcome and return nil: any
		// two epochs mapping to the same ring slot differ by a whole multiple of
		// the ring lap (idx = (epoch/width) % N), so a slot epoch strictly later
		// than ours is at least one full FailureDetectionWindow ahead — this
		// caller's outcome has already aged out and recording it into the newer
		// bucket would count an expired failure/success as current. Preserve the
		// newer bucket untouched; only a rollback artifact falls through to be
		// rebased below.
		if st != nil && st.epochNano > bucketStart && st.epochNano <= d.nowNano() {
			return nil
		}
		// st is nil (slot never used), holds an older epoch (a previous lap of
		// the ring), or holds a future epoch from a backward wall-clock step
		// (VM restore, NTP step). None describe the current bucket, so stamp a
		// fresh one. Rebasing the rollback case is what keeps outcomes recorded
		// after a rollback visible to snapshot (which excludes future epochs);
		// returning the stale slot would silently drop them until wall time
		// caught back up.
		fresh := &bucketState{epochNano: bucketStart}
		if b.state.CompareAndSwap(st, fresh) {
			return fresh
		}
		// Another writer won the race; reload and decide again.
	}
}

// snapshot sums the outcomes across every bucket whose time slot overlaps
// the trailing window. A bucket spans [epoch, epoch + bucketWidth) and is
// included when (epoch + bucketWidth) > (now - window), i.e. when
// epoch > now - window - bucketWidth. The cutoff is precomputed below.
//
// The sum is not atomic across buckets, which is acceptable for a failure
// detector: a snapshot can interleave with concurrent writers, but the
// aggregated counts only ever undercount the true value by at most the
// in-flight writes.
func (d *CommandFailureDetector) snapshot() (successes, failures uint64) {
	nowNano := d.nowNano()
	cutoff := nowNano - d.windowNano - d.bucketWidthNano
	for i := range d.buckets {
		st := d.buckets[i].state.Load()
		// The upper bound guards against wall-clock rollback (VM restore,
		// NTP step): buckets written before the step carry epochs later than
		// the new now, and counting them would pin stale failures into
		// ShouldFailover for the rollback duration plus the window.
		if st != nil && st.epochNano > cutoff && st.epochNano <= nowNano {
			successes += st.successes.Load()
			failures += st.failures.Load()
		}
	}
	return successes, failures
}
