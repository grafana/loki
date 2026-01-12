package parquet

import (
	"slices"

	"github.com/parquet-go/parquet-go/encoding"
)

// computeUnencodedByteArraySize calculates the unencoded size of byte array data
// in a page, excluding 4-byte length prefixes.
func computeUnencodedByteArraySize(page Page) int64 {
	values := page.Data()
	if values.Kind() != encoding.ByteArray {
		return 0
	}
	return values.Size()
}

// accumulateLevelHistogram adds level counts from levels to histogram.
func accumulateLevelHistogram(histogram []int64, levels []byte) {
	for _, level := range levels {
		histogram[level]++
	}
}

// appendPageLevelHistogram creates a per-page histogram and appends it to histograms.
// Returns the updated histogram slice with (maxLevel + 1) new elements appended.
func appendPageLevelHistogram(histograms []int64, levels []byte, maxLevel byte) []int64 {
	histSize := int(maxLevel) + 1
	startIndex := len(histograms)
	histograms = slices.Grow(histograms, histSize)[:startIndex+histSize]

	for i := range histSize {
		histograms[startIndex+i] = 0
	}

	for _, level := range levels {
		histograms[startIndex+int(level)]++
	}

	return histograms
}
