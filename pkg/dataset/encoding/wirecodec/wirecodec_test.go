package wirecodec_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/columnartest"
	"github.com/grafana/loki/v3/pkg/columnar/types"
	"github.com/grafana/loki/v3/pkg/dataset/array"
	"github.com/grafana/loki/v3/pkg/dataset/buffer"
	"github.com/grafana/loki/v3/pkg/dataset/encoding/wirecodec"
	"github.com/grafana/loki/v3/pkg/dataset/layout"
	"github.com/grafana/loki/v3/pkg/expr"
	"github.com/grafana/loki/v3/pkg/memory"
)

func Test(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	root, expected := writeTestStruct(t, &alloc, &store)

	actualLayout := roundTrip(t, root, &store)
	actual := readAll(t, &alloc, actualLayout, &store)

	require.Equal(t, root.DataType(), actualLayout.DataType())
	require.Equal(t, root.Len(), actualLayout.Len())
	columnartest.RequireArraysEqual(t, expected, actual, memory.Bitmap{})
}

func TestEncodings(t *testing.T) {
	tests := []struct {
		name  string
		spec  array.Spec
		typ   types.Type
		input func(*testing.T, *memory.Allocator) columnar.Array
	}{
		{
			name: "bool",
			spec: &array.SpecBool{},
			typ:  &types.Bool{},
			input: func(t *testing.T, alloc *memory.Allocator) columnar.Array {
				return columnartest.Array(t, types.KindBool, alloc, true, false, true)
			},
		},
		{
			name: "plain",
			spec: &array.SpecPlain{},
			typ:  &types.Int64{},
			input: func(t *testing.T, alloc *memory.Allocator) columnar.Array {
				return columnartest.Array(t, types.KindInt64, alloc, int64(-10), int64(0), int64(20))
			},
		},
		{
			name: "binary",
			spec: &array.SpecBinary{Offsets: &array.SpecPlain{}},
			typ:  &types.UTF8{},
			input: func(t *testing.T, alloc *memory.Allocator) columnar.Array {
				return columnartest.Array(t, types.KindUTF8, alloc, "first", "second", "third")
			},
		},
		{
			name: "bitpacked",
			spec: &array.SpecBitpacked{BlockSize: 8, Widths: &array.SpecPlain{}},
			typ:  &types.Uint64{},
			input: func(t *testing.T, alloc *memory.Allocator) columnar.Array {
				return columnartest.Array(t, types.KindUint64, alloc, uint64(1), uint64(2), uint64(7))
			},
		},
		{
			name: "zigzag int32",
			spec: &array.SpecZigZag{Data: &array.SpecPlain{}},
			typ:  &types.Int32{},
			input: func(t *testing.T, alloc *memory.Allocator) columnar.Array {
				return columnartest.Array(t, types.KindInt32, alloc, int32(-10), int32(0), int32(20))
			},
		},
		{
			name: "nullable zigzag int64",
			spec: &array.SpecZigZag{Data: &array.SpecPlain{Validity: &array.SpecBool{}}},
			typ:  &types.Int64{Nullable: true},
			input: func(t *testing.T, alloc *memory.Allocator) columnar.Array {
				return columnartest.Array(t, types.KindInt64, alloc, int64(-10), nil, int64(20))
			},
		},
		{
			name: "delta",
			spec: &array.SpecDelta{Data: &array.SpecPlain{}},
			typ:  &types.Int64{},
			input: func(t *testing.T, alloc *memory.Allocator) columnar.Array {
				return columnartest.Array(t, types.KindInt64, alloc, int64(10), int64(11), int64(15))
			},
		},
		{
			name: "zstd",
			spec: &array.SpecZstd{Offsets: &array.SpecPlain{}},
			typ:  &types.UTF8{},
			input: func(t *testing.T, alloc *memory.Allocator) columnar.Array {
				return columnartest.Array(t, types.KindUTF8, alloc, "repeated", "repeated", "different")
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var alloc memory.Allocator
			var store buffer.MemoryStore

			expected := tc.input(t, &alloc)
			root := writeLayout(t, &alloc, &store, &layout.SpecArray{Spec: tc.spec}, tc.typ, expected)

			actualLayout := roundTrip(t, root, &store)
			actual := readAll(t, &alloc, actualLayout, &store)

			expectedMetadata := root.(*layout.Array).Metadata
			actualMetadata := actualLayout.(*layout.Array).Metadata
			require.Equal(t, expectedMetadata.Encoding, actualMetadata.Encoding)
			require.Equal(t, expectedMetadata.Stats, actualMetadata.Stats)
			columnartest.RequireArraysEqual(t, expected, actual, memory.Bitmap{})
		})
	}
}

func writeTestStruct(t *testing.T, alloc *memory.Allocator, store buffer.Store) (layout.Layout, columnar.Array) {
	t.Helper()

	typ := &types.Struct{
		Fields: []types.StructField{
			{Name: "id", Type: &types.Int64{}},
			{Name: "message", Type: &types.UTF8{Nullable: true}},
		},
		Nullable: true,
	}
	spec := &layout.SpecStruct{
		Fields: []layout.Spec{
			&layout.SpecChunked{
				MaxRowCount: 2,
				Chunk:       &layout.SpecArray{Spec: &array.SpecPlain{}},
			},
			&layout.SpecChunked{
				MaxRowCount: 2,
				Chunk: &layout.SpecArray{Spec: &array.SpecBinary{
					Offsets:  &array.SpecPlain{},
					Validity: &array.SpecBool{},
				}},
			},
		},
		Validity: &layout.SpecArray{Spec: &array.SpecBool{}},
	}
	input := columnartest.StructWithValidity(t, alloc, []bool{true, false, true, true},
		columnartest.Field("id", types.KindInt64, int64(1), int64(2), int64(3), int64(4)),
		columnartest.Field("message", types.KindUTF8, "first", nil, "third", "fourth"),
	)

	return writeLayout(t, alloc, store, spec, typ, input), input
}

func writeLayout(t *testing.T, alloc *memory.Allocator, sink buffer.Sink, spec layout.Spec, typ types.Type, inputs ...columnar.Array) layout.Layout {
	t.Helper()

	w, err := layout.NewWriter(alloc, sink, spec, typ)
	require.NoError(t, err)
	for _, input := range inputs {
		require.NoError(t, w.Append(t.Context(), input))
	}

	root, err := w.Flush(t.Context())
	require.NoError(t, err)
	return root
}

func roundTrip(t *testing.T, root layout.Layout, store buffer.Store) layout.Layout {
	t.Helper()

	manifest, err := wirecodec.Build(t.Context(), root, store)
	require.NoError(t, err)

	actual, err := manifest.Load(t.Context(), store)
	require.NoError(t, err)
	return actual
}

func readAll(t *testing.T, alloc *memory.Allocator, root layout.Layout, source buffer.Source) columnar.Array {
	t.Helper()

	r, err := layout.NewReader(alloc, root, source)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, r.Close()) })

	_, err = r.Next(t.Context(), math.MaxInt)
	require.NoError(t, err)

	actual, err := r.Project(t.Context(), alloc, &expr.Identity{}, memory.Bitmap{})
	require.NoError(t, err)
	return actual
}
