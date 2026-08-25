package layout_test

import (
	"io"
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/columnartest"
	"github.com/grafana/loki/v3/pkg/columnar/types"
	"github.com/grafana/loki/v3/pkg/compute"
	"github.com/grafana/loki/v3/pkg/dataset/array"
	"github.com/grafana/loki/v3/pkg/dataset/buffer"
	"github.com/grafana/loki/v3/pkg/dataset/layout"
	"github.com/grafana/loki/v3/pkg/expr"
	"github.com/grafana/loki/v3/pkg/memory"
)

func TestCodec_Struct_WriteRead(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	typ := &types.Struct{
		Fields: []types.StructField{
			{Name: "x", Type: &types.Int64{}},
			{Name: "y", Type: &types.UTF8{}},
		},
	}

	spec := &layout.SpecStruct{
		Fields: []layout.Spec{
			&layout.SpecArray{Spec: &array.SpecPlain{}},
			&layout.SpecArray{Spec: &array.SpecBinary{Offsets: &array.SpecPlain{}}},
		},
	}

	w, err := layout.NewWriter(&alloc, &store, spec, typ)
	require.NoError(t, err)

	input := columnartest.Struct(t, &alloc,
		columnartest.Field("x", types.KindInt64, int64(10), int64(20), int64(30)),
		columnartest.Field("y", types.KindUTF8, "a", "b", "c"),
	)
	require.NoError(t, w.Append(t.Context(), input))

	l, err := w.Flush(t.Context())
	require.NoError(t, err)

	r, err := layout.NewReader(&alloc, l, &store)
	require.NoError(t, err)
	defer r.Close()

	n, err := r.Next(t.Context(), math.MaxInt)
	require.NoError(t, err)
	require.Equal(t, 3, n)

	projection := &expr.Include{Names: []string{"x", "y"}, Value: &expr.Identity{}}
	actual, err := r.Project(t.Context(), &alloc, projection, memory.Bitmap{})
	require.NoError(t, err)
	columnartest.RequireArraysEqual(t, input, actual, memory.Bitmap{})
}

func TestCodec_Struct_RejectsValidityMismatch(t *testing.T) {
	tests := []struct {
		name     string
		nullable bool
		validity layout.Spec
	}{
		{
			name:     "nullable type without validity spec",
			nullable: true,
		},
		{
			name:     "non-nullable type with validity spec",
			validity: &layout.SpecArray{Spec: &array.SpecBool{}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var alloc memory.Allocator
			var store buffer.MemoryStore

			_, err := layout.NewWriter(&alloc, &store, &layout.SpecStruct{
				Fields:   []layout.Spec{&layout.SpecArray{Spec: &array.SpecPlain{}}},
				Validity: tc.validity,
			}, &types.Struct{
				Fields:   []types.StructField{{Name: "x", Type: &types.Int64{}}},
				Nullable: tc.nullable,
			})
			require.Error(t, err)
		})
	}
}

func TestCodec_Struct_RejectsUnexpectedValidity(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	w, err := layout.NewWriter(&alloc, &store, &layout.SpecStruct{
		Fields: []layout.Spec{&layout.SpecArray{Spec: &array.SpecPlain{}}},
	}, &types.Struct{
		Fields: []types.StructField{{Name: "x", Type: &types.Int64{}}},
	})
	require.NoError(t, err)

	input := columnartest.StructWithValidity(t, &alloc, []bool{true, false},
		columnartest.Field("x", types.KindInt64, int64(10), int64(20)),
	)
	require.Error(t, w.Append(t.Context(), input))
}

