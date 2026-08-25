package rangeset_test

import (
	"testing"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/rangeset"
)

func TestIntersect(t *testing.T) {
	tests := []struct {
		name     string
		initial  []rangeset.Range
		with     []rangeset.Range
		expected []rangeset.Range
	}{
		{
			name:     "no overlap",
			initial:  []rangeset.Range{{10, 20}},
			with:     []rangeset.Range{{30, 40}},
			expected: []rangeset.Range{},
		},
		{
			name:     "single overlap",
			initial:  []rangeset.Range{{10, 30}},
			with:     []rangeset.Range{{20, 40}},
			expected: []rangeset.Range{{20, 30}},
		},
		{
			name:     "multiple ranges one overlap",
			initial:  []rangeset.Range{{10, 20}, {30, 40}},
			with:     []rangeset.Range{{15, 25}},
			expected: []rangeset.Range{{15, 20}},
		},
		{
			name:     "multiple ranges multiple overlaps",
			initial:  []rangeset.Range{{10, 20}, {30, 40}, {50, 60}},
			with:     []rangeset.Range{{15, 35}, {45, 55}},
			expected: []rangeset.Range{{15, 20}, {30, 35}, {50, 55}},
		},
		{
			name:     "exact match",
			initial:  []rangeset.Range{{10, 20}},
			with:     []rangeset.Range{{10, 20}},
			expected: []rangeset.Range{{10, 20}},
		},
		{
			name:     "contained range",
			initial:  []rangeset.Range{{10, 30}},
			with:     []rangeset.Range{{15, 25}},
			expected: []rangeset.Range{{15, 25}},
		},
		{
			name:     "empty initial ranges",
			initial:  nil,
			with:     []rangeset.Range{{10, 20}},
			expected: []rangeset.Range{},
		},
		{
			name:     "empty with ranges",
			initial:  []rangeset.Range{{10, 20}},
			with:     nil,
			expected: []rangeset.Range{},
		},
		{
			name:     "multiple ranges with partial overlaps",
			initial:  []rangeset.Range{{10, 30}, {40, 60}, {70, 90}},
			with:     []rangeset.Range{{20, 26}, {26, 28}, {28, 31}, {31, 45}, {55, 75}},
			expected: []rangeset.Range{{20, 30}, {40, 45}, {55, 60}, {70, 75}},
		},
		{
			name:     "adjacent ranges without overlap",
			initial:  []rangeset.Range{{10, 20}},
			with:     []rangeset.Range{{21, 30}},
			expected: []rangeset.Range{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := rangeset.Intersect(
				rangeset.From(tt.initial...),
				rangeset.From(tt.with...),
			)

			testNoOverlaps(t, res)
			testBoundaries(t, res, tt.expected...)
		})
	}
}
