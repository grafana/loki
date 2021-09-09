package queryrange

import (
	"context"
	"net/http"
	"time"

	"github.com/cortexproject/cortex/pkg/querier/queryrange"
	"github.com/grafana/dskit/tenant"
	"github.com/opentracing/opentracing-go"
	otlog "github.com/opentracing/opentracing-go/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/weaveworks/common/httpgrpc"

	"github.com/grafana/loki/pkg/logproto"
)

type lokiResult struct {
	req queryrange.Request
	ch  chan *packedResp
}

type packedResp struct {
	resp queryrange.Response
	err  error
}

type SplitByMetrics struct {
	splits prometheus.Histogram
}

func NewSplitByMetrics(r prometheus.Registerer) *SplitByMetrics {
	return &SplitByMetrics{
		splits: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: "loki",
			Name:      "query_frontend_partitions",
			Help:      "Number of time-based partitions (sub-requests) per request",
			Buckets:   prometheus.ExponentialBuckets(1, 4, 5), // 1 -> 1024
		}),
	}
}

type splitByInterval struct {
	next     queryrange.Handler
	limits   Limits
	merger   queryrange.Merger
	metrics  *SplitByMetrics
	splitter Splitter
}

type Splitter func(req queryrange.Request, interval time.Duration) []queryrange.Request

// SplitByIntervalMiddleware creates a new Middleware that splits log requests by a given interval.
func SplitByIntervalMiddleware(limits Limits, merger queryrange.Merger, splitter Splitter, metrics *SplitByMetrics) queryrange.Middleware {
	return queryrange.MiddlewareFunc(func(next queryrange.Handler) queryrange.Handler {
		return &splitByInterval{
			next:     next,
			limits:   limits,
			merger:   merger,
			metrics:  metrics,
			splitter: splitter,
		}
	})
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
	userID string,
) ([]queryrange.Response, error) {
	var responses []queryrange.Response
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	ch := h.Feed(ctx, input)

	// queries with 0 limits should not be exited early
	var unlimited bool
	if threshold == 0 {
		unlimited = true
	}

	// don't spawn unnecessary goroutines
	var p int = parallelism
	if len(input) < parallelism {
		p = len(input)
	}

	// per request wrapped handler for limiting the amount of series.
	next := newSeriesLimiter(h.limits.MaxQuerySeries(userID)).Wrap(h.next)
	for i := 0; i < p; i++ {
		go h.loop(ctx, ch, next)
	}

	for _, x := range input {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case data := <-x.ch:
			if data.err != nil {
				return nil, data.err
			}

			responses = append(responses, data.resp)

			// see if we can exit early if a limit has been reached
			if casted, ok := data.resp.(*LokiResponse); !unlimited && ok {
				threshold -= casted.Count()

				if threshold <= 0 {
					return responses, nil
				}

			}

		}
	}

	return responses, nil
}

func (h *splitByInterval) loop(ctx context.Context, ch <-chan *lokiResult, next queryrange.Handler) {
	for data := range ch {

		sp, ctx := opentracing.StartSpanFromContext(ctx, "interval")
		data.req.LogToSpan(sp)

		resp, err := next.Do(ctx, data.req)

		select {
		case <-ctx.Done():
			sp.Finish()
			return
		case data.ch <- &packedResp{resp, err}:
			sp.Finish()
		}
	}
}

