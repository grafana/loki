package distributor

import "time"

// Limits is an interface for distributor limits/related configs
type Limits interface {
	MaxLineSize(userID string) int
	EnforceMetricName(userID string) bool
	MaxLabelNamesPerSeries(userID string) int
	MaxLabelNameLength(userID string) int
	MaxLabelValueLength(userID string) int

	CreationGracePeriod(userID string) time.Duration
	RejectOldSamples(userID string) bool
	RejectOldSamplesMaxAge(userID string) time.Duration
}

// PriorityLimits returns the first non-zero result from a set of []Limits
type PriorityLimits []Limits

func (ls PriorityLimits) MaxLineSize(userID string) (res int) {
	for _, l := range ls {
		if res = l.MaxLineSize(userID); res != 0 {
			return res
		}
	}
	return res
}
