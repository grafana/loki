package columnar_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/columnartest"
	"github.com/grafana/loki/v3/pkg/columnar/types"
	"github.com/grafana/loki/v3/pkg/memory"
)

func TestNewChunkedUTF8(t *testing.T) {
	t.Run("panics when a chunk has a null element", func(t *testing.T) {
		var alloc memory.Allocator
		chunks := []*columnar.UTF8{utf8Chunk(t, &alloc, "a", "b"), utf8ChunkWithNull(t, &alloc)}

		require.PanicsWithValue(t, "columnar: ChunkedUTF8 chunk has a null element", func() { columnar.NewChunkedUTF8(chunks) })
	})

	t.Run("panics when a chunk is nil", func(t *testing.T) {
		var alloc memory.Allocator
		chunks := []*columnar.UTF8{utf8Chunk(t, &alloc, "a"), nil}

		require.PanicsWithValue(t, "columnar: ChunkedUTF8 chunk is nil", func() { columnar.NewChunkedUTF8(chunks) })
	})

	t.Run("reads as an empty array when there is no chunk", func(t *testing.T) {
		view := columnar.NewChunkedUTF8(nil)

		require.Equal(t, 0, view.Len())
		require.Equal(t, 0, view.DataLen())
		require.Equal(t, 0, view.NumChunks())
	})

	t.Run("skips the empty chunks", func(t *testing.T) {
		var alloc memory.Allocator
		view := columnar.NewChunkedUTF8([]*columnar.UTF8{utf8Chunk(t, &alloc), utf8Chunk(t, &alloc, "a"), utf8Chunk(t, &alloc), utf8Chunk(t, &alloc, "b")})

		require.Equal(t, 2, view.NumChunks())
		require.Equal(t, 2, view.Len())
		require.Equal(t, "b", string(view.Get(1)))
	})

	t.Run("keeps reading the same values when the caller changes the slice of chunks", func(t *testing.T) {
		var alloc memory.Allocator
		chunks := []*columnar.UTF8{utf8Chunk(t, &alloc, "a"), utf8Chunk(t, &alloc, "b")}
		view := columnar.NewChunkedUTF8(chunks)

		chunks[0] = utf8Chunk(t, &alloc, "x", "y", "z")

		require.Equal(t, 2, view.Len())
		require.Equal(t, "a", string(view.Get(0)))
	})
}

