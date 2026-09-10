package queryfrontend

import (
	"context"
	"time"

	"github.com/grafana/loki/v3/pkg/util/querylimits"

	"github.com/grafana/loki/v3/pkg/logline/hintprovider"
)

// buildPlannedQueryRanges is the time that will actually be queried:
// store windows from hints, plus the ingester window if the query reaches it.
// Index-empty holes are omitted.
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
// store-only. If the last store window already touches cutoff, extend it.
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

// injectPlannedQueryRanges puts a finished plan on ctx when the lookup
// completed without error. Timeout (done still open) or error leaves ctx
// unchanged so the size limiter uses the full request range.
func injectPlannedQueryRanges(ctx context.Context, result *hintPrefetchResult) context.Context {
	if result == nil || result.err != nil {
		return ctx
	}
	select {
	case <-result.done:
	default:
		return ctx
	}
	return querylimits.InjectPlannedQueryRanges(ctx, buildPlannedQueryRanges(
		result.ranges, result.queryStart, result.queryEnd, result.ingesterCutoff,
	))
}
