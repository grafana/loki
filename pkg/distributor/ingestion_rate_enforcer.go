package distributor

import (
	"context"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

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

		key := tenantID
		if b.hasOverride {
			key = encodeRateLimitKey(tenantID, b.policy)
		}
		key = escapeThrottlerKey(key)

		order = append(order, b)
		entries = append(entries, throttler.RequestEntry{
			Key: key,
			// Units/Period express the rate as an exact ratio (rateBytes
			// bytes per second) rather than a "nanoseconds per byte"
			// duration -- the latter can't represent anything above 1
			// byte/ns (1e9 bytes/sec, ~954MiB/s): for any tenant configured
			// faster than that, computing a duration would truncate to
			// zero, which the throttler treats as a nonsensical limit and
			// fails closed on unconditionally, regardless of Capacity.
			// Units/Period has no such ceiling.
			Limit: throttler.Limit{Capacity: int64(burstBytes), Rate: throttler.Rate{Units: int64(rateBytes), Period: time.Second}},
			Cost:  int64(b.bytes),
		})
	}

	results, err := e.caller.Throttle(ctx, tenantID, entries)

	// results is always fully populated at the same length as entries, in the
	// same order, even when err is non-nil (see the Throttler contract) --
	// err here is diagnostic only, for logging by the caller.
	// Throttled is each entry's own check, not the group's decision: an entry
	// whose own bucket had room reports false even when a sibling failed and
	// the whole all-or-none call was therefore not committed. So the call was
	// denied iff any entry is Throttled, and those are exactly the buckets
	// that individually exceeded -- including one whose cost can never fit
	// its burst (Throttled with RetryAfter == 0), which RetryAfter alone
	// could not distinguish from a bucket that was merely swept in.
	var exceeded []*rateLimitBucket
	for i, r := range results {
		if r.Throttled {
			exceeded = append(exceeded, order[i])
		}
	}
	return exceeded, err
}

// escapeThrottlerKey makes key safe to send to the external throttler, whose
// wire protocol forbids keys containing whitespace or '|' -- its client
// reports such an entry as throttled unconditionally, even with fail_open,
// which would turn every push for that bucket into a permanent 429 (and the
// request's other buckets would still be charged). Tenant IDs can't contain
// those characters (see tenant.ValidTenantID), but policy names come from
// operator config and are unconstrained.
//
// Offending characters, and '%' itself, are percent-escaped byte by byte, so
// the mapping is injective -- two different keys never collide into one
// bucket -- and a key with none of them (every valid tenant ID, and any
// ordinary policy name) is returned unchanged.
func escapeThrottlerKey(key string) string {
	needsEscape := func(r rune) bool { return r == '%' || r == '|' || unicode.IsSpace(r) }
	if !strings.ContainsFunc(key, needsEscape) {
		return key
	}

	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(key); {
		r, size := utf8.DecodeRuneInString(key[i:])
		if r != utf8.RuneError && needsEscape(r) {
			for _, c := range []byte(key[i : i+size]) {
				b.WriteByte('%')
				b.WriteByte(hex[c>>4])
				b.WriteByte(hex[c&0xF])
			}
		} else {
			b.WriteString(key[i : i+size])
		}
		i += size
	}
	return b.String()
}

// shadowTimeout bounds how long a shadow check may run once detached from
// the request that triggered it. It's generous relative to the throttler
// client's own per-call timeout (milliseconds) specifically so a slow
// throttler shows up as a shadow failure rather than leaking the goroutine.
const shadowTimeout = 2 * time.Second

// shadowEnforcer enforces exactly like the wrapped enforcer -- in practice,
// reservationEnforcer backed by the ring-divided "global" strategy -- while
// also sending the same buckets to the external throttler for observation
// only: its decision never affects what's returned to the caller. The
// shadow check runs asynchronously, off a context already detached from the
// request's own (context.WithoutCancel), so a slow or unreachable
// throttler can never add latency or risk to the real push path. This is
// how validation.ShadowIngestionRateStrategy proves "exact" safe against
// real production traffic -- with a metric showing what it would have
// decided -- before switching real enforcement over to it.
type shadowEnforcer struct {
	enforcing ingestionRateEnforcer
	shadow    *throttlerEnforcer
	metrics   *metrics
}

func newShadowEnforcer(enforcing ingestionRateEnforcer, shadow *throttlerEnforcer, m *metrics) *shadowEnforcer {
	return &shadowEnforcer{enforcing: enforcing, shadow: shadow, metrics: m}
}

// limit reports the wrapped (real) enforcer's limit -- the shadow path
// never rejects anything for real, so it has no limit value that a
// client-facing error message would ever need.
func (e *shadowEnforcer) limit(now time.Time, tenantID string, b *rateLimitBucket) int {
	return e.enforcing.limit(now, tenantID, b)
}

func (e *shadowEnforcer) enforce(ctx context.Context, now time.Time, tenantID string, rlBuckets map[string]*rateLimitBucket) ([]*rateLimitBucket, error) {
	go e.runShadow(context.WithoutCancel(ctx), now, tenantID, rlBuckets)
	return e.enforcing.enforce(ctx, now, tenantID, rlBuckets)
}

// runShadow evaluates rlBuckets against the external throttler purely for
// observation, recording what it would have decided. ctx must already be
// detached from the request's own context (see enforce above) -- the
// request returning, and its context being canceled, must never cut this
// check short or this would silently degrade into "shadow never actually
// completes."
func (e *shadowEnforcer) runShadow(ctx context.Context, now time.Time, tenantID string, rlBuckets map[string]*rateLimitBucket) {
	ctx, cancel := context.WithTimeout(ctx, shadowTimeout)
	defer cancel()

	exceeded, err := e.shadow.enforce(ctx, now, tenantID, rlBuckets)

	decision := "admit"
	if len(exceeded) > 0 {
		decision = "throttled"
	}
	e.metrics.exactShadowDecisions.WithLabelValues(tenantID, decision).Inc()
	if err != nil {
		e.metrics.exactShadowFailed.WithLabelValues(tenantID).Inc()
	}
}
