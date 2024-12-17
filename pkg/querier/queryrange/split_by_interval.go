package queryrange

import (
	"context"
	"net/http"
	"time"

	"github.com/grafana/dskit/httpgrpc"
	"github.com/opentracing/opentracing-go"
	otlog "github.com/opentracing/opentracing-go/log"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/util/math"

	"github.com/grafana/dskit/tenant"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/util/validation"
)

type lokiResult struct {
	req queryrangebase.Request
	ch  chan *packedResp
}

type packedResp struct {
	resp queryrangebase.Response
	err  error
}

type SplitByMetrics struct {
	splits prometheus.Histogram
}

func NewSplitByMetrics(r prometheus.Registerer) *SplitByMetrics {
	return &SplitByMetrics{
		splits: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Name:      "query_frontend_partitions",
			Help:      "Number of time-based partitions (sub-requests) per request",
			Buckets:   prometheus.ExponentialBuckets(1, 4, 5), // 1 -> 1024
		}),
	}
}

type splitByInterval struct {
	configs  []config.PeriodConfig
	next     queryrangebase.Handler
	limits   Limits
	merger   queryrangebase.Merger
	metrics  *SplitByMetrics
	splitter splitter
}

// SplitByIntervalMiddleware creates a new Middleware that splits log requests by a given interval.
func SplitByIntervalMiddleware(configs []config.PeriodConfig, limits Limits, merger queryrangebase.Merger, splitter splitter, metrics *SplitByMetrics) queryrangebase.Middleware {
	if metrics == nil {
		metrics = NewSplitByMetrics(nil)
	}

	return queryrangebase.MiddlewareFunc(func(next queryrangebase.Handler) queryrangebase.Handler {
		return &splitByInterval{
			configs:  configs,
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
	maxSeries int,
) ([]queryrangebase.Response, error) {
	var responses []queryrangebase.Response
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(errors.New("split by interval process canceled"))

	ch := h.Feed(ctx, input)

	// queries with 0 limits should not be exited early
	var unlimited bool
	if threshold == 0 {
		unlimited = true
	}

	// Parallelism will be at least 1
	p := math.Max(parallelism, 1)
	// don't spawn unnecessary goroutines
	if len(input) < parallelism {
		p = len(input)
	}

	// per request wrapped handler for limiting the amount of series.
	next := newSeriesLimiter(maxSeries).Wrap(h.next)
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

func (h *splitByInterval) loop(ctx context.Context, ch <-chan *lokiResult, next queryrangebase.Handler) {
	for data := range ch {

		sp, ctx := opentracing.StartSpanFromContext(ctx, "interval")
		data.req.LogToSpan(sp)

		resp, err := next.Do(ctx, data.req)
		sp.Finish()

		select {
		case <-ctx.Done():
			return
		case data.ch <- &packedResp{resp, err}:
			// The parent Process method will return on the first error. So stop
			// processng.
			if err != nil {
				return
			}
		}
	}
}

func (h *splitByInterval) Do(ctx context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
	tenantIDs, err := tenant.TenantIDs(ctx)
	if err != nil {
		return nil, httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
	}

	var interval time.Duration
	switch r.(type) {
	case *LokiSeriesRequest, *LabelRequest:
		interval = validation.MaxDurationOrZeroPerTenant(tenantIDs, h.limits.MetadataQuerySplitDuration)
	default:
		interval = validation.SmallestPositiveNonZeroDurationPerTenant(tenantIDs, h.limits.QuerySplitDuration)
	}

	// skip split by if unset
	if interval == 0 {
		return h.next.Do(ctx, r)
	}

	intervals, err := h.splitter.split(time.Now().UTC(), tenantIDs, r, interval)
	if err != nil {
		return nil, err
	}

	h.metrics.splits.Observe(float64(len(intervals)))

	// no interval should not be processed by the frontend.
	if len(intervals) == 0 {
		return h.next.Do(ctx, r)
	}

	if sp := opentracing.SpanFromContext(ctx); sp != nil {
		sp.LogFields(otlog.Int("n_intervals", len(intervals)))
	}

	if len(intervals) == 1 {
		return h.next.Do(ctx, intervals[0])
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
	case *DetectedFieldsRequest:
		limit = int64(req.LineLimit)
		for i, j := 0, len(intervals)-1; i < j; i, j = i+1, j-1 {
			intervals[i], intervals[j] = intervals[j], intervals[i]
		}
	case *LokiSeriesRequest, *LabelRequest, *logproto.IndexStatsRequest, *logproto.VolumeRequest, *logproto.ShardsRequest, *DetectedLabelsRequest:
		// Set this to 0 since this is not used in Series/Labels/Index Request.
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

	maxSeriesCapture := func(id string) int { return h.limits.MaxQuerySeries(ctx, id) }
	maxSeries := validation.SmallestPositiveIntPerTenant(tenantIDs, maxSeriesCapture)
	maxParallelism := MinWeightedParallelism(ctx, tenantIDs, h.configs, h.limits, model.Time(r.GetStart().UnixMilli()), model.Time(r.GetEnd().UnixMilli()))
	resps, err := h.Process(ctx, maxParallelism, limit, input, maxSeries)
	if err != nil {
		return nil, err
	}
	return h.merger.MergeResponse(resps...)
}

// maxRangeVectorAndOffsetDurationFromQueryString
func maxRangeVectorAndOffsetDurationFromQueryString(q string) (time.Duration, time.Duration, error) {
	parsed, err := syntax.ParseExpr(q)
	if err != nil {
		return 0, 0, err
	}
	return maxRangeVectorAndOffsetDuration(parsed)
}

// maxRangeVectorAndOffsetDuration returns the maximum range vector and offset duration within a LogQL query.
func maxRangeVectorAndOffsetDuration(expr syntax.Expr) (time.Duration, time.Duration, error) {
	if _, ok := expr.(syntax.SampleExpr); !ok {
		return 0, 0, nil
	}

	var maxRVDuration, maxOffset time.Duration
	expr.Walk(func(e syntax.Expr) {
		if r, ok := e.(*syntax.LogRange); ok {
			if r.Interval > maxRVDuration {
				maxRVDuration = r.Interval
			}
			if r.Offset > maxOffset {
				maxOffset = r.Offset
			}
		}
	})
	return maxRVDuration, maxOffset, nil
}