func TestCodec_Struct_ProjectionValidity(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	w, err := layout.NewWriter(&alloc, &store, &layout.SpecStruct{
		Fields: []layout.Spec{
			&layout.SpecArray{Spec: &array.SpecPlain{}},
			&layout.SpecArray{Spec: &array.SpecPlain{}},
		},
		Validity: &layout.SpecArray{Spec: &array.SpecBool{}},
	}, &types.Struct{
		Fields: []types.StructField{
			{Name: "x", Type: &types.Int64{}},
			{Name: "y", Type: &types.Int64{}},
		},
		Nullable: true,
	})
	require.NoError(t, err)

	input := columnartest.StructWithValidity(t, &alloc, []bool{true, false, true},
		columnartest.Field("x", types.KindInt64, int64(10), int64(20), int64(30)),
		columnartest.Field("y", types.KindInt64, int64(40), int64(50), int64(60)),
	)
	require.NoError(t, w.Append(t.Context(), input))
	encoded, err := w.Flush(t.Context())
	require.NoError(t, err)

	r, err := layout.NewReader(&alloc, encoded, &store)
	require.NoError(t, err)
	defer r.Close()
	_, err = r.Next(t.Context(), math.MaxInt)
	require.NoError(t, err)

	makeStructProjection := func() *expr.MakeStruct {
		return &expr.MakeStruct{
			Names: []string{"constant", "x"},
			Values: []expr.Expression{
				&expr.Constant{Value: &columnar.NumberScalar[int64]{Value: 5}},
				&expr.Column{Name: "x"},
			},
		}
	}

	t.Run("Identity preserves struct validity", func(t *testing.T) {
		actual, err := r.Project(t.Context(), &alloc, &expr.Identity{}, memory.Bitmap{})
		require.NoError(t, err)

		columnartest.RequireArraysEqual(t, input, actual, memory.Bitmap{})
	})

	t.Run("Include preserves struct validity", func(t *testing.T) {
		actual, err := r.Project(t.Context(), &alloc, &expr.Include{
			Names: []string{"x", "y"},
			Value: &expr.Identity{},
		}, memory.Bitmap{})
		require.NoError(t, err)

		columnartest.RequireArraysEqual(t, input, actual, memory.Bitmap{})
	})

	t.Run("Exclude preserves struct validity", func(t *testing.T) {
		actual, err := r.Project(t.Context(), &alloc, &expr.Exclude{
			Names: nil,
			Value: &expr.Identity{},
		}, memory.Bitmap{})
		require.NoError(t, err)

		columnartest.RequireArraysEqual(t, input, actual, memory.Bitmap{})
	})

	t.Run("propagates validity to a field", func(t *testing.T) {
		actual, err := r.Project(t.Context(), &alloc, &expr.Column{Name: "x"}, memory.Bitmap{})
		require.NoError(t, err)

		columnartest.RequireArraysEqual(t,
			columnartest.Array(t, types.KindInt64, &alloc, int64(10), nil, int64(30)),
			actual,
			memory.Bitmap{},
		)
	})

	t.Run("propagates validity per MakeStruct field", func(t *testing.T) {
		actual, err := r.Project(t.Context(), &alloc, makeStructProjection(), memory.Bitmap{})
		require.NoError(t, err)

		columnartest.RequireArraysEqual(t,
			columnartest.Struct(t, &alloc,
				columnartest.Field("constant", types.KindInt64, int64(5), int64(5), int64(5)),
				columnartest.Field("x", types.KindInt64, int64(10), nil, int64(30)),
			),
			actual,
			memory.Bitmap{},
		)
	})

	t.Run("preserves MakeStruct validity through Include", func(t *testing.T) {
		expected, err := r.Project(t.Context(), &alloc, makeStructProjection(), memory.Bitmap{})
		require.NoError(t, err)
		actual, err := r.Project(t.Context(), &alloc, &expr.Include{
			Names: []string{"constant", "x"},
			Value: makeStructProjection(),
		}, memory.Bitmap{})
		require.NoError(t, err)

		columnartest.RequireArraysEqual(t, expected, actual, memory.Bitmap{})
	})
}

func TestCodec_Struct_NestedProjectionValidity(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	nestedType := &types.Struct{
		Fields:   []types.StructField{{Name: "value", Type: &types.Int64{}}},
		Nullable: true,
	}
	w, err := layout.NewWriter(&alloc, &store, &layout.SpecStruct{
		Fields: []layout.Spec{&layout.SpecStruct{
			Fields:   []layout.Spec{&layout.SpecArray{Spec: &array.SpecPlain{}}},
			Validity: &layout.SpecArray{Spec: &array.SpecBool{}},
		}},
		Validity: &layout.SpecArray{Spec: &array.SpecBool{}},
	}, &types.Struct{
		Fields:   []types.StructField{{Name: "nested", Type: nestedType}},
		Nullable: true,
	})
	require.NoError(t, err)

	nested := columnartest.StructWithValidity(t, &alloc, []bool{true, true, false, true},
		columnartest.Field("value", types.KindInt64, int64(10), int64(20), int64(30), int64(40)),
	)
	input := columnartest.StructWithValidity(t, &alloc, []bool{true, false, true, true},
		columnartest.Field("nested", types.KindStruct, nested),
	)
	require.NoError(t, w.Append(t.Context(), input))
	encoded, err := w.Flush(t.Context())
	require.NoError(t, err)

	r, err := layout.NewReader(&alloc, encoded, &store)
	require.NoError(t, err)
	defer r.Close()
	_, err = r.Next(t.Context(), math.MaxInt)
	require.NoError(t, err)

	actual, err := r.Project(t.Context(), &alloc, &expr.Extract{
		Name:  "value",
		Value: &expr.Column{Name: "nested"},
	}, memory.Bitmap{})
	require.NoError(t, err)

	columnartest.RequireArraysEqual(t,
		columnartest.Array(t, types.KindInt64, &alloc, int64(10), nil, nil, int64(40)),
		actual,
		memory.Bitmap{},
	)
}

