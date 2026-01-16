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
