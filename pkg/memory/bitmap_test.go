package memory_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/memory"
)

func TestBitmap_Append(t *testing.T) {
	var bmap memory.Bitmap

	require.Equal(t, 0, bmap.Len(), "empty bitmaps should have no length")
	require.Equal(t, 0, bmap.Cap(), "empty bitmaps should have no capacity")

	// Add 20 elements of varying values to bmap; using 20 will ensure that we
	// hit [memory.Bitmap.Grow], and writes beyond word boundaries.
	for i := range 20 {
		bmap.Append(i%2 == 0)
		require.Equal(t, i+1, bmap.Len(), "length should match number of appends")
		require.GreaterOrEqual(t, bmap.Cap(), bmap.Len(), "capacity should always be greater or equal to length")
	}

	// Read back all the values and make sure they're still correct.
	for i := range 20 {
		expect := i%2 == 0
		require.Equal(t, expect, bmap.Get(i))
	}
}

func TestBitmap_AppendCount(t *testing.T) {
	var bmap memory.Bitmap
	bmap.AppendCount(false, 3)
	bmap.AppendCount(true, 5)

	expect := []bool{false, false, false, true, true, true, true, true}
	for i := range expect {
		require.Equal(t, expect[i], bmap.Get(i), "unexpected value at index %d", i)
	}
}

func TestBitmap_AppendBitmap(t *testing.T) {
	t.Run("empty destination", func(t *testing.T) {
		var src, dst memory.Bitmap

		src.AppendValues(false, true, false, false)
		dst.AppendBitmap(src)

		expect := []bool{false, true, false, false}
		for i := range expect {
			require.Equal(t, expect[i], dst.Get(i), "unexpected value at index %d", i)
		}
	})

	t.Run("two non-empty bitmaps", func(t *testing.T) {
		var src, dst memory.Bitmap

		dst.AppendValues(false, true, false, false)
		src.AppendValues(true, true, false, true, true)
		dst.AppendBitmap(src)

		expect := []bool{false, true, false, false, true, true, false, true, true}
		for i := range expect {
			require.Equal(t, expect[i], dst.Get(i), "unexpected value at index %d", i)
		}
	})
}

func TestBitmap_Set(t *testing.T) {
	var bmap memory.Bitmap
	bmap.Resize(16) // Make room for at least 10 elements.

	bmap.Set(6, true)
	bmap.Set(8, true)
	bmap.Set(9, false)
	bmap.Set(13, true) // Set bit in another word boundary

	require.True(t, bmap.Get(6), "bit 6 should be true")
	require.True(t, bmap.Get(8), "bit 8 should be true")
	require.False(t, bmap.Get(9), "bit 9 should be false")
	require.True(t, bmap.Get(13), "bit 13 should be true")

	// Verify other bits remain false
	for i := range bmap.Len() {
		// Ignore bits we explicitly set.
		if i == 6 || i == 8 || i == 9 || i == 13 {
			continue
		}
		require.False(t, bmap.Get(i), "bit %d should be false", i)
	}
}

func TestBitmap_SetRange(t *testing.T) {
	bmap := memory.NewBitmap(nil, 64)
	bmap.Resize(64)
	bmap.SetRange(0, 5, true)
	bmap.SetRange(7, 10, true)

	for i := range bmap.Len() {
		value := bmap.Get(i)

		switch {
		case i >= 0 && i < 5:
			require.True(t, value, "bit %d should be true", i)
		case i >= 7 && i < 10:
			require.True(t, value, "bit %d should be true", i)
		default:
			require.False(t, value, "bit %d should be false", i)
		}
	}
}

func TestBitmap_IterValue_true(t *testing.T) {
	bmap := memory.NewBitmap(nil, 128)
	bmap.Resize(128) // 16 words, 8 bits each

	bitsToSet := []int{1, 3, 5, 65, 70, 127}
	for _, bit := range bitsToSet {
		bmap.Set(bit, true)
	}

	var indices []int
	for index := range bmap.IterValues(true) {
		indices = append(indices, index)
	}

	expected := []int{1, 3, 5, 65, 70, 127}
	require.Equal(t, expected, indices)
}

