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
	// ReasonNotOwned is returned by CheckLimitsAndShard when the instance that
	// received the stream does not own its partition, so it cannot make a
	// decision (e.g. stale frontend routing during a rebalance). The stream
	// defaults to 1 shard and callers must treat it as "not checked here".
	ReasonNotOwned
)

func (r Reason) String() string {
	switch r {
	case ReasonFailed:
		return "failed"
	case ReasonMaxStreams:
		return "max streams"
	case ReasonNotOwned:
		return "not owned"
	default:
		return "unknown reason"
	}
}
