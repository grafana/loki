package hintprovider

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHintsToProtoRoundTrip(t *testing.T) {
	start := time.Date(2026, 3, 11, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)

	stats := NewQueryStats()
	stats.termDictReads.Add(3)
	stats.bitmapReads.Add(4)
	stats.totalIOWaitNanos.Add(1500)
	stats.totalIOBytes.Add(99)
	stats.peakConcurrency.Store(7)
	stats.indexQueriesTotal.Add(5)
	stats.indexQueriesPositive.Add(1)
	stats.totalTermBatchesProcessed.Add(8)

	in := &Hints{TimeRanges: []HintTimeRange{
		{Start: time.Time{}, End: start, Source: HintSourcePreMinDate},
		{Start: start, End: end, Source: "index-a"},
	}}

	gotHints, gotStats := ProtoToHints(HintsToProto(in, stats))
	require.Equal(t, in.TimeRanges, gotHints.TimeRanges)
	require.True(t, gotHints.TimeRanges[0].IsPassthrough())

	snap := gotStats.Snapshot()
	require.Equal(t, int64(3), snap.TermDictReads)
	require.Equal(t, int64(4), snap.BitmapReads)
	require.Equal(t, int64(1500), snap.TotalIOWait.Nanoseconds())
	require.Equal(t, int64(99), snap.TotalIOBytes)
	require.Equal(t, int32(7), snap.PeakConcurrency)
	require.Equal(t, int64(5), snap.IndexQueriesTotal)
	require.Equal(t, int64(1), snap.IndexQueriesPositive)
	require.Equal(t, int64(8), snap.TotalTermBatchesProcessed)
}

func TestHintsToProtoOmitsEmptyStats(t *testing.T) {
	resp := HintsToProto(&Hints{}, NewQueryStats())
	require.Nil(t, resp.Stats)
	require.Empty(t, resp.TimeRanges)
}
