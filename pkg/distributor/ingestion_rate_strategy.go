package distributor

import (
	"strings"

	"github.com/grafana/dskit/limiter"
)

// ReadLifecycler represents the read interface to the lifecycler.
type ReadLifecycler interface {
	HealthyInstancesCount() int
}

// rateLimitKeySeparator separates the tenant from the policy in a composite rate-limit
// key. Tenant IDs are numeric in practice so they never contain this separator; a key
// without it is therefore tenant-only (the tenant-wide bucket), preserving backwards
// compatibility with the previous tenant-keyed limiter.
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
		// A policy may set a rate without an explicit burst; fall back to the tenant burst
		// when the per-policy burst is unset (0).
		if burst, ok := s.limits.PolicyIngestionBurstSizeBytes(tenant, policy); ok && burst > 0 {
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
		if burst, ok := s.limits.PolicyIngestionBurstSizeBytes(tenant, policy); ok && burst > 0 {
			return burst
		}
	}
	return s.limits.IngestionBurstSizeBytes(tenant)
}
