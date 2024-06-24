package v1

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMergeBlockQuerier_NonOverlapping(t *testing.T) {
	t.Parallel()
	var (
		numSeries   = 100
		numQueriers = 4
		queriers    []PeekingIterator[*SeriesWithBlooms]
		data, _     = MkBasicSeriesWithBlooms(numSeries, 0, 0xffff, 0, 10000)
	)
	for i := 0; i < numQueriers; i++ {
		var ptrs []*SeriesWithBlooms
		for j := 0; j < numSeries/numQueriers; j++ {
			ptrs = append(ptrs, &data[i*numSeries/numQueriers+j])
		}
		queriers = append(queriers, NewPeekingIter[*SeriesWithBlooms](NewSliceIter[*SeriesWithBlooms](ptrs)))
	}

	mbq := NewHeapIterForSeriesWithBloom(queriers...)

	for i := 0; i < numSeries; i++ {
		require.True(t, mbq.Next())
		exp := data[i].Series.Fingerprint
		got := mbq.At().Series.Fingerprint
		require.Equal(t, exp, got, "on iteration %d", i)
	}
	require.False(t, mbq.Next())
}

func TestMergeBlockQuerier_Duplicate(t *testing.T) {
	t.Parallel()
	var (
		numSeries   = 100
		numQueriers = 2
		queriers    []PeekingIterator[*SeriesWithBlooms]
		data, _     = MkBasicSeriesWithBlooms(numSeries, 0, 0xffff, 0, 10000)
	)
	for i := 0; i < numQueriers; i++ {
		queriers = append(
			queriers,
			NewPeekingIter[*SeriesWithBlooms](
				NewSliceIter[*SeriesWithBlooms](
					PointerSlice[SeriesWithBlooms](data),
				),
			),
		)
	}

	mbq := NewHeapIterForSeriesWithBloom(queriers...)

	for i := 0; i < numSeries*2; i++ {
		require.True(t, mbq.Next())
		exp := data[i/2].Series.Fingerprint
		got := mbq.At().Series.Fingerprint
		require.Equal(t, exp, got, "on iteration %d", i)
	}
	require.False(t, mbq.Next())
}

func TestMergeBlockQuerier_Overlapping(t *testing.T) {
	t.Parallel()

	var (
		numSeries   = 100
		numQueriers = 4
		queriers    []PeekingIterator[*SeriesWithBlooms]
		data, _     = MkBasicSeriesWithBlooms(numSeries, 0, 0xffff, 0, 10000)
		slices      = make([][]*SeriesWithBlooms, numQueriers)
	)
	for i := 0; i < numSeries; i++ {
		slices[i%numQueriers] = append(slices[i%numQueriers], &data[i])
	}
	for i := 0; i < numQueriers; i++ {
		queriers = append(queriers, NewPeekingIter[*SeriesWithBlooms](NewSliceIter[*SeriesWithBlooms](slices[i])))
	}

	mbq := NewHeapIterForSeriesWithBloom(queriers...)

	for i := 0; i < numSeries; i++ {
		require.True(t, mbq.Next())
		exp := data[i].Series.Fingerprint
		got := mbq.At().Series.Fingerprint
		require.Equal(t, exp, got, "on iteration %d", i)
	}
	require.False(t, mbq.Next())

}
