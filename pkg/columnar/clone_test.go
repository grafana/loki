package columnar_test

import (
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/columnartest"
	"github.com/grafana/loki/v3/pkg/columnar/types"
	"github.com/grafana/loki/v3/pkg/memory"
)

func TestClone(t *testing.T) {
	tests := []struct {
		name   string
		kind   types.Kind
		values []any
		slice  [2]int // if non-zero, slice the source before cloning
	}{
		// Null
		{name: "Null/empty", kind: types.KindNull},
		{name: "Null/single", kind: types.KindNull, values: []any{nil}},
		{name: "Null/multiple", kind: types.KindNull, values: []any{nil, nil, nil, nil, nil}},

		// Bool
		{name: "Bool/empty", kind: types.KindBool},
		{name: "Bool/single true", kind: types.KindBool, values: []any{true}},
		{name: "Bool/single false", kind: types.KindBool, values: []any{false}},
		{name: "Bool/mixed", kind: types.KindBool, values: []any{true, false, true, true, false}},
		{name: "Bool/with nulls", kind: types.KindBool, values: []any{true, nil, false, nil, true}},
		{name: "Bool/all nulls", kind: types.KindBool, values: []any{nil, nil, nil}},
		{name: "Bool/slice", kind: types.KindBool, values: []any{true, false, true, true, false, nil}, slice: [2]int{1, 4}},

		// Int32
		{name: "Int32/empty", kind: types.KindInt32},
		{name: "Int32/single", kind: types.KindInt32, values: []any{42}},
		{name: "Int32/multiple", kind: types.KindInt32, values: []any{1, 2, 3, 4, 5}},
		{name: "Int32/with nulls", kind: types.KindInt32, values: []any{1, nil, 3, nil, 5}},
		{name: "Int32/slice", kind: types.KindInt32, values: []any{10, 20, 30, 40, 50}, slice: [2]int{1, 4}},

		// Int64
		{name: "Int64/empty", kind: types.KindInt64},
		{name: "Int64/single", kind: types.KindInt64, values: []any{42}},
		{name: "Int64/multiple", kind: types.KindInt64, values: []any{1, 2, 3, 4, 5}},
		{name: "Int64/with nulls", kind: types.KindInt64, values: []any{1, nil, 3, nil, 5}},
		{name: "Int64/slice", kind: types.KindInt64, values: []any{10, 20, 30, 40, 50}, slice: [2]int{1, 4}},

		// Uint32
		{name: "Uint32/empty", kind: types.KindUint32},
		{name: "Uint32/single", kind: types.KindUint32, values: []any{42}},
		{name: "Uint32/multiple", kind: types.KindUint32, values: []any{1, 2, 3, 4, 5}},
		{name: "Uint32/with nulls", kind: types.KindUint32, values: []any{1, nil, 3, nil, 5}},
		{name: "Uint32/slice", kind: types.KindUint32, values: []any{10, 20, 30, 40, 50}, slice: [2]int{1, 4}},

		// Uint64
		{name: "Uint64/empty", kind: types.KindUint64},
		{name: "Uint64/single", kind: types.KindUint64, values: []any{42}},
		{name: "Uint64/multiple", kind: types.KindUint64, values: []any{1, 2, 3, 4, 5}},
		{name: "Uint64/with nulls", kind: types.KindUint64, values: []any{1, nil, 3, nil, 5}},
		{name: "Uint64/slice", kind: types.KindUint64, values: []any{10, 20, 30, 40, 50}, slice: [2]int{1, 4}},

		// UTF8
		{name: "UTF8/empty", kind: types.KindUTF8},
		{name: "UTF8/single", kind: types.KindUTF8, values: []any{"hello"}},
		{name: "UTF8/multiple", kind: types.KindUTF8, values: []any{"hello", "world", "foo", "bar"}},
		{name: "UTF8/with nulls", kind: types.KindUTF8, values: []any{"hello", nil, "world", nil}},
		{name: "UTF8/all nulls", kind: types.KindUTF8, values: []any{nil, nil, nil}},
		{name: "UTF8/slice", kind: types.KindUTF8, values: []any{"hello", "world", "foo", "bar", "baz"}, slice: [2]int{1, 4}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var srcAlloc, dstAlloc memory.Allocator

			src := columnartest.Array(t, tt.kind, &srcAlloc, tt.values...)
			if tt.slice != [2]int{} {
				src = src.Slice(tt.slice[0], tt.slice[1])
			}

			result, err := columnar.Clone(&dstAlloc, src)
			require.NoError(t, err)

			actual := result.(columnar.Array)
			columnartest.RequireArraysEqual(t, src, actual, memory.Bitmap{})
			requireDistinctMemory(t, src, actual)
		})
	}
}

