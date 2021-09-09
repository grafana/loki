package queryrange

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/cortexproject/cortex/pkg/cortexpb"
	"github.com/cortexproject/cortex/pkg/querier/queryrange"
	"github.com/grafana/dskit/tenant"
	"github.com/opentracing/opentracing-go"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/logql"
)

const (
	limitErrTmpl = "maximum of series (%d) reached for a single query"
)

// Limits extends the cortex limits interface with support for per tenant splitby parameters
type Limits interface {
	queryrange.Limits
	logql.Limits
	QuerySplitDuration(string) time.Duration
	MaxQuerySeries(string) int
	MaxEntriesLimitPerQuery(string) int
	MinShardingLookback(string) time.Duration
}

type limits struct {
	Limits
	splitDuration time.Duration
	overrides     bool
}

func (l limits) QuerySplitDuration(user string) time.Duration {
	if !l.overrides {
		return l.splitDuration
	}
	dur := l.Limits.QuerySplitDuration(user)
	if dur == 0 {
		return l.splitDuration
	}
	return dur
}

// WithDefaults will construct a Limits with a default value for QuerySplitDuration when no overrides are present.
func WithDefaultLimits(l Limits, conf queryrange.Config) Limits {
	res := limits{
		Limits:    l,
		overrides: true,
	}

	if conf.SplitQueriesByInterval != 0 {
		res.splitDuration = conf.SplitQueriesByInterval
	}

	return res
}

// WithSplitByLimits will construct a Limits with a static split by duration.
func WithSplitByLimits(l Limits, splitBy time.Duration) Limits {
	return limits{
		Limits:        l,
		splitDuration: splitBy,
	}
}

// cacheKeyLimits intersects Limits and CacheSplitter
type cacheKeyLimits struct {
	Limits
}

// GenerateCacheKey will panic if it encounters a 0 split duration. We ensure against this by requiring
// a nonzero split interval when caching is enabled
func (l cacheKeyLimits) GenerateCacheKey(userID string, r queryrange.Request) string {
	split := l.QuerySplitDuration(userID)
	currentInterval := r.GetStart() / int64(split/time.Millisecond)
	// include both the currentInterval and the split duration in key to ensure
	// a cache key can't be reused when an interval changes
	return fmt.Sprintf("%s:%s:%d:%d:%d", userID, r.GetQuery(), r.GetStep(), currentInterval, split)
}

type seriesLimiter struct {
	hashes map[uint64]struct{}
	rw     sync.RWMutex
	buf    []byte // buf used for hashing to avoid allocations.

	maxSeries int
	next      queryrange.Handler
}

type seriesLimiterMiddleware int

// newSeriesLimiter creates a new series limiter middleware for use for a single request.
func newSeriesLimiter(maxSeries int) queryrange.Middleware {
	return seriesLimiterMiddleware(maxSeries)
}

// Wrap wraps a global handler and returns a per request limited handler.
// The handler returned is thread safe.
func (slm seriesLimiterMiddleware) Wrap(next queryrange.Handler) queryrange.Handler {
	return &seriesLimiter{
		hashes:    make(map[uint64]struct{}),
		maxSeries: int(slm),
		buf:       make([]byte, 0, 1024),
		next:      next,
	}
}

func (sl *seriesLimiter) Do(ctx context.Context, req queryrange.Request) (queryrange.Response, error) {
	// no need to fire a request if the limit is already reached.
	if sl.isLimitReached() {
		return nil, httpgrpc.Errorf(http.StatusBadRequest, limitErrTmpl, sl.maxSeries)
	}
	res, err := sl.next.Do(ctx, req)
	if err != nil {
		return res, err
	}
	promResponse, ok := res.(*LokiPromResponse)
	if !ok {
		return res, nil
	}
	if promResponse.Response == nil {
		return res, nil
	}
	sl.rw.Lock()
	var hash uint64
	for _, s := range promResponse.Response.Data.Result {
		lbs := cortexpb.FromLabelAdaptersToLabels(s.Labels)
		hash, sl.buf = lbs.HashWithoutLabels(sl.buf, []string(nil)...)
		sl.hashes[hash] = struct{}{}
	}
	sl.rw.Unlock()
	if sl.isLimitReached() {
		return nil, httpgrpc.Errorf(http.StatusBadRequest, limitErrTmpl, sl.maxSeries)
	}
	return res, nil
}

func (sl *seriesLimiter) isLimitReached() bool {
	sl.rw.RLock()
	defer sl.rw.RUnlock()
	return len(sl.hashes) > sl.maxSeries
}

type limitedRoundTripper struct {
	next   http.RoundTripper
	limits Limits

	codec      queryrange.Codec
	middleware queryrange.Middleware
}

// NewLimitedRoundTripper creates a new roundtripper that enforces MaxQueryParallelism to the `next` roundtripper across `middlewares`.
func NewLimitedRoundTripper(next http.RoundTripper, codec queryrange.Codec, limits Limits, middlewares ...queryrange.Middleware) http.RoundTripper {
	transport := limitedRoundTripper{
		next:       next,
		codec:      codec,
		limits:     limits,
		middleware: queryrange.MergeMiddlewares(middlewares...),
	}
	return transport
}

type work struct {
	req    queryrange.Request
	ctx    context.Context
	result chan result
}

type result struct {
	response queryrange.Response
	err      error
}

func newWork(ctx context.Context, req queryrange.Request) work {
	return work{
		req:    req,
		ctx:    ctx,
		result: make(chan result, 1),
	}
}

func (rt limitedRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	var wg sync.WaitGroup
	intermediate := make(chan work)
	defer func() {
		wg.Wait()
		close(intermediate)
	}()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	request, err := rt.codec.DecodeRequest(ctx, r)
	if err != nil {
		return nil, err
	}

	if span := opentracing.SpanFromContext(ctx); span != nil {
		request.LogToSpan(span)
	}
	userid, err := tenant.ID(ctx)
	if err != nil {
		return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
	}

	parallelism := rt.limits.MaxQueryParallelism(userid)

	for i := 0; i < parallelism; i++ {
		go func() {
			for w := range intermediate {
				resp, err := rt.do(w.ctx, w.req)
				select {
				case w.result <- result{response: resp, err: err}:
				case <-w.ctx.Done():
					w.result <- result{err: w.ctx.Err()}
				}

			}
		}()
	}

	response, err := rt.middleware.Wrap(
		queryrange.HandlerFunc(func(ctx context.Context, r queryrange.Request) (queryrange.Response, error) {
			wg.Add(1)
			defer wg.Done()

			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			w := newWork(ctx, r)
			intermediate <- w
			select {
			case response := <-w.result:
				return response.response, response.err
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		})).Do(ctx, request)
	if err != nil {
		return nil, err
	}
	return rt.codec.EncodeResponse(ctx, response)
}

func (rt limitedRoundTripper) do(ctx context.Context, r queryrange.Request) (queryrange.Response, error) {
	request, err := rt.codec.EncodeRequest(ctx, r)
	if err != nil {
		return nil, err
	}

	if err := user.InjectOrgIDIntoHTTPRequest(ctx, request); err != nil {
		return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
	}

	response, err := rt.next.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()

	return rt.codec.DecodeResponse(ctx, response, r)
}
