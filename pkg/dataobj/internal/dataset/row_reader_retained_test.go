package dataset

import (
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
)

func Test_retainedValues(t *testing.T) {
	t.Run("restores numbers and nulls from the survivor that the caller names", func(t *testing.T) {
		store := newRetainedStore(t, datasetmd.PHYSICAL_TYPE_INT64, datasetmd.PHYSICAL_TYPE_UINT64)
		store.add([]Row{
			{Values: []Value{Int64Value(-5), Uint64Value(7)}},
			{Values: []Value{{}, Uint64Value(math.MaxUint64)}},
			{Values: []Value{Int64Value(math.MinInt64), {}}},
		})

		got := []Row{{Values: make([]Value, 2)}, {Values: make([]Value, 2)}}
		store.restore(1, got)

		require.Equal(t, [][]string{
			{"nil", fmt.Sprintf("u:%d", uint64(math.MaxUint64))},
			{fmt.Sprintf("i:%d", int64(math.MinInt64)), "nil"},
		}, describeRows(got))
	})

	t.Run("sets a null over the value that the destination row holds", func(t *testing.T) {
		store := newRetainedStore(t, datasetmd.PHYSICAL_TYPE_INT64, datasetmd.PHYSICAL_TYPE_BINARY)
		store.add([]Row{{Values: []Value{{}, {}}}})

		got := []Row{{Values: []Value{Int64Value(9), BinaryValue([]byte("stale"))}}}
		store.restore(0, got)

		require.Equal(t, [][]string{{"nil", "nil"}}, describeRows(got))
	})

	t.Run("counts the bytes of the flags, numbers, offsets and binary data", func(t *testing.T) {
		store := newRetainedStore(t, datasetmd.PHYSICAL_TYPE_INT64, datasetmd.PHYSICAL_TYPE_BINARY)
		store.add([]Row{
			{Values: []Value{Int64Value(1), BinaryValue([]byte("0123456789"))}},
			{Values: []Value{Int64Value(2), BinaryValue([]byte("abcdefghij"))}},
			{Values: []Value{{}, {}}},
		})

		const rows = 3
		require.Equal(t, 2*rows+8*rows+8*rows+20, store.size())
	})

	t.Run("restores binary values, nulls and empty values", func(t *testing.T) {
		store := newBinaryStore(t)

		got := newRestoreRows(4)
		store.restore(0, got)

		require.Equal(t, [][]string{{"b:alpha"}, {"nil"}, {"b:"}, {"b:beta"}}, describeRows(got))
	})

	t.Run("reuses the buffer of the row when it restores a binary value", func(t *testing.T) {
		store := newBinaryStore(t)

		got := newRestoreRows(4)
		store.restore(0, got)
		buffer := got[0].Values[0].Buffer()
		store.restore(0, got[:1])

		require.Same(t, &buffer[0], &got[0].Values[0].Buffer()[0])
	})

	t.Run("restores the original binary value after the caller changed an earlier one", func(t *testing.T) {
		store := newBinaryStore(t)

		got := newRestoreRows(4)
		store.restore(0, got)
		got[0].Values[0].Binary()[0] = 'X'
		store.restore(0, got[:1])

		require.Equal(t, [][]string{{"b:alpha"}}, describeRows(got[:1]))
	})

	t.Run("does not store a column of an unsupported type", func(t *testing.T) {
		_, ok := newRetainedColumn(0, datasetmd.PHYSICAL_TYPE_UNSPECIFIED)
		require.False(t, ok)
	})
}

func newBinaryStore(t *testing.T) *retainedValues {
	t.Helper()

	store := newRetainedStore(t, datasetmd.PHYSICAL_TYPE_BINARY)
	store.add([]Row{
		{Values: []Value{BinaryValue([]byte("alpha"))}},
		{Values: []Value{{}}},
		{Values: []Value{BinaryValue([]byte{})}},
		{Values: []Value{BinaryValue([]byte("beta"))}},
	})
	return store
}

func newRestoreRows(n int) []Row {
	rows := make([]Row, n)
	for i := range rows {
		rows[i].Values = make([]Value, 1)
	}
	return rows
}

func newRetainedStore(t *testing.T, kinds ...datasetmd.PhysicalType) *retainedValues {
	t.Helper()

	store := &retainedValues{}
	for i, kind := range kinds {
		column, ok := newRetainedColumn(i, kind)
		require.True(t, ok)
		store.columns = append(store.columns, column)
	}
	return store
}

func describeRows(rows []Row) [][]string {
	out := make([][]string, len(rows))
	for i := range rows {
		for j := range rows[i].Values {
			v := &rows[i].Values[j]
			switch {
			case v.IsNil():
				out[i] = append(out[i], "nil")
			case v.Type() == datasetmd.PHYSICAL_TYPE_INT64:
				out[i] = append(out[i], fmt.Sprintf("i:%d", v.Int64()))
			case v.Type() == datasetmd.PHYSICAL_TYPE_UINT64:
				out[i] = append(out[i], fmt.Sprintf("u:%d", v.Uint64()))
			default:
				out[i] = append(out[i], "b:"+string(v.Binary()))
			}
		}
	}
	return out
}