func TestChunkedUTF8(t *testing.T) {
	t.Run("reads every element across the chunk boundaries", func(t *testing.T) {
		var alloc memory.Allocator
		view := columnar.NewChunkedUTF8([]*columnar.UTF8{
			utf8Chunk(t, &alloc, "hello", "world"),
			utf8Chunk(t, &alloc, "foo"),
			utf8Chunk(t, &alloc, "bar", "baz", "qux"),
		})

		want := []string{"hello", "world", "foo", "bar", "baz", "qux"}
		require.Equal(t, len(want), view.Len())
		for i, w := range want {
			require.Equal(t, w, string(view.Get(i)), "element %d", i)
		}
	})

	for _, numChunks := range []int{1, 2, 8, 9, 20} {
		t.Run(fmt.Sprintf("finds the chunk of every element and slices across chunks when there are %d chunks", numChunks), func(t *testing.T) {
			var alloc memory.Allocator
			var (
				chunks []*columnar.UTF8
				want   []string
			)
			for k := range numChunks {
				values := make([]string, k%4+2)
				for i := range values {
					values[i] = fmt.Sprintf("chunk-%d-value-%d", k, i)
				}
				want = append(want, values...)
				chunks = append(chunks, utf8Chunk(t, &alloc, values...))
			}

			view := columnar.NewChunkedUTF8(chunks)

			require.Equal(t, len(want), view.Len())
			require.Equal(t, numChunks, view.NumChunks())
			for i, w := range want {
				require.Equal(t, w, string(view.Get(i)), "element %d", i)
			}

			sliced := view.Slice(1, len(want)-1).(columnar.UTF8Reader)
			require.Equal(t, len(want)-2, sliced.Len())
			for i, w := range want[1 : len(want)-1] {
				require.Equal(t, w, string(sliced.Get(i)), "sliced element %d", i)
			}
		})
	}

	t.Run("reports the length, the data length and the size of its chunks", func(t *testing.T) {
		var alloc memory.Allocator
		first, second := utf8Chunk(t, &alloc, "hello", "world"), utf8Chunk(t, &alloc, "foo")
		view := columnar.NewChunkedUTF8([]*columnar.UTF8{first, second})

		require.Equal(t, 3, view.Len())
		require.Equal(t, 13, view.DataLen())
		require.Equal(t, first.Size()+second.Size(), view.Size())
	})

	t.Run("reports no null and the kind of a UTF8 array", func(t *testing.T) {
		var alloc memory.Allocator
		view := columnar.NewChunkedUTF8([]*columnar.UTF8{utf8Chunk(t, &alloc, "a"), utf8Chunk(t, &alloc, "b")})

		require.Equal(t, 0, view.Nulls())
		require.False(t, view.IsNull(1))
		validity := view.Validity()
		require.Equal(t, 0, validity.Len())
		require.Equal(t, types.KindUTF8, view.Kind())
	})

	t.Run("is an empty array when it is the zero value", func(t *testing.T) {
		var view columnar.ChunkedUTF8

		require.Equal(t, 0, view.Len())
		require.Equal(t, 0, view.DataLen())
		require.Equal(t, 0, view.Slice(0, 0).Len())
		require.PanicsWithValue(t, "columnar: index 0 out of range [0, 0)", func() { view.Get(0) })
	})

	t.Run("panics when the index is out of range", func(t *testing.T) {
		var alloc memory.Allocator
		view := columnar.NewChunkedUTF8([]*columnar.UTF8{utf8Chunk(t, &alloc, "a"), utf8Chunk(t, &alloc, "b")})

		require.PanicsWithValue(t, "columnar: index 2 out of range [0, 2)", func() { view.Get(2) })
		require.PanicsWithValue(t, "columnar: index -1 out of range [0, 2)", func() { view.Get(-1) })
		require.Panics(t, func() { view.Chunk(2) })
	})
}

func TestChunkedUTF8_Slice(t *testing.T) {
	newView := func(t *testing.T, alloc *memory.Allocator) *columnar.ChunkedUTF8 {
		return columnar.NewChunkedUTF8([]*columnar.UTF8{
			utf8Chunk(t, alloc, "a0", "a1", "a2"),
			utf8Chunk(t, alloc, "b0", "b1"),
			utf8Chunk(t, alloc, "c0", "c1", "c2"),
		})
	}

	t.Run("returns a UTF8 array when the elements lie in one chunk", func(t *testing.T) {
		var alloc memory.Allocator

		got := newView(t, &alloc).Slice(1, 3)

		require.IsType(t, &columnar.UTF8{}, got)
		columnartest.RequireArraysEqual(t, columnartest.Array(t, types.KindUTF8, &alloc, "a1", "a2"), got, memory.Bitmap{})
	})

	t.Run("returns a view over the cut chunks when the elements span chunks", func(t *testing.T) {
		var alloc memory.Allocator

		got := newView(t, &alloc).Slice(2, 7)

		require.IsType(t, &columnar.ChunkedUTF8{}, got)
		columnartest.RequireArraysEqual(t, columnartest.Array(t, types.KindUTF8, &alloc, "a2", "b0", "b1", "c0", "c1"), got, memory.Bitmap{})
	})

	t.Run("reports the data length of the cut chunks only", func(t *testing.T) {
		var alloc memory.Allocator

		got := newView(t, &alloc).Slice(2, 7).(columnar.UTF8Reader)

		require.Equal(t, 10, got.DataLen())
	})

	t.Run("returns an empty array when the slice is empty", func(t *testing.T) {
		var alloc memory.Allocator

		got := newView(t, &alloc).Slice(4, 4)

		require.Equal(t, 0, got.Len())
	})

	t.Run("returns the whole content when the slice covers the array", func(t *testing.T) {
		var alloc memory.Allocator
		view := newView(t, &alloc)

		columnartest.RequireArraysEqual(t, view, view.Slice(0, view.Len()), memory.Bitmap{})
	})

	t.Run("panics when the slice is out of range", func(t *testing.T) {
		var alloc memory.Allocator
		view := newView(t, &alloc)

		require.Panics(t, func() { view.Slice(3, 9) })
		require.Panics(t, func() { view.Slice(4, 3) })
		require.Panics(t, func() { view.Slice(-1, 3) })
	})
}

