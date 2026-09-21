package hintprovider

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestHintsToProtoRoundTrip(t *testing.T) {
	start := time.Date(2026, 3, 11, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)

	stats := NewQueryStats()
	stats.headerReads.Add(1)
	stats.metadataReads.Add(2)
	stats.termDictReads.Add(3)
	stats.bitmapReads.Add(4)
	stats.totalIOWaitNanos.Add(1500)
	stats.totalIOBytes.Add(99)
	stats.peakConcurrency.Store(7)
	stats.indexQueriesTotal.Add(5)
	stats.indexQueriesPositive.Add(1)
	stats.totalTermBatchesProcessed.Add(8)

	in := &Hints{TimeRanges: []logproto.HintTimeRange{
		{Start: time.Time{}, End: start},
		{Start: start, End: end},
	}}

	gotHints, gotStats := ProtoToHints(HintsToProto(in, stats))
	require.Len(t, gotHints.TimeRanges, 2)
	require.True(t, gotHints.TimeRanges[0].Start.IsZero())
	require.True(t, gotHints.TimeRanges[0].End.Equal(start))
	require.True(t, gotHints.TimeRanges[0].IsPassthrough())
	require.True(t, gotHints.TimeRanges[1].Start.Equal(start))
	require.True(t, gotHints.TimeRanges[1].End.Equal(end))

	snap := gotStats.Snapshot()
	require.Equal(t, int64(1), snap.HeaderReads)
	require.Equal(t, int64(2), snap.MetadataReads)
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
