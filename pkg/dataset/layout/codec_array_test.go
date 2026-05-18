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

func TestCodec_Array(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	spec := &layout.SpecArray{
		Spec: &array.SpecBool{Validity: nil},
	}

	w, err := layout.NewWriter(&alloc, &store, spec, &types.Bool{Nullable: false})
	require.NoError(t, err)

	expect := columnartest.Array(t, types.KindBool, &alloc, true, false, true, true)
	require.NoError(t, w.Append(t.Context(), expect))

	l, err := w.Flush(t.Context())
	require.NoError(t, err)

	r, err := layout.NewReader(&alloc, l, &store)
	require.NoError(t, err)

	// Advance the reader to get everything.
	n, err := r.Next(t.Context(), math.MaxInt)
	require.NoError(t, err)
	require.Equal(t, 4, n)
	actual, err := r.Project(t.Context(), &alloc, &expr.Identity{}, memory.Bitmap{})
	require.NoError(t, err)

	columnartest.RequireArraysEqual(t, expect, actual, memory.Bitmap{})
}

func TestCodec_Array_Windowing(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	spec := &layout.SpecArray{
		Spec: &array.SpecPlain{Validity: nil},
	}

	w, err := layout.NewWriter(&alloc, &store, spec, &types.Int64{Nullable: false})
	require.NoError(t, err)

	input := columnartest.Array(t, types.KindInt64, &alloc,
		int64(10), int64(20), int64(30), int64(40), int64(50), int64(60),
	)
	require.NoError(t, w.Append(t.Context(), input))

	l, err := w.Flush(t.Context())
	require.NoError(t, err)

	r, err := layout.NewReader(&alloc, l, &store)
	require.NoError(t, err)

	// First window: 2 rows.
	n, err := r.Next(t.Context(), 2)
	require.NoError(t, err)
	require.Equal(t, 2, n)

	actual, err := r.Project(t.Context(), &alloc, &expr.Identity{}, memory.Bitmap{})
	require.NoError(t, err)
	columnartest.RequireArraysEqual(t,
		columnartest.Array(t, types.KindInt64, &alloc, int64(10), int64(20)),
		actual,
		memory.Bitmap{},
	)

	// Second window: 2 rows.
	n, err = r.Next(t.Context(), 2)
	require.NoError(t, err)
	require.Equal(t, 2, n)

	actual, err = r.Project(t.Context(), &alloc, &expr.Identity{}, memory.Bitmap{})
	require.NoError(t, err)
	columnartest.RequireArraysEqual(t,
		columnartest.Array(t, types.KindInt64, &alloc, int64(30), int64(40)),
		actual,
		memory.Bitmap{},
	)

	// Third window: 2 remaining rows.
	n, err = r.Next(t.Context(), 2)
	require.NoError(t, err)
	require.Equal(t, 2, n)

	actual, err = r.Project(t.Context(), &alloc, &expr.Identity{}, memory.Bitmap{})
	require.NoError(t, err)
	columnartest.RequireArraysEqual(t,
		columnartest.Array(t, types.KindInt64, &alloc, int64(50), int64(60)),
		actual,
		memory.Bitmap{},
	)

	// Fourth call: EOF.
	n, err = r.Next(t.Context(), 2)
	require.ErrorIs(t, err, io.EOF)
	require.Equal(t, 0, n)
}

func TestCodec_Array_Filter(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	spec := &layout.SpecArray{
		Spec: &array.SpecPlain{Validity: nil},
	}

	w, err := layout.NewWriter(&alloc, &store, spec, &types.Int64{Nullable: false})
	require.NoError(t, err)

	input := columnartest.Array(t, types.KindInt64, &alloc,
		int64(10), int64(20), int64(30), int64(40),
	)
	require.NoError(t, w.Append(t.Context(), input))

	l, err := w.Flush(t.Context())
	require.NoError(t, err)

	r, err := layout.NewReader(&alloc, l, &store)
	require.NoError(t, err)

	n, err := r.Next(t.Context(), math.MaxInt)
	require.NoError(t, err)
	require.Equal(t, 4, n)

	// Filter: Identity > 20 → keeps rows where value > 20.
	filterExpr := &expr.Binary{
		Left:  &expr.Identity{},
		Op:    expr.BinaryOpGT,
		Right: &expr.Constant{Value: &columnar.NumberScalar[int64]{Value: 20}},
	}

	mask, err := r.Filter(t.Context(), &alloc, filterExpr, memory.Bitmap{})
	require.NoError(t, err)

	// Project with the filter mask; Project returns full window, so
	// apply compute.Filter to compact.
	projected, err := r.Project(t.Context(), &alloc, &expr.Identity{}, mask)
	require.NoError(t, err)
	filtered, err := compute.Filter(&alloc, projected, mask)
	require.NoError(t, err)
	columnartest.RequireArraysEqual(t,
		columnartest.Array(t, types.KindInt64, &alloc, int64(30), int64(40)),
		filtered.(columnar.Array),
		memory.Bitmap{},
	)
}