func TestConcatView(t *testing.T) {
	t.Run("returns a view over the inputs without copying their values", func(t *testing.T) {
		var alloc memory.Allocator
		first, second := utf8Chunk(t, &alloc, "a", "b"), utf8Chunk(t, &alloc, "c")

		got, err := columnar.ConcatView(&alloc, []columnar.Array{first, second})
		require.NoError(t, err)

		view, ok := got.(*columnar.ChunkedUTF8)
		require.True(t, ok, "got %T, want a view", got)
		require.Same(t, first, view.Chunk(0))
		require.Same(t, second, view.Chunk(1))
		columnartest.RequireArraysEqual(t, columnartest.Array(t, types.KindUTF8, &alloc, "a", "b", "c"), got, memory.Bitmap{})
	})

	t.Run("joins the chunks of an input view into the result", func(t *testing.T) {
		var alloc memory.Allocator
		inner := columnar.NewChunkedUTF8([]*columnar.UTF8{utf8Chunk(t, &alloc, "a"), utf8Chunk(t, &alloc, "b")})

		got, err := columnar.ConcatView(&alloc, []columnar.Array{inner, utf8Chunk(t, &alloc, "c")})
		require.NoError(t, err)

		view, ok := got.(*columnar.ChunkedUTF8)
		require.True(t, ok, "got %T, want a view", got)
		require.Equal(t, 3, view.NumChunks())
		columnartest.RequireArraysEqual(t, columnartest.Array(t, types.KindUTF8, &alloc, "a", "b", "c"), got, memory.Bitmap{})
	})

	t.Run("returns the only chunk when the other inputs are empty", func(t *testing.T) {
		var alloc memory.Allocator
		only := utf8Chunk(t, &alloc, "a", "b")

		got, err := columnar.ConcatView(&alloc, []columnar.Array{utf8Chunk(t, &alloc), only, utf8Chunk(t, &alloc)})
		require.NoError(t, err)

		require.Same(t, only, got)
	})

	t.Run("returns an empty array when every input is empty", func(t *testing.T) {
		var alloc memory.Allocator

		got, err := columnar.ConcatView(&alloc, []columnar.Array{utf8Chunk(t, &alloc), utf8Chunk(t, &alloc)})
		require.NoError(t, err)

		require.Equal(t, 0, got.Len())
		require.Equal(t, types.KindUTF8, got.Kind())
	})

	t.Run("returns an empty array when the only input is an empty view", func(t *testing.T) {
		var alloc memory.Allocator

		got, err := columnar.ConcatView(&alloc, []columnar.Array{&columnar.ChunkedUTF8{}})
		require.NoError(t, err)

		require.Equal(t, 0, got.Len())
		require.Equal(t, types.KindUTF8, got.Kind())
	})

	t.Run("returns nil when there is no input", func(t *testing.T) {
		var alloc memory.Allocator

		got, err := columnar.ConcatView(&alloc, nil)
		require.NoError(t, err)

		require.Nil(t, got)
	})

	t.Run("copies the values when an input has a null element", func(t *testing.T) {
		var alloc memory.Allocator
		in := []columnar.Array{utf8Chunk(t, &alloc, "a"), columnartest.Array(t, types.KindUTF8, &alloc, "b", nil)}

		got, err := columnar.ConcatView(&alloc, in)
		require.NoError(t, err)

		require.IsType(t, &columnar.UTF8{}, got)
		columnartest.RequireArraysEqual(t, columnartest.Array(t, types.KindUTF8, &alloc, "a", "b", nil), got, memory.Bitmap{})
	})

	t.Run("copies the values of arrays that are not UTF8", func(t *testing.T) {
		var alloc memory.Allocator
		in := []columnar.Array{
			columnartest.Array(t, types.KindInt64, &alloc, 1, 2),
			columnartest.Array(t, types.KindInt64, &alloc, 3),
		}

		got, err := columnar.ConcatView(&alloc, in)
		require.NoError(t, err)

		columnartest.RequireArraysEqual(t, columnartest.Array(t, types.KindInt64, &alloc, 1, 2, 3), got, memory.Bitmap{})
	})

	t.Run("returns an error when the inputs are of different kinds", func(t *testing.T) {
		var alloc memory.Allocator
		numbers := columnartest.Array(t, types.KindInt64, &alloc, 1)
		strings := utf8Chunk(t, &alloc, "a")

		_, err := columnar.ConcatView(&alloc, []columnar.Array{strings, numbers})
		require.Error(t, err)

		_, err = columnar.ConcatView(&alloc, []columnar.Array{numbers, strings})
		require.Error(t, err)
	})
}

