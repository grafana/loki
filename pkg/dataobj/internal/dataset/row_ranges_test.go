package dataset

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Test_rowRanges_Add_Start tests for the behaviour of adding a new range which is before any existing ranges.
func Test_rowRanges_Add_Start(t *testing.T) {
	var rr rowRanges

	rr.Add(rowRange{10, 25})
	rr.Add(rowRange{30, 100})
	rr.Add(rowRange{9, 50})

	require.Len(t, rr, 3)

	testNoOverlaps(t, rr)
	testBoundaries(t, rr, rowRange{9, 100})
}

// Test_rowRanges_Add_Expand tests that if a range is included by a smaller
// range, the smaller range is grown to include the larger range.
func Test_rowRanges_Add_Expand(t *testing.T) {
	var rr rowRanges

	rr.Add(rowRange{10, 25})
	rr.Add(rowRange{10, 50})

	require.Len(t, rr, 1)

	testNoOverlaps(t, rr)
	testBoundaries(t, rr, rowRange{10, 50})
}

// Test_rowRanges_Add_Overlapping tests that an overlapping range is merged
// with the existing range.
func Test_rowRanges_Add_Overlapping(t *testing.T) {
	var rr rowRanges

	rr.Add(rowRange{10, 25})
	rr.Add(rowRange{11, 50})

	require.Len(t, rr, 1)

	testNoOverlaps(t, rr)
	testBoundaries(t, rr, rowRange{10, 50})
}

// Test_rowRanges_Add_Subset tests that if a range is already included by a
// larger range, than the slice of included ranges does not change.
func Test_rowRanges_Add_Subset(t *testing.T) {
	var rr rowRanges

	rr.Add(rowRange{10, 20})
	rr.Add(rowRange{15, 17})

	require.Len(t, rr, 1)

	testNoOverlaps(t, rr)
	testBoundaries(t, rr, rowRange{10, 20})
}

// Test_rowRanges_Add_MultipleOverlapping tests the behaviour of adding a range
// which overlaps with multiple existing ranges.
func Test_rowRanges_Add_MultipleOverlapping(t *testing.T) {
	var rr rowRanges

	rr.Add(rowRange{100, 200})
	rr.Add(rowRange{500, 1000})
	rr.Add(rowRange{250, 850}) // Adding this range should cause a split, where ranges get reassigned.

	require.Len(t, rr, 3)

	testNoOverlaps(t, rr)
	testBoundaries(t, rr, rowRange{100, 200}, rowRange{250, 1000})
}

// Test_rowRanges tests that a range is included correctly.
func Test_rowRanges_Includes(t *testing.T) {
	var rr rowRanges

	require.False(t, rr.Includes(100))
	require.False(t, rr.Includes(150))
	require.False(t, rr.Includes(200))

	rr.Add(rowRange{10, 25})
	rr.Add(rowRange{100, 200})

	require.Len(t, rr, 2)

	require.True(t, rr.Includes(100))
	require.True(t, rr.Includes(150))
	require.True(t, rr.Includes(200))
	require.False(t, rr.Includes(201))

	testNoOverlaps(t, rr)
	testBoundaries(t, rr, rowRange{10, 25}, rowRange{100, 200})
}

func Test_rowRanges_IncludesRange_Overlap(t *testing.T) {
	tests := []struct {
		name          string
		ranges        rowRanges
		check         rowRange
		includesRange bool
		overlaps      bool
	}{
		{
			name:          "empty ranges",
			ranges:        rowRanges{},
			check:         rowRange{10, 20},
			includesRange: false,
			overlaps:      false,
		},
		{
			name:          "exact match",
			ranges:        rowRanges{{10, 20}},
			check:         rowRange{10, 20},
			includesRange: true,
			overlaps:      true,
		},
		{
			name:          "subset range",
			ranges:        rowRanges{{10, 30}},
			check:         rowRange{15, 25},
			includesRange: true,
			overlaps:      true,
		},
		{
			name:          "partial overlap start",
			ranges:        rowRanges{{10, 20}},
			check:         rowRange{5, 15},
			includesRange: false,
			overlaps:      true,
		},
		{
			name:          "partial overlap end",
			ranges:        rowRanges{{10, 20}},
			check:         rowRange{15, 25},
			includesRange: false,
			overlaps:      true,
		},
		{
			name:          "no overlap",
			ranges:        rowRanges{{10, 20}},
			check:         rowRange{30, 40},
			includesRange: false,
			overlaps:      false,
		},
		{
			name:          "split across multiple ranges",
			ranges:        rowRanges{{10, 20}, {21, 30}, {31, 40}},
			check:         rowRange{15, 35},
			includesRange: true,
			overlaps:      true,
		},
		{
			name:          "split with gap",
			ranges:        rowRanges{{10, 20}, {30, 40}},
			check:         rowRange{15, 35},
			includesRange: false,
			overlaps:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.includesRange, tt.ranges.IncludesRange(tt.check), "IncludesRange should match expected value")
			require.Equal(t, tt.overlaps, tt.ranges.Overlaps(tt.check), "Overlaps should match expected value")
		})
	}
}