func TestCodec_Struct_ConstantProjection(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	r := buildStructReaderXY(t, &alloc, &store,
		[]int64{10, 20, 30},
		[]string{"a", "b", "c"},
	)
	defer r.Close()
	_, err := r.Next(t.Context(), math.MaxInt)
	require.NoError(t, err)

	actual, err := r.Project(t.Context(), &alloc, &expr.Constant{
		Value: &columnar.NumberScalar[int64]{Value: 5},
	}, memory.Bitmap{})
	require.NoError(t, err)
	columnartest.RequireArraysEqual(t,
		columnartest.Array(t, types.KindInt64, &alloc, int64(5), int64(5), int64(5)),
		actual,
		memory.Bitmap{},
	)

	actual, err = r.Project(t.Context(), &alloc, &expr.MakeStruct{
		Names: []string{"x", "constant"},
		Values: []expr.Expression{
			&expr.Column{Name: "x"},
			&expr.Constant{Value: &columnar.NumberScalar[int64]{Value: 5}},
		},
	}, memory.Bitmap{})
	require.NoError(t, err)
	columnartest.RequireArraysEqual(t,
		columnartest.Struct(t, &alloc,
			columnartest.Field("x", types.KindInt64, int64(10), int64(20), int64(30)),
			columnartest.Field("constant", types.KindInt64, int64(5), int64(5), int64(5)),
		),
		actual,
		memory.Bitmap{},
	)
}

func TestCodec_Struct_EmptyMakeStructProjection(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	r := buildStructReaderXY(t, &alloc, &store,
		[]int64{10, 20, 30},
		[]string{"a", "b", "c"},
	)
	defer r.Close()
	_, err := r.Next(t.Context(), math.MaxInt)
	require.NoError(t, err)

	actual, err := r.Project(t.Context(), &alloc, &expr.MakeStruct{}, memory.Bitmap{})
	require.NoError(t, err)

	result := actual.(*columnar.Struct)
	require.Equal(t, 3, result.Len())
	require.Equal(t, 0, result.NumFields())
}

func TestCodec_Struct_PrunesExcludedMakeStructFields(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	r := buildStructReaderXY(t, &alloc, &store,
		[]int64{10, 20, 30},
		[]string{"a", "b", "c"},
	)
	defer r.Close()
	_, err := r.Next(t.Context(), math.MaxInt)
	require.NoError(t, err)

	projection := &expr.Include{
		Names: []string{"constant"},
		Value: &expr.MakeStruct{
			Names: []string{"constant", "unused"},
			Values: []expr.Expression{
				&expr.Constant{Value: &columnar.NumberScalar[int64]{Value: 5}},
				&expr.Unary{Op: expr.UnaryOpInvalid, Value: &expr.Column{Name: "x"}},
			},
		},
	}

	bufs, err := r.AppendBuffers(nil, nil, projection, memory.Bitmap{})
	require.NoError(t, err)
	require.Empty(t, bufs)
	actual, err := r.Project(t.Context(), &alloc, projection, memory.Bitmap{})
	require.NoError(t, err)

	columnartest.RequireArraysEqual(t,
		columnartest.Struct(t, &alloc,
			columnartest.Field("constant", types.KindInt64, int64(5), int64(5), int64(5)),
		),
		actual,
		memory.Bitmap{},
	)
}

