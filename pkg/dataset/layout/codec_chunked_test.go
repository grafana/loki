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

func TestCodec_Chunked(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	spec := &layout.SpecChunked{
		MaxRowCount: 100,
		Chunk: &layout.SpecArray{
			Spec: &array.SpecPlain{Validity: nil},
		},
	}

	w, err := layout.NewWriter(&alloc, &store, spec, &types.Int64{Nullable: false})
	require.NoError(t, err)

	expect := columnartest.Array(t, types.KindInt64, &alloc,
		int64(10), int64(20), int64(30), int64(40),
	)
	require.NoError(t, w.Append(t.Context(), expect))

	l, err := w.Flush(t.Context())
	require.NoError(t, err)

	r, err := layout.NewReader(&alloc, l, &store)
	require.NoError(t, err)
	defer r.Close()

	n, err := r.Next(t.Context(), math.MaxInt)
	require.NoError(t, err)
	require.Equal(t, 4, n)

	actual, err := r.Project(t.Context(), &alloc, &expr.Identity{}, memory.Bitmap{})
	require.NoError(t, err)
	columnartest.RequireArraysEqual(t, expect, actual, memory.Bitmap{})
}

func TestCodec_Chunked_Empty(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	spec := &layout.SpecChunked{
		Chunk: &layout.SpecArray{
			Spec: &array.SpecPlain{Validity: nil},
		},
	}

	w, err := layout.NewWriter(&alloc, &store, spec, &types.Int64{Nullable: false})
	require.NoError(t, err)

	l, err := w.Flush(t.Context())
	require.NoError(t, err)

	r, err := layout.NewReader(&alloc, l, &store)
	require.NoError(t, err)
	defer r.Close()

	n, err := r.Next(t.Context(), math.MaxInt)
	require.ErrorIs(t, err, io.EOF)
	require.Equal(t, 0, n)
}

