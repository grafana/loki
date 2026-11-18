package dataset

import (
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
)

func TestNewSelectionAllRows(t *testing.T) {
	t.Run("returns a selection that selects every row of n rows", func(t *testing.T) {
		sel := newSelectionAllRows(8)

		require.True(t, sel.isAllRows())
		require.Equal(t, 8, sel.n)
		require.Equal(t, 8, sel.count())
	})
}

func TestNewSelection(t *testing.T) {
	var alloc memory.Allocator

	t.Run("returns the selection of every row when value is true", func(t *testing.T) {
		sel := newSelection(&alloc, 8, true)

		require.True(t, sel.isAllRows())
		require.Equal(t, 8, sel.n)
		require.Equal(t, 8, sel.count())
	})

	t.Run("returns a selection of n rows that selects no row when value is false", func(t *testing.T) {
		sel := newSelection(&alloc, 8, false)

		require.False(t, sel.isAllRows())
		require.Equal(t, 8, sel.n)
		require.Equal(t, 8, sel.bits.Len())
		require.True(t, sel.empty())
	})
}

func TestSelection_empty(t *testing.T) {
	var alloc memory.Allocator

	lengths := []int{1, 7, 8, 9, 15, 16, 17, 63, 64, 65, 72, 73, 130, 8192}

	t.Run("returns true when the selection selects no row", func(t *testing.T) {
		for _, n := range lengths {
			require.True(t, newSelection(&alloc, n, false).empty(), "length %d", n)
		}
	})

	t.Run("returns false when the selection selects one row, wherever it is", func(t *testing.T) {
		for _, n := range lengths {
			for _, row := range []int{0, 1, n / 2, n - 2, n - 1} {
				if row < 0 || row >= n {
					continue
				}

				require.False(t, selectionOf(&alloc, n, row).empty(), "length %d, row %d", n, row)
			}
		}
	})

	t.Run("returns false when the selection selects every row with a bitmap", func(t *testing.T) {
		for _, n := range lengths {
			require.False(t, fullBitmapSelection(&alloc, n).empty(), "length %d", n)
		}
	})

	t.Run("returns true for a slice that has set bits only outside its rows", func(t *testing.T) {
		for _, start := range []int{0, 1, 3, 7, 8, 9, 13} {
			for _, length := range []int{1, 2, 5, 8, 11, 64, 77} {
				full := fullBitmapSelection(&alloc, start+length+20)
				full.bits.SetRange(start, start+length, false)

				sel := full.slice(start, start+length)
				require.True(t, sel.empty(), "start %d, length %d", start, length)
			}
		}
	})

	t.Run("returns false for a slice that selects only its first or last row", func(t *testing.T) {
		for _, start := range []int{0, 1, 3, 7, 8, 9, 13} {
			for _, length := range []int{1, 2, 5, 8, 11, 64, 77} {
				for _, row := range []int{0, length - 1} {
					full := selectionOf(&alloc, start+length+20, start+row)

					sel := full.slice(start, start+length)
					require.False(t, sel.empty(), "start %d, length %d, row %d", start, length, row)
				}
			}
		}
	})

	t.Run("returns whether the selection of every row has any row to select", func(t *testing.T) {
		require.False(t, newSelectionAllRows(5).empty())
		require.True(t, newSelectionAllRows(0).empty())
	})

	t.Run("gives the same answer as count for random selections with and without an offset", func(t *testing.T) {
		rng := rand.New(rand.NewPCG(1, 2))

		for trial := range 5000 {
			var (
				offset = rng.IntN(20)
				length = 1 + rng.IntN(300)
				tail   = rng.IntN(20)
			)

			full := newSelection(&alloc, offset+length+tail, false)
			for range rng.IntN(4) {
				full.set(rng.IntN(offset + length + tail))
			}
			sel := full.slice(offset, offset+length)

			require.Equal(t, sel.count() == 0, sel.empty(), "trial %d", trial)
		}
	})
}