func TestCodec_Struct_Projection(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	typ := &types.Struct{
		Fields: []types.StructField{
			{Name: "x", Type: &types.Int64{}},
			{Name: "y", Type: &types.UTF8{}},
			{Name: "z", Type: &types.Int64{}},
		},
	}

	spec := &layout.SpecStruct{
		Fields: []layout.Spec{
			&layout.SpecArray{Spec: &array.SpecPlain{}},
			&layout.SpecArray{Spec: &array.SpecBinary{Offsets: &array.SpecPlain{}}},
			&layout.SpecArray{Spec: &array.SpecPlain{}},
		},
	}

	w, err := layout.NewWriter(&alloc, &store, spec, typ)
	require.NoError(t, err)

	input := columnartest.Struct(t, &alloc,
		columnartest.Field("x", types.KindInt64, int64(1), int64(2)),
		columnartest.Field("y", types.KindUTF8, "a", "b"),
		columnartest.Field("z", types.KindInt64, int64(10), int64(20)),
	)
	require.NoError(t, w.Append(t.Context(), input))

	l, err := w.Flush(t.Context())
	require.NoError(t, err)

	r, err := layout.NewReader(&alloc, l, &store)
	require.NoError(t, err)
	defer r.Close()

	n, err := r.Next(t.Context(), math.MaxInt)
	require.NoError(t, err)
	require.Equal(t, 2, n)

	projection := &expr.Include{Names: []string{"x", "z"}, Value: &expr.Identity{}}

	actual, err := r.Project(t.Context(), &alloc, projection, memory.Bitmap{})
	require.NoError(t, err)

	expected := columnartest.Struct(t, &alloc,
		columnartest.Field("x", types.KindInt64, int64(1), int64(2)),
		columnartest.Field("z", types.KindInt64, int64(10), int64(20)),
	)
	columnartest.RequireArraysEqual(t, expected, actual, memory.Bitmap{})
}

func TestCodec_Struct_Windowing(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	typ := &types.Struct{
		Fields: []types.StructField{
			{Name: "x", Type: &types.Int64{}},
		},
	}

	spec := &layout.SpecStruct{
		Fields: []layout.Spec{
			&layout.SpecArray{Spec: &array.SpecPlain{}},
		},
	}

	w, err := layout.NewWriter(&alloc, &store, spec, typ)
	require.NoError(t, err)

	input := columnartest.Struct(t, &alloc,
		columnartest.Field("x", types.KindInt64, int64(1), int64(2), int64(3), int64(4)),
	)
	require.NoError(t, w.Append(t.Context(), input))

	l, err := w.Flush(t.Context())
	require.NoError(t, err)

	r, err := layout.NewReader(&alloc, l, &store)
	require.NoError(t, err)
	defer r.Close()

	// Window 1
	n, err := r.Next(t.Context(), 2)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	projection := &expr.Include{Names: []string{"x"}, Value: &expr.Identity{}}
	actual, err := r.Project(t.Context(), &alloc, projection, memory.Bitmap{})
	require.NoError(t, err)
	columnartest.RequireArraysEqual(t,
		columnartest.Struct(t, &alloc, columnartest.Field("x", types.KindInt64, int64(1), int64(2))),
		actual, memory.Bitmap{},
	)

	// Window 2
	n, err = r.Next(t.Context(), 2)
	require.NoError(t, err)
	require.Equal(t, 2, n)
	actual, err = r.Project(t.Context(), &alloc, projection, memory.Bitmap{})
	require.NoError(t, err)
	columnartest.RequireArraysEqual(t,
		columnartest.Struct(t, &alloc, columnartest.Field("x", types.KindInt64, int64(3), int64(4))),
		actual, memory.Bitmap{},
	)

	// EOF
	_, err = r.Next(t.Context(), 2)
	require.ErrorIs(t, err, io.EOF)
}

func TestCodec_Struct_Filter(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	typ := &types.Struct{
		Fields: []types.StructField{
			{Name: "x", Type: &types.Int64{}},
			{Name: "y", Type: &types.UTF8{}},
		},
	}

	spec := &layout.SpecStruct{
		Fields: []layout.Spec{
			&layout.SpecArray{Spec: &array.SpecPlain{}},
			&layout.SpecArray{Spec: &array.SpecBinary{Offsets: &array.SpecPlain{}}},
		},
	}

	w, err := layout.NewWriter(&alloc, &store, spec, typ)
	require.NoError(t, err)

	input := columnartest.Struct(t, &alloc,
		columnartest.Field("x", types.KindInt64, int64(10), int64(20), int64(30), int64(40)),
		columnartest.Field("y", types.KindUTF8, "a", "b", "c", "d"),
	)
	require.NoError(t, w.Append(t.Context(), input))

	l, err := w.Flush(t.Context())
	require.NoError(t, err)

	r, err := layout.NewReader(&alloc, l, &store)
	require.NoError(t, err)
	defer r.Close()

	n, err := r.Next(t.Context(), math.MaxInt)
	require.NoError(t, err)
	require.Equal(t, 4, n)

	filterExpr := &expr.Binary{
		Left:  &expr.Column{Name: "x"},
		Op:    expr.BinaryOpGT,
		Right: &expr.Constant{Value: &columnar.NumberScalar[int64]{Value: 15}},
	}

	mask, err := r.Filter(t.Context(), &alloc, filterExpr, memory.Bitmap{})
	require.NoError(t, err)

	projection := &expr.Include{Names: []string{"x", "y"}, Value: &expr.Identity{}}
	projected, err := r.Project(t.Context(), &alloc, projection, mask)
	require.NoError(t, err)
	filtered, err := compute.Filter(&alloc, projected, mask)
	require.NoError(t, err)

	expected := columnartest.Struct(t, &alloc,
		columnartest.Field("x", types.KindInt64, int64(20), int64(30), int64(40)),
		columnartest.Field("y", types.KindUTF8, "b", "c", "d"),
	)
	columnartest.RequireArraysEqual(t, expected, filtered.(columnar.Array), memory.Bitmap{})
}

