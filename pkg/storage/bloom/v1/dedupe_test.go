package v1

import (
	"testing"

	"github.com/stretchr/testify/require"

	iter "github.com/grafana/loki/v3/pkg/iter/v2"
)

func TestMergeDedupeIter(t *testing.T) {
	t.Parallel()
	var (
		numSeries = 100
		data, _   = MkBasicSeriesWithBlooms(numSeries, 0, 0xffff, 0, 10000)
		dataPtr   = PointerSlice(data)
		queriers  = make([]iter.PeekIterator[*SeriesWithBlooms], 4)
	)

	for i := 0; i < len(queriers); i++ {
		queriers[i] = iter.NewPeekIter[*SeriesWithBlooms](iter.NewSliceIter[*SeriesWithBlooms](dataPtr))
	}

	mbq := NewHeapIterForSeriesWithBloom(queriers...)
	eq := func(a, b *SeriesWithBlooms) bool {
		return a.Series.Fingerprint == b.Series.Fingerprint
	}
	merge := func(a, _ *SeriesWithBlooms) *SeriesWithBlooms {
		return a
	}
	deduper := iter.NewDedupingIter[*SeriesWithBlooms, *SeriesWithBlooms](
		eq,
		iter.Identity[*SeriesWithBlooms],
		merge,
		iter.NewPeekIter[*SeriesWithBlooms](mbq),
	)

	for i := 0; i < len(data); i++ {
		require.True(t, deduper.Next())
		exp := data[i].Series.Fingerprint
		got := deduper.At().Series.Fingerprint
		require.Equal(t, exp, got, "on iteration %d", i)
	}
	require.False(t, deduper.Next(), "finished iteration")
}
