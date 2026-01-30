package goldfish

import (
	"math/rand"
	"sync"
	"time"
)

// Sampler determines whether a query should be sampled
type Sampler struct {
	config SamplingConfig
	rng    *rand.Rand
	mu     sync.Mutex
}

// NewSampler creates a new sampler
func NewSampler(config SamplingConfig) *Sampler {
	return &Sampler{
		config: config,
		rng:    rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// ShouldSample determines if a query from a tenant should be sampled
func (s *Sampler) ShouldSample(tenantID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	rate := s.getSamplingRate(tenantID)
	if rate <= 0 {
		return false
	}
	if rate >= 1 {
		return true
	}

	return s.rng.Float64() < rate
}

// getSamplingRate returns the sampling rate for a tenant
// Must be called with s.mu held
func (s *Sampler) getSamplingRate(tenantID string) float64 {
	if rate, ok := s.config.TenantRules[tenantID]; ok {
		return rate
	}
	return s.config.DefaultRate
}

// UpdateConfig updates the sampling configuration
func (s *Sampler) UpdateConfig(config SamplingConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config = config
}