func TestCodec_Struct_Filter_AND(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	typ := &types.Struct{
		Fields: []types.StructField{
			{Name: "x", Type: &types.Int64{}},
			{Name: "y", Type: &types.Int64{}},
		},
	}

	spec := &layout.SpecStruct{
		Fields: []layout.Spec{
			&layout.SpecArray{Spec: &array.SpecPlain{}},
			&layout.SpecArray{Spec: &array.SpecPlain{}},
		},
	}

	w, err := layout.NewWriter(&alloc, &store, spec, typ)
	require.NoError(t, err)

	input := columnartest.Struct(t, &alloc,
		columnartest.Field("x", types.KindInt64, int64(1), int64(2), int64(3)),
		columnartest.Field("y", types.KindInt64, int64(10), int64(20), int64(30)),
	)
	require.NoError(t, w.Append(t.Context(), input))

	l, err := w.Flush(t.Context())
	require.NoError(t, err)

	r, err := layout.NewReader(&alloc, l, &store)
	require.NoError(t, err)
	defer r.Close()

	n, err := r.Next(t.Context(), math.MaxInt)
	require.NoError(t, err)
	require.Equal(t, 3, n)

	// x > 1 AND y > 15
	filterExpr := &expr.Binary{
		Left: &expr.Binary{
			Left:  &expr.Column{Name: "x"},
			Op:    expr.BinaryOpGT,
			Right: &expr.Constant{Value: &columnar.NumberScalar[int64]{Value: 1}},
		},
		Op: expr.BinaryOpAND,
		Right: &expr.Binary{
			Left:  &expr.Column{Name: "y"},
			Op:    expr.BinaryOpGT,
			Right: &expr.Constant{Value: &columnar.NumberScalar[int64]{Value: 15}},
		},
	}

	mask, err := r.Filter(t.Context(), &alloc, filterExpr, memory.Bitmap{})
	require.NoError(t, err)

	projection := &expr.Include{Names: []string{"x", "y"}, Value: &expr.Identity{}}
	projected, err := r.Project(t.Context(), &alloc, projection, mask)
	require.NoError(t, err)
	filtered, err := compute.Filter(&alloc, projected, mask)
	require.NoError(t, err)

	expected := columnartest.Struct(t, &alloc,
		columnartest.Field("x", types.KindInt64, int64(2), int64(3)),
		columnartest.Field("y", types.KindInt64, int64(20), int64(30)),
	)
	columnartest.RequireArraysEqual(t, expected, filtered.(columnar.Array), memory.Bitmap{})
}

func TestCodec_Struct_Filter_OR(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	typ := &types.Struct{
		Fields: []types.StructField{
			{Name: "x", Type: &types.Int64{}},
			{Name: "y", Type: &types.Int64{}},
		},
	}

	spec := &layout.SpecStruct{
		Fields: []layout.Spec{
			&layout.SpecArray{Spec: &array.SpecPlain{}},
			&layout.SpecArray{Spec: &array.SpecPlain{}},
		},
	}

	w, err := layout.NewWriter(&alloc, &store, spec, typ)
	require.NoError(t, err)

	input := columnartest.Struct(t, &alloc,
		columnartest.Field("x", types.KindInt64, int64(1), int64(2), int64(3)),
		columnartest.Field("y", types.KindInt64, int64(30), int64(10), int64(10)),
	)
	require.NoError(t, w.Append(t.Context(), input))

	l, err := w.Flush(t.Context())
	require.NoError(t, err)

	r, err := layout.NewReader(&alloc, l, &store)
	require.NoError(t, err)
	defer r.Close()

	n, err := r.Next(t.Context(), math.MaxInt)
	require.NoError(t, err)
	require.Equal(t, 3, n)

	// x == 1 OR y == 10
	filterExpr := &expr.Binary{
		Left: &expr.Binary{
			Left:  &expr.Column{Name: "x"},
			Op:    expr.BinaryOpEQ,
			Right: &expr.Constant{Value: &columnar.NumberScalar[int64]{Value: 1}},
		},
		Op: expr.BinaryOpOR,
		Right: &expr.Binary{
			Left:  &expr.Column{Name: "y"},
			Op:    expr.BinaryOpEQ,
			Right: &expr.Constant{Value: &columnar.NumberScalar[int64]{Value: 10}},
		},
	}

	mask, err := r.Filter(t.Context(), &alloc, filterExpr, memory.Bitmap{})
	require.NoError(t, err)

	projection := &expr.Include{Names: []string{"x", "y"}, Value: &expr.Identity{}}
	projected, err := r.Project(t.Context(), &alloc, projection, mask)
	require.NoError(t, err)
	filtered, err := compute.Filter(&alloc, projected, mask)
	require.NoError(t, err)

	// All 3 rows match (row 0: x==1, row 1: y==10, row 2: y==10)
	columnartest.RequireArraysEqual(t, input, filtered.(columnar.Array), memory.Bitmap{})
}

