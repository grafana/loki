package v1

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMergeDedupeIter(t *testing.T) {
	t.Parallel()
	var (
		numSeries        = 100
		numKeysPerSeries = 10000
		data, _          = MkBasicSeriesWithBlooms(numSeries, numKeysPerSeries, 0, 0xffff, 0, 10000)
		dataPtr          = PointerSlice(data)
		queriers         = make([]PeekingIterator[*SeriesWithBloom], 4)
	)

	for i := 0; i < len(queriers); i++ {
		queriers[i] = NewPeekingIter[*SeriesWithBloom](NewSliceIter[*SeriesWithBloom](dataPtr))
	}

	mbq := NewHeapIterForSeriesWithBloom(queriers...)
	eq := func(a, b *SeriesWithBloom) bool {
		return a.Series.Fingerprint == b.Series.Fingerprint
	}
	merge := func(a, _ *SeriesWithBloom) *SeriesWithBloom {
		return a
	}
	deduper := NewDedupingIter[*SeriesWithBloom, *SeriesWithBloom](
		eq,
		Identity[*SeriesWithBloom],
		merge,
		NewPeekingIter[*SeriesWithBloom](mbq),
	)

	for i := 0; i < len(data); i++ {
		require.True(t, deduper.Next())
		exp := data[i].Series.Fingerprint
		got := deduper.At().Series.Fingerprint
		require.Equal(t, exp, got, "on iteration %d", i)
	}
	require.False(t, deduper.Next(), "finished iteration")
}
