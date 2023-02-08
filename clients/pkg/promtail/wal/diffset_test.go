package wal

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestDiffset(t *testing.T) {
	type testCase struct {
		A, B     diffset
		Expected diffset
	}
	testCases := map[string]testCase{
		"difference between emtpy and something should be empty": {
			A:        diffset{},
			B:        newDiffset([]int{1, 2, 3}),
			Expected: diffset{},
		},
		"difference with non empty sets": {
			A:        newDiffset([]int{1, 2, 3, 4, 5}),
			B:        newDiffset([]int{3, 4, 5, 7, 8}),
			Expected: newDiffset([]int{1, 2}),
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			diff := tc.A.Difference(tc.B)
			require.Equal(t, tc.Expected, diff, "expected other difference result")
		})
	}
}
