package v1

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMergeDedupeIter(t *testing.T) {
	t.Parallel()
	var (
		numSeries = 100
		data, _   = MkBasicSeriesWithBlooms(numSeries, 0, 0xffff, 0, 10000)
		dataPtr   = PointerSlice(data)
		queriers  = make([]PeekingIterator[*SeriesWithBlooms], 4)
	)

	for i := 0; i < len(queriers); i++ {
		queriers[i] = NewPeekingIter[*SeriesWithBlooms](NewSliceIter[*SeriesWithBlooms](dataPtr))
	}

	mbq := NewHeapIterForSeriesWithBloom(queriers...)
	eq := func(a, b *SeriesWithBlooms) bool {
		return a.Series.Fingerprint == b.Series.Fingerprint
	}
	merge := func(a, _ *SeriesWithBlooms) *SeriesWithBlooms {
		return a
	}
	deduper := NewDedupingIter[*SeriesWithBlooms, *SeriesWithBlooms](
		eq,
		Identity[*SeriesWithBlooms],
		merge,
		NewPeekingIter[*SeriesWithBlooms](mbq),
	)

	for i := 0; i < len(data); i++ {
		require.True(t, deduper.Next())
		exp := data[i].Series.Fingerprint
		got := deduper.At().Series.Fingerprint
		require.Equal(t, exp, got, "on iteration %d", i)
	}
	require.False(t, deduper.Next(), "finished iteration")
}
