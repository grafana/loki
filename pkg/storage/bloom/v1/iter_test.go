package v1

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
