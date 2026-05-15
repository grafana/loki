package rangeset_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/rangeset"
)

// TestSet_Add_Start tests for the behaviour of adding a new range which is before any existing ranges.
func TestSet_Add_Start(t *testing.T) {
	var set rangeset.Set

	set.Add(rangeset.Range{10, 25})
	set.Add(rangeset.Range{30, 100})
	set.Add(rangeset.Range{9, 50})

	testNoOverlaps(t, set)
	testBoundaries(t, set, rangeset.Range{9, 100})
}

// TestSet_Add_Expand tests that if a range is included by a smaller
// range, the smaller range is grown to include the larger range.
func TestSet_Add_Expand(t *testing.T) {
	var set rangeset.Set

	set.Add(rangeset.Range{10, 25})
	set.Add(rangeset.Range{10, 50})

	testNoOverlaps(t, set)
	testBoundaries(t, set, rangeset.Range{10, 50})
}

// TestSet_Add_Overlapping tests that an overlapping range is merged
// with the existing range.
func TestSet_Add_Overlapping(t *testing.T) {
	var set rangeset.Set

	set.Add(rangeset.Range{10, 25})
	set.Add(rangeset.Range{11, 50})

	testNoOverlaps(t, set)
	testBoundaries(t, set, rangeset.Range{10, 50})
}

// TestSet_Add_Subset tests that if a range is already included by a
// larger range, than the slice of included ranges does not change.
func TestSet_Add_Subset(t *testing.T) {
	var set rangeset.Set

	set.Add(rangeset.Range{10, 20})
	set.Add(rangeset.Range{15, 17})

	testNoOverlaps(t, set)
	testBoundaries(t, set, rangeset.Range{10, 20})
}

// TestSet_Add_MultipleOverlapping tests the behaviour of adding a range
// which overlaps with multiple existing ranges.
func TestSet_Add_MultipleOverlapping(t *testing.T) {
	var set rangeset.Set

	set.Add(rangeset.Range{100, 200})
	set.Add(rangeset.Range{500, 1000})
	set.Add(rangeset.Range{250, 850}) // Adding this range should cause a split, where ranges get reassigned.

	testNoOverlaps(t, set)
	testBoundaries(t, set, rangeset.Range{100, 200}, rangeset.Range{250, 1000})
}

// TestSet_IncludesValue tests that a range is included correctly.
func TestSet_IncludesValue(t *testing.T) {
	var set rangeset.Set

	require.False(t, set.IncludesValue(100))
	require.False(t, set.IncludesValue(150))
	require.False(t, set.IncludesValue(200))

	set.Add(rangeset.Range{10, 25})
	set.Add(rangeset.Range{100, 200})

	require.True(t, set.IncludesValue(100))
	require.True(t, set.IncludesValue(150))
	require.True(t, set.IncludesValue(199))
	require.False(t, set.IncludesValue(200))

	testNoOverlaps(t, set)
	testBoundaries(t, set, rangeset.Range{10, 25}, rangeset.Range{100, 200})
}

