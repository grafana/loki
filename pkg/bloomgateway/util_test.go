package bloomgateway

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSliceIterWithIndex(t *testing.T) {
	t.Run("SliceIterWithIndex implements v1.Iterator interface", func(t *testing.T) {
		xs := []string{"a", "b", "c"}
		it := NewIterWithIndex(0, xs)
		require.True(t, it.Next())
		require.Equal(t, "a", it.At())
		require.True(t, it.Next())
		require.Equal(t, "b", it.At())
		require.True(t, it.Next())
		require.Equal(t, "c", it.At())
		require.False(t, it.Next())
		require.NoError(t, it.Err())
	})
	t.Run("SliceIterWithIndex implements v1.PeekingIterator interface", func(t *testing.T) {
		xs := []string{"a", "b", "c"}
		it := NewIterWithIndex(0, xs)
		p, ok := it.Peek()
		require.True(t, ok)
		require.Equal(t, "a", p)
		require.True(t, it.Next())
		require.Equal(t, "a", it.At())
		require.True(t, it.Next())
		require.True(t, it.Next())
		p, ok = it.Peek()
		require.False(t, ok)
		require.Equal(t, "", p) // "" is zero value for type string
	})
}

func TestGetDay(t *testing.T) {}

func TestGetDayTime(t *testing.T) {}

func TestFilterRequestForDay(t *testing.T) {}