func TestBitmap_IterValue_false(t *testing.T) {
	bmap := memory.NewBitmap(nil, 128)
	bmap.Resize(128) // 16 words, 8 bits each

	// Set all bits first
	bmap.SetRange(0, 128, true)

	bitsToClear := []int{0, 2, 4, 64, 69, 126}
	for _, bit := range bitsToClear {
		bmap.Set(bit, false)
	}

	var indices []int
	for index := range bmap.IterValues(false) {
		indices = append(indices, index)
	}

	expected := []int{0, 2, 4, 64, 69, 126}
	require.Equal(t, expected, indices)
}

func TestBitmap_Slice(t *testing.T) {
	t.Run("Get", func(t *testing.T) {
		var bmap memory.Bitmap
		bmap.AppendValues(false, false, false, true, true, false, true, true, false, false, true, true, false, false, false, false)

		slice := bmap.Slice(3, 11)
		require.Equal(t, 8, slice.Len(), "slice length should be 8")

		for i := range slice.Len() {
			require.Equal(t, bmap.Get(i+3), slice.Get(i), "unexpected value at index %d", i)
		}
	})

	t.Run("Set", func(t *testing.T) {
		var bmap memory.Bitmap
		bmap.AppendCount(false, 20)
		bmap.Set(5, true)
		bmap.Set(8, true)
		bmap.Set(12, true)

		off := 3
		slice := bmap.Slice(off, 15)

		// Modify the slice
		slice.Set(4, true)
		require.True(t, slice.Get(4), "set should update bitmap")
		require.Equal(t, bmap.Get(off+4), slice.Get(4), "updating slice should update original bitmap")
	})

	t.Run("SetRange", func(t *testing.T) {
		var bmap memory.Bitmap
		bmap.AppendCount(false, 24)

		slice := bmap.Slice(3, 19)
		slice.SetRange(2, 8, true)

		for i := range slice.Len() {
			if i >= 2 && i < 8 {
				require.True(t, slice.Get(i), "slice bit %d should be true", i)
			} else {
				require.False(t, slice.Get(i), "slice bit %d should be false", i)
			}
		}

		// Ensure consistency with unsliced bitmap
		for i := range slice.Len() {
			require.Equal(t, bmap.Get(i+3), slice.Get(i), "unexpected value at index %d", i)
		}
	})

	t.Run("SetCount", func(t *testing.T) {
		var bmap memory.Bitmap
		bmap.AppendCount(false, 24)
		bmap.SetRange(5, 15, true)

		slice := bmap.Slice(3, 19)

		var (
			count      = slice.SetCount()
			clearCount = slice.ClearCount()
		)

		require.Equal(t, 10, count, "slice should have 10 set bits")
		require.Equal(t, 6, clearCount, "slice should have 6 clear bits")
	})

	t.Run("IterValues/value=true", func(t *testing.T) {
		var bmap memory.Bitmap
		bmap.AppendCount(false, 24)
		bmap.Set(5, true)
		bmap.Set(8, true)
		bmap.Set(12, true)
		bmap.Set(15, true)

		off := 3
		slice := bmap.Slice(off, bmap.Len())

		var indices []int
		for index := range slice.IterValues(true) {
			indices = append(indices, off+index)
		}

		expected := []int{5, 8, 12, 15} // These are all adjusted for off for easier reading
		require.Equal(t, expected, indices)
	})

	t.Run("IterValues/value=false", func(t *testing.T) {
		var bmap memory.Bitmap
		bmap.AppendCount(true, 24)
		bmap.Set(5, false)
		bmap.Set(8, false)
		bmap.Set(12, false)
		bmap.Set(15, false)

		off := 3
		slice := bmap.Slice(off, bmap.Len())

		var indices []int
		for index := range slice.IterValues(false) {
			indices = append(indices, off+index)
		}

		expected := []int{5, 8, 12, 15} // These are all adjusted for off for easier reading
		require.Equal(t, expected, indices)
	})

	t.Run("AppendBitmap", func(t *testing.T) {
		var bmap memory.Bitmap
		bmap.AppendValues(false, false, false, true, true, false, true, true, false, false, true, true, false, false, false, false)

		off := 3
		slice := bmap.Slice(off, bmap.Len())

		var dst memory.Bitmap
		dst.AppendBitmap(*slice)

		require.Equal(t, bmap.Len()-off, dst.Len(), "mismatch in lengths")

		// Verify the appended values match the slice
		for i := range dst.Len() {
			require.Equal(t, slice.Get(i), dst.Get(i), "bit %d should match", i)
		}
	})

	t.Run("Sub-slice", func(t *testing.T) {
		var bmap memory.Bitmap
		bmap.AppendCount(false, 32)
		bmap.Set(7, true)
		bmap.Set(12, true)
		bmap.Set(18, true)

		slice1 := bmap.Slice(3, 23)
		slice2 := slice1.Slice(2, 14)

		totalOff := 3 + 2
		require.True(t, slice2.Get(7-totalOff))
		require.True(t, slice2.Get(12-totalOff))
		require.False(t, slice2.Get(15-totalOff))
	})

	t.Run("ByteAligned", func(t *testing.T) {
		// Regression test: Slice where the end is exactly byte-aligned should
		// not panic. The old formula (off+j)/8+1 would overshoot by 1 byte
		// when (off+j) is a multiple of 8, causing an out-of-bounds slice.
		var bmap memory.Bitmap
		n := 65536 // exactly 8192 bytes
		bmap.AppendCount(true, n)

		require.NotPanics(t, func() {
			slice := bmap.Slice(0, n)
			require.Equal(t, n, slice.Len())
		})

		// Also test a smaller byte-aligned boundary with an offset.
		bmap2 := memory.Bitmap{}
		bmap2.AppendCount(false, 24) // 3 bytes
		require.NotPanics(t, func() {
			slice := bmap2.Slice(0, 16) // end at byte 2, exactly aligned
			require.Equal(t, 16, slice.Len())
		})
	})

	t.Run("Clone", func(t *testing.T) {
		var bmap memory.Bitmap
		bmap.AppendValues(false, false, false, true, true, false, true, true, false, false, true, true, false, false, false, false)

		slice := bmap.Slice(3, 11)
		cloned := slice.Clone(nil)

		require.Equal(t, slice.Len(), cloned.Len(), "cloned bitmap should have same length")

		// Verify all values match
		for i := range slice.Len() {
			require.Equal(t, slice.Get(i), cloned.Get(i), "bit %d should match", i)
		}

		// Flip index 2 in the clone, and make sure it doesn't affect the original slice.
		before := bmap.Get(2 + 3 /* slice offset */)
		require.Equal(t, before, slice.Get(2), "slice bit 2 should match original bmap")
		require.Equal(t, before, cloned.Get(2), "slice bit 2 should match original bmap")

		cloned.Set(2, !before)
		require.Equal(t, before, slice.Get(2), "original slice bit 2 should remain unchanged")
		require.Equal(t, !before, cloned.Get(2), "cloned bit 2 should be flipped")
	})
}

