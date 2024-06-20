package v2

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSliceIterWithIndex(t *testing.T) {
	t.Parallel()
	t.Run("SliceIterWithIndex implements PeekingIterator interface", func(t *testing.T) {
		xs := []string{"a", "b", "c"}
		it := NewSliceIterWithIndex(xs, 123)

		// peek at first item
		p, ok := it.Peek()
		require.True(t, ok)
		require.Equal(t, "a", p.val)
		require.Equal(t, 123, p.idx)

		// proceed to first item
		require.True(t, it.Next())
		require.Equal(t, "a", it.At().val)
		require.Equal(t, 123, it.At().idx)

		// proceed to second and third item
		require.True(t, it.Next())
		require.True(t, it.Next())

		// peek at non-existing fourth item
		p, ok = it.Peek()
		require.False(t, ok)
		require.Equal(t, "", p.val) // "" is zero value for type string
		require.Equal(t, 123, p.idx)
	})
}

func TestPeekingIterator(t *testing.T) {
	t.Parallel()
	data := []int{1, 2, 3, 4, 5}
	itr := NewPeekingIter[int](NewSliceIter[int](data))

	for i := 0; i < len(data)*2; i++ {
		if i%2 == 0 {
			peek, ok := itr.Peek()
			require.True(t, ok, "iter %d", i)
			require.Equal(t, data[i/2], peek)
		} else {
			require.True(t, itr.Next())
			require.Equal(t, data[i/2], itr.At())
		}
	}
	_, ok := itr.Peek()
	require.False(t, ok, "final iteration")
	require.False(t, itr.Next())

}

func TestCounterIter(t *testing.T) {
	t.Parallel()

	data := []int{1, 2, 3, 4, 5}
	itr := NewCounterIter[int](NewSliceIter[int](data))
	peekItr := NewPeekingIter[int](itr)

	// Consume the outer iter and use peek
	for {
		if _, ok := peekItr.Peek(); !ok {
			break
		}
		if !peekItr.Next() {
			break
		}
	}
	// Both iterators should be exhausted
	require.False(t, itr.Next())
	require.Nil(t, itr.Err())
	require.False(t, peekItr.Next())
	require.Nil(t, peekItr.Err())

	// Assert that the count is correct and peeking hasn't jeopardized the count
	require.Equal(t, len(data), itr.Count())
}

func TestSliceIterRemaining(t *testing.T) {
	ln := 5
	itr := NewSliceIter(make([]int, ln))

	for i := 0; i < ln; i++ {
		require.Equal(t, ln-i, itr.Remaining())
		require.True(t, itr.Next())
		require.Equal(t, ln-i-1, itr.Remaining())
	}

	require.False(t, itr.Next())
}
