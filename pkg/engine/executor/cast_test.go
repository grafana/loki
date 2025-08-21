package executor

import (
	"math"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/require"
)

func TestCastStringToInt64(t *testing.T) {
	t.Run("cast string column to int64", func(t *testing.T) {
		// Create a string column with numeric values and one non-numeric value
		mem := memory.NewGoAllocator()
		builder := array.NewStringBuilder(mem)
		defer builder.Release()

		// Input: ["100", "200", "abc"]
		builder.Append("100")
		builder.Append("200")
		builder.Append("abc")

		stringCol := builder.NewArray()
		defer stringCol.Release()

		// Cast to int64
		int64Col, err := CastStringToInt64(stringCol, mem)
		require.NoError(t, err)
		defer int64Col.Release()

		// Assert the result is an int64 array
		int64Array, ok := int64Col.(*array.Int64)
		require.True(t, ok, "Expected int64 array")

		// Assert: [100, 200, NULL]
		require.Equal(t, 3, int64Array.Len())

		// First value: 100
		require.True(t, int64Array.IsValid(0))
		require.Equal(t, int64(100), int64Array.Value(0))

		// Second value: 200
		require.True(t, int64Array.IsValid(1))
		require.Equal(t, int64(200), int64Array.Value(1))

		// Third value: NULL (because "abc" cannot be parsed as int64)
		require.False(t, int64Array.IsValid(2), "Third value should be NULL")
	})
}

func TestCastStringToFloat64(t *testing.T) {
	t.Run("cast string column to float64", func(t *testing.T) {
		// Create a string column with float values, integer values, and non-numeric value
		mem := memory.NewGoAllocator()
		builder := array.NewStringBuilder(mem)
		defer builder.Release()

		// Input: ["1.5", "2.7", "100", "NaN", "invalid"]
		builder.Append("1.5")
		builder.Append("2.7")
		builder.Append("100")
		builder.Append("NaN")
		builder.Append("invalid")

		stringCol := builder.NewArray()
		defer stringCol.Release()

		// Cast to float64
		float64Col, err := CastStringToFloat64(stringCol, mem)
		require.NoError(t, err)
		defer float64Col.Release()

		// Assert the result is a float64 array
		float64Array, ok := float64Col.(*array.Float64)
		require.True(t, ok, "Expected float64 array")

		// Assert: [1.5, 2.7, 100.0, NaN, NULL]
		require.Equal(t, 5, float64Array.Len())

		// First value: 1.5
		require.True(t, float64Array.IsValid(0))
		require.Equal(t, 1.5, float64Array.Value(0))

		// Second value: 2.7
		require.True(t, float64Array.IsValid(1))
		require.Equal(t, 2.7, float64Array.Value(1))

		// Third value: 100.0 (integer parsed as float)
		require.True(t, float64Array.IsValid(2))
		require.Equal(t, 100.0, float64Array.Value(2))

		// Fourth value: NaN (special float value)
		require.True(t, float64Array.IsValid(3))
		require.True(t, math.IsNaN(float64Array.Value(3)), "Fourth value should be NaN")

		// Fifth value: NULL (because "invalid" cannot be parsed as float)
		require.False(t, float64Array.IsValid(4), "Fifth value should be NULL")
	})
}
