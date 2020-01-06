package queryrange

import (
	"context"
	"time"

	"github.com/cortexproject/cortex/pkg/querier/queryrange"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/opentracing/opentracing-go"
	otlog "github.com/opentracing/opentracing-go/log"
	"github.com/weaveworks/common/user"
)

// SplitByIntervalMiddleware creates a new Middleware that splits log requests by a given interval.
func SplitByIntervalMiddleware(interval time.Duration, limits queryrange.Limits, merger queryrange.Merger) queryrange.Middleware {
	return queryrange.MiddlewareFunc(func(next queryrange.Handler) queryrange.Handler {
		return &splitByInterval{
			next:     next,
			limits:   limits,
			merger:   merger,
			interval: interval,
		}
	})
}

type lokiResult struct {
	req  queryrange.Request
	resp chan queryrange.Response
	err  chan error
}

type splitByInterval struct {
	next     queryrange.Handler
	limits   queryrange.Limits
	merger   queryrange.Merger
	interval time.Duration
}

func (h *splitByInterval) Feed(ctx context.Context, input []*lokiResult) chan *lokiResult {
	ch := make(chan *lokiResult)

	go func() {
		defer close(ch)
		for _, d := range input {
			select {
			case <-ctx.Done():
				return
			case ch <- d:
				continue
			}
		}
	}()

	return ch
}

func (h *splitByInterval) Process(
	ctx context.Context,
	parallelism int,
	threshold int64,
	input []*lokiResult,
) (responses []queryrange.Response, err error) {
	ch := h.Feed(ctx, input)

	for i := 0; i < parallelism; i++ {
		go h.loop(ctx, ch)
	}

	for _, x := range input {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case err := <-x.err:
			return nil, err
		case resp := <-x.resp:

			responses = append(responses, resp)

			// see if we can exit early if a limit has been reached
			threshold -= resp.(*LokiResponse).Count()
			if threshold <= 0 {
				return responses, nil
			}
		}

	}

	return responses, nil
}

func (h *splitByInterval) loop(ctx context.Context, ch <-chan *lokiResult) {

	for data := range ch {
		resp, err := h.next.Do(ctx, data.req)
		if err != nil {
			data.err <- err
		} else {
			data.resp <- resp
		}
	}
}

func (h *splitByInterval) Do(ctx context.Context, r queryrange.Request) (queryrange.Response, error) {
	lokiRequest := r.(*LokiRequest)

	userid, err := user.ExtractOrgID(ctx)
	if err != nil {
		return nil, err
	}

	intervals := splitByTime(lokiRequest, h.interval)

	if lokiRequest.Direction == logproto.BACKWARD {
		for i, j := 0, len(intervals)-1; i < j; i, j = i+1, j-1 {
			intervals[i], intervals[j] = intervals[j], intervals[i]
		}
	}

	input := make([]*lokiResult, 0, len(intervals))
	for _, interval := range intervals {
		input = append(input, &lokiResult{
			req:  interval,
			resp: make(chan queryrange.Response),
			err:  make(chan error),
		})
	}

	resps, err := h.Process(ctx, h.limits.MaxQueryParallelism(userid), int64(lokiRequest.Limit), input)
	if err != nil {
		return nil, err
	}

	return h.merger.MergeResponse(resps...)
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
