package discover

import (
	"time"
)

const (
	// DefaultMaxStreams is the default cap on the number of streams returned by
	// RunDiscovery. Callers can override via DiscoverConfig.MaxStreams.
	DefaultMaxStreams = 250

	// DefaultParallelism is the default number of concurrent series API calls
	// issued by RunDiscovery. Callers can override via DiscoverConfig.Parallelism.
	DefaultParallelism = 5
)

// DiscoverConfig controls assembly and validation of the metadata pipeline.
// Storage discovery, content probing, and other pipeline options are configured
// directly on their respective config types (TSDBStructuralConfig, ProbeConfig).
type DiscoverConfig struct {
	// From is the start of the query time range. When zero-valued, defaults to
	// 24 hours before To (or before now when To is also zero).
	From time.Time

	// To is the end of the query time range. When zero-valued, defaults to the
	// current time.
	To time.Time
}

// effectiveFrom returns the resolved From boundary, defaulting to 24h before
// the effective To when From is zero-valued.
func (c DiscoverConfig) effectiveFrom() time.Time {
	if !c.From.IsZero() {
		return c.From
	}
	return c.effectiveTo().Add(-24 * time.Hour)
}

// effectiveTo returns the resolved To boundary, defaulting to the current time
// when To is zero-valued.
func (c DiscoverConfig) effectiveTo() time.Time {
	if !c.To.IsZero() {
		return c.To
	}
	return time.Now()
}
