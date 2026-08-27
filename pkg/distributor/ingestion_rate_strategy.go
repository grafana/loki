package distributor

import (
	"strings"

	"github.com/grafana/dskit/limiter"
)

// ReadLifecycler represents the read interface to the lifecycler.
type ReadLifecycler interface {
	HealthyInstancesCount() int
}

// rateLimitKeySeparator separates the tenant from the policy in a composite rate-limit key.
//
// decodeRateLimitKey splits on the FIRST ":", so a policy name may safely contain ":" — it is
// kept verbatim as the suffix. The only ambiguity would be a tenant ID that itself contains
// ":", which is vanishingly unlikely in practice (tenant/org IDs are numeric in our
// deployments) and not worth guarding against with extra validation on the hot write path. A
// key with no ":" is a tenant-only key (the tenant-wide bucket), which keeps the previous
// tenant-keyed limiter behavior intact.
const rateLimitKeySeparator = ":"

// encodeRateLimitKey builds the key passed to the dskit rate limiter. With an empty policy
// it returns the bare tenant (tenant-wide bucket); otherwise it returns "tenant:policy".
func encodeRateLimitKey(tenant, policy string) string {
	if policy == "" {
		return tenant
	}
	return tenant + rateLimitKeySeparator + policy
}

// decodeRateLimitKey splits a key produced by encodeRateLimitKey back into its tenant and
// policy parts. It splits on the first separator so that policy names containing the
// separator are preserved. A key without a separator is tenant-only.
func decodeRateLimitKey(key string) (tenant, policy string) {
	tenant, policy, found := strings.Cut(key, rateLimitKeySeparator)
	if !found {
		return key, ""
	}
	return tenant, policy
}

type localStrategy struct {
	limits Limits
}

func newLocalIngestionRateStrategy(limits Limits) limiter.RateLimiterStrategy {
	return &localStrategy{
		limits: limits,
	}
}

func (s *localStrategy) Limit(key string) float64 {
	tenant, policy := decodeRateLimitKey(key)
	if policy != "" {
		if rate, ok := s.limits.PolicyIngestionRateBytes(tenant, policy); ok {
			return rate
		}
	}
	return s.limits.IngestionRateBytes(tenant)
}

func (s *localStrategy) Burst(key string) int {
	tenant, policy := decodeRateLimitKey(key)
	if policy != "" {
		// ok means the policy explicitly set a burst; honor it even when it's 0 (a 0 burst
		// blocks ingestion for the policy). An unset per-policy burst (ok=false) inherits the
		// tenant burst.
		if burst, ok := s.limits.PolicyIngestionBurstSizeBytes(tenant, policy); ok {
			return burst
		}
	}
	return s.limits.IngestionBurstSizeBytes(tenant)
}

type globalStrategy struct {
	limits Limits
	ring   ReadLifecycler
}

func newGlobalIngestionRateStrategy(limits Limits, ring ReadLifecycler) limiter.RateLimiterStrategy {
	return &globalStrategy{
		limits: limits,
		ring:   ring,
	}
}

func (s *globalStrategy) Limit(key string) float64 {
	tenant, policy := decodeRateLimitKey(key)

	rateBytes := s.limits.IngestionRateBytes(tenant)
	if policy != "" {
		if r, ok := s.limits.PolicyIngestionRateBytes(tenant, policy); ok {
			rateBytes = r
		}
	}

	numDistributors := s.ring.HealthyInstancesCount()
	if numDistributors == 0 {
		return rateBytes
	}

	return rateBytes / float64(numDistributors)
}

func (s *globalStrategy) Burst(key string) int {
	// The meaning of burst doesn't change for the global strategy, in order
	// to keep it easier to understand for users / operators.
	tenant, policy := decodeRateLimitKey(key)
	if policy != "" {
		// ok means the policy explicitly set a burst; honor it even when it's 0.
		if burst, ok := s.limits.PolicyIngestionBurstSizeBytes(tenant, policy); ok {
			return burst
		}
	}
	return s.limits.IngestionBurstSizeBytes(tenant)
}
