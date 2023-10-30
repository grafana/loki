package v1

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPeekingIterator(t *testing.T) {
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
