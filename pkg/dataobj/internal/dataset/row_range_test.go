package dataset

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_rowRange_Contains(t *testing.T) {
	tt := []struct {
		name     string
		rowRange rowRange
		row      uint64
		expect   bool
	}{
		{
			name:     "row is inside range",
			rowRange: rowRange{Start: 1, End: 10},
			row:      5,
			expect:   true,
		},
		{
			name:     "row is outside range",
			rowRange: rowRange{Start: 1, End: 10},
			row:      15,
			expect:   false,
		},
		{
			name:     "row is at start of range",
			rowRange: rowRange{Start: 1, End: 10},
			row:      1,
			expect:   true,
		},
		{
			name:     "row is at end of range",
			rowRange: rowRange{Start: 1, End: 10},
			row:      10,
			expect:   true,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			actual := tc.rowRange.Contains(tc.row)
			require.Equal(t, tc.expect, actual, "unexpected contains result for rowRange %s and row %d", tc.rowRange, tc.row)
		})
	}
}

func Test_rowRange_ContainsRange(t *testing.T) {
	tt := []struct {
		name   string
		rangeA rowRange
		rangeB rowRange
		expect bool
	}{
		{
			name:   "rangeB is completely inside rangeA",
			rangeA: rowRange{Start: 1, End: 10},
			rangeB: rowRange{Start: 5, End: 7},
			expect: true,
		},
		{
			name:   "rangeB is first half of rangeA",
			rangeA: rowRange{Start: 1, End: 10},
			rangeB: rowRange{Start: 1, End: 5},
			expect: true,
		},
		{
			name:   "rangeB is second half of rangeA",
			rangeA: rowRange{Start: 1, End: 10},
			rangeB: rowRange{Start: 5, End: 10},
			expect: true,
		},
		{
			name:   "rangeB is outside rangeA",
			rangeA: rowRange{Start: 1, End: 10},
			rangeB: rowRange{Start: 15, End: 20},
			expect: false,
		},
		{
			name:   "rangeB partially overlaps start of rangeA",
			rangeA: rowRange{Start: 100, End: 200},
			rangeB: rowRange{Start: 50, End: 150},
			expect: false,
		},
		{
			name:   "rangeB partially overlaps end of rangeA",
			rangeA: rowRange{Start: 100, End: 200},
			rangeB: rowRange{Start: 150, End: 250},
			expect: false,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			actual := tc.rangeA.ContainsRange(tc.rangeB)
			require.Equal(t, tc.expect, actual, "unexpected contains range result for rangeA %s and rangeB %s", tc.rangeA, tc.rangeB)
		})
	}
}

func Test_rowRange_Overlaps(t *testing.T) {
	tt := []struct {
		name   string
		rangeA rowRange
		rangeB rowRange
		expect bool
	}{
		{
			name:   "rangeB is completely inside rangeA",
			rangeA: rowRange{Start: 1, End: 10},
			rangeB: rowRange{Start: 5, End: 7},
			expect: true,
		},
		{
			name:   "rangeB is first half of rangeA",
			rangeA: rowRange{Start: 1, End: 10},
			rangeB: rowRange{Start: 1, End: 5},
			expect: true,
		},
		{
			name:   "rangeB is second half of rangeA",
			rangeA: rowRange{Start: 1, End: 10},
			rangeB: rowRange{Start: 5, End: 10},
			expect: true,
		},
		{
			name:   "rangeB is outside rangeA",
			rangeA: rowRange{Start: 1, End: 10},
			rangeB: rowRange{Start: 15, End: 20},
			expect: false,
		},
		{
			name:   "rangeB partially overlaps start of rangeA",
			rangeA: rowRange{Start: 100, End: 200},
			rangeB: rowRange{Start: 50, End: 150},
			expect: true,
		},
		{
			name:   "rangeB partially overlaps end of rangeA",
			rangeA: rowRange{Start: 100, End: 200},
			rangeB: rowRange{Start: 150, End: 250},
			expect: true,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			actual := tc.rangeA.Overlaps(tc.rangeB)
			require.Equal(t, tc.expect, actual, "unexpected overlaps result for rangeA %s and rangeB %s", tc.rangeA, tc.rangeB)
		})
	}
}
