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
