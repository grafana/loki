package frontend

// Limits contains all limits enforced by the limits frontend.
type Limits interface {
	IngestionRateBytes(userID string) float64
	IngestionBurstSizeBytes(userID string) int
	MaxGlobalStreamsPerUser(userID string) int
}

// rateLimitsAdapter implements the dskit.RateLimiterStrategy interface. We use
// it to load per-tenant rate limits into dskit.RateLimiter.
type rateLimitsAdapter struct {
	limits Limits
}

func newRateLimitsAdapter(limits Limits) *rateLimitsAdapter {
	return &rateLimitsAdapter{limits: limits}
}

// Limit implements dskit.RateLimiterStrategy.
func (s *rateLimitsAdapter) Limit(tenantID string) float64 {
	return s.limits.IngestionRateBytes(tenantID)
}

// Burst implements dskit.RateLimiterStrategy.
func (s *rateLimitsAdapter) Burst(tenantID string) int {
	return s.limits.IngestionBurstSizeBytes(tenantID)
}
