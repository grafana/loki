package wirecodec

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDictionary(t *testing.T) {
	t.Run("assigns stable indices", func(t *testing.T) {
		var dict dictionary

		require.Equal(t, uint64(0), dict.add("dataset.array"))
		require.Equal(t, uint64(0), dict.add("dataset.array"))
		require.Equal(t, uint64(1), dict.add("dataset.chunked"))

		require.Equal(t, []string{"dataset.array", "dataset.chunked"}, dict.keys)
	})

	t.Run("loads existing keys", func(t *testing.T) {
		dict := newDictionary([]string{"dataset.array", "dataset.chunked"})

		require.Equal(t, uint64(0), dict.add("dataset.array"))
		require.Equal(t, uint64(1), dict.add("dataset.chunked"))
		first, err := dict.lookup(0)
		require.NoError(t, err)
		second, err := dict.lookup(1)
		require.NoError(t, err)

		require.Equal(t, "dataset.array", first)
		require.Equal(t, "dataset.chunked", second)
	})

	t.Run("rejects out-of-range indices", func(t *testing.T) {
		dict := newDictionary([]string{"dataset.array"})

		_, err := dict.lookup(99)

		require.Error(t, err)
	})
}