func TestBitmap_SetAlgebra(t *testing.T) {
	// Helper: build a Bitmap from explicit set bits.
	build := func(bits ...int) *memory.Bitmap {
		b := memory.NewBitmap(nil, 0)
		maxBit := -1
		for _, x := range bits {
			if x > maxBit {
				maxBit = x
			}
		}
		b.Resize(maxBit + 1)
		for _, x := range bits {
			b.Set(x, true)
		}
		return &b
	}
	ids := func(b *memory.Bitmap) []int {
		var out []int
		for id := range b.IterValues(true) {
			out = append(out, id)
		}
		return out
	}

	a := build(1, 2)
	c := build(0, 1)

	require.Equal(t, []int{0, 1, 2}, ids(a.Or(c)))
	require.Equal(t, []int{1}, ids(a.And(c)))
	require.Equal(t, []int{2}, ids(a.AndNot(c))) // in a, not c

	// Mismatched lengths: shorter operand is zero-extended.
	wide := build(70) // bit beyond one byte
	narrow := build(1)
	require.Equal(t, []int{1, 70}, ids(wide.Or(narrow)))
	require.Empty(t, ids(wide.And(narrow)))
	require.Equal(t, []int{70}, ids(wide.AndNot(narrow)))

	// Empty detection via existing SetCount.
	require.Equal(t, 0, build().SetCount())
	require.NotEqual(t, 0, a.SetCount())

	// Offset operand (sliced) is normalized before combining.
	sliced := build(0, 1, 2, 3).Slice(1, 4) // bits 0,1,2 within the slice
	require.Equal(t, []int{0, 1, 2}, ids(sliced.Or(build())))
	require.Equal(t, []int{0, 2}, ids(sliced.AndNot(build(1))))

	// Offset operand combined with a wider operand: the offset operand is
	// zero-extended past its backing bytes.
	offsetNarrow := build(0, 1, 2, 9).Slice(1, 3) // bits 0,1 within the slice, off != 0
	require.Equal(t, []int{0, 1, 70}, ids(offsetNarrow.Or(build(70))))

	// Stale bits beyond Len in an operand's trailing byte do not leak into the result.
	stale := build(0, 1, 2, 3, 4, 5, 6, 7)
	stale.Resize(2) // bits 2..7 remain set in the backing byte but are out of range
	require.Equal(t, []int{0, 1}, ids(stale.Or(build())))
	data, _ := stale.Or(build()).BytesTrimmed()
	require.Equal(t, uint8(0b0000_0011), data[0])
}