func TestSet_IncludesRange_Overlap(t *testing.T) {
	tests := []struct {
		name          string
		ranges        []rangeset.Range
		check         rangeset.Range
		includesRange bool
		overlaps      bool
	}{
		{
			name:          "empty ranges",
			ranges:        nil,
			check:         rangeset.Range{10, 20},
			includesRange: false,
			overlaps:      false,
		},
		{
			name:          "exact match",
			ranges:        []rangeset.Range{{10, 20}},
			check:         rangeset.Range{10, 20},
			includesRange: true,
			overlaps:      true,
		},
		{
			name:          "off by one - start",
			ranges:        []rangeset.Range{{10, 20}},
			check:         rangeset.Range{9, 20},
			includesRange: false,
			overlaps:      true,
		},
		{
			name:          "off by one - end",
			ranges:        []rangeset.Range{{10, 20}},
			check:         rangeset.Range{10, 21},
			includesRange: false,
			overlaps:      true,
		},
		{
			name:          "subset range",
			ranges:        []rangeset.Range{{10, 30}},
			check:         rangeset.Range{15, 25},
			includesRange: true,
			overlaps:      true,
		},
		{
			name:          "partial overlap start",
			ranges:        []rangeset.Range{{10, 20}},
			check:         rangeset.Range{5, 15},
			includesRange: false,
			overlaps:      true,
		},
		{
			name:          "partial overlap end",
			ranges:        []rangeset.Range{{10, 20}},
			check:         rangeset.Range{15, 25},
			includesRange: false,
			overlaps:      true,
		},
		{
			name:          "no overlap",
			ranges:        []rangeset.Range{{10, 20}},
			check:         rangeset.Range{30, 40},
			includesRange: false,
			overlaps:      false,
		},
		{
			name:          "split across multiple ranges",
			ranges:        []rangeset.Range{{10, 21}, {21, 31}, {31, 40}},
			check:         rangeset.Range{15, 35},
			includesRange: true,
			overlaps:      true,
		},
		{
			name:          "split with gap",
			ranges:        []rangeset.Range{{10, 20}, {30, 40}},
			check:         rangeset.Range{15, 35},
			includesRange: false,
			overlaps:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ranges := rangeset.From(tt.ranges...)

			require.Equal(t, tt.includesRange, ranges.IncludesRange(tt.check), "IncludesRange should match expected value")
			require.Equal(t, tt.overlaps, ranges.Overlaps(tt.check), "Overlaps should match expected value")
		})
	}
}

func TestSet_Next(t *testing.T) {
	tests := []struct {
		name   string
		ranges []rangeset.Range
		input  uint64
		expect uint64
		valid  bool
	}{
		{
			name:   "empty ranges",
			ranges: nil,
			input:  10,
			valid:  false,
		},
		{
			name:   "next in same range",
			ranges: []rangeset.Range{{10, 20}},
			input:  15,
			expect: 16,
			valid:  true,
		},
		{
			name:   "next at range boundary",
			ranges: []rangeset.Range{{10, 21}},
			input:  19,
			expect: 20,
			valid:  true,
		},
		{
			name:   "next beyond range",
			ranges: []rangeset.Range{{10, 20}},
			input:  19,
			valid:  false,
		},
		{
			name:   "next jumps to next range",
			ranges: []rangeset.Range{{10, 20}, {30, 40}},
			input:  19,
			expect: 30,
			valid:  true,
		},
		{
			name:   "next in gap between ranges",
			ranges: []rangeset.Range{{10, 20}, {30, 40}},
			input:  25,
			expect: 30,
			valid:  true,
		},
		{
			name:   "next before first range",
			ranges: []rangeset.Range{{10, 20}},
			input:  5,
			expect: 10,
			valid:  true,
		},
		{
			name:   "next at start of range",
			ranges: []rangeset.Range{{10, 20}},
			input:  10,
			expect: 11,
			valid:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ranges := rangeset.From(tt.ranges...)

			row, valid := ranges.Next(tt.input)
			require.Equal(t, tt.valid, valid)
			if valid {
				require.Equal(t, tt.expect, row)
			}
		})
	}
}

// testNoOverlaps asserts that no two ranges in the slice overlap, and that the
// start of each range is greater than the end of the previous range.
func testNoOverlaps(t *testing.T, set rangeset.Set) {
	t.Helper()

	var prev rangeset.Range
	for r := range set.Iter() {
		t.Log(r.String())

		if !prev.IsValid() {
			prev = r
			continue
		}

		require.Greater(t, r.Start, prev.End)
		prev = r
	}
}

// testBoundaries checks the boundaries of each range in the tests slice for
// being included.
func testBoundaries(t *testing.T, set rangeset.Set, tests ...rangeset.Range) {
	t.Helper()

	for _, tc := range tests {
		require.False(t, set.IncludesValue(tc.Start-1), "expected %d to not be included, but ranges were %v", tc.Start-1, set)

		// This is slow, but checking every value in the range helps prevent
		// obscure bugs.
		for i := tc.Start; i < tc.End; i++ {
			require.True(t, set.IncludesValue(i), "expected %d to be included, but ranges were %v", i, set)
		}

		require.False(t, set.IncludesValue(tc.End), "expected %d to not be included, but ranges were %v", tc.End, set)
	}
}