func Test_rowRanges_Next(t *testing.T) {
	tests := []struct {
		name   string
		ranges rowRanges
		input  uint64
		expect uint64
		valid  bool
	}{
		{
			name:   "empty ranges",
			ranges: rowRanges{},
			input:  10,
			expect: 0,
			valid:  false,
		},
		{
			name:   "next in same range",
			ranges: rowRanges{{10, 20}},
			input:  15,
			expect: 16,
			valid:  true,
		},
		{
			name:   "next at range boundary",
			ranges: rowRanges{{10, 20}},
			input:  19,
			expect: 20,
			valid:  true,
		},
		{
			name:   "next beyond range",
			ranges: rowRanges{{10, 20}},
			input:  20,
			expect: 0,
			valid:  false,
		},
		{
			name:   "next jumps to next range",
			ranges: rowRanges{{10, 20}, {30, 40}},
			input:  20,
			expect: 30,
			valid:  true,
		},
		{
			name:   "next in gap between ranges",
			ranges: rowRanges{{10, 20}, {30, 40}},
			input:  25,
			expect: 30,
			valid:  true,
		},
		{
			name:   "next before first range",
			ranges: rowRanges{{10, 20}},
			input:  5,
			expect: 10,
			valid:  true,
		},
		{
			name:   "next at start of range",
			ranges: rowRanges{{10, 20}},
			input:  9,
			expect: 10,
			valid:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row, valid := tt.ranges.Next(tt.input)
			require.Equal(t, tt.valid, valid)
			if valid {
				require.Equal(t, tt.expect, row)
			}
		})
	}
}

// Test_intersectRanges tests the intersection operation between row ranges.
func Test_intersectRanges(t *testing.T) {
	tests := []struct {
		name     string
		initial  rowRanges
		with     rowRanges
		expected rowRanges
	}{
		{
			name:     "no overlap",
			initial:  rowRanges{{10, 20}},
			with:     rowRanges{{30, 40}},
			expected: rowRanges{},
		},
		{
			name:     "single overlap",
			initial:  rowRanges{{10, 30}},
			with:     rowRanges{{20, 40}},
			expected: rowRanges{{20, 30}},
		},
		{
			name:     "multiple ranges one overlap",
			initial:  rowRanges{{10, 20}, {30, 40}},
			with:     rowRanges{{15, 25}},
			expected: rowRanges{{15, 20}},
		},
		{
			name:     "multiple ranges multiple overlaps",
			initial:  rowRanges{{10, 20}, {30, 40}, {50, 60}},
			with:     rowRanges{{15, 35}, {45, 55}},
			expected: rowRanges{{15, 20}, {30, 35}, {50, 55}},
		},
		{
			name:     "exact match",
			initial:  rowRanges{{10, 20}},
			with:     rowRanges{{10, 20}},
			expected: rowRanges{{10, 20}},
		},
		{
			name:     "contained range",
			initial:  rowRanges{{10, 30}},
			with:     rowRanges{{15, 25}},
			expected: rowRanges{{15, 25}},
		},
		{
			name:     "empty initial ranges",
			initial:  rowRanges{},
			with:     rowRanges{{10, 20}},
			expected: rowRanges{},
		},
		{
			name:     "empty with ranges",
			initial:  rowRanges{{10, 20}},
			with:     rowRanges{},
			expected: rowRanges{},
		},
		{
			name:     "multiple ranges with partial overlaps",
			initial:  rowRanges{{10, 30}, {40, 60}, {70, 90}},
			with:     rowRanges{{20, 25}, {26, 27}, {28, 30}, {31, 45}, {55, 75}},
			expected: rowRanges{{20, 30}, {40, 45}, {55, 60}, {70, 75}},
		},
		{
			name:     "adjacent ranges without overlap",
			initial:  rowRanges{{10, 20}},
			with:     rowRanges{{21, 30}},
			expected: rowRanges{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := intersectRanges(nil, tt.initial, tt.with)

			// Verify no overlaps in result
			testNoOverlaps(t, res)

			// Verify boundaries for non-empty results
			if len(tt.expected) > 0 {
				testBoundaries(t, res, tt.expected...)
			}
		})
	}
}

