package planner

import "sort"

// Sizer is an interface for items that have a size.
// Used by generic bin-packing algorithm.
type Sizer interface {
	GetSize() int64
}

// BinPackResult represents a bin containing groups and their total size.
type BinPackResult[G Sizer] struct {
	Groups []G
	Size   int64
}

// BinPack performs best-fit decreasing bin packing with overflow and merging.
//
// Algorithm:
//  1. Sort items by size (largest first) for better packing
//  2. For each item, find the best-fit bin (smallest remaining capacity that fits)
//  3. If no bin fits within targetUncompressedSize, create a new bin if we are below the estimated bin count
//  4. If no bin fits within targetUncompressedSize and we have reached the estimated bin count, try overflowing bins with upto targetUncompressedSize * maxOutputMultiple
//  5. If still no fit, create a new bin
//  6. Merge under-filled bins (below minFillPercent) in a post-processing step
func BinPack[G Sizer](groups []G) []BinPackResult[G] {
	if len(groups) == 0 {
		return nil
	}

	// Sort by size descending for better packing
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].GetSize() > groups[j].GetSize()
	})

	var totalUncompressedSize int64
	for _, group := range groups {
		totalUncompressedSize += group.GetSize()
	}
	estimatedBinCount := int((totalUncompressedSize + targetUncompressedSize - 1) / targetUncompressedSize)
	if estimatedBinCount < 1 {
		estimatedBinCount = 1
	}

	maxOverflowSize := targetUncompressedSize * maxOutputMultiple
	var bins []BinPackResult[G]

	for _, group := range groups {
		groupSize := group.GetSize()

		// Find best-fit bin within target size
		bestIdx := findBestFitBin(bins, groupSize, targetUncompressedSize)

		if bestIdx >= 0 {
			bins[bestIdx].Groups = append(bins[bestIdx].Groups, group)
			bins[bestIdx].Size += groupSize
			continue
		}

		// No bin has capacity within target size
		if len(bins) < estimatedBinCount {
			// Create a new bin since we haven't reached the estimated bin count
			bins = append(bins, BinPackResult[G]{
				Groups: []G{group},
				Size:   groupSize,
			})
			continue
		}

		// No bin has capacity within target size, and we have already created the estimated number of bins, try overflowing the bins
		bestIdx = findBestFitBin(bins, groupSize, maxOverflowSize)
		if bestIdx >= 0 {
			bins[bestIdx].Groups = append(bins[bestIdx].Groups, group)
			bins[bestIdx].Size += groupSize
			continue
		}

		// None of the existing bins has capacity even after overflow, create a new bin
		bins = append(bins, BinPackResult[G]{
			Groups: []G{group},
			Size:   groupSize,
		})
	}

	// Post-processing: merge under-filled bins
	return mergeUnderfilledBins(bins)
}

// findBestFitBin finds the bin with smallest remaining capacity that can fit the given size.
// Returns -1 if no bin can fit the item within maxSize.
func findBestFitBin[G Sizer](bins []BinPackResult[G], itemSize, maxSize int64) int {
	bestIdx := -1
	bestRemaining := maxSize + 1

	for i, bin := range bins {
		if bin.Size > maxSize {
			// Bin already exceeds maxSize and will be split into multiple output objects.
			// Try to fill the partially filled object up to the next targetUncompressedSize boundary.

			// Skip if already at a perfect multiple (output objects are fully filled).
			partialSize := bin.Size % targetUncompressedSize
			if partialSize == 0 {
				continue
			}

			extraSpace := targetUncompressedSize - partialSize
			remaining := extraSpace - itemSize

			// Only consider adding this item to the partially filled object if it does not exceed the remaining space.
			if remaining < 0 {
				continue
			}

			if remaining < bestRemaining {
				bestRemaining = remaining
				bestIdx = i
			}
		} else {
			newSize := bin.Size + itemSize
			if newSize <= maxSize {
				remaining := maxSize - newSize
				if remaining < bestRemaining {
					bestRemaining = remaining
					bestIdx = i
				}
			}
		}
	}

	return bestIdx
}

// mergeUnderfilledBins merges bins that are below the minimum fill threshold.
// Objects below 70% of target size are merged with other objects. This allows
// the builder to create better-filled actual objects.
//
// Example: Let us assume our target size is 1GB. Given 3 objects of 600MB each:
//   - Without merging underfilled bins:
//   - We will end up with 2 bins, one with two 600MB objects and another with one 600MB object.
//   - No matter how much we try, the output objects from first bin would be overfilled or underfilled.
//   - The output object from second bin would be below the 70% of target size.
//   - With merging underfilled bins:
//   - We will end up with a single bin with three 600MB objects to merge.
//   - We will then create 2 output objects from the single bin, each above the 70% of target size.
func mergeUnderfilledBins[G Sizer](bins []BinPackResult[G]) []BinPackResult[G] {
	if len(bins) <= 1 {
		return bins
	}

	minDesiredSize := targetUncompressedSize * minFillPercent / 100
	maxAllowedSize := targetUncompressedSize * maxOutputMultiple

	// Sort by size (smallest first) to process under-filled bins first
	sort.Slice(bins, func(i, j int) bool {
		return bins[i].Size < bins[j].Size
	})

	// Use write index to compact the slice as we merge
	writeIdx := 0
	for readIdx := 0; readIdx < len(bins); readIdx++ {
		bin := &bins[readIdx]

		// If this bin is well-filled, keep it as is
		if bin.Size >= minDesiredSize {
			bins[writeIdx] = *bin
			writeIdx++
			continue
		}

		// Try to merge with another bin (look for best fit)
		bestMergeIdx := -1
		bestFillRemainder := int64(0)

		for i := readIdx + 1; i < len(bins); i++ {
			candidate := &bins[i]
			combinedSize := bin.Size + candidate.Size

			// Only merge if combined size is within max allowed
			if combinedSize <= maxAllowedSize {
				// Calculate how well-filled the last object would be after builder splits
				remainder := combinedSize % targetUncompressedSize
				if remainder == 0 {
					remainder = targetUncompressedSize // Perfect fit = 100%
				}

				// Prefer merges that result in better fill ratio
				if remainder > bestFillRemainder {
					bestMergeIdx = i
					bestFillRemainder = remainder
				}
			}
		}

		if bestMergeIdx >= 0 {
			// Merge the bins
			candidate := &bins[bestMergeIdx]
			bin.Groups = append(bin.Groups, candidate.Groups...)
			bin.Size += candidate.Size

			// Remove the merged candidate
			bins[bestMergeIdx] = bins[len(bins)-1]
			bins = bins[:len(bins)-1]

			// Re-sort remaining unprocessed bins
			if bestMergeIdx < len(bins) {
				remaining := bins[readIdx+1:]
				sort.Slice(remaining, func(i, j int) bool {
					return remaining[i].Size < remaining[j].Size
				})
			}

			// Check if merged bin is still under-filled
			if bin.Size < minDesiredSize {
				readIdx-- // Process this bin again
				continue
			}
		}

		bins[writeIdx] = *bin
		writeIdx++
	}

	return bins[:writeIdx]
}
