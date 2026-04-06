package tsdb

import (
	"time"

	bench "github.com/grafana/loki/v3/pkg/logql/bench"
)

// rangeBuckets are probed smallest-to-largest. For each bucket d, we query the
// window [to-d, to] to determine whether enough entries exist. Anchoring at
// the END of the full time range ensures we sample the most recent data.
var rangeBuckets = []time.Duration{
	5 * time.Minute,
	10 * time.Minute,
	15 * time.Minute,
	30 * time.Minute,
	1 * time.Hour,
}

// rangeEntryThreshold is the minimum number of entries required in a bucket
// for that bucket to qualify as min_range (used for range queries).
const rangeEntryThreshold = 5

// instantEntryThreshold is the minimum number of entries required in a bucket
// for that bucket to qualify as min_instant_range (used for instant queries).
const instantEntryThreshold = 10

// RangeResult holds the output of a completed range-metadata derivation.
type RangeResult struct {
	// MetadataBySelector maps each canonical selector to its probed range
	// durations. Only selectors that met at least one threshold are present.
	MetadataBySelector map[string]*bench.SerializableStreamMetadata

	// Warnings accumulates non-fatal issues encountered during probing.
	Warnings []string

	// TotalProbed is the number of streams probed.
	TotalProbed int

	// TotalSkipped is the number of streams where no bucket met any threshold.
	TotalSkipped int
}