func TestSelection_count(t *testing.T) {
	var alloc memory.Allocator

	t.Run("returns the number of rows that the selection selects", func(t *testing.T) {
		require.Equal(t, 3, selectionOf(&alloc, 8, 1, 4, 7).count())
		require.Equal(t, 0, selectionOf(&alloc, 8).count())
	})

	t.Run("returns the number of rows of the record batch for the selection of every row", func(t *testing.T) {
		require.Equal(t, 8, newSelectionAllRows(8).count())
	})
}

func TestSelection_forEach(t *testing.T) {
	var alloc memory.Allocator

	collect := func(sel selection) []int {
		var rows []int
		sel.forEach(func(i int) { rows = append(rows, i) })
		return rows
	}

	t.Run("calls fn for each selected row in ascending order", func(t *testing.T) {
		require.Equal(t, []int{1, 4, 7}, collect(selectionOf(&alloc, 8, 7, 1, 4)))
	})

	t.Run("calls fn for every row for the selection of every row", func(t *testing.T) {
		require.Equal(t, []int{0, 1, 2, 3}, collect(newSelectionAllRows(4)))
	})

	t.Run("does not call fn when the selection selects no row", func(t *testing.T) {
		require.Empty(t, collect(selectionOf(&alloc, 8)))
	})
}

func TestSelection_unselectedRuns(t *testing.T) {
	var alloc memory.Allocator

	collect := func(sel selection) [][2]int {
		var runs [][2]int
		for start, end := range sel.unselectedRuns() {
			runs = append(runs, [2]int{start, end})
		}
		return runs
	}

	t.Run("returns the runs of consecutive rows that the selection does not select", func(t *testing.T) {
		sel := selectionOf(&alloc, 10, 2, 3, 7)

		require.Equal(t, [][2]int{{0, 2}, {4, 7}, {8, 10}}, collect(sel))
	})

	t.Run("returns runs of one row between selected rows", func(t *testing.T) {
		sel := selectionOf(&alloc, 5, 0, 2, 4)

		require.Equal(t, [][2]int{{1, 2}, {3, 4}}, collect(sel))
	})

	t.Run("returns one run of every row when the selection selects no row", func(t *testing.T) {
		require.Equal(t, [][2]int{{0, 5}}, collect(selectionOf(&alloc, 5)))
	})

	t.Run("returns no run when a bitmap selects every row", func(t *testing.T) {
		require.Empty(t, collect(fullBitmapSelection(&alloc, 5)))
	})

	t.Run("returns no run for the selection of every row", func(t *testing.T) {
		require.Empty(t, collect(newSelectionAllRows(5)))
	})

	t.Run("stops when the loop body stops", func(t *testing.T) {
		sel := selectionOf(&alloc, 10, 2, 3, 7)

		var runs [][2]int
		for start, end := range sel.unselectedRuns() {
			runs = append(runs, [2]int{start, end})
			break
		}
		require.Equal(t, [][2]int{{0, 2}}, runs)
	})
}

func TestSelection_slice(t *testing.T) {
	var alloc memory.Allocator

	t.Run("returns the rows of the range, numbered from zero", func(t *testing.T) {
		got := selectionOf(&alloc, 10, 1, 4, 5, 9).slice(3, 8)

		require.Equal(t, 5, got.n)
		require.Equal(t, []int{1, 2}, selectedRows(got))
	})

	t.Run("returns a selection of no row when the range holds no selected row", func(t *testing.T) {
		got := selectionOf(&alloc, 10, 1, 9).slice(3, 8)

		require.Equal(t, 5, got.n)
		require.False(t, got.isAllRows())
		require.True(t, got.empty())
	})

	t.Run("returns a selection of every row of the range when the selection selects every row", func(t *testing.T) {
		got := newSelectionAllRows(10).slice(3, 8)

		require.True(t, got.isAllRows())
		require.Equal(t, 5, got.n)
	})
}

