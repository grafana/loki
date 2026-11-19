package columnar_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
)

func TestAsArray(t *testing.T) {
	t.Run("returns the same array when the datum is an array", func(t *testing.T) {
		arr := columnar.NewNumber([]int64{1, 2, 3}, memory.Bitmap{})

		got, err := columnar.AsArray(arr)
		require.NoError(t, err)
		require.Same(t, arr, got)
	})

	t.Run("returns an error when the datum is a scalar", func(t *testing.T) {
		_, err := columnar.AsArray(&columnar.NumberScalar[int64]{Value: 7})
		require.EqualError(t, err, "AsArray: *columnar.NumberScalar[int64] is not an array")
	})

	t.Run("returns an error when the datum is nil", func(t *testing.T) {
		_, err := columnar.AsArray(nil)
		require.EqualError(t, err, "AsArray: <nil> is not an array")
	})
}