func TestCodec_Struct_ChunkedFields(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	typ := &types.Struct{
		Fields: []types.StructField{
			{Name: "x", Type: &types.Int64{}},
			{Name: "y", Type: &types.UTF8{}},
		},
	}

	spec := &layout.SpecStruct{
		Fields: []layout.Spec{
			&layout.SpecChunked{
				MaxRowCount: 2,
				Chunk:       &layout.SpecArray{Spec: &array.SpecPlain{}},
			},
			&layout.SpecChunked{
				MaxRowCount: 2,
				Chunk:       &layout.SpecArray{Spec: &array.SpecBinary{Offsets: &array.SpecPlain{}}},
			},
		},
	}

	w, err := layout.NewWriter(&alloc, &store, spec, typ)
	require.NoError(t, err)

	input := columnartest.Struct(t, &alloc,
		columnartest.Field("x", types.KindInt64, int64(1), int64(2), int64(3), int64(4), int64(5)),
		columnartest.Field("y", types.KindUTF8, "a", "b", "c", "d", "e"),
	)
	require.NoError(t, w.Append(t.Context(), input))

	l, err := w.Flush(t.Context())
	require.NoError(t, err)

	r, err := layout.NewReader(&alloc, l, &store)
	require.NoError(t, err)
	defer r.Close()

	n, err := r.Next(t.Context(), math.MaxInt)
	require.NoError(t, err)
	require.Equal(t, 5, n)

	filterExpr := &expr.Binary{
		Left:  &expr.Column{Name: "x"},
		Op:    expr.BinaryOpGT,
		Right: &expr.Constant{Value: &columnar.NumberScalar[int64]{Value: 2}},
	}

	mask, err := r.Filter(t.Context(), &alloc, filterExpr, memory.Bitmap{})
	require.NoError(t, err)

	projection := &expr.Include{Names: []string{"x", "y"}, Value: &expr.Identity{}}
	projected, err := r.Project(t.Context(), &alloc, projection, mask)
	require.NoError(t, err)
	filtered, err := compute.Filter(&alloc, projected, mask)
	require.NoError(t, err)

	expected := columnartest.Struct(t, &alloc,
		columnartest.Field("x", types.KindInt64, int64(3), int64(4), int64(5)),
		columnartest.Field("y", types.KindUTF8, "c", "d", "e"),
	)
	columnartest.RequireArraysEqual(t, expected, filtered.(columnar.Array), memory.Bitmap{})
}

// TestStructReader_Filter_NestedStructExpression exercises the simplification
// path inside structReader.Filter: an Extract{name, Identity{}} on a struct
// boundary must fold to Column{name} so the filter pushes down to the child
// reader. Pre-fix, the filter falls into evalFallbackFilter against an empty
// input and produces a null-vs-utf8 type mismatch.
func TestStructReader_Filter_NestedStructExpression(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	r := buildStructReaderXY(t, &alloc, &store,
		[]int64{1, 2, 3, 4},
		[]string{"alpha", "beta", "alpha", "gamma"},
	)
	defer r.Close()

	n, err := r.Next(t.Context(), math.MaxInt)
	require.NoError(t, err)
	require.Equal(t, 4, n)

	// Equivalent of: y == "alpha", expressed via Extract{Name, Identity}.
	nestedFilter := &expr.Binary{
		Left: &expr.Extract{
			Name:  "y",
			Value: &expr.Identity{},
		},
		Op:    expr.BinaryOpEQ,
		Right: &expr.Constant{Value: &columnar.UTF8Scalar{Value: []byte("alpha")}},
	}

	gotMask, err := r.Filter(t.Context(), &alloc, nestedFilter, memory.Bitmap{})
	require.NoError(t, err)

	// Reference: same predicate written with a plain Column reference. After
	// simplification the two forms must produce identical results.
	plainFilter := &expr.Binary{
		Left:  &expr.Column{Name: "y"},
		Op:    expr.BinaryOpEQ,
		Right: &expr.Constant{Value: &columnar.UTF8Scalar{Value: []byte("alpha")}},
	}
	wantMask, err := r.Filter(t.Context(), &alloc, plainFilter, memory.Bitmap{})
	require.NoError(t, err)

	require.Equal(t, wantMask.Len(), gotMask.Len())
	for i := 0; i < gotMask.Len(); i++ {
		require.Equal(t, wantMask.Get(i), gotMask.Get(i), "row %d mismatch", i)
	}
}

