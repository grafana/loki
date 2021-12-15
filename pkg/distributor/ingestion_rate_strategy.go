package distributor

import (
	"github.com/grafana/dskit/limiter"

	"github.com/grafana/loki/pkg/validation"
)

// RateLimitStrat represents a rate limiting strategy to be followed by the distributor
// regarding when to discard received input.
//
// As of now, only two strategies are supported: local, where the ingestion rate limit should is
// applied individually to each distributor instance, and global, where the ingestion rate limit
// is shared across the cluster. The ingestion rate strategy cannot be overridden on a per-tenant basis.
type RateLimitStrat int64

const (
	// GlobalRateLimitStrat represents a ingestion rate limiting strategy that enforces the rate
	// limiting globally, configuring a per-distributor local rate limiter as "ingestion_rate / N",
	// where N is the number of distributor replicas (it's automatically adjusted if the
	// number of replicas change).
	//
	// The global strategy requires the distributors to form their own ring, which
	// is used to keep track of the current number of healthy distributor replicas.
	GlobalRateLimitStrat RateLimitStrat = iota

	// LocalRateLimitStrat represents a ingestion rate limiting strategy that enforces the limit
	// on a per distributor basis. The actual effective rate limit will be N times higher, where
	// N is the number of distributor replicas.
	LocalRateLimitStrat
)

// ReadLifecycler represents the read interface to the lifecycler.
type ReadLifecycler interface {
	HealthyInstancesCount() int
}

type localStrategy struct {
	limits *validation.Overrides
}

func newLocalIngestionRateStrategy(limits *validation.Overrides) limiter.RateLimiterStrategy {
	return &localStrategy{
		limits: limits,
	}
}

func (s *localStrategy) Limit(userID string) float64 {
	return s.limits.IngestionRateBytes(userID)
}

func (s *localStrategy) Burst(userID string) int {
	return s.limits.IngestionBurstSizeBytes(userID)
}

type globalStrategy struct {
	limits *validation.Overrides
	ring   ReadLifecycler
}

func newGlobalIngestionRateStrategy(limits *validation.Overrides, ring ReadLifecycler) limiter.RateLimiterStrategy {
	return &globalStrategy{
		limits: limits,
		ring:   ring,
	}
}

func (s *globalStrategy) Limit(userID string) float64 {
	numDistributors := s.ring.HealthyInstancesCount()

	if numDistributors == 0 {
		return s.limits.IngestionRateBytes(userID)
	}

	return s.limits.IngestionRateBytes(userID) / float64(numDistributors)
}

func (s *globalStrategy) Burst(userID string) int {
	// The meaning of burst doesn't change for the global strategy, in order
	// to keep it easier to understand for users / operators.
	return s.limits.IngestionBurstSizeBytes(userID)
}
