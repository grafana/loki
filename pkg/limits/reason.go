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
	// ReasonStreamShardsCapped is returned by CheckLimitsAndShard when a stream's
	// shard count was granted, but capped below the rate-justified ideal
	// because there was not enough room left in the tenant's stream-count
	// budget. The stream is still accepted.
	ReasonStreamShardsCapped
)

func (r Reason) String() string {
	switch r {
	case ReasonFailed:
		return "failed"
	case ReasonMaxStreams:
		return "max streams"
	case ReasonStreamShardsCapped:
		return "shards capped"
	default:
		return "unknown reason"
	}
}
