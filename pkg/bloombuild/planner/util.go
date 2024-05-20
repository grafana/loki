package planner

import (
	"math"

	"github.com/prometheus/common/model"

	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
)

// SplitFingerprintKeyspaceByFactor splits the keyspace covered by model.Fingerprint into contiguous non-overlapping ranges.
func SplitFingerprintKeyspaceByFactor(factor int) []v1.FingerprintBounds {
	if factor <= 0 {
		return nil
	}

	bounds := make([]v1.FingerprintBounds, 0, factor)

	// The keyspace of a Fingerprint is from 0 to max uint64.
	keyspaceSize := uint64(math.MaxUint64)

	// Calculate the size of each range.
	rangeSize := keyspaceSize / uint64(factor)

	for i := 0; i < factor; i++ {
		// Calculate the start and end of the range.
		start := uint64(i) * rangeSize
		end := start + rangeSize - 1

		// For the last range, make sure it ends at the end of the keyspace.
		if i == factor-1 {
			end = keyspaceSize
		}

		// Create a FingerprintBounds for the range and add it to the slice.
		bounds = append(bounds, v1.FingerprintBounds{
			Min: model.Fingerprint(start),
			Max: model.Fingerprint(end),
		})
	}

	return bounds
}
