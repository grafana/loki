package distributor

import (
	"context"
	"time"

	"github.com/grafana/dskit/limiter"
	"golang.org/x/time/rate"

	throttler "github.com/spiridonov/deadhorse"
)

// ingestionRateEnforcer decides, for one push, whether every rate-limit
// bucket built for a tenant is jointly admitted, and reports the configured
// limit for a bucket that wasn't -- the two things enforceIngestionRateLimits
// and rateLimitError need, regardless of which mechanism backs the decision:
// the local dskit limiter (used by both the "local" and ring-divided
// "global" strategies) or the external global throttler.
type ingestionRateEnforcer interface {
	// enforce evaluates every bucket in rlBuckets as a single all-or-nothing
	// decision for tenantID, returning the buckets that individually caused
	// rejection (nil if the request is admitted). A non-nil err is
	// diagnostic only -- exceeded already reflects the actual outcome.
	enforce(ctx context.Context, now time.Time, tenantID string, rlBuckets map[string]*rateLimitBucket) (exceeded []*rateLimitBucket, err error)
	// limit reports the configured bytes/sec limit for a bucket, for the
	// client-facing rate-limited error message.
	limit(now time.Time, tenantID string, b *rateLimitBucket) int
}

// reservationEnforcer wraps the existing dskit local token bucket -- used
// for both the "local" and ring-divided "global" strategies, which differ
// only in how their RateLimiterStrategy computes Limit/Burst, never in how
// admission is decided. This is the exact Reserve/Cancel logic
// enforceIngestionRateLimits and rateLimitError used inline before the
// exact strategy existed, moved here unchanged.
type reservationEnforcer struct {
	limiter *limiter.RateLimiter
}

func newReservationEnforcer(l *limiter.RateLimiter) *reservationEnforcer {
	return &reservationEnforcer{limiter: l}
}

func (e *reservationEnforcer) limit(now time.Time, tenantID string, b *rateLimitBucket) int {
	key := tenantID
	if b.hasOverride {
		key = encodeRateLimitKey(tenantID, b.policy)
	}
	return int(e.limiter.Limit(now, key))
}

func (e *reservationEnforcer) enforce(_ context.Context, now time.Time, tenantID string, rlBuckets map[string]*rateLimitBucket) ([]*rateLimitBucket, error) {
	type bucketReservation struct {
		bucket      *rateLimitBucket
		reservation *rate.Reservation
	}
	reservations := make([]bucketReservation, 0, len(rlBuckets))
	var exceeded []*rateLimitBucket
	for _, b := range rlBuckets {
		limiterKey := tenantID
		if b.hasOverride {
			limiterKey = encodeRateLimitKey(tenantID, b.policy)
		}
		r := e.limiter.ReserveN(now, limiterKey, b.bytes)
		reservations = append(reservations, bucketReservation{bucket: b, reservation: r})
		// A reservation that isn't OK (bytes exceed the burst) or that requires a wait is not
		// immediately allowed, which is equivalent to AllowN returning false. We evaluate every
		// bucket (rather than stopping at the first failure) so the rejection error can
		// deterministically report all exceeded buckets.
		if !r.OK() || r.DelayFrom(now) > 0 {
			exceeded = append(exceeded, b)
		}
	}

	if len(exceeded) == 0 {
		return nil, nil
	}

	// Roll back every reservation so no tokens are consumed for a rejected request.
	for _, br := range reservations {
		br.reservation.CancelAt(now)
	}
	return exceeded, nil
}

// throttleCaller is the one method throttlerEnforcer needs from
// *globalThrottler -- narrowed to an interface so tests can exercise the
// bucket-attribution logic below against a fake, without a real throttler
// server.
type throttleCaller interface {
	Throttle(ctx context.Context, tenantID string, entries []throttler.RequestEntry) ([]throttler.ResponseEntry, error)
}

// throttlerEnforcer backs validation.ExactIngestionRateStrategy:
// every bucket for one push becomes one entry in a single Throttle call,
// shardKey'd by tenantID so the whole group lands on one shard and commits
// as one all-or-none transaction (see the external throttler's own
// documentation on same-line transactions).
type throttlerEnforcer struct {
	limits Limits
	caller throttleCaller
}

func newThrottlerEnforcer(limits Limits, caller throttleCaller) *throttlerEnforcer {
	return &throttlerEnforcer{limits: limits, caller: caller}
}

func (e *throttlerEnforcer) limit(_ time.Time, tenantID string, b *rateLimitBucket) int {
	if b.hasOverride {
		if r, ok := e.limits.PolicyIngestionRateBytes(tenantID, b.policy); ok {
			return int(r)
		}
	}
	return int(e.limits.IngestionRateBytes(tenantID))
}

func (e *throttlerEnforcer) enforce(ctx context.Context, _ time.Time, tenantID string, rlBuckets map[string]*rateLimitBucket) ([]*rateLimitBucket, error) {
	if len(rlBuckets) == 0 {
		return nil, nil
	}

	order := make([]*rateLimitBucket, 0, len(rlBuckets))
	entries := make([]throttler.RequestEntry, 0, len(rlBuckets))
	for _, b := range rlBuckets {
		rateBytes := e.limits.IngestionRateBytes(tenantID)
		burstBytes := e.limits.IngestionBurstSizeBytes(tenantID)
		if b.hasOverride {
			if r, ok := e.limits.PolicyIngestionRateBytes(tenantID, b.policy); ok {
				rateBytes = r
			}
			if burst, ok := e.limits.PolicyIngestionBurstSizeBytes(tenantID, b.policy); ok {
				burstBytes = burst
			}
		}

		var emission time.Duration
		if rateBytes > 0 {
			emission = time.Duration(1e9 / rateBytes)
		}

		key := tenantID
		if b.hasOverride {
			key = encodeRateLimitKey(tenantID, b.policy)
		}

		order = append(order, b)
		entries = append(entries, throttler.RequestEntry{
			Key:   key,
			Limit: throttler.Limit{Capacity: int64(burstBytes), EmissionInterval: emission},
			Cost:  int64(b.bytes),
		})
	}

	results, err := e.caller.Throttle(ctx, tenantID, entries)

	// results is always fully populated at the same length as entries, in the
	// same order, even when err is non-nil (see the Throttler contract) --
	// err here is diagnostic only, for logging by the caller.
	if len(results) == 0 || !results[0].Throttled {
		return nil, err
	}

	// The call was denied as a whole. Throttled is the group's decision, not
	// each entry's own in isolation -- every entry reports Throttled here,
	// including ones whose own bucket had room. RetryAfter is *not*
	// overwritten by the group decision, so it's each entry's own truth:
	// RetryAfter > 0 means this bucket itself was the one that exceeded;
	// RetryAfter == 0 means this bucket was fine and only got swept in by a
	// sibling. Attribute the error message to exactly the buckets that were
	// individually over, not the whole group.
	var exceeded []*rateLimitBucket
	for i, r := range results {
		if r.RetryAfter > 0 {
			exceeded = append(exceeded, order[i])
		}
	}
	if len(exceeded) == 0 {
		// The group was denied but no entry's RetryAfter isolated a cause
		// (e.g. every bucket's own cost individually exceeds its own
		// capacity in the same instant). Attribute the whole group rather
		// than return an empty exceeded slice for a request that was
		// actually denied.
		exceeded = order
	}
	return exceeded, err
}
