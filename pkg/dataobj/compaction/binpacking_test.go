package planner

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// mockSizer is a simple implementation of Sizer for testing.
type mockSizer struct {
	size int64
	id   string
}

func (m *mockSizer) GetSize() int64 { return m.size }

// Helper to create mock sizers
func newMockSizer(id string, size int64) *mockSizer {
	return &mockSizer{id: id, size: size}
}

// Helper to get total size of bins
func getTotalBinSize[G Sizer](bins []BinPackResult[G]) int64 {
	var total int64
	for _, bin := range bins {
		total += bin.Size
	}
	return total
}

// Helper to get total items across all bins
func getTotalItemCount[G Sizer](bins []BinPackResult[G]) int {
	var total int
	for _, bin := range bins {
		total += len(bin.Groups)
	}
	return total
}

func TestBinPack(t *testing.T) {
	t.Run("empty input returns nil", func(t *testing.T) {
		result := BinPack([]*mockSizer{})
		require.Nil(t, result)
		require.Len(t, result, 0) // 0 bins expected
	})

	t.Run("single small item creates one bin", func(t *testing.T) {
		items := []*mockSizer{
			newMockSizer("a", targetUncompressedSize/2),
		}
		result := BinPack(items)

		require.Len(t, result, 1) // 1 bin expected
		require.Len(t, result[0].Groups, 1)
		require.Equal(t, targetUncompressedSize/2, result[0].Size)
	})

	t.Run("items fitting in one bin are grouped", func(t *testing.T) {
		items := []*mockSizer{
			newMockSizer("a", targetUncompressedSize/4),
			newMockSizer("b", targetUncompressedSize/4),
			newMockSizer("c", targetUncompressedSize/4),
		}
		result := BinPack(items)

		// All items should fit in one bin (75% fill)
		require.Len(t, result, 1) // 1 bin expected
		require.Equal(t, 3, getTotalItemCount(result))
		require.Equal(t, 3*targetUncompressedSize/4, getTotalBinSize(result))
	})

	t.Run("items exceeding target create multiple bins", func(t *testing.T) {
		items := []*mockSizer{
			newMockSizer("a", targetUncompressedSize/2),
			newMockSizer("b", targetUncompressedSize/2),
			newMockSizer("c", targetUncompressedSize/2),
		}
		result := BinPack(items)

		// Total is 1.5x target - first two items (1.0x) go in bin 1, third item (0.5x) in bin 2
		// After mergeUnderfilledBins, bin 2 gets merged in bin 1 since bin 2 is underfilled
		require.Len(t, result, 1) // 1 bin expected (merged)
		totalSize := getTotalBinSize(result)
		require.Equal(t, 3*targetUncompressedSize/2, totalSize)
		require.Equal(t, 3, getTotalItemCount(result))
	})

	t.Run("large item creates its own bin", func(t *testing.T) {
		items := []*mockSizer{
			newMockSizer("large", targetUncompressedSize+1),
			newMockSizer("small", targetUncompressedSize/4),
		}
		result := BinPack(items)

		// Large item needs its own bin, small item can fit in the overflow space
		// (large bin is at 1x + 1 byte, so partial space is ~1x, small item fits)
		require.Len(t, result, 1) // 1 bin expected (small fits in overflow partial space)
		totalSize := getTotalBinSize(result)
		require.Equal(t, targetUncompressedSize+1+targetUncompressedSize/4, totalSize)
		require.Equal(t, 2, getTotalItemCount(result))
	})

	t.Run("sorting by size descending", func(t *testing.T) {
		items := []*mockSizer{
			newMockSizer("small", 100),
			newMockSizer("large", targetUncompressedSize/2),
			newMockSizer("medium", targetUncompressedSize/4),
		}
		result := BinPack(items)

		// All items fit in one bin (75% + 100 bytes)
		require.Len(t, result, 1) // 1 bin expected
		require.Equal(t, 3, getTotalItemCount(result))
	})

	t.Run("preserves all items", func(t *testing.T) {
		items := []*mockSizer{
			newMockSizer("a", targetUncompressedSize/3),
			newMockSizer("b", targetUncompressedSize/3),
			newMockSizer("c", targetUncompressedSize/3),
			newMockSizer("d", targetUncompressedSize/3),
			newMockSizer("e", targetUncompressedSize/3),
		}
		result := BinPack(items)

		// Total is 5/3 = ~1.67x target, fits in 1 bin with upto 2x the target size
		require.Len(t, result, 1)

		// Verify all items are preserved
		require.Equal(t, 5, getTotalItemCount(result))

		// Verify total size is preserved
		var inputTotal int64
		for _, item := range items {
			inputTotal += item.GetSize()
		}
		require.Equal(t, inputTotal, getTotalBinSize(result))
	})

	t.Run("two full bins", func(t *testing.T) {
		items := []*mockSizer{
			newMockSizer("a", targetUncompressedSize),
			newMockSizer("b", targetUncompressedSize),
		}
		result := BinPack(items)

		// Each item is exactly target size, so 2 bins
		require.Len(t, result, 2) // 2 bins expected
		require.Equal(t, 2, getTotalItemCount(result))
		require.Equal(t, 2*targetUncompressedSize, getTotalBinSize(result))
	})

	t.Run("two full bins with many small items", func(t *testing.T) {
		items := []*mockSizer{
			newMockSizer("a", targetUncompressedSize/3),
			newMockSizer("b", targetUncompressedSize/3),
			newMockSizer("c", targetUncompressedSize/3),
			newMockSizer("d", targetUncompressedSize/3),
			newMockSizer("e", targetUncompressedSize/3),
			newMockSizer("f", targetUncompressedSize/3),
		}
		result := BinPack(items)

		// Each item is exactly target size, so 2 bins
		require.Len(t, result, 2) // 2 bins expected
		require.Equal(t, 6, getTotalItemCount(result))
		require.Equal(t, 2*targetUncompressedSize, getTotalBinSize(result))
	})

	t.Run("three bins with different fill levels", func(t *testing.T) {
		// Create items that will result in 3 well-filled bins
		items := []*mockSizer{
			newMockSizer("a", targetUncompressedSize*80/100), // 80%
			newMockSizer("b", targetUncompressedSize*80/100), // 80%
			newMockSizer("c", targetUncompressedSize*80/100), // 80%
		}
		result := BinPack(items)

		// Total is 2.4x target, estimated bin count = 3.
		// Each 80% item can't fit with another (160% > 100%), so each gets its own bin.
		// mergeUnderfilledBins keeps them separate since each bin is >= 70% threshold.
		require.Len(t, result, 3) // 3 bins expected
		require.Equal(t, 3, getTotalItemCount(result))
	})
}