func TestClone_Struct(t *testing.T) {
	var srcAlloc, dstAlloc memory.Allocator

	original := columnartest.Struct(t, &srcAlloc,
		columnartest.Field("x", types.KindInt64, int64(1), int64(2)),
		columnartest.Field("y", types.KindUTF8, "a", "b"),
	)

	cloned, err := columnar.Clone(&dstAlloc, original)
	require.NoError(t, err)

	cs := cloned.(*columnar.Struct)
	require.Equal(t, 2, cs.Len())
	require.Equal(t, 2, cs.NumFields())
	columnartest.RequireArraysEqual(t, original.Field(0), cs.Field(0), memory.Bitmap{})
	columnartest.RequireArraysEqual(t, original.Field(1), cs.Field(1), memory.Bitmap{})
}

func TestClone_Struct_WithValidity(t *testing.T) {
	var srcAlloc, dstAlloc memory.Allocator

	schema := columnar.NewSchema([]columnar.Column{{Name: "x"}})
	validity := memory.NewBitmap(&srcAlloc, 2)
	validity.AppendValues(true, false)
	original := columnar.NewStruct(schema, []columnar.Array{
		columnartest.Array(t, types.KindInt64, &srcAlloc, int64(1), int64(0)),
	}, 2, validity)

	cloned, err := columnar.Clone(&dstAlloc, original)
	require.NoError(t, err)

	cs := cloned.(*columnar.Struct)
	require.Equal(t, 1, cs.Nulls())
	require.False(t, cs.IsNull(0))
	require.True(t, cs.IsNull(1))
}

// requireDistinctMemory asserts that two arrays do not share backing memory.
func requireDistinctMemory(t *testing.T, a, b columnar.Array) {
	t.Helper()

	if a.Len() == 0 {
		return
	}

	switch src := a.(type) {
	case *columnar.Null:
		// Null arrays only have a validity bitmap; nothing else to check.
	case *columnar.Bool:
		dst := b.(*columnar.Bool)
		requireDistinctBitmaps(t, src.Values(), dst.Values())
	case *columnar.Number[int32]:
		requireDistinctSlices(t, src.Values(), b.(*columnar.Number[int32]).Values())
	case *columnar.Number[int64]:
		requireDistinctSlices(t, src.Values(), b.(*columnar.Number[int64]).Values())
	case *columnar.Number[uint32]:
		requireDistinctSlices(t, src.Values(), b.(*columnar.Number[uint32]).Values())
	case *columnar.Number[uint64]:
		requireDistinctSlices(t, src.Values(), b.(*columnar.Number[uint64]).Values())
	case *columnar.UTF8:
		dst := b.(*columnar.UTF8)
		requireDistinctSlices(t, src.Offsets(), dst.Offsets())
		if len(src.Data()) > 0 {
			requireDistinctSlices(t, src.Data(), dst.Data())
		}
	}
}

func requireDistinctBitmaps(t *testing.T, a, b memory.Bitmap) {
	t.Helper()
	aData, _ := a.Bytes()
	bData, _ := b.Bytes()
	requireDistinctSlices(t, aData, bData)
}

func requireDistinctSlices[T any](t *testing.T, a, b []T) {
	t.Helper()
	if len(a) == 0 && len(b) == 0 {
		return
	}
	require.NotEqual(t,
		uintptr(unsafe.Pointer(unsafe.SliceData(a))),
		uintptr(unsafe.Pointer(unsafe.SliceData(b))),
		"expected cloned slice to have distinct backing memory",
	)
}
