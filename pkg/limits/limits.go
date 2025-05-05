package limits

type Reason int

const (
	// ReasonExceedsMaxStreams is returned when a tenant exceeds the maximum
	// number of active streams as per their per-tenant limit.
	ReasonExceedsMaxStreams Reason = iota

	// ReasonExceedsRateLimit is returned when a tenant exceeds their maximum
	// rate limit as per their per-tenant limit.
	ReasonExceedsRateLimit
)

func (r Reason) String() string {
	switch r {
	case ReasonExceedsMaxStreams:
		return "max streams exceeded"
	case ReasonExceedsRateLimit:
		return "rate limit exceeded"
	default:
		return "unknown reason"
	}
}

// Limits contains all limits enforced by the limits frontend.
type Limits interface {
	IngestionRateBytes(userID string) float64
	IngestionBurstSizeBytes(userID string) int
	MaxGlobalStreamsPerUser(userID string) int
}

// RateLimitsAdapter implements the dskit.RateLimiterStrategy interface. We use
// it to load per-tenant rate limits into dskit.RateLimiter.
type RateLimitsAdapter struct {
	limits Limits
}

func NewRateLimitsAdapter(limits Limits) *RateLimitsAdapter {
	return &RateLimitsAdapter{limits: limits}
}

// Limit implements dskit.RateLimiterStrategy.
func (s *RateLimitsAdapter) Limit(tenantID string) float64 {
	return s.limits.IngestionRateBytes(tenantID)
}

// Burst implements dskit.RateLimiterStrategy.
func (s *RateLimitsAdapter) Burst(tenantID string) int {
	return s.limits.IngestionBurstSizeBytes(tenantID)
}