func TestFindBestFitBin(t *testing.T) {
	t.Run("empty bins returns -1", func(t *testing.T) {
		var bins []BinPackResult[*mockSizer]
		result := findBestFitBin(bins, 100, targetUncompressedSize)
		require.Equal(t, -1, result)
	})

	t.Run("item too large for any bin returns -1", func(t *testing.T) {
		bins := []BinPackResult[*mockSizer]{
			{Groups: []*mockSizer{newMockSizer("a", targetUncompressedSize/2)}, Size: targetUncompressedSize / 2},
		}
		// Item that would exceed maxSize
		result := findBestFitBin(bins, targetUncompressedSize, targetUncompressedSize)
		require.Equal(t, -1, result)
	})

	t.Run("finds bin with best fit", func(t *testing.T) {
		bins := []BinPackResult[*mockSizer]{
			{Groups: []*mockSizer{newMockSizer("a", targetUncompressedSize/4)}, Size: targetUncompressedSize / 4},       // 25% full
			{Groups: []*mockSizer{newMockSizer("b", targetUncompressedSize/2)}, Size: targetUncompressedSize / 2},       // 50% full
			{Groups: []*mockSizer{newMockSizer("c", targetUncompressedSize*3/4)}, Size: targetUncompressedSize * 3 / 4}, // 75% full
		}

		// Item of size 20% should best fit in bin at 75% (leaving 5% space)
		result := findBestFitBin(bins, targetUncompressedSize/5, targetUncompressedSize)
		require.Equal(t, 2, result) // bin at index 2 (75% full)
	})

	t.Run("handles bin already at maxSize boundary", func(t *testing.T) {
		bins := []BinPackResult[*mockSizer]{
			{Groups: []*mockSizer{newMockSizer("a", targetUncompressedSize)}, Size: targetUncompressedSize}, // exactly at target
		}

		// Should not fit in a bin that's exactly at maxSize
		result := findBestFitBin(bins, 100, targetUncompressedSize)
		require.Equal(t, -1, result)
	})

	t.Run("handles overflowed bin with partial fill", func(t *testing.T) {
		// Bin is at 1.5x target (has partial object at 0.5x)
		bins := []BinPackResult[*mockSizer]{
			{Groups: []*mockSizer{newMockSizer("a", targetUncompressedSize*3/2)}, Size: targetUncompressedSize * 3 / 2},
		}

		// Small item should fit in the partial space
		result := findBestFitBin(bins, targetUncompressedSize/4, targetUncompressedSize)
		require.Equal(t, 0, result)
	})

	t.Run("skips bin at perfect multiple of target", func(t *testing.T) {
		// Bin is exactly at 2x target (no partial object)
		bins := []BinPackResult[*mockSizer]{
			{Groups: []*mockSizer{newMockSizer("a", targetUncompressedSize*2)}, Size: targetUncompressedSize * 2},
		}

		// Should skip this bin as it's at a perfect boundary
		result := findBestFitBin(bins, 100, targetUncompressedSize)
		require.Equal(t, -1, result)
	})
}

