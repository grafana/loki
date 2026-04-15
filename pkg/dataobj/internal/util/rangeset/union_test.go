package rangeset_test

import (
	"testing"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/util/rangeset"
)

func Test_unionRanges(t *testing.T) {
	tests := []struct {
		name     string
		initial  []rangeset.Range
		other    []rangeset.Range
		expected []rangeset.Range
	}{
		{
			name:     "empty initial ranges",
			initial:  nil,
			other:    []rangeset.Range{{1, 5}, {10, 15}},
			expected: []rangeset.Range{{1, 5}, {10, 15}},
		},
		{
			name:     "empty other ranges",
			initial:  []rangeset.Range{{1, 5}, {10, 15}},
			other:    nil,
			expected: []rangeset.Range{{1, 5}, {10, 15}},
		},
		{
			name:     "non-overlapping ranges",
			initial:  []rangeset.Range{{1, 5}, {20, 25}},
			other:    []rangeset.Range{{10, 15}, {30, 35}},
			expected: []rangeset.Range{{1, 5}, {10, 15}, {20, 25}, {30, 35}},
		},
		{
			name:     "overlapping ranges",
			initial:  []rangeset.Range{{1, 10}, {20, 30}},
			other:    []rangeset.Range{{5, 15}, {25, 35}},
			expected: []rangeset.Range{{1, 15}, {20, 35}},
		},
		{
			name:     "adjacent ranges",
			initial:  []rangeset.Range{{1, 6}, {11, 15}},
			other:    []rangeset.Range{{6, 11}, {15, 20}},
			expected: []rangeset.Range{{1, 20}},
		},
		{
			name:     "completely contained ranges",
			initial:  []rangeset.Range{{1, 20}},
			other:    []rangeset.Range{{5, 10}, {15, 18}},
			expected: []rangeset.Range{{1, 20}},
		},
		{
			name:     "multiple overlapping ranges",
			initial:  []rangeset.Range{{1, 5}, {10, 15}, {20, 25}},
			other:    []rangeset.Range{{3, 12}, {14, 22}},
			expected: []rangeset.Range{{1, 25}},
		},
		{
			name:     "exact same ranges",
			initial:  []rangeset.Range{{1, 5}, {10, 15}},
			other:    []rangeset.Range{{1, 5}, {10, 15}},
			expected: []rangeset.Range{{1, 5}, {10, 15}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := rangeset.Union(
				rangeset.From(tt.initial...),
				rangeset.From(tt.other...),
			)

			testNoOverlaps(t, res)
			testBoundaries(t, res, tt.expected...)
		})
	}
}
