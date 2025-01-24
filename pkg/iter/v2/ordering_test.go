package v2

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOrdering(t *testing.T) {
	mkOrderable := func(x Iterator[int]) Iterator[OrderedImpl[int]] {
		return NewMapIter(x, func(i int) OrderedImpl[int] {
			return NewOrderable(i, func(a, b int) Ord {
				if a < b {
					return Less
				} else if a > b {
					return Greater
				}
				return Eq
			})
		})
	}

	for _, tc := range []struct {
		desc     string
		inputs   []int
		unless   []int
		expected []int
	}{
		{
			desc: "unless empty",
			inputs: []int{
				1, 2, 3,
			},
			unless: nil,
			expected: []int{
				1, 2, 3,
			},
		},
		{
			desc:   "src empty",
			inputs: nil,
			unless: []int{
				1, 2, 3,
			},
			expected: nil,
		},
		{
			desc:     "src and unless empty",
			inputs:   nil,
			unless:   nil,
			expected: nil,
		},
		{
			desc:     "inputs overlap_right unless",
			inputs:   []int{3, 4, 5, 6},
			unless:   []int{1, 2, 3, 4},
			expected: []int{5, 6},
		},
		{
			desc:     "inputs overlap_left unless",
			inputs:   []int{1, 2, 3, 4},
			unless:   []int{3, 4, 5, 6},
			expected: []int{1, 2},
		},
		{
			desc:     "interspersed src superset",
			inputs:   []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
			unless:   []int{2, 4, 6, 8, 10},
			expected: []int{1, 3, 5, 7, 9},
		},
		{
			desc:     "interspersed unless superset",
			inputs:   []int{2, 4, 6, 8, 10},
			unless:   []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
			expected: nil,
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			as := NewSliceIter(tc.inputs)
			bs := NewSliceIter(tc.unless)

			unless := NewUnlessIterator(mkOrderable(as), mkOrderable(bs))
			unmap := NewMapIter[OrderedImpl[int], int](unless, func(o OrderedImpl[int]) int {
				return o.Unwrap()
			})

			EqualIterators[int](t, func(_, _ int) {}, NewSliceIter(tc.expected), unmap)
		})
	}
}

func EqualIterators[T any](t *testing.T, test func(a, b T), expected, actual Iterator[T]) {
	for expected.Next() {
		require.True(t, actual.Next())
		a, b := expected.At(), actual.At()
		test(a, b)
	}
	require.False(t, actual.Next())
	require.Nil(t, expected.Err())
	require.Nil(t, actual.Err())
}