func TestMergeUnderfilledBins(t *testing.T) {
	minDesiredSize := targetUncompressedSize * minFillPercent / 100

	t.Run("single bin unchanged", func(t *testing.T) {
		bins := []BinPackResult[*mockSizer]{
			{Groups: []*mockSizer{newMockSizer("a", minDesiredSize/2)}, Size: minDesiredSize / 2},
		}
		result := mergeUnderfilledBins(bins)

		require.Len(t, result, 1)
		require.Equal(t, minDesiredSize/2, result[0].Size)
	})

	t.Run("well-filled bins unchanged", func(t *testing.T) {
		bins := []BinPackResult[*mockSizer]{
			{Groups: []*mockSizer{newMockSizer("a", minDesiredSize)}, Size: minDesiredSize},
			{Groups: []*mockSizer{newMockSizer("b", minDesiredSize)}, Size: minDesiredSize},
		}
		result := mergeUnderfilledBins(bins)

		require.Len(t, result, 2)
	})

	t.Run("underfilled bins are merged", func(t *testing.T) {
		// Two bins each at 30% (below 70% threshold)
		bins := []BinPackResult[*mockSizer]{
			{Groups: []*mockSizer{newMockSizer("a", minDesiredSize/2)}, Size: minDesiredSize / 2},
			{Groups: []*mockSizer{newMockSizer("b", minDesiredSize/2)}, Size: minDesiredSize / 2},
		}
		result := mergeUnderfilledBins(bins)

		// Should be merged into one bin
		require.Len(t, result, 1)
		require.Equal(t, minDesiredSize, result[0].Size)
		require.Equal(t, 2, len(result[0].Groups))
	})

	t.Run("preserves total size after merge", func(t *testing.T) {
		bins := []BinPackResult[*mockSizer]{
			{Groups: []*mockSizer{newMockSizer("a", minDesiredSize/3)}, Size: minDesiredSize / 3},
			{Groups: []*mockSizer{newMockSizer("b", minDesiredSize/3)}, Size: minDesiredSize / 3},
			{Groups: []*mockSizer{newMockSizer("c", minDesiredSize/3)}, Size: minDesiredSize / 3},
		}

		inputTotal := getTotalBinSize(bins)
		result := mergeUnderfilledBins(bins)
		outputTotal := getTotalBinSize(result)

		require.Equal(t, inputTotal, outputTotal)
	})

	t.Run("preserves all items after merge", func(t *testing.T) {
		bins := []BinPackResult[*mockSizer]{
			{Groups: []*mockSizer{newMockSizer("a", minDesiredSize/4)}, Size: minDesiredSize / 4},
			{Groups: []*mockSizer{newMockSizer("b", minDesiredSize/4)}, Size: minDesiredSize / 4},
			{Groups: []*mockSizer{newMockSizer("c", minDesiredSize/4)}, Size: minDesiredSize / 4},
		}

		inputCount := getTotalItemCount(bins)
		result := mergeUnderfilledBins(bins)
		outputCount := getTotalItemCount(result)

		require.Equal(t, inputCount, outputCount)
	})
}
