package queryfrontend

import (
	"context"
	"time"

	"github.com/grafana/loki/v3/pkg/util/querylimits"

	"github.com/grafana/loki/v3/pkg/logline/hintprovider"
)

// plannedRangeSource lets Loki's size limiter wait on hint prefetch only
// when the full-span query would exceed MaxQueryBytesRead.
type plannedRangeSource struct {
	result  *hintPrefetchResult
	timeout time.Duration
}

func (s *plannedRangeSource) Wait(ctx context.Context) ([]querylimits.TimeRange, bool) {
	if s == nil || s.result == nil {
		return nil, false
	}
	timer := time.NewTimer(s.timeout)
	defer timer.Stop()
	select {
	case <-s.result.done:
	case <-ctx.Done():
		return nil, false
	case <-timer.C:
		return nil, false
	}
	if s.result.err != nil {
		return nil, false
	}
	return buildPlannedQueryRanges(s.result.ranges, s.result.queryStart, s.result.queryEnd, s.result.ingesterCutoff), true
}

// buildPlannedQueryRanges is the time that will actually be queried:
// store windows from hints, plus the ingester window if the query reaches it.
// Index-empty holes are omitted.
//
// hints are already sorted and adjacent-merged by the provider. Store hits are
// intersected with [queryStart, min(queryEnd, cutoff)) the same way filter
// clips to a shard; max(zero, queryStart) rewrites the pre-min_date sentinel.
// Ingester time is attached after; hints never include it.
func buildPlannedQueryRanges(
	hints []hintprovider.HintTimeRange,
	queryStart, queryEnd, ingesterCutoff time.Time,
) []querylimits.TimeRange {
	queryStart = queryStart.UTC()
	queryEnd = queryEnd.UTC()
	ingesterCutoff = ingesterCutoff.UTC()
	storeEnd := minTime(queryEnd, ingesterCutoff)

	out := make([]querylimits.TimeRange, 0, len(hints)+1)
	for _, hint := range hints {
		start := maxTime(hint.Start.UTC(), queryStart)
		end := minTime(hint.End.UTC(), storeEnd)
		if start.Before(end) {
			out = append(out, querylimits.TimeRange{Start: start, End: end})
		}
	}

	out = attachIngesterRange(out, queryStart, queryEnd, ingesterCutoff)
	if len(out) == 0 {
		return nil
	}
	return out
}

// attachIngesterRange adds [max(queryStart, cutoff), queryEnd). Hints are
// store-only, so this window is never in the list. If the last store window
// already touches cutoff, extend it instead of appending a twin for Loki to
// stats separately.
func attachIngesterRange(store []querylimits.TimeRange, queryStart, queryEnd, ingesterCutoff time.Time) []querylimits.TimeRange {
	if !queryEnd.After(ingesterCutoff) {
		return store
	}
	ingester := querylimits.TimeRange{
		Start: maxTime(queryStart, ingesterCutoff),
		End:   queryEnd,
	}
	if len(store) == 0 {
		return append(store, ingester)
	}
	last := &store[len(store)-1]
	if !ingester.Start.After(last.End) {
		last.End = ingester.End
		return store
	}
	return append(store, ingester)
}