func TestFlatten(t *testing.T) {
	t.Run("copies a view into one UTF8 array with the same values", func(t *testing.T) {
		var alloc memory.Allocator
		view := columnar.NewChunkedUTF8([]*columnar.UTF8{utf8Chunk(t, &alloc, "a", "b"), utf8Chunk(t, &alloc, "c")})

		got, err := columnar.Flatten(&alloc, view)
		require.NoError(t, err)

		require.IsType(t, &columnar.UTF8{}, got)
		columnartest.RequireArraysEqual(t, columnartest.Array(t, types.KindUTF8, &alloc, "a", "b", "c"), got, memory.Bitmap{})
	})

	t.Run("returns an empty array when the view has no chunk", func(t *testing.T) {
		var alloc memory.Allocator

		got, err := columnar.Flatten(&alloc, &columnar.ChunkedUTF8{})
		require.NoError(t, err)

		require.IsType(t, &columnar.UTF8{}, got)
		require.Equal(t, 0, got.Len())
	})

	t.Run("copies a slice of a view that starts inside a chunk", func(t *testing.T) {
		var alloc memory.Allocator
		view := columnar.NewChunkedUTF8([]*columnar.UTF8{utf8Chunk(t, &alloc, "a0", "a1", "a2"), utf8Chunk(t, &alloc, "b0", "b1")})

		got, err := columnar.Flatten(&alloc, view.Slice(1, 5))
		require.NoError(t, err)

		require.IsType(t, &columnar.UTF8{}, got)
		columnartest.RequireArraysEqual(t, columnartest.Array(t, types.KindUTF8, &alloc, "a1", "a2", "b0", "b1"), got, memory.Bitmap{})
	})

	t.Run("returns an array that is not a view unchanged", func(t *testing.T) {
		var alloc memory.Allocator
		arr := utf8Chunk(t, &alloc, "a")

		got, err := columnar.Flatten(&alloc, arr)
		require.NoError(t, err)

		require.Same(t, arr, got)
	})
}

func utf8Chunk(t testing.TB, alloc *memory.Allocator, values ...string) *columnar.UTF8 {
	t.Helper()

	args := make([]any, len(values))
	for i, v := range values {
		args[i] = v
	}
	return columnartest.Array(t, types.KindUTF8, alloc, args...).(*columnar.UTF8)
}

func utf8ChunkWithNull(t testing.TB, alloc *memory.Allocator) *columnar.UTF8 {
	t.Helper()

	return columnartest.Array(t, types.KindUTF8, alloc, "a", nil).(*columnar.UTF8)
}
