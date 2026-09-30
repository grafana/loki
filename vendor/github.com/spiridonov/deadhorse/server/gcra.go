package server

import (
	"math"
	"math/bits"
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
// capacity is the bucket size in cost units, cost is how many units this
// request consumes, and the bucket drains at a rate of units cost units
// every period -- see deadhorse.Rate. Both units and period act as the
// ratio's denominator/numerator and so, like a divide-by-zero guard, must be
// strictly positive; callers normalize a caller-supplied zero/negative units
// to 1 before calling in (see deadhorse.EffectiveUnits) the same way they
// already do for cost (see deadhorse.EffectiveCost).
//
// It returns whether the request is throttled, the bucket's headroom in cost
// units as of just before this request (not shrunk by this request's own
// cost, and the same whether or not it ends up throttled), how long until
// this request would fit if it didn't, and the TAT the caller should persist
// if the request was admitted and is being applied for real (gcraCheck
// itself never mutates anything -- it's a pure function).
func gcraCheck(tat, now, capacity, units int64, period time.Duration, cost int64) (throttled bool, remaining int64, retryAfter time.Duration, admittedTAT int64) {
	if period <= 0 || units <= 0 || capacity < 0 || cost < 0 {
		// No sane leak rate, or a caller-controlled shape that can't be
		// multiplied below without risking overflow (see mulNonNeg): fail
		// closed rather than divide by zero or wrap.
		return true, 0, 0, tat
	}
	p := int64(period)

	// maxDebt is how many nanoseconds of debt capacity cost units are worth
	// -- the bucket's size, expressed on the same timeline as tat -- and
	// costNs is how many nanoseconds of debt this request's own cost adds.
	// Both come from the same units-per-period ratio, so both go through
	// the same helper; units==1 (a plain "one unit per period" rate, by far
	// the common case) gets a dedicated fast path that's just a 64-bit
	// multiply instead of a 128-bit multiply-then-divide.
	var maxDebt, costNs int64
	var ok bool
	if units == 1 {
		if maxDebt, ok = mulNonNeg(capacity, p); !ok {
			return true, 0, 0, tat
		}
		if costNs, ok = mulNonNeg(cost, p); !ok {
			return true, 0, 0, tat
		}
	} else {
		// maxDebt rounds down and costNs rounds up: both directions make
		// the bucket strictly no more permissive than the configured rate,
		// rather than letting a fractional remainder round the wrong way
		// into over-admitting.
		if maxDebt, ok = mulDivFloor(capacity, p, units); !ok {
			return true, 0, 0, tat
		}
		if costNs, ok = mulDivCeil(cost, p, units); !ok {
			return true, 0, 0, tat
		}
	}

	effectiveTAT := max(tat, now)
	level := effectiveTAT - now // ns of "debt" currently sitting in the bucket

	var debtUnits int64
	if units == 1 {
		debtUnits = ceilDiv(level, p)
	} else if d, ok := mulDivCeil(level, units, p); ok {
		debtUnits = d
	} else {
		// level*units doesn't fit in the 128-bit intermediate this call's
		// own maxDebt/costNs comfortably stay within -- only reachable when
		// tat was pushed far out by a wildly different (much larger
		// capacity/rate) Limit sharing this key. remainingUnits is only a
		// reported figure here; report it as fully depleted rather than
		// guess, safe because level being this far past what this call's
		// own maxDebt allows already means the checks below throttle it
		// regardless.
		debtUnits = capacity + 1
	}
	remainingUnits := max(capacity-debtUnits, 0)

	if cost > capacity {
		// This single request can never be admitted under this Limit, no
		// matter how long the caller waits: even a completely empty bucket
		// only ever allows maxDebt worth of debt, and a cost this large
		// permanently exceeds it (rejected requests never advance tat, so
		// retrying later doesn't change this arithmetic at all). Report
		// that as RetryAfter=0 -- "don't bother retrying" -- the same
		// signal already used for a nonsensical Limit above, rather than a
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

// mulDivFloor computes floor(a*b/c) for non-negative a, b and positive c,
// via a 128-bit intermediate product (math/bits.Mul64) so a*b overflowing
// int64 -- routine once a or b is a large Units/Period/cost -- doesn't wrap.
// ok is false if the mathematical result itself wouldn't fit in an int64,
// which the caller must treat as an overflow, not as 0.
func mulDivFloor(a, b, c int64) (result int64, ok bool) {
	if a < 0 || b < 0 || c <= 0 {
		return 0, false
	}
	hi, lo := bits.Mul64(uint64(a), uint64(b))
	if hi >= uint64(c) {
		// The quotient alone would already need more than 64 bits.
		return 0, false
	}
	q, _ := bits.Div64(hi, lo, uint64(c))
	if q > uint64(math.MaxInt64) {
		return 0, false
	}
	return int64(q), true
}

// mulDivCeil is mulDivFloor rounded up instead of down -- see its doc
// comment for the overflow contract.
func mulDivCeil(a, b, c int64) (result int64, ok bool) {
	if a < 0 || b < 0 || c <= 0 {
		return 0, false
	}
	hi, lo := bits.Mul64(uint64(a), uint64(b))
	if hi >= uint64(c) {
		return 0, false
	}
	q, r := bits.Div64(hi, lo, uint64(c))
	if r != 0 {
		if q == math.MaxUint64 {
			return 0, false
		}
		q++
	}
	if q > uint64(math.MaxInt64) {
		return 0, false
	}
	return int64(q), true
}