func TestCodec_Array_Filter_WithInputMask(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	spec := &layout.SpecArray{
		Spec: &array.SpecPlain{Validity: nil},
	}

	w, err := layout.NewWriter(&alloc, &store, spec, &types.Int64{Nullable: false})
	require.NoError(t, err)

	input := columnartest.Array(t, types.KindInt64, &alloc,
		int64(10), int64(20), int64(30), int64(40),
	)
	require.NoError(t, w.Append(t.Context(), input))

	l, err := w.Flush(t.Context())
	require.NoError(t, err)

	r, err := layout.NewReader(&alloc, l, &store)
	require.NoError(t, err)

	n, err := r.Next(t.Context(), math.MaxInt)
	require.NoError(t, err)
	require.Equal(t, 4, n)

	// Filter: Identity > 15 → would match rows 1,2,3 (values 20,30,40).
	// But provide an input mask that only includes rows 0 and 2.
	filterExpr := &expr.Binary{
		Left:  &expr.Identity{},
		Op:    expr.BinaryOpGT,
		Right: &expr.Constant{Value: &columnar.NumberScalar[int64]{Value: 15}},
	}

	inputMask := memory.NewBitmap(&alloc, 4)
	inputMask.AppendValues(true, false, true, false)

	mask, err := r.Filter(t.Context(), &alloc, filterExpr, inputMask)
	require.NoError(t, err)

	// Project with the intersected mask → only row 2 (value 30).
	projected, err := r.Project(t.Context(), &alloc, &expr.Identity{}, mask)
	require.NoError(t, err)
	filtered, err := compute.Filter(&alloc, projected, mask)
	require.NoError(t, err)
	columnartest.RequireArraysEqual(t,
		columnartest.Array(t, types.KindInt64, &alloc, int64(30)),
		filtered.(columnar.Array),
		memory.Bitmap{},
	)
}

func TestCodec_Array_Filter_NullsDoNotLeak(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	spec := &layout.SpecArray{
		Spec: &array.SpecPlain{Validity: &array.SpecBool{}},
	}

	w, err := layout.NewWriter(&alloc, &store, spec, &types.Int64{Nullable: true})
	require.NoError(t, err)

	// Rows 1 and 3 are null. NumberBuilder.AppendNull writes the zero value
	// for the backing storage, so the predicate "Identity > -5" evaluates to
	// true at those positions in the compute kernel's values bitmap.
	input := columnartest.Array(t, types.KindInt64, &alloc,
		int64(10), nil, int64(30), nil,
	)
	require.NoError(t, w.Append(t.Context(), input))

	l, err := w.Flush(t.Context())
	require.NoError(t, err)

	r, err := layout.NewReader(&alloc, l, &store)
	require.NoError(t, err)

	n, err := r.Next(t.Context(), math.MaxInt)
	require.NoError(t, err)
	require.Equal(t, 4, n)

	// Identity > -5: backing zero for nulls also satisfies this.
	filterExpr := &expr.Binary{
		Left:  &expr.Identity{},
		Op:    expr.BinaryOpGT,
		Right: &expr.Constant{Value: &columnar.NumberScalar[int64]{Value: -5}},
	}

	mask, err := r.Filter(t.Context(), &alloc, filterExpr, memory.Bitmap{})
	require.NoError(t, err)

	require.Equal(t, 4, mask.Len())
	require.True(t, mask.Get(0), "row 0 (value 10) should pass")
	require.False(t, mask.Get(1), "row 1 (null) must not pass")
	require.True(t, mask.Get(2), "row 2 (value 30) should pass")
	require.False(t, mask.Get(3), "row 3 (null) must not pass")
}

func TestCodec_Array_ProjectAllRows(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	spec := &layout.SpecArray{
		Spec: &array.SpecPlain{Validity: nil},
	}

	w, err := layout.NewWriter(&alloc, &store, spec, &types.Int64{Nullable: false})
	require.NoError(t, err)

	input := columnartest.Array(t, types.KindInt64, &alloc,
		int64(1), int64(2), int64(3),
	)
	require.NoError(t, w.Append(t.Context(), input))

	l, err := w.Flush(t.Context())
	require.NoError(t, err)

	r, err := layout.NewReader(&alloc, l, &store)
	require.NoError(t, err)

	n, err := r.Next(t.Context(), math.MaxInt)
	require.NoError(t, err)
	require.Equal(t, 3, n)

	// Project with an all-set mask should return everything.
	allSet := memory.NewBitmap(&alloc, 3)
	allSet.AppendCount(true, 3)

	projected, err := r.Project(t.Context(), &alloc, &expr.Identity{}, allSet)
	require.NoError(t, err)
	filtered, err := compute.Filter(&alloc, projected, allSet)
	require.NoError(t, err)
	columnartest.RequireArraysEqual(t, input, filtered.(columnar.Array), memory.Bitmap{})
}
