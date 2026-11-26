package dataset

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/memory"
)

func TestScalarOf(t *testing.T) {
	t.Run("returns a scalar of the physical type of the value", func(t *testing.T) {
		require.Equal(t, &columnar.NumberScalar[int64]{Value: 7}, scalarOf(Int64Value(7)))
		require.Equal(t, &columnar.NumberScalar[uint64]{Value: 7}, scalarOf(Uint64Value(7)))
		require.Equal(t, &columnar.UTF8Scalar{Value: []byte("a")}, scalarOf(BinaryValue([]byte("a"))))
	})

	t.Run("returns an empty binary scalar for an empty binary value", func(t *testing.T) {
		scalar, ok := scalarOf(BinaryValue(nil)).(*columnar.UTF8Scalar)
		require.True(t, ok)
		require.Empty(t, scalar.Value)
	})

	t.Run("returns nil for a null value", func(t *testing.T) {
		require.Nil(t, scalarOf(Value{}))
	})

	t.Run("panics on a physical type that it does not know", func(t *testing.T) {
		require.PanicsWithValue(t, "dataset.scalarOf: unsupported physical type 99", func() {
			scalarOf(Value{kind: datasetmd.PhysicalType(99)})
		})
	})
}

func TestValueAt(t *testing.T) {
	var alloc memory.Allocator

	t.Run("returns the value at an index of each array type", func(t *testing.T) {
		ints := columnar.NewNumber([]int64{3, 4}, memory.Bitmap{})
		uints := columnar.NewNumber([]uint64{5, 6}, memory.Bitmap{})
		strings := columnar.NewUTF8([]byte("abcd"), []int32{0, 1, 4}, memory.Bitmap{})

		require.Equal(t, Int64Value(4), valueAt(ints, 1))
		require.Equal(t, Uint64Value(5), valueAt(uints, 0))
		require.Equal(t, BinaryValue([]byte("bcd")), valueAt(strings, 1))
	})

	t.Run("returns the nil value for a null element", func(t *testing.T) {
		validity := memory.NewBitmap(&alloc, 2)
		validity.AppendValues(true, false)

		isNil := func(v Value) bool { return v.IsNil() }
		require.True(t, isNil(valueAt(columnar.NewNumber([]int64{3, 0}, validity), 1)))
		require.True(t, isNil(valueAt(columnar.NewUTF8([]byte("a"), []int32{0, 1, 1}, validity), 1)))

		allNull := memory.NewBitmap(&alloc, 1)
		allNull.AppendValues(false)
		require.True(t, isNil(valueAt(columnar.NewNull(allNull), 0)))
	})

	t.Run("shares the memory of the array for a binary value", func(t *testing.T) {
		data := []byte("abc")
		value := valueAt(columnar.NewUTF8(data, []int32{0, 3}, memory.Bitmap{}), 0)

		data[0] = 'x'
		require.Equal(t, []byte("xbc"), value.Binary())
	})

	t.Run("panics on an array type that a page reader never returns", func(t *testing.T) {
		require.PanicsWithValue(t, "dataset.valueAt: unsupported array type *columnar.Number[int32]", func() {
			valueAt(columnar.NewNumber([]int32{1}, memory.Bitmap{}), 0)
		})
	})
}
