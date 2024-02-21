package v1

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMergeBlockQuerier_NonOverlapping(t *testing.T) {
	t.Parallel()
	var (
		numSeries        = 100
		numKeysPerSeries = 10000
		numQueriers      = 4
		queriers         []PeekingIterator[*SeriesWithBloom]
		data, _          = MkBasicSeriesWithBlooms(numSeries, numKeysPerSeries, 0, 0xffff, 0, 10000)
	)
	for i := 0; i < numQueriers; i++ {
		var ptrs []*SeriesWithBloom
		for j := 0; j < numSeries/numQueriers; j++ {
			ptrs = append(ptrs, &data[i*numSeries/numQueriers+j])
		}
		queriers = append(queriers, NewPeekingIter[*SeriesWithBloom](NewSliceIter[*SeriesWithBloom](ptrs)))
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
		numSeries        = 100
		numKeysPerSeries = 10000
		numQueriers      = 2
		queriers         []PeekingIterator[*SeriesWithBloom]
		data, _          = MkBasicSeriesWithBlooms(numSeries, numKeysPerSeries, 0, 0xffff, 0, 10000)
	)
	for i := 0; i < numQueriers; i++ {
		queriers = append(
			queriers,
			NewPeekingIter[*SeriesWithBloom](
				NewSliceIter[*SeriesWithBloom](
					PointerSlice[SeriesWithBloom](data),
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
		numSeries        = 100
		numKeysPerSeries = 10000
		numQueriers      = 4
		queriers         []PeekingIterator[*SeriesWithBloom]
		data, _          = MkBasicSeriesWithBlooms(numSeries, numKeysPerSeries, 0, 0xffff, 0, 10000)
		slices           = make([][]*SeriesWithBloom, numQueriers)
	)
	for i := 0; i < numSeries; i++ {
		slices[i%numQueriers] = append(slices[i%numQueriers], &data[i])
	}
	for i := 0; i < numQueriers; i++ {
		queriers = append(queriers, NewPeekingIter[*SeriesWithBloom](NewSliceIter[*SeriesWithBloom](slices[i])))
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