func Test_unionRanges(t *testing.T) {
	tests := []struct {
		name     string
		initial  rowRanges
		other    rowRanges
		expected rowRanges
	}{
		{
			name:     "empty initial ranges",
			initial:  nil,
			other:    rowRanges{{1, 5}, {10, 15}},
			expected: rowRanges{{1, 5}, {10, 15}},
		},
		{
			name:     "empty other ranges",
			initial:  rowRanges{{1, 5}, {10, 15}},
			other:    nil,
			expected: rowRanges{{1, 5}, {10, 15}},
		},
		{
			name:     "non-overlapping ranges",
			initial:  rowRanges{{1, 5}, {20, 25}},
			other:    rowRanges{{10, 15}, {30, 35}},
			expected: rowRanges{{1, 5}, {10, 15}, {20, 25}, {30, 35}},
		},
		{
			name:     "overlapping ranges",
			initial:  rowRanges{{1, 10}, {20, 30}},
			other:    rowRanges{{5, 15}, {25, 35}},
			expected: rowRanges{{1, 15}, {20, 35}},
		},
		{
			name:     "adjacent ranges",
			initial:  rowRanges{{1, 5}, {11, 15}},
			other:    rowRanges{{6, 10}, {16, 20}},
			expected: rowRanges{{1, 20}},
		},
		{
			name:     "completely contained ranges",
			initial:  rowRanges{{1, 20}},
			other:    rowRanges{{5, 10}, {15, 18}},
			expected: rowRanges{{1, 20}},
		},
		{
			name:     "multiple overlapping ranges",
			initial:  rowRanges{{1, 5}, {10, 15}, {20, 25}},
			other:    rowRanges{{3, 12}, {14, 22}},
			expected: rowRanges{{1, 25}},
		},
		{
			name:     "exact same ranges",
			initial:  rowRanges{{1, 5}, {10, 15}},
			other:    rowRanges{{1, 5}, {10, 15}},
			expected: rowRanges{{1, 5}, {10, 15}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := unionRanges(nil, tt.initial, tt.other)

			require.Equal(t, tt.expected, res, "ranges should match expected union")

			// Verify no overlaps in result
			testNoOverlaps(t, res)

			// Verify boundaries for non-empty results
			if len(tt.expected) > 0 {
				for _, r := range tt.expected {
					testBoundaries(t, res, r)
				}
			}
		})
	}
}

// testNoOverlaps asserts that no two ranges in the slice overlap, and that the
// start of each range is greater than the end of the previous range.
func testNoOverlaps(t *testing.T, rr rowRanges) {
	t.Helper()

	for i := 1; i < len(rr); i++ {
		require.Greater(t, rr[i].Start, rr[i-1].End)
	}
}

// testBoundaries checks the boundaries of each range in the tests slice for
// being included. For each range, everything from the start to the end
// (inclusive) should be included, and one value before the start and after the
// end should _not_ be included.
func testBoundaries(t *testing.T, rr rowRanges, tests ...rowRange) {
	t.Helper()

	for _, tc := range tests {
		require.False(t, rr.Includes(tc.Start-1), "expected %d to not be included, but ranges were %v", tc.Start-1, rr)

		// This is slow, but checking every value in the range helps prevent
		// obscure bugs.
		for i := tc.Start; i <= tc.End; i++ {
			require.True(t, rr.Includes(i), "expected %d to be included, but ranges were %v", i, rr)
		}

		require.False(t, rr.Includes(tc.End+1), "expected %d to not be included, but ranges were %v", tc.End+1, rr)
	}
}