func TestSelection_filterArray(t *testing.T) {
	var alloc memory.Allocator

	t.Run("returns the elements of the selected rows", func(t *testing.T) {
		arr := columnar.NewNumber([]int64{10, 20, 30, 40}, memory.Bitmap{})

		got, err := selectionOf(&alloc, 4, 1, 3).filterArray(&alloc, arr)
		require.NoError(t, err)
		require.Equal(t, []int64{20, 40}, got.(*columnar.Number[int64]).Values())
	})

	t.Run("returns the binary values of the selected rows", func(t *testing.T) {
		arr := columnar.NewUTF8([]byte("abbcccdddd"), []int32{0, 1, 3, 6, 10}, memory.Bitmap{})

		got, err := selectionOf(&alloc, 4, 0, 2).filterArray(&alloc, arr)
		require.NoError(t, err)

		values, ok := got.(*columnar.UTF8)
		require.True(t, ok)
		require.Equal(t, 2, values.Len())
		require.Equal(t, "a", string(values.Get(0)))
		require.Equal(t, "ccc", string(values.Get(1)))
	})

	t.Run("keeps the nulls of the selected rows", func(t *testing.T) {
		arr := columnar.NewNumber([]int64{10, 20, 30, 40}, bitmapOf(&alloc, true, false, true, true))

		got, err := selectionOf(&alloc, 4, 1, 2).filterArray(&alloc, arr)
		require.NoError(t, err)
		require.Equal(t, 2, got.Len())
		require.True(t, got.IsNull(0))
		require.False(t, got.IsNull(1))
		require.Equal(t, int64(30), got.(*columnar.Number[int64]).Values()[1])
	})

	t.Run("returns an error when the selection has a different number of rows than the array", func(t *testing.T) {
		arr := columnar.NewNumber([]int64{10, 20, 30, 40}, memory.Bitmap{})

		_, err := selectionOf(&alloc, 3, 1).filterArray(&alloc, arr)
		require.EqualError(t, err, "mask length 3 does not match input length 4")
	})

	for _, tc := range []struct {
		name string
		sel  selection
	}{
		{"returns a copy that shares no memory with the array when the selection has no bitmap", newSelectionAllRows(3)},
		{"returns a copy that shares no memory with the array when a bitmap selects every row", fullBitmapSelection(&alloc, 3)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			arr := columnar.NewNumber([]int64{10, 20, 30}, memory.Bitmap{})

			got, err := tc.sel.filterArray(&alloc, arr)
			require.NoError(t, err)

			values := got.(*columnar.Number[int64]).Values()
			require.Equal(t, []int64{10, 20, 30}, values)
			require.NotSame(t, &arr.Values()[0], &values[0])
		})
	}
}

func TestSelection_and(t *testing.T) {
	const n = 6

	var alloc memory.Allocator

	t.Run("returns the rows that both selections select", func(t *testing.T) {
		a := selectionOf(&alloc, n, 0, 1, 2, 3)
		b := selectionOf(&alloc, n, 2, 3, 4)

		got := a.and(&alloc, b)
		require.Equal(t, n, got.n)
		require.Equal(t, []int{2, 3}, selectedRows(got))
	})

	t.Run("returns the rows of other when the selection selects every row", func(t *testing.T) {
		other := selectionOf(&alloc, n, 1, 4)

		require.Equal(t, []int{1, 4}, selectedRows(newSelectionAllRows(n).and(&alloc, other)))
	})

	t.Run("returns the rows of the selection when other selects every row", func(t *testing.T) {
		a := selectionOf(&alloc, n, 1, 4)

		require.Equal(t, []int{1, 4}, selectedRows(a.and(&alloc, newSelectionAllRows(n))))
	})

	t.Run("returns no row when the selections share no row", func(t *testing.T) {
		a := selectionOf(&alloc, n, 0, 1)
		b := selectionOf(&alloc, n, 4, 5)

		require.Empty(t, selectedRows(a.and(&alloc, b)))
	})

	t.Run("panics when the selections have a different number of rows", func(t *testing.T) {
		a := selectionOf(&alloc, n, 1)

		require.PanicsWithValue(t, "dataset: selections of 6 rows and 4 rows", func() {
			a.and(&alloc, selectionOf(&alloc, 4, 1))
		})
		require.PanicsWithValue(t, "dataset: selections of 6 rows and 4 rows", func() {
			a.and(&alloc, newSelectionAllRows(4))
		})
	})
}

