package distributor

import (
	"golang.org/x/time/rate"

	"github.com/cortexproject/cortex/pkg/util/limiter"
	"github.com/cortexproject/cortex/pkg/util/validation"
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

func (s *localStrategy) Limit(tenantID string) float64 {
	return s.limits.IngestionRate(tenantID)
}

func (s *localStrategy) Burst(tenantID string) int {
	return s.limits.IngestionBurstSize(tenantID)
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

func (s *globalStrategy) Limit(tenantID string) float64 {
	numDistributors := s.ring.HealthyInstancesCount()

	if numDistributors == 0 {
		return s.limits.IngestionRate(tenantID)
	}

	return s.limits.IngestionRate(tenantID) / float64(numDistributors)
}

func (s *globalStrategy) Burst(tenantID string) int {
	// The meaning of burst doesn't change for the global strategy, in order
	// to keep it easier to understand for users / operators.
	return s.limits.IngestionBurstSize(tenantID)
}

type infiniteStrategy struct{}

func newInfiniteIngestionRateStrategy() limiter.RateLimiterStrategy {
	return &infiniteStrategy{}
}

func (s *infiniteStrategy) Limit(tenantID string) float64 {
	return float64(rate.Inf)
}

func (s *infiniteStrategy) Burst(tenantID string) int {
	// Burst is ignored when limit = rate.Inf
	return 0
}