// TestBitmap_SetAlgebra_GhostBits guards against a shorter operand leaking
// stale bits past its Len when the other operand is wide enough that those
// positions fall within the combined length n. build(1,2,3,4) sliced down to
// len 2 leaves bits 2,3,4 set in the backing byte; OR'd with a wide {100} must
// yield {1,100}, never {1,2,3,4,100}.
func TestBitmap_SetAlgebra_GhostBits(t *testing.T) {
	build := func(bits ...int) *memory.Bitmap {
		b := memory.NewBitmap(nil, 0)
		maxBit := -1
		for _, x := range bits {
			if x > maxBit {
				maxBit = x
			}
		}
		b.Resize(maxBit + 1)
		for _, x := range bits {
			b.Set(x, true)
		}
		return &b
	}
	ids := func(b *memory.Bitmap) []int {
		var out []int
		for id := range b.IterValues(true) {
			out = append(out, id)
		}
		return out
	}

	shortStale := build(1, 2, 3, 4).Slice(0, 2) // value {1}, backing byte 0b0001_1110
	require.Equal(t, []int{1}, ids(shortStale), "sanity: sliced operand only contains bit 1")

	wideOther := build(100)
	require.Equal(t, []int{1, 100}, ids(shortStale.Or(wideOther)),
		"stale bits 2,3,4 from the shorter operand must not leak into Or")
	require.Empty(t, ids(shortStale.And(wideOther)),
		"stale bits must not produce phantom intersections in And")
	require.Equal(t, []int{1}, ids(shortStale.AndNot(wideOther)),
		"AndNot must reflect only the shorter operand's in-range bits")
}

func TestBitmap_SetAlgebra_Nil(t *testing.T) {
	build := func(bits ...int) *memory.Bitmap {
		b := memory.NewBitmap(nil, 0)
		maxBit := -1
		for _, x := range bits {
			if x > maxBit {
				maxBit = x
			}
		}
		b.Resize(maxBit + 1)
		for _, x := range bits {
			b.Set(x, true)
		}
		return &b
	}
	ids := func(b *memory.Bitmap) []int {
		var out []int
		for id := range b.IterValues(true) {
			out = append(out, id)
		}
		return out
	}

	var nilBmap *memory.Bitmap
	a := build(1, 3)

	// A nil operand is treated as an empty bitmap.
	require.Equal(t, []int{1, 3}, ids(a.Or(nil)))
	require.Equal(t, []int{1, 3}, ids(nilBmap.Or(a)))
	require.Empty(t, ids(a.And(nil)))
	require.Empty(t, ids(nilBmap.And(a)))
	require.Equal(t, []int{1, 3}, ids(a.AndNot(nil)))
	require.Empty(t, ids(nilBmap.AndNot(a)))

	// A nil receiver and nil arg combine to an empty, non-nil result.
	require.NotNil(t, nilBmap.Or(nil))
	require.Equal(t, 0, nilBmap.Or(nil).SetCount())

	// SetCount and ClearCount tolerate a nil receiver.
	require.Equal(t, 0, nilBmap.SetCount())
	require.Equal(t, 0, nilBmap.ClearCount())

	// Accumulating into a nil pointer yields a fresh bitmap that does not alias src.
	var dst *memory.Bitmap
	src := build(1, 3)
	dst = dst.Or(src)
	require.Equal(t, []int{1, 3}, ids(dst))
	src.Set(5, true)
	require.Equal(t, []int{1, 3}, ids(dst), "result must not alias src's backing array")
}
