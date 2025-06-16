package limits

type Reason int

const (
	// ReasonExceedsMaxStreams is returned when a tenant exceeds the maximum
	// number of active streams as per their per-tenant limit.
	ReasonExceedsMaxStreams Reason = iota
)

func (r Reason) String() string {
	switch r {
	case ReasonExceedsMaxStreams:
		return "max streams exceeded"
	default:
		return "unknown reason"
	}
}
