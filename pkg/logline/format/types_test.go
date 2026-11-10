package format_test

import (
	"testing"
	"time"

	"github.com/RoaringBitmap/roaring"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logline/format"
)

func TestBitmap_Or(t *testing.T) {
	bm1 := roaring.BitmapOf(1, 2, 3)
	bm2 := roaring.BitmapOf(3, 4, 5)
	all := format.Bitmap{MatchesAll: true}

	// Two normal bitmaps union.
	r := format.Bitmap{Roaring: bm1.Clone()}.Or(format.Bitmap{Roaring: bm2})
	require.False(t, r.MatchesAll)
	require.Equal(t, []uint32{1, 2, 3, 4, 5}, r.Roaring.ToArray())

	// OR(all, x) = all
	r2 := all.Or(format.Bitmap{Roaring: bm1})
	require.True(t, r2.MatchesAll)

	// OR(x, all) = all
	r3 := format.Bitmap{Roaring: bm1}.Or(all)
	require.True(t, r3.MatchesAll)

	// OR(all, all) = all
	r4 := all.Or(all)
	require.True(t, r4.MatchesAll)

	// Zero-value is identity for Or.
	r5 := format.Bitmap{}.Or(format.Bitmap{Roaring: roaring.BitmapOf(1, 2)})
	require.Equal(t, []uint32{1, 2}, r5.Roaring.ToArray())

	r6 := format.Bitmap{Roaring: roaring.BitmapOf(1, 2)}.Or(format.Bitmap{})
	require.Equal(t, []uint32{1, 2}, r6.Roaring.ToArray())
}

func TestBitmap_And(t *testing.T) {
	bm1 := roaring.BitmapOf(1, 2, 3)
	bm2 := roaring.BitmapOf(2, 3, 4)
	all := format.Bitmap{MatchesAll: true}

	// Two normal bitmaps intersect.
	r := format.Bitmap{Roaring: bm1.Clone()}.And(format.Bitmap{Roaring: bm2})
	require.Equal(t, []uint32{2, 3}, r.Roaring.ToArray())

	// AND(all, x) = x
	r2 := all.And(format.Bitmap{Roaring: bm1})
	require.Equal(t, bm1.ToArray(), r2.Roaring.ToArray())

	// AND(x, all) = x
	r3 := format.Bitmap{Roaring: bm2}.And(all)
	require.Equal(t, bm2.ToArray(), r3.Roaring.ToArray())
}

func TestBitmap_IsEmpty(t *testing.T) {
	require.True(t, format.Bitmap{}.IsEmpty())
	require.True(t, format.Bitmap{Roaring: roaring.New()}.IsEmpty())
	require.False(t, format.Bitmap{Roaring: roaring.BitmapOf(1)}.IsEmpty())
	require.False(t, format.Bitmap{MatchesAll: true}.IsEmpty())
}

func TestDocumentLayout(t *testing.T) {
	day := time.Date(2026, 3, 22, 0, 0, 0, 0, time.UTC)
	layout := format.DocumentLayout{Interval: 16 * time.Second, ShardBits: 5, DayStart: day}
	require.Equal(t, uint64(172800), layout.CellsPerDay())
	require.Zero(t, format.DocumentLayout{}.CellsPerDay())

	for _, tt := range []struct {
		cell  uint32
		start time.Time
	}{
		{cell: 0, start: day},
		{cell: 31, start: day},
		{cell: 32, start: day.Add(16 * time.Second)},
		{cell: 172799, start: day.Add(24*time.Hour - 16*time.Second)},
	} {
		start, end := layout.TimeRange(tt.cell)
		require.Equal(t, tt.start, start, "cell %d", tt.cell)
		require.Equal(t, tt.start.Add(16*time.Second), end, "cell %d", tt.cell)
	}
}
