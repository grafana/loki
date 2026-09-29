package server

import (
	"math"
	"time"
)

// gcraCheck evaluates one leaky-bucket admission check using GCRA (the Generic
// Cell Rate Algorithm): the leaky bucket "as a meter", reformulated so the
// only state that needs to persist between calls is a single timestamp
// instead of a decaying counter that has to be refilled on every read.
//
// tat and now are instants in nanoseconds since the same epoch (0 for tat
// means "never seen," which is deliberately treated the same as "now" -- a
// fresh key starts with a fully compliant history, i.e. an empty bucket).
// capacity is the bucket size in cost units, emissionInterval is how long it
// takes to leak one unit, and cost is how many units this request consumes.
//
// It returns whether the request is throttled, the bucket's headroom in cost
// units as of just before this request (not shrunk by this request's own
// cost, and the same whether or not it ends up throttled), how long until
// this request would fit if it didn't, and the TAT the caller should persist
// if the request was admitted and is being applied for real (gcraCheck
// itself never mutates anything -- it's a pure function).
func gcraCheck(tat, now, capacity int64, emissionInterval time.Duration, cost int64) (throttled bool, remaining int64, retryAfter time.Duration, admittedTAT int64) {
	if emissionInterval <= 0 || capacity < 0 || cost < 0 {
		// No sane leak rate, or a caller-controlled shape that can't be
		// multiplied below without risking overflow (see mulNonNeg): fail
		// closed rather than divide by zero or wrap.
		return true, 0, 0, tat
	}
	ei := int64(emissionInterval)

	// capacity, cost, and emissionInterval all arrive over the wire with no
	// upper bound (see textserver.parseEntry), so multiplying any pair of
	// them can overflow int64. Left unchecked, an overflowing product wraps
	// into a small (or negative) number, which would silently admit a
	// request for free instead of throttling it -- reject anything whose
	// product would overflow rather than let it wrap.
	maxDebt, ok := mulNonNeg(capacity, ei)
	if !ok {
		return true, 0, 0, tat
	}
	costNs, ok := mulNonNeg(cost, ei)
	if !ok {
		return true, 0, 0, tat
	}

	effectiveTAT := max(tat, now)
	level := effectiveTAT - now // ns of "debt" currently sitting in the bucket
	remainingUnits := max(capacity-ceilDiv(level, ei), 0)

	if cost > capacity {
		// This single request can never be admitted under this Limit, no
		// matter how long the caller waits: even a completely empty bucket
		// only ever allows capacity*ei of debt, and cost*ei > capacity*ei
		// permanently (rejected requests never advance tat, so retrying
		// later doesn't change this arithmetic at all). Report that as
		// RetryAfter=0 -- "don't bother retrying" -- the same signal
		// already used for a nonsensical Limit above, rather than a
		// positive-looking value that would never actually resolve.
		return true, remainingUnits, 0, tat
	}

	candidateTAT, ok := addNonNeg(effectiveTAT, costNs)
	if !ok {
		return true, remainingUnits, 0, tat
	}

	if candidateTAT-now > maxDebt {
		return true, remainingUnits, time.Duration(candidateTAT - now - maxDebt), tat
	}
	return false, remainingUnits, 0, candidateTAT
}

func ceilDiv(a, b int64) int64 {
	if a <= 0 {
		return 0
	}
	// Not (a + b - 1) / b: that addition can itself overflow when a is close
	// to math.MaxInt64 (reachable via a large, but individually valid, tat),
	// wrapping into a negative result.
	q := a / b
	if a%b != 0 {
		q++
	}
	return q
}

// mulNonNeg multiplies two non-negative int64s, reporting ok=false instead
// of silently wrapping if the product would overflow.
func mulNonNeg(a, b int64) (product int64, ok bool) {
	if a == 0 || b == 0 {
		return 0, true
	}
	if a > math.MaxInt64/b {
		return 0, false
	}
	return a * b, true
}

// addNonNeg adds two non-negative int64s, reporting ok=false instead of
// silently wrapping if the sum would overflow.
func addNonNeg(a, b int64) (sum int64, ok bool) {
	if a > math.MaxInt64-b {
		return 0, false
	}
	return a + b, true
}