func TestCodec_Chunked_MultiChunk(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	spec := &layout.SpecChunked{
		MaxRowCount: 3,
		Chunk: &layout.SpecArray{
			Spec: &array.SpecPlain{Validity: nil},
		},
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
	defer r.Close()

	n, err := r.Next(t.Context(), math.MaxInt)
	require.NoError(t, err)
	require.Equal(t, 6, n)

	actual, err := r.Project(t.Context(), &alloc, &expr.Identity{}, memory.Bitmap{})
	require.NoError(t, err)
	columnartest.RequireArraysEqual(t, input, actual, memory.Bitmap{})
}

func TestCodec_Chunked_Windowing(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	spec := &layout.SpecChunked{
		MaxRowCount: 3,
		Chunk: &layout.SpecArray{
			Spec: &array.SpecPlain{Validity: nil},
		},
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
	defer r.Close()

	// Window 1: 4 rows spanning chunk boundary [10,20,30,40].
	n, err := r.Next(t.Context(), 4)
	require.NoError(t, err)
	require.Equal(t, 4, n)

	actual, err := r.Project(t.Context(), &alloc, &expr.Identity{}, memory.Bitmap{})
	require.NoError(t, err)
	columnartest.RequireArraysEqual(t,
		columnartest.Array(t, types.KindInt64, &alloc, int64(10), int64(20), int64(30), int64(40)),
		actual,
		memory.Bitmap{},
	)

	// Window 2: remaining 2 rows [50,60].
	n, err = r.Next(t.Context(), 4)
	require.NoError(t, err)
	require.Equal(t, 2, n)

	actual, err = r.Project(t.Context(), &alloc, &expr.Identity{}, memory.Bitmap{})
	require.NoError(t, err)
	columnartest.RequireArraysEqual(t,
		columnartest.Array(t, types.KindInt64, &alloc, int64(50), int64(60)),
		actual,
		memory.Bitmap{},
	)

	// Window 3: EOF.
	n, err = r.Next(t.Context(), 4)
	require.ErrorIs(t, err, io.EOF)
	require.Equal(t, 0, n)
}

func TestCodec_Chunked_Filter(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	spec := &layout.SpecChunked{
		MaxRowCount: 3,
		Chunk: &layout.SpecArray{
			Spec: &array.SpecPlain{Validity: nil},
		},
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
	defer r.Close()

	n, err := r.Next(t.Context(), 4)
	require.NoError(t, err)
	require.Equal(t, 4, n)

	filterExpr := &expr.Binary{
		Left:  &expr.Identity{},
		Op:    expr.BinaryOpGT,
		Right: &expr.Constant{Value: &columnar.NumberScalar[int64]{Value: 25}},
	}

	mask, err := r.Filter(t.Context(), &alloc, filterExpr, memory.Bitmap{})
	require.NoError(t, err)

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

func TestCodec_Chunked_Filter_WithInputMask(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	spec := &layout.SpecChunked{
		MaxRowCount: 3,
		Chunk: &layout.SpecArray{
			Spec: &array.SpecPlain{Validity: nil},
		},
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
	defer r.Close()

	n, err := r.Next(t.Context(), 4)
	require.NoError(t, err)
	require.Equal(t, 4, n)

	filterExpr := &expr.Binary{
		Left:  &expr.Identity{},
		Op:    expr.BinaryOpGT,
		Right: &expr.Constant{Value: &columnar.NumberScalar[int64]{Value: 15}},
	}

	inputMask := memory.NewBitmap(&alloc, 4)
	inputMask.AppendValues(true, false, true, false)

	mask, err := r.Filter(t.Context(), &alloc, filterExpr, inputMask)
	require.NoError(t, err)

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

func TestCodec_Chunked_SplitByMaxRowCount(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	spec := &layout.SpecChunked{
		MaxRowCount: 3,
		Chunk: &layout.SpecArray{
			Spec: &array.SpecPlain{Validity: nil},
		},
	}

	w, err := layout.NewWriter(&alloc, &store, spec, &types.Int64{Nullable: false})
	require.NoError(t, err)

	// Single 7-element array, should split into 3+3+1.
	input := columnartest.Array(t, types.KindInt64, &alloc,
		int64(1), int64(2), int64(3), int64(4), int64(5), int64(6), int64(7),
	)
	require.NoError(t, w.Append(t.Context(), input))

	l, err := w.Flush(t.Context())
	require.NoError(t, err)

	cl := l.(*layout.Chunked)
	require.Equal(t, 3, len(cl.Chunks), "expected 3 chunks (3+3+1)")

	r, err := layout.NewReader(&alloc, l, &store)
	require.NoError(t, err)
	defer r.Close()

	n, err := r.Next(t.Context(), math.MaxInt)
	require.NoError(t, err)
	require.Equal(t, 7, n)

	actual, err := r.Project(t.Context(), &alloc, &expr.Identity{}, memory.Bitmap{})
	require.NoError(t, err)
	columnartest.RequireArraysEqual(t, input, actual, memory.Bitmap{})
}

func TestCodec_Chunked_SplitBySizeHint(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	// Int64 Size() = 8 bytes/element. SizeHint of 16 should fit 2 elements.
	spec := &layout.SpecChunked{
		SizeHint: 16,
		Chunk: &layout.SpecArray{
			Spec: &array.SpecPlain{Validity: nil},
		},
	}

	w, err := layout.NewWriter(&alloc, &store, spec, &types.Int64{Nullable: false})
	require.NoError(t, err)

	input := columnartest.Array(t, types.KindInt64, &alloc,
		int64(1), int64(2), int64(3), int64(4), int64(5), int64(6),
	)
	require.NoError(t, w.Append(t.Context(), input))

	l, err := w.Flush(t.Context())
	require.NoError(t, err)

	cl := l.(*layout.Chunked)
	require.Equal(t, 3, len(cl.Chunks), "expected 3 chunks (2+2+2)")

	r, err := layout.NewReader(&alloc, l, &store)
	require.NoError(t, err)
	defer r.Close()

	n, err := r.Next(t.Context(), math.MaxInt)
	require.NoError(t, err)
	require.Equal(t, 6, n)

	actual, err := r.Project(t.Context(), &alloc, &expr.Identity{}, memory.Bitmap{})
	require.NoError(t, err)
	columnartest.RequireArraysEqual(t, input, actual, memory.Bitmap{})
}

func TestCodec_Chunked_PartialThenOversized(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	spec := &layout.SpecChunked{
		MaxRowCount: 5,
		Chunk: &layout.SpecArray{
			Spec: &array.SpecPlain{Validity: nil},
		},
	}

	w, err := layout.NewWriter(&alloc, &store, spec, &types.Int64{Nullable: false})
	require.NoError(t, err)

	// First append: 3 rows, partially fills chunk.
	first := columnartest.Array(t, types.KindInt64, &alloc,
		int64(1), int64(2), int64(3),
	)
	require.NoError(t, w.Append(t.Context(), first))

	// Second append: 10 rows, oversized.
	// Expect: first chunk gets 2 more (total 5), then 5, then 3.
	second := columnartest.Array(t, types.KindInt64, &alloc,
		int64(4), int64(5), int64(6), int64(7), int64(8),
		int64(9), int64(10), int64(11), int64(12), int64(13),
	)
	require.NoError(t, w.Append(t.Context(), second))

	l, err := w.Flush(t.Context())
	require.NoError(t, err)

	cl := l.(*layout.Chunked)
	require.Equal(t, 3, len(cl.Chunks), "expected 3 chunks (5+5+3)")

	r, err := layout.NewReader(&alloc, l, &store)
	require.NoError(t, err)
	defer r.Close()

	n, err := r.Next(t.Context(), math.MaxInt)
	require.NoError(t, err)
	require.Equal(t, 13, n)

	expected := columnartest.Array(t, types.KindInt64, &alloc,
		int64(1), int64(2), int64(3), int64(4), int64(5),
		int64(6), int64(7), int64(8), int64(9), int64(10),
		int64(11), int64(12), int64(13),
	)
	actual, err := r.Project(t.Context(), &alloc, &expr.Identity{}, memory.Bitmap{})
	require.NoError(t, err)
	columnartest.RequireArraysEqual(t, expected, actual, memory.Bitmap{})
}

func TestCodec_Chunked_ExactBoundary(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	spec := &layout.SpecChunked{
		MaxRowCount: 4,
		Chunk: &layout.SpecArray{
			Spec: &array.SpecPlain{Validity: nil},
		},
	}

	w, err := layout.NewWriter(&alloc, &store, spec, &types.Int64{Nullable: false})
	require.NoError(t, err)

	input := columnartest.Array(t, types.KindInt64, &alloc,
		int64(1), int64(2), int64(3), int64(4),
	)
	require.NoError(t, w.Append(t.Context(), input))

	l, err := w.Flush(t.Context())
	require.NoError(t, err)

	cl := l.(*layout.Chunked)
	require.Equal(t, 1, len(cl.Chunks), "exact fit should produce 1 chunk")

	r, err := layout.NewReader(&alloc, l, &store)
	require.NoError(t, err)
	defer r.Close()

	n, err := r.Next(t.Context(), math.MaxInt)
	require.NoError(t, err)
	require.Equal(t, 4, n)

	actual, err := r.Project(t.Context(), &alloc, &expr.Identity{}, memory.Bitmap{})
	require.NoError(t, err)
	columnartest.RequireArraysEqual(t, input, actual, memory.Bitmap{})
}

func TestCodec_Chunked_AppendBuffers(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	spec := &layout.SpecChunked{
		MaxRowCount: 3,
		Chunk: &layout.SpecArray{
			Spec: &array.SpecPlain{Validity: nil},
		},
	}

	w, err := layout.NewWriter(&alloc, &store, spec, &types.Int64{Nullable: false})
	require.NoError(t, err)

	// Two separate appends to ensure two distinct chunks are flushed.
	chunk1 := columnartest.Array(t, types.KindInt64, &alloc,
		int64(10), int64(20), int64(30),
	)
	require.NoError(t, w.Append(t.Context(), chunk1))

	chunk2 := columnartest.Array(t, types.KindInt64, &alloc,
		int64(40), int64(50), int64(60),
	)
	require.NoError(t, w.Append(t.Context(), chunk2))

	l, err := w.Flush(t.Context())
	require.NoError(t, err)

	r, err := layout.NewReader(&alloc, l, &store)
	require.NoError(t, err)
	defer r.Close()

	n, err := r.Next(t.Context(), 6)
	require.NoError(t, err)
	require.Equal(t, 6, n)

	bufs, err := r.AppendBuffers(nil, nil, &expr.Identity{}, memory.Bitmap{})
	require.NoError(t, err)
	require.Greater(t, len(bufs), 1)
}

func TestCodec_Chunked_Prune(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	spec := &layout.SpecChunked{
		MaxRowCount: 3,
		Chunk: &layout.SpecArray{
			Spec: &array.SpecPlain{Validity: nil},
		},
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
	defer r.Close()

	n, err := r.Next(t.Context(), 4)
	require.NoError(t, err)
	require.Equal(t, 4, n)

	mask, err := r.Prune(t.Context(), &alloc, nil, memory.Bitmap{})
	require.NoError(t, err)
	require.Equal(t, 0, mask.Len())

	inputMask := memory.NewBitmap(&alloc, 4)
	inputMask.AppendValues(true, false, true, false)

	mask, err = r.Prune(t.Context(), &alloc, nil, inputMask)
	require.NoError(t, err)
	require.Equal(t, 4, mask.Len())

	for i := range 4 {
		require.Equal(t, inputMask.Get(i), mask.Get(i), "bit %d", i)
	}
}

func TestCodec_Chunked_ProjectAllRows(t *testing.T) {
	var alloc memory.Allocator
	var store buffer.MemoryStore

	spec := &layout.SpecChunked{
		MaxRowCount: 3,
		Chunk: &layout.SpecArray{
			Spec: &array.SpecPlain{Validity: nil},
		},
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
	defer r.Close()

	n, err := r.Next(t.Context(), 4)
	require.NoError(t, err)
	require.Equal(t, 4, n)

	allSet := memory.NewBitmap(&alloc, 4)
	allSet.AppendCount(true, 4)

	projected, err := r.Project(t.Context(), &alloc, &expr.Identity{}, allSet)
	require.NoError(t, err)
	filtered, err := compute.Filter(&alloc, projected, allSet)
	require.NoError(t, err)
	columnartest.RequireArraysEqual(t,
		columnartest.Array(t, types.KindInt64, &alloc, int64(10), int64(20), int64(30), int64(40)),
		filtered.(columnar.Array),
		memory.Bitmap{},
	)
}