// TestStructReader_Filter_ExtractWithInclude exercises the Include fold rule
// in simplifyProjection. Pre-fix, Extract{y, Include{[y], Identity{}}} has
// zero collected Column references so walkFilter routes it to
// evalFallbackFilter, which builds an empty input struct and produces a Null
// column: comparison with a UTF8 literal then fails with a kind mismatch.
// Post-fix, the simplification folds Include{[y], Identity{}} to MakeStruct{y:
// Column{y}}, then Extract{y, ...} folds to Column{y}, and the filter pushes
// down to the y child reader.
func TestStructReader_Filter_ExtractWithInclude(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	r := buildStructReaderXY(t, &alloc, &store,
		[]int64{10, 20, 30, 40},
		[]string{"a", "b", "c", "d"},
	)
	defer r.Close()

	n, err := r.Next(t.Context(), math.MaxInt)
	require.NoError(t, err)
	require.Equal(t, 4, n)

	// Equivalent of: y == "b", but expressed via Extract{y, Include{[y], Identity{}}}.
	nestedFilter := &expr.Binary{
		Left: &expr.Extract{
			Name: "y",
			Value: &expr.Include{
				Names: []string{"y"},
				Value: &expr.Identity{},
			},
		},
		Op:    expr.BinaryOpEQ,
		Right: &expr.Constant{Value: &columnar.UTF8Scalar{Value: []byte("b")}},
	}

	gotMask, err := r.Filter(t.Context(), &alloc, nestedFilter, memory.Bitmap{})
	require.NoError(t, err)

	plainFilter := &expr.Binary{
		Left:  &expr.Column{Name: "y"},
		Op:    expr.BinaryOpEQ,
		Right: &expr.Constant{Value: &columnar.UTF8Scalar{Value: []byte("b")}},
	}
	wantMask, err := r.Filter(t.Context(), &alloc, plainFilter, memory.Bitmap{})
	require.NoError(t, err)

	require.Equal(t, wantMask.Len(), gotMask.Len())
	for i := 0; i < gotMask.Len(); i++ {
		require.Equal(t, wantMask.Get(i), gotMask.Get(i), "row %d mismatch", i)
	}
}

func TestStructReader_MissingColumn(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	r := buildStructReaderXY(t, &alloc, &store,
		[]int64{10, 20, 30, 40},
		[]string{"alpha", "beta", "alpha", "gamma"},
	)
	defer r.Close()
	_, err := r.Next(t.Context(), math.MaxInt)
	require.NoError(t, err)

	projected, err := r.Project(t.Context(), &alloc, &expr.Column{Name: "missing"}, memory.Bitmap{})
	require.NoError(t, err)
	require.Equal(t, types.KindNull, projected.Kind())
	require.Equal(t, 4, projected.Len())

	_, err = r.Filter(t.Context(), &alloc, &expr.Binary{
		Left:  &expr.Column{Name: "missing"},
		Op:    expr.BinaryOpEQ,
		Right: &expr.Constant{Value: &columnar.UTF8Scalar{Value: []byte("value")}},
	}, memory.Bitmap{})
	require.Error(t, err)
}

func TestStructReader_EmptyProjectionPreservesLength(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	r := buildStructReaderXY(t, &alloc, &store,
		[]int64{10, 20, 30, 40},
		[]string{"alpha", "beta", "alpha", "gamma"},
	)
	defer r.Close()
	_, err := r.Next(t.Context(), math.MaxInt)
	require.NoError(t, err)

	projected, err := r.Project(t.Context(), &alloc, &expr.Include{Value: &expr.Identity{}}, memory.Bitmap{})
	require.NoError(t, err)
	require.Equal(t, 4, projected.Len())
	require.Equal(t, 0, projected.(*columnar.Struct).NumFields())
}

