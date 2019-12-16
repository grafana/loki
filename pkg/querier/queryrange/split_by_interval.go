package queryrange

import (
	"context"
	"time"

	"github.com/cortexproject/cortex/pkg/querier/queryrange"
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
	var result *LokiResponse

	for _, interval := range intervals {
		linterval := interval.(*LokiRequest)
		reqs := splitByTime(linterval, linterval.EndTs.Sub(linterval.StartTs)/time.Duration(s.batchSize))

		reqResps, err := queryrange.DoRequests(ctx, s.next, reqs, s.limits)
		if err != nil {
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
			return nil, err
		}

		lokiRes := resp.(*LokiResponse)
		if lokiRes.isFull() {
			return resp, nil
		}
		if result == nil {
			result = lokiRes
		}
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
