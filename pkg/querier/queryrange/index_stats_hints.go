package queryrange

import (
	"context"
	"time"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/util"
)

// indexStatsHintsMiddleware prepares the hint ranges of an index stats request
// before it is split and cached. Ingester stats are never filtered, so it adds
// the ingester window to the hints. It answers a request whose hints overlap
// nothing with zero stats, without calling the queriers. Other requests pass
// through.
func indexStatsHintsMiddleware(iqo util.IngesterQueryOptions, now func() time.Time) queryrangebase.Middleware {
	return queryrangebase.MiddlewareFunc(func(next queryrangebase.Handler) queryrangebase.Handler {
		return queryrangebase.HandlerFunc(func(ctx context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
			req, ok := r.(*logproto.IndexStatsRequest)
			if !ok || req.HintRanges == nil {
				return next.Do(ctx, r)
			}

			clone := *req
			clone.HintRanges = withIngesterWindow(req.HintRanges, iqo, now(), req.GetEnd()).Clip(req.GetStart(), req.GetEnd())
			if isEmptyHintSet(clone.HintRanges) {
				return emptyIndexStatsResponse(), nil
			}
			return next.Do(ctx, &clone)
		})
	})
}

// withIngesterWindow adds the range that the queriers read from the ingesters,
// [now - query_ingesters_within, end), to hints. It returns nil when the
// queriers read every range from the ingesters, because then no range can be
// filtered.
func withIngesterWindow(hints *logproto.HintTimeRanges, iqo util.IngesterQueryOptions, now, end time.Time) *logproto.HintTimeRanges {
	if iqo == nil || iqo.QueryStoreOnly() {
		return hints
	}
	within := iqo.QueryIngestersWithin()
	if within <= 0 {
		return nil
	}
	ranges := make([]logproto.HintTimeRange, 0, len(hints.Ranges)+1)
	ranges = append(ranges, hints.Ranges...)
	ranges = append(ranges, logproto.HintTimeRange{Start: now.Add(-within), End: end})
	return &logproto.HintTimeRanges{Ranges: ranges}
}

// splitIndexStatsHintRanges clips hints to the split [start, end]. It returns
// nil when the hints cover the whole split, because they filter nothing there,
// and a split without hints can use the results cache.
func splitIndexStatsHintRanges(hints *logproto.HintTimeRanges, start, end time.Time) *logproto.HintTimeRanges {
	if hints == nil || iter.NewHintTimeRangeSet(hints, start, end).Covers(start, end) {
		return nil
	}
	return hints.Clip(start, end)
}

// emptyHintsIndexStatsMiddleware answers an index stats request that has an
// empty hint set with zero stats, without calling the queriers, because no
// chunk overlaps an empty set. Other requests pass through.
//
// It must sit below the split middleware, which clips the hints of each split
// and can leave a split without any.
func emptyHintsIndexStatsMiddleware() queryrangebase.Middleware {
	return queryrangebase.MiddlewareFunc(func(next queryrangebase.Handler) queryrangebase.Handler {
		return queryrangebase.HandlerFunc(func(ctx context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
			if req, ok := r.(*logproto.IndexStatsRequest); ok && isEmptyHintSet(req.HintRanges) {
				return emptyIndexStatsResponse(), nil
			}
			return next.Do(ctx, r)
		})
	})
}

func isEmptyHintSet(hints *logproto.HintTimeRanges) bool {
	return hints != nil && len(hints.Ranges) == 0
}

func emptyIndexStatsResponse() *IndexStatsResponse {
	return &IndexStatsResponse{Response: &logproto.IndexStatsResponse{}}
}
