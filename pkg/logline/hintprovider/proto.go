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
	if snap.TermDictReads == 0 &&
		snap.BitmapReads == 0 &&
		snap.TotalIOWait == 0 &&
		snap.TotalIOBytes == 0 &&
		snap.PeakConcurrency == 0 &&
		snap.IndexQueriesTotal == 0 &&
		snap.IndexQueriesTermMiss == 0 &&
		snap.IndexQueriesEmptyAnd == 0 &&
		snap.IndexQueriesPositive == 0 &&
		snap.TotalTermBatchesProcessed == 0 {
		return nil
	}
	return &logproto.HintQueryStats{
		TermDictReads:             snap.TermDictReads,
		BitmapReads:               snap.BitmapReads,
		TotalIoWaitNanos:          snap.TotalIOWait.Nanoseconds(),
		TotalIoBytes:              snap.TotalIOBytes,
		PeakConcurrency:           snap.PeakConcurrency,
		IndexQueriesTotal:         snap.IndexQueriesTotal,
		IndexQueriesTermMiss:      snap.IndexQueriesTermMiss,
		IndexQueriesEmptyAnd:      snap.IndexQueriesEmptyAnd,
		IndexQueriesPositive:      snap.IndexQueriesPositive,
		TotalTermBatchesProcessed: snap.TotalTermBatchesProcessed,
	}
}

func protoToQueryStats(ps *logproto.HintQueryStats) *QueryStats {
	s := NewQueryStats()
	if ps == nil {
		return s
	}
	s.termDictReads.Add(ps.TermDictReads)
	s.bitmapReads.Add(ps.BitmapReads)
	s.totalIOWaitNanos.Add(ps.TotalIoWaitNanos)
	s.totalIOBytes.Add(ps.TotalIoBytes)
	s.peakConcurrency.Store(ps.PeakConcurrency)
	s.indexQueriesTotal.Add(ps.IndexQueriesTotal)
	s.indexQueriesTermMiss.Add(ps.IndexQueriesTermMiss)
	s.indexQueriesEmptyAnd.Add(ps.IndexQueriesEmptyAnd)
	s.indexQueriesPositive.Add(ps.IndexQueriesPositive)
	s.totalTermBatchesProcessed.Add(ps.TotalTermBatchesProcessed)
	return s
}