func TestStructReader_AppendBuffers_DirectProjection(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	r := buildStructReaderXY(t, &alloc, &store,
		[]int64{10, 20, 30, 40},
		[]string{"alpha", "beta", "alpha", "gamma"},
	)
	defer r.Close()

	n, err := r.Next(t.Context(), math.MaxInt)
	require.NoError(t, err)
	require.Equal(t, 4, n)

	directBufs, err := r.AppendBuffers(nil, nil, &expr.Column{Name: "x"}, memory.Bitmap{})
	require.NoError(t, err)

	includeBufs, err := r.AppendBuffers(nil, nil, &expr.Include{
		Names: []string{"x"},
		Value: &expr.Identity{},
	}, memory.Bitmap{})
	require.NoError(t, err)
	require.NotEmpty(t, includeBufs)
	require.ElementsMatch(t, includeBufs, directBufs)
}

// TestStructReader_AppendBuffers_FilterIdentityRoot exercises the
// simplification of the filter argument inside AppendBuffers. Pre-fix, the
// projection arg is simplified at line 196 but the filter arg is not, so a
// filter like Extract{name, Identity{}} == "x" has zero collectColumns
// matches and contributes no fields to the union: buffers for fields used
// only by the filter are missed. The projection here deliberately omits the
// filter field so the filter is the only path that should pull in that
// field's buffers.
func TestStructReader_AppendBuffers_FilterIdentityRoot(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	r := buildStructReaderXY(t, &alloc, &store,
		[]int64{10, 20, 30, 40},
		[]string{"alpha", "beta", "alpha", "gamma"},
	)
	defer r.Close()

	n, err := r.Next(t.Context(), math.MaxInt)
	require.NoError(t, err)
	require.Equal(t, 4, n)

	// Filter on y, project only x: y's buffers must be reached only via the
	// filter argument's column references.
	projection := &expr.Include{Names: []string{"x"}, Value: &expr.Identity{}}

	nestedFilter := &expr.Binary{
		Left: &expr.Extract{
			Name:  "y",
			Value: &expr.Identity{},
		},
		Op:    expr.BinaryOpEQ,
		Right: &expr.Constant{Value: &columnar.UTF8Scalar{Value: []byte("alpha")}},
	}
	plainFilter := &expr.Binary{
		Left:  &expr.Column{Name: "y"},
		Op:    expr.BinaryOpEQ,
		Right: &expr.Constant{Value: &columnar.UTF8Scalar{Value: []byte("alpha")}},
	}

	gotBufs, err := r.AppendBuffers(nil, nestedFilter, projection, memory.Bitmap{})
	require.NoError(t, err)

	wantBufs, err := r.AppendBuffers(nil, plainFilter, projection, memory.Bitmap{})
	require.NoError(t, err)

	// Sanity: the plain-Column form must reach more buffers than the
	// projection alone would; otherwise the test wouldn't distinguish
	// pre-fix from post-fix.
	projOnlyBufs, err := r.AppendBuffers(nil, nil, projection, memory.Bitmap{})
	require.NoError(t, err)
	require.Greater(t, len(wantBufs), len(projOnlyBufs),
		"plain-Column filter must reach buffers the projection alone misses")

	require.ElementsMatch(t, wantBufs, gotBufs)
}

// buildStructReaderXY is a helper to build a struct reader with two fields
// (x: Int64, y: UTF8) populated with the given parallel data.
func buildStructReaderXY(t *testing.T, alloc *memory.Allocator, store *buffer.MemoryStore, xs []int64, ys []string) layout.Reader {
	t.Helper()

	typ := &types.Struct{
		Fields: []types.StructField{
			{Name: "x", Type: &types.Int64{}},
			{Name: "y", Type: &types.UTF8{}},
		},
	}

	spec := &layout.SpecStruct{
		Fields: []layout.Spec{
			&layout.SpecArray{Spec: &array.SpecPlain{}},
			&layout.SpecArray{Spec: &array.SpecBinary{Offsets: &array.SpecPlain{}}},
		},
	}

	w, err := layout.NewWriter(alloc, store, spec, typ)
	require.NoError(t, err)

	xVals := make([]any, len(xs))
	for i, v := range xs {
		xVals[i] = v
	}
	yVals := make([]any, len(ys))
	for i, v := range ys {
		yVals[i] = v
	}

	input := columnartest.Struct(t, alloc,
		columnartest.Field("x", types.KindInt64, xVals...),
		columnartest.Field("y", types.KindUTF8, yVals...),
	)
	require.NoError(t, w.Append(t.Context(), input))

	l, err := w.Flush(t.Context())
	require.NoError(t, err)

	r, err := layout.NewReader(alloc, l, store)
	require.NoError(t, err)
	return r
}
