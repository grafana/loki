package metastore

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/memory"
)

func TestBitmapOperations(t *testing.T) {
	leftBase := bitmapWithBits(12, 3, 7, 9)
	rightBase := bitmapWithBits(11, 3, 6, 7)
	left := leftBase.Slice(3, 10)
	right := rightBase.Slice(2, 9)

	tests := []struct {
		name string
		op   func(*memory.Bitmap, *memory.Bitmap) *memory.Bitmap
		want []int
	}{
		{name: "difference", op: differenceBitmaps, want: []int{0, 6}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.op(left, right)
			require.Equal(t, 7, got.Len())
			require.Equal(t, tc.want, bitmapSetBits(got))
		})
	}
}

func TestIntersectInto(t *testing.T) {
	leftBase := bitmapWithBits(12, 3, 7, 9)
	rightBase := bitmapWithBits(11, 3, 6, 7)
	left := leftBase.Slice(3, 10)
	right := rightBase.Slice(2, 9)

	got := intersectInto(left, right)

	require.Same(t, left, got)
	require.Equal(t, 7, got.Len())
	require.Equal(t, []int{4}, bitmapSetBits(got))

	aligned := bitmapWithBits(7, 0, 4, 6)
	alignedSrc := bitmapWithBits(7, 1, 4, 5)
	require.Zero(t, testing.AllocsPerRun(100, func() {
		intersectInto(aligned, alignedSrc)
	}))
}

func TestUnionInto(t *testing.T) {
	leftBase := bitmapWithBits(12, 3, 7, 9)
	rightBase := bitmapWithBits(11, 3, 6, 7)
	left := leftBase.Slice(3, 10)
	right := rightBase.Slice(2, 9)

	got := unionInto(left, right)

	require.Same(t, left, got)
	require.Equal(t, 7, got.Len())
	require.Equal(t, []int{0, 1, 4, 5, 6}, bitmapSetBits(got))

	aligned := bitmapWithBits(7, 0, 4)
	alignedSrc := bitmapWithBits(7, 1, 4, 5)
	require.Zero(t, testing.AllocsPerRun(100, func() {
		unionInto(aligned, alignedSrc)
	}))
}

func TestBitmapOperations_ZeroExtend(t *testing.T) {
	short := bitmapWithBits(3, 1)
	long := bitmapWithBits(10, 1, 8)

	union := unionInto(short, long)
	require.Same(t, short, union)
	require.Equal(t, 10, union.Len())
	require.Equal(t, []int{1, 8}, bitmapSetBits(union))

	intersectionDst := bitmapWithBits(3, 1)
	intersection := intersectInto(intersectionDst, long)
	require.Same(t, intersectionDst, intersection)
	require.Equal(t, 10, intersection.Len())
	require.Equal(t, []int{1}, bitmapSetBits(intersection))

	intersectionLongDst := bitmapWithBits(10, 1, 8)
	intersection = intersectInto(intersectionLongDst, bitmapWithBits(3, 1))
	require.Equal(t, 10, intersection.Len())
	require.Equal(t, []int{1}, bitmapSetBits(intersection))

	difference := differenceBitmaps(long, bitmapWithBits(3, 1))
	require.Equal(t, 10, difference.Len())
	require.Equal(t, []int{8}, bitmapSetBits(difference))
}

func TestBitmapOperations_NilOperands(t *testing.T) {
	bitmap := bitmapWithBits(5, 1, 4)

	union := unionInto(nil, bitmap)
	require.NotSame(t, bitmap, union)
	require.Equal(t, []int{1, 4}, bitmapSetBits(union))
	require.Empty(t, bitmapSetBits(intersectInto(nil, bitmap)))
	require.Equal(t, []int{1, 4}, bitmapSetBits(differenceBitmaps(bitmap, nil)))
}

func bitmapWithBits(length int, set ...int) *memory.Bitmap {
	bitmap := memory.NewBitmap(nil, length)
	bitmap.Resize(length)
	for _, index := range set {
		bitmap.Set(index, true)
	}
	return &bitmap
}

func bitmapSetBits(bitmap *memory.Bitmap) []int {
	var set []int
	for index := range bitmap.IterValues(true) {
		set = append(set, index)
	}
	return set
}
