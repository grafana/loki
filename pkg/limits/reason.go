package limits

type Reason int

const (
	// ReasonUnknown is the zero value.
	ReasonUnknown Reason = iota
	// ReasonFailed is the reason returned when a stream cannot be checked
	// against limits due to an error.
	ReasonFailed
	// ReasonMaxStreams is returned when a stream cannot be accepted because
	// the tenant has either reached or exceeded their maximum stream limit.
	ReasonMaxStreams
)

func (r Reason) String() string {
	switch r {
	case ReasonFailed:
		return "failed"
	case ReasonMaxStreams:
		return "max streams"
	default:
		return "unknown reason"
	}
}
