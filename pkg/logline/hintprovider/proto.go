package hintprovider

import (
	"github.com/grafana/loki/v3/pkg/logproto"
)

// HintsToProto maps ProvideHints results onto the querier wire type.
func HintsToProto(hints *Hints, stats *QueryStats) *logproto.HintResponse {
	resp := &logproto.HintResponse{}
	if hints != nil {
		resp.TimeRanges = hints.TimeRanges
	}
	resp.Stats = queryStatsToProto(stats)
	return resp
}

// ProtoToHints reconstructs ProvideHints results from a querier HintResponse.
func ProtoToHints(resp *logproto.HintResponse) (*Hints, *QueryStats) {
	if resp == nil {
		return &Hints{}, NewQueryStats()
	}
	return &Hints{TimeRanges: resp.TimeRanges}, protoToQueryStats(resp.Stats)
}

func queryStatsToProto(stats *QueryStats) *logproto.HintQueryStats {
	if stats == nil {
		return nil
	}
	snap := stats.Snapshot()
	if snap == (logproto.HintQueryStats{}) {
		return nil
	}
	return &snap
}

func protoToQueryStats(ps *logproto.HintQueryStats) *QueryStats {
	s := NewQueryStats()
	if ps == nil {
		return s
	}
	s.headerReads.Add(ps.HeaderReads)
	s.metadataReads.Add(ps.MetadataReads)
	s.termDictReads.Add(ps.TermDictReads)
	s.bitmapReads.Add(ps.BitmapReads)
	s.totalIOWaitNanos.Add(ps.TotalIOWait.Nanoseconds())
	s.totalIOBytes.Add(ps.TotalIOBytes)
	s.peakConcurrency.Store(ps.PeakConcurrency)
	s.prefetchCalls.Store(ps.PrefetchCalls)
	s.prefetchTimeouts.Store(ps.PrefetchTimeouts)
	s.indexQueriesTotal.Add(ps.IndexQueriesTotal)
	s.indexQueriesTermMiss.Add(ps.IndexQueriesTermMiss)
	s.indexQueriesEmptyAnd.Add(ps.IndexQueriesEmptyAnd)
	s.indexQueriesPositive.Add(ps.IndexQueriesPositive)
	s.totalTermBatchesProcessed.Add(ps.TotalTermBatchesProcessed)
	if ps.HintCacheResult != "" || ps.HintCacheDaysFetched != 0 || ps.HintCacheDaysHit != 0 {
		s.ObserveHintCache(ps.HintCacheResult, int(ps.HintCacheDaysFetched), int(ps.HintCacheDaysHit))
	}
	return s
}
