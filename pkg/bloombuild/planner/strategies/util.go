package strategies

import (
	"fmt"
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

func FindGapsInFingerprintBounds(ownershipRange v1.FingerprintBounds, metas []v1.FingerprintBounds) (gaps []v1.FingerprintBounds, err error) {
	if len(metas) == 0 {
		return []v1.FingerprintBounds{ownershipRange}, nil
	}

	// turn the available metas into a list of non-overlapping metas
	// for easier processing
	var nonOverlapping []v1.FingerprintBounds
	// First, we reduce the metas into a smaller set by combining overlaps. They must be sorted.
	var cur *v1.FingerprintBounds
	for i := 0; i < len(metas); i++ {
		j := i + 1

		// first iteration (i == 0), set the current meta
		if cur == nil {
			cur = &metas[i]
		}

		if j >= len(metas) {
			// We've reached the end of the list. Add the last meta to the non-overlapping set.
			nonOverlapping = append(nonOverlapping, *cur)
			break
		}

		combined := cur.Union(metas[j])
		if len(combined) == 1 {
			// There was an overlap between the two tested ranges. Combine them and keep going.
			cur = &combined[0]
			continue
		}

		// There was no overlap between the two tested ranges. Add the first to the non-overlapping set.
		// and keep the second for the next iteration.
		nonOverlapping = append(nonOverlapping, combined[0])
		cur = &combined[1]
	}

	// Now, detect gaps between the non-overlapping metas and the ownership range.
	// The left bound of the ownership range will be adjusted as we go.
	leftBound := ownershipRange.Min
	for _, meta := range nonOverlapping {

		clippedMeta := meta.Intersection(ownershipRange)
		// should never happen as long as we are only combining metas
		// that intersect with the ownership range
		if clippedMeta == nil {
			return nil, fmt.Errorf("meta is not within ownership range: %v", meta)
		}

		searchRange := ownershipRange.Slice(leftBound, clippedMeta.Max)
		// update the left bound for the next iteration
		// We do the max to prevent the max bound to overflow from MaxUInt64 to 0
		leftBound = min(
			max(clippedMeta.Max+1, clippedMeta.Max),
			max(ownershipRange.Max+1, ownershipRange.Max),
		)

		// since we've already ensured that the meta is within the ownership range,
		// we know the xor will be of length zero (when the meta is equal to the ownership range)
		// or 1 (when the meta is a subset of the ownership range)
		xors := searchRange.Unless(*clippedMeta)
		if len(xors) == 0 {
			// meta is equal to the ownership range. This means the meta
			// covers this entire section of the ownership range.
			continue
		}

		gaps = append(gaps, xors[0])
	}

	// If the leftBound is less than the ownership range max, and it's smaller than MaxUInt64,
	// There is a gap between the last meta and the end of the ownership range.
	// Note: we check `leftBound < math.MaxUint64` since in the loop above we clamp the
	// leftBound to MaxUint64 to prevent an overflow to 0: `max(clippedMeta.Max+1, clippedMeta.Max)`
	if leftBound < math.MaxUint64 && leftBound <= ownershipRange.Max {
		gaps = append(gaps, v1.NewBounds(leftBound, ownershipRange.Max))
	}

	return gaps, nil
}