func TestSelection_or(t *testing.T) {
	const n = 6

	var (
		alloc memory.Allocator
		outer = selectionOf(&alloc, n, 0, 1, 2, 3, 4)
	)

	t.Run("returns the rows that either selection selects", func(t *testing.T) {
		a := selectionOf(&alloc, n, 0, 1)
		b := selectionOf(&alloc, n, 1, 3)

		got := a.or(&alloc, b, outer)
		require.Equal(t, n, got.n)
		require.Equal(t, []int{0, 1, 3}, selectedRows(got))
	})

	t.Run("returns outer when the selection selects every row", func(t *testing.T) {
		other := selectionOf(&alloc, n, 1)

		require.Equal(t, selectedRows(outer), selectedRows(newSelectionAllRows(n).or(&alloc, other, outer)))
	})

	t.Run("returns outer when other selects every row", func(t *testing.T) {
		a := selectionOf(&alloc, n, 1)

		require.Equal(t, selectedRows(outer), selectedRows(a.or(&alloc, newSelectionAllRows(n), outer)))
	})

	t.Run("returns no row when both selections select none", func(t *testing.T) {
		a := selectionOf(&alloc, n)
		b := selectionOf(&alloc, n)

		require.Empty(t, selectedRows(a.or(&alloc, b, outer)))
	})

	t.Run("panics when the selections or outer have a different number of rows", func(t *testing.T) {
		a := selectionOf(&alloc, n, 1)

		require.PanicsWithValue(t, "dataset: selections of 6 rows and 4 rows", func() {
			a.or(&alloc, selectionOf(&alloc, 4, 1), outer)
		})
		require.PanicsWithValue(t, "dataset: selections of 6 rows and 4 rows", func() {
			a.or(&alloc, a, selectionOf(&alloc, 4, 1))
		})
	})
}

func TestSelection_subtract(t *testing.T) {
	const n = 6

	var alloc memory.Allocator

	t.Run("returns the rows that the selection selects and other does not", func(t *testing.T) {
		a := selectionOf(&alloc, n, 0, 1, 2, 3)
		b := selectionOf(&alloc, n, 2, 3, 4)

		got := a.subtract(&alloc, b)
		require.Equal(t, n, got.n)
		require.Equal(t, []int{0, 1}, selectedRows(got))
	})

	t.Run("returns the rows of the selection when other selects no row", func(t *testing.T) {
		a := selectionOf(&alloc, n, 1, 4)
		b := selectionOf(&alloc, n)

		require.Equal(t, []int{1, 4}, selectedRows(a.subtract(&alloc, b)))
	})

	t.Run("returns the rows that other does not select when the selection selects every row", func(t *testing.T) {
		b := selectionOf(&alloc, n, 1, 2)

		got := newSelectionAllRows(n).subtract(&alloc, b)
		require.Equal(t, n, got.n)
		require.Equal(t, []int{0, 3, 4, 5}, selectedRows(got))
	})

	t.Run("returns a selection of no row when other selects every row", func(t *testing.T) {
		a := selectionOf(&alloc, n, 1, 4)

		got := a.subtract(&alloc, newSelectionAllRows(n))
		require.Equal(t, n, got.n)
		require.Equal(t, n, got.bits.Len())
		require.True(t, got.empty())
	})

	t.Run("returns a selection of no row when both selections select the same rows", func(t *testing.T) {
		a := selectionOf(&alloc, n, 1, 4)
		b := selectionOf(&alloc, n, 1, 4)

		got := a.subtract(&alloc, b)
		require.Equal(t, n, got.n)
		require.Equal(t, n, got.bits.Len())
		require.True(t, got.empty())
	})

	t.Run("panics when the selections have a different number of rows", func(t *testing.T) {
		a := selectionOf(&alloc, n, 1)

		require.PanicsWithValue(t, "dataset: selections of 6 rows and 4 rows", func() {
			a.subtract(&alloc, selectionOf(&alloc, 4, 1))
		})
	})
}

