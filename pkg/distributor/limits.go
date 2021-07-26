package distributor

import "time"

// Limits is an interface for distributor limits/related configs
type Limits interface {
	MaxLineSizeShouldTruncate(userID string) bool
	MaxLineSize(userID string) int
	EnforceMetricName(userID string) bool
	MaxLabelNamesPerSeries(userID string) int
	MaxLabelNameLength(userID string) int
	MaxLabelValueLength(userID string) int

	CreationGracePeriod(userID string) time.Duration
	RejectOldSamples(userID string) bool
	RejectOldSamplesMaxAge(userID string) time.Duration
}
