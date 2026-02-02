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