func TestSelectionFromComparison(t *testing.T) {
	const n = 4

	var (
		alloc    memory.Allocator
		values   = bitmapOf(&alloc, true, true, false, false)
		validity = bitmapOf(&alloc, true, false, true, false)
	)

	t.Run("selects the rows where the comparison is true", func(t *testing.T) {
		res := columnar.NewBool(bitmapOf(&alloc, true, false, true, false), memory.Bitmap{})

		got, err := selectionFromComparison(&alloc, res, false, newSelectionAllRows(n))
		require.NoError(t, err)
		require.Equal(t, n, got.n)
		require.Equal(t, []int{0, 2}, selectedRows(got))
	})

	t.Run("selects only the rows that sel selects", func(t *testing.T) {
		res := columnar.NewBool(bitmapOf(&alloc, true, true, true, true), memory.Bitmap{})

		got, err := selectionFromComparison(&alloc, res, false, selectionOf(&alloc, n, 1, 2))
		require.NoError(t, err)
		require.Equal(t, []int{1, 2}, selectedRows(got))
	})

	t.Run("drops the rows where the kernel input was null when nullPasses is false", func(t *testing.T) {
		got, err := selectionFromComparison(&alloc, columnar.NewBool(values, validity), false, newSelectionAllRows(n))
		require.NoError(t, err)
		require.Equal(t, []int{0}, selectedRows(got))
	})

	t.Run("keeps the rows where the kernel input was null when nullPasses is true", func(t *testing.T) {
		got, err := selectionFromComparison(&alloc, columnar.NewBool(values, validity), true, newSelectionAllRows(n))
		require.NoError(t, err)
		require.Equal(t, []int{0, 1, 3}, selectedRows(got))
	})

	t.Run("selects only the rows that sel selects when null rows pass", func(t *testing.T) {
		got, err := selectionFromComparison(&alloc, columnar.NewBool(values, validity), true, selectionOf(&alloc, n, 1, 2))
		require.NoError(t, err)
		require.Equal(t, []int{1}, selectedRows(got))
	})

	t.Run("returns an error when the kernel result is not a boolean array", func(t *testing.T) {
		res := columnar.NewNumber([]int64{1, 2, 3, 4}, memory.Bitmap{})

		_, err := selectionFromComparison(&alloc, res, false, newSelectionAllRows(n))
		require.EqualError(t, err, "comparison kernel returned *columnar.Number[int64], want a boolean array")
	})

	t.Run("returns an error when the kernel result has a different number of rows than sel", func(t *testing.T) {
		res := columnar.NewBool(bitmapOf(&alloc, true, false, true, false), memory.Bitmap{})

		_, err := selectionFromComparison(&alloc, res, false, newSelectionAllRows(3))
		require.EqualError(t, err, "comparison kernel returned 4 rows, want 3")
	})
}

func selectionOf(alloc *memory.Allocator, n int, rows ...int) selection {
	sel := newSelection(alloc, n, false)
	for _, row := range rows {
		sel.set(row)
	}
	return sel
}

func fullBitmapSelection(alloc *memory.Allocator, n int) selection {
	bits := memory.NewBitmap(alloc, n)
	bits.AppendCount(true, n)
	return selection{bits: bits, n: n}
}

func selectedRows(sel selection) []int {
	return slices.Collect(sel.bits.IterValues(true))
}

func bitmapOf(alloc *memory.Allocator, values ...bool) memory.Bitmap {
	bm := memory.NewBitmap(alloc, len(values))
	bm.AppendValues(values...)
	return bm
}
