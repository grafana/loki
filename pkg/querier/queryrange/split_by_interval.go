package queryrange

import (
	"context"
	"time"

	"github.com/cortexproject/cortex/pkg/querier/queryrange"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/opentracing/opentracing-go"
	otlog "github.com/opentracing/opentracing-go/log"
)

// SplitByIntervalMiddleware creates a new Middleware that splits log requests by a given interval.
func SplitByIntervalMiddleware(interval time.Duration, batchSize int, limits queryrange.Limits, merger queryrange.Merger) queryrange.Middleware {
	return queryrange.MiddlewareFunc(func(next queryrange.Handler) queryrange.Handler {
		return splitByInterval{
			next:      next,
			limits:    limits,
			merger:    merger,
			interval:  interval,
			batchSize: batchSize,
		}
	})
}

type splitByInterval struct {
	next      queryrange.Handler
	limits    queryrange.Limits
	merger    queryrange.Merger
	interval  time.Duration
	batchSize int
}

func (s splitByInterval) Do(ctx context.Context, r queryrange.Request) (queryrange.Response, error) {
	lokiRequest := r.(*LokiRequest)
	intervals := splitByTime(lokiRequest, s.interval)
	var result queryrange.Response

	if lokiRequest.Direction == logproto.BACKWARD {
		for i, j := 0, len(intervals)-1; i < j; i, j = i+1, j-1 {
			intervals[i], intervals[j] = intervals[j], intervals[i]
		}
	}

	for _, interval := range intervals {
		sp, ctx := opentracing.StartSpanFromContext(ctx, "splitByInterval.interval")
		linterval := interval.(*LokiRequest)
		logRequest(sp, linterval)
		reqs := splitByTime(linterval, linterval.EndTs.Sub(linterval.StartTs)/time.Duration(s.batchSize))

		reqResps, err := queryrange.DoRequests(ctx, s.next, reqs, s.limits)
		if err != nil {
			sp.Finish()
			return nil, err
		}

		resps := make([]queryrange.Response, 0, len(reqResps))
		if result != nil {
			resps = append(resps, result)
		}
		for _, reqResp := range reqResps {
			resps = append(resps, reqResp.Response)
		}

		resp, err := s.merger.MergeResponse(resps...)
		if err != nil {
			sp.Finish()
			return nil, err
		}

		lokiRes := resp.(*LokiResponse)
		if lokiRes.isFull() {
			sp.Finish()
			return resp, nil
		}

		result = lokiRes
		sp.Finish()
	}

	return result, nil
}

func splitByTime(r *LokiRequest, interval time.Duration) []queryrange.Request {
	var reqs []queryrange.Request
	for start := r.StartTs; start.Before(r.EndTs); start = start.Add(interval) {
		end := start.Add(interval)
		if end.After(r.EndTs) {
			end = r.EndTs
		}
		reqs = append(reqs, &LokiRequest{
			Query:     r.Query,
			Limit:     r.Limit,
			Step:      r.Step,
			Direction: r.Direction,
			Path:      r.Path,
			StartTs:   start,
			EndTs:     end,
		})
	}
	return reqs
}

func logRequest(span opentracing.Span, r *LokiRequest) {
	span.LogFields(
		otlog.String("query", r.GetQuery()),
		otlog.String("start", r.StartTs.String()),
		otlog.String("end", r.EndTs.String()),
		otlog.Int64("step (ms)", r.GetStep()),
		otlog.String("direction", r.Direction.String()),
		otlog.Uint32("limit", r.Limit),
		otlog.String("path", r.Path),
	)
}