func (h *splitByInterval) Do(ctx context.Context, r queryrange.Request) (queryrange.Response, error) {
	userid, err := tenant.ID(ctx)
	if err != nil {
		return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
	}

	interval := h.limits.QuerySplitDuration(userid)
	// skip split by if unset
	if interval == 0 {
		return h.next.Do(ctx, r)
	}

	intervals := h.splitter(r, interval)
	h.metrics.splits.Observe(float64(len(intervals)))

	// no interval should not be processed by the frontend.
	if len(intervals) == 0 {
		return h.next.Do(ctx, r)
	}

	if sp := opentracing.SpanFromContext(ctx); sp != nil {
		sp.LogFields(otlog.Int("n_intervals", len(intervals)))
	}

	var limit int64
	switch req := r.(type) {
	case *LokiRequest:
		limit = int64(req.Limit)
		if req.Direction == logproto.BACKWARD {
			for i, j := 0, len(intervals)-1; i < j; i, j = i+1, j-1 {
				intervals[i], intervals[j] = intervals[j], intervals[i]
			}
		}
	case *LokiSeriesRequest, *LokiLabelNamesRequest:
		// Set this to 0 since this is not used in Series/Labels Request.
		limit = 0
	default:
		return nil, httpgrpc.Errorf(http.StatusBadRequest, "unknown request type")
	}

	input := make([]*lokiResult, 0, len(intervals))
	for _, interval := range intervals {
		input = append(input, &lokiResult{
			req: interval,
			ch:  make(chan *packedResp),
		})
	}

	resps, err := h.Process(ctx, h.limits.MaxQueryParallelism(userid), limit, input, userid)
	if err != nil {
		return nil, err
	}
	return h.merger.MergeResponse(resps...)
}

func splitByTime(req queryrange.Request, interval time.Duration) []queryrange.Request {
	var reqs []queryrange.Request

	switch r := req.(type) {
	case *LokiRequest:
		forInterval(interval, r.StartTs, r.EndTs, func(start, end time.Time) {
			reqs = append(reqs, &LokiRequest{
				Query:     r.Query,
				Limit:     r.Limit,
				Step:      r.Step,
				Direction: r.Direction,
				Path:      r.Path,
				StartTs:   start,
				EndTs:     end,
			})
		})
	case *LokiSeriesRequest:
		forInterval(interval, r.StartTs, r.EndTs, func(start, end time.Time) {
			reqs = append(reqs, &LokiSeriesRequest{
				Match:   r.Match,
				Path:    r.Path,
				StartTs: start,
				EndTs:   end,
				Shards:  r.Shards,
			})
		})
	case *LokiLabelNamesRequest:
		forInterval(interval, r.StartTs, r.EndTs, func(start, end time.Time) {
			reqs = append(reqs, &LokiLabelNamesRequest{
				Path:    r.Path,
				StartTs: start,
				EndTs:   end,
			})
		})
	default:
		return nil
	}
	return reqs
}

func forInterval(interval time.Duration, start, end time.Time, callback func(start, end time.Time)) {
	for start := start; start.Before(end); start = start.Add(interval) {
		newEnd := start.Add(interval)
		if newEnd.After(end) {
			newEnd = end
		}
		callback(start, newEnd)
	}
}

func splitMetricByTime(r queryrange.Request, interval time.Duration) []queryrange.Request {
	var reqs []queryrange.Request
	lokiReq := r.(*LokiRequest)
	for start := lokiReq.StartTs; start.Before(lokiReq.EndTs); start = nextIntervalBoundary(start, r.GetStep(), interval).Add(time.Duration(r.GetStep()) * time.Millisecond) {
		end := nextIntervalBoundary(start, r.GetStep(), interval)
		if end.Add(time.Duration(r.GetStep())*time.Millisecond).After(lokiReq.EndTs) || end.Add(time.Duration(r.GetStep())*time.Millisecond) == lokiReq.EndTs {
			end = lokiReq.EndTs
		}
		reqs = append(reqs, &LokiRequest{
			Query:     lokiReq.Query,
			Limit:     lokiReq.Limit,
			Step:      lokiReq.Step,
			Direction: lokiReq.Direction,
			Path:      lokiReq.Path,
			StartTs:   start,
			EndTs:     end,
		})
	}
	return reqs
}

// Round up to the step before the next interval boundary.
func nextIntervalBoundary(t time.Time, step int64, interval time.Duration) time.Time {
	stepNs := step * 1e6
	nsPerInterval := interval.Nanoseconds()
	startOfNextInterval := ((t.UnixNano() / nsPerInterval) + 1) * nsPerInterval
	// ensure that target is a multiple of steps away from the start time
	target := startOfNextInterval - ((startOfNextInterval - t.UnixNano()) % stepNs)
	if target == startOfNextInterval {
		target -= stepNs
	}
	return time.Unix(0, target)
}
