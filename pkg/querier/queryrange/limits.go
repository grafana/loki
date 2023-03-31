package queryrange

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/tenant"
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/timestamp"
	"github.com/weaveworks/common/httpgrpc"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/index/stats"
	"github.com/grafana/loki/pkg/util"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/util/spanlogger"
	"github.com/grafana/loki/pkg/util/validation"
)

const (
	limitErrTmpl                             = "maximum of series (%d) reached for a single query"
	maxSeriesErrTmpl                         = "max entries limit per query exceeded, limit > max_entries_limit (%d > %d)"
	requiredLabelsErrTmpl                    = "stream selector is missing required matchers [%s], labels present in the query were [%s]"
	requiredNumberLabelsErrTmpl              = "stream selector has less label matchers than required: (present: [%s], number_present: %d, required_number_label_matchers: %d)"
	limErrQueryTooManyBytesTmpl              = "the query would read too many bytes (query: %s, limit: %s); consider adding more specific stream selectors or reduce the time range of the query"
	limErrQuerierTooManyBytesTmpl            = "query too large to execute on a single querier: (query: %s, limit: %s); consider adding more specific stream selectors, reduce the time range of the query, or adjust parallelization settings"
	limErrQuerierTooManyBytesUnshardableTmpl = "un-shardable query too large to execute on a single querier: (query: %s, limit: %s); consider adding more specific stream selectors or reduce the time range of the query"
	limErrQuerierTooManyBytesShardableTmpl   = "shard query is too large to execute on a single querier: (query: %s, limit: %s); consider adding more specific stream selectors or reduce the time range of the query"
)

var (
	ErrMaxQueryParalellism = fmt.Errorf("querying is disabled, please contact your Loki operator")
)

// Limits extends the cortex limits interface with support for per tenant splitby parameters
type Limits interface {
	queryrangebase.Limits
	logql.Limits
	QuerySplitDuration(string) time.Duration
	MaxQuerySeries(context.Context, string) int
	MaxEntriesLimitPerQuery(context.Context, string) int
	MinShardingLookback(string) time.Duration
	// TSDBMaxQueryParallelism returns the limit to the number of split queries the
	// frontend will process in parallel for TSDB queries.
	TSDBMaxQueryParallelism(context.Context, string) int
	RequiredLabels(context.Context, string) []string
	RequiredNumberLabels(context.Context, string) int
	MaxQueryBytesRead(context.Context, string) int
	MaxQuerierBytesRead(context.Context, string) int
}

type limits struct {
	Limits
	// Use pointers so nil value can indicate if the value was set.
	splitDuration       *time.Duration
	maxQueryParallelism *int
	maxQueryBytesRead   *int
}

func (l limits) QuerySplitDuration(user string) time.Duration {
	if l.splitDuration == nil {
		return l.Limits.QuerySplitDuration(user)
	}
	return *l.splitDuration
}

func (l limits) TSDBMaxQueryParallelism(ctx context.Context, user string) int {
	if l.maxQueryParallelism == nil {
		return l.Limits.TSDBMaxQueryParallelism(ctx, user)
	}
	return *l.maxQueryParallelism
}

func (l limits) MaxQueryParallelism(ctx context.Context, user string) int {
	if l.maxQueryParallelism == nil {
		return l.Limits.MaxQueryParallelism(ctx, user)
	}
	return *l.maxQueryParallelism
}

// WithSplitByLimits will construct a Limits with a static split by duration.
func WithSplitByLimits(l Limits, splitBy time.Duration) Limits {
	return limits{
		Limits:        l,
		splitDuration: &splitBy,
	}
}

func WithMaxParallelism(l Limits, maxParallelism int) Limits {
	return limits{
		Limits:              l,
		maxQueryParallelism: &maxParallelism,
	}
}

type UserIDTransformer func(context.Context, string) string

// cacheKeyLimits intersects Limits and CacheSplitter
type cacheKeyLimits struct {
	Limits
	transformer UserIDTransformer
}

func (l cacheKeyLimits) GenerateCacheKey(ctx context.Context, userID string, r queryrangebase.Request) string {
	split := l.QuerySplitDuration(userID)

	var currentInterval int64
	if denominator := int64(split / time.Millisecond); denominator > 0 {
		currentInterval = r.GetStart() / denominator
	}

	if l.transformer != nil {
		userID = l.transformer(ctx, userID)
	}

	// include both the currentInterval and the split duration in key to ensure
	// a cache key can't be reused when an interval changes
	return fmt.Sprintf("%s:%s:%d:%d:%d", userID, r.GetQuery(), r.GetStep(), currentInterval, split)
}

type limitsMiddleware struct {
	Limits
	next queryrangebase.Handler
}

// NewLimitsMiddleware creates a new Middleware that enforces query limits.
func NewLimitsMiddleware(l Limits) queryrangebase.Middleware {
	return queryrangebase.MiddlewareFunc(func(next queryrangebase.Handler) queryrangebase.Handler {
		return limitsMiddleware{
			next:   next,
			Limits: l,
		}
	})
}

func (l limitsMiddleware) Do(ctx context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "limits")
	defer span.Finish()
	log := spanlogger.FromContext(ctx)
	defer log.Finish()

	tenantIDs, err := tenant.TenantIDs(ctx)
	if err != nil {
		return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
	}

	// Clamp the time range based on the max query lookback.
	lookbackCapture := func(id string) time.Duration { return l.MaxQueryLookback(ctx, id) }
	if maxQueryLookback := validation.SmallestPositiveNonZeroDurationPerTenant(tenantIDs, lookbackCapture); maxQueryLookback > 0 {
		minStartTime := util.TimeToMillis(time.Now().Add(-maxQueryLookback))

		if r.GetEnd() < minStartTime {
			// The request is fully outside the allowed range, so we can return an
			// empty response.
			level.Debug(log).Log(
				"msg", "skipping the execution of the query because its time range is before the 'max query lookback' setting",
				"reqStart", util.FormatTimeMillis(r.GetStart()),
				"redEnd", util.FormatTimeMillis(r.GetEnd()),
				"maxQueryLookback", maxQueryLookback)

			return NewEmptyResponse(r)
		}

		if r.GetStart() < minStartTime {
			// Replace the start time in the request.
			level.Debug(log).Log(
				"msg", "the start time of the query has been manipulated because of the 'max query lookback' setting",
				"original", util.FormatTimeMillis(r.GetStart()),
				"updated", util.FormatTimeMillis(minStartTime))

			r = r.WithStartEnd(minStartTime, r.GetEnd())
		}
	}

	// Enforce the max query length.
	lengthCapture := func(id string) time.Duration { return l.MaxQueryLength(ctx, id) }
	if maxQueryLength := validation.SmallestPositiveNonZeroDurationPerTenant(tenantIDs, lengthCapture); maxQueryLength > 0 {
		queryLen := timestamp.Time(r.GetEnd()).Sub(timestamp.Time(r.GetStart()))
		if queryLen > maxQueryLength {
			return nil, httpgrpc.Errorf(http.StatusBadRequest, validation.ErrQueryTooLong, queryLen, model.Duration(maxQueryLength))
		}
	}

	return l.next.Do(ctx, r)
}

type querySizeLimiter struct {
	logger            log.Logger
	next              queryrangebase.Handler
	statsHandler      queryrangebase.Handler
	cfg               []config.PeriodConfig
	maxLookBackPeriod time.Duration
	limitFunc         func(context.Context, string) int
	limitErrorTmpl    string
}

func newQuerySizeLimiter(
	next queryrangebase.Handler,
	cfg []config.PeriodConfig,
	logger log.Logger,
	limits Limits,
	codec queryrangebase.Codec,
	limitFunc func(context.Context, string) int,
	limitErrorTmpl string,
	statsHandler ...queryrangebase.Handler,
) *querySizeLimiter {
	q := &querySizeLimiter{
		logger:         logger,
		next:           next,
		cfg:            cfg,
		limitFunc:      limitFunc,
		limitErrorTmpl: limitErrorTmpl,
	}

	q.statsHandler = next
	if len(statsHandler) > 0 {
		q.statsHandler = statsHandler[0]
	}

	// Get MaxLookBackPeriod from downstream engine. This is needed for instant limited queries at getStatsForMatchers
	ng := logql.NewDownstreamEngine(logql.EngineOpts{LogExecutingQuery: false}, DownstreamHandler{next: next, limits: limits}, limits, logger)
	q.maxLookBackPeriod = ng.Opts().MaxLookBackPeriod

	return q
}

// NewQuerierSizeLimiterMiddleware creates a new Middleware that enforces query size limits after sharding and splitting.
// The errorTemplate should format two strings: the bytes that would be read and the bytes limit.
func NewQuerierSizeLimiterMiddleware(
	cfg []config.PeriodConfig,
	logger log.Logger,
	limits Limits,
	codec queryrangebase.Codec,
	statsHandler ...queryrangebase.Handler,
) queryrangebase.Middleware {
	return queryrangebase.MiddlewareFunc(func(next queryrangebase.Handler) queryrangebase.Handler {
		return newQuerySizeLimiter(next, cfg, logger, limits, codec, limits.MaxQuerierBytesRead, limErrQuerierTooManyBytesTmpl, statsHandler...)
	})
}

// NewQuerySizeLimiterMiddleware creates a new Middleware that enforces query size limits.
// The errorTemplate should format two strings: the bytes that would be read and the bytes limit.
func NewQuerySizeLimiterMiddleware(
	cfg []config.PeriodConfig,
	logger log.Logger,
	limits Limits,
	codec queryrangebase.Codec,
	statsHandler ...queryrangebase.Handler,
) queryrangebase.Middleware {
	return queryrangebase.MiddlewareFunc(func(next queryrangebase.Handler) queryrangebase.Handler {
		return newQuerySizeLimiter(next, cfg, logger, limits, codec, limits.MaxQueryBytesRead, limErrQueryTooManyBytesTmpl, statsHandler...)
	})
}

// getBytesReadForRequest returns the number of bytes that would be read for the query in r.
// Since the query expression may contain multiple stream matchers, this function sums up the
// bytes that will be read for each stream.
// E.g. for the following query:
//
//	count_over_time({job="foo"}[5m]) / count_over_time({job="bar"}[5m] offset 10m)
//
// this function will sum the bytes read for each of the following streams, taking into account
// individual intervals and offsets
//   - {job="foo"}
//   - {job="bar"}
func (q *querySizeLimiter) getBytesReadForRequest(ctx context.Context, r queryrangebase.Request) (uint64, error) {
	sp, ctx := opentracing.StartSpanFromContext(ctx, "querySizeLimiter.getBytesReadForRequest")
	defer sp.Finish()
	log := spanlogger.FromContextWithFallback(ctx, q.logger)
	defer log.Finish()

	expr, err := syntax.ParseExpr(r.GetQuery())
	if err != nil {
		return 0, err
	}

	matcherGroups, err := syntax.MatcherGroups(expr)
	if err != nil {
		return 0, err
	}

	// TODO: Set concurrency dynamically as in shardResolverForConf?
	start := time.Now()
	const maxConcurrentIndexReq = 10
	matcherStats, err := getStatsForMatchers(ctx, q.logger, q.statsHandler, model.Time(r.GetStart()), model.Time(r.GetEnd()), matcherGroups, maxConcurrentIndexReq, q.maxLookBackPeriod)
	if err != nil {
		return 0, err
	}

	combinedStats := stats.MergeStats(matcherStats...)

	level.Debug(log).Log(
		append(
			combinedStats.LoggingKeyValues(),
			"msg", "queried index",
			"type", "combined",
			"len", len(matcherStats),
			"max_parallelism", maxConcurrentIndexReq,
			"duration", time.Since(start),
			"total_bytes", strings.Replace(humanize.Bytes(combinedStats.Bytes), " ", "", 1),
		)...,
	)

	return combinedStats.Bytes, nil
}

func (q *querySizeLimiter) getSchemaCfg(r queryrangebase.Request) (config.PeriodConfig, error) {
	maxRVDuration, maxOffset, err := maxRangeVectorAndOffsetDuration(r.GetQuery())
	if err != nil {
		return config.PeriodConfig{}, errors.New("failed to get range-vector and offset duration: " + err.Error())
	}

	adjustedStart := int64(model.Time(r.GetStart()).Add(-maxRVDuration).Add(-maxOffset))
	adjustedEnd := int64(model.Time(r.GetEnd()).Add(-maxOffset))

	return ShardingConfigs(q.cfg).ValidRange(adjustedStart, adjustedEnd)
}

func (q *querySizeLimiter) guessLimitName() string {
	if q.limitErrorTmpl == limErrQueryTooManyBytesTmpl {
		return "MaxQueryBytesRead"
	}
	if q.limitErrorTmpl == limErrQuerierTooManyBytesTmpl ||
		q.limitErrorTmpl == limErrQuerierTooManyBytesShardableTmpl ||
		q.limitErrorTmpl == limErrQuerierTooManyBytesUnshardableTmpl {
		return "MaxQuerierBytesRead"
	}
	return "unknown"
}

func (q *querySizeLimiter) Do(ctx context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
	span, ctx := opentracing.StartSpanFromContext(ctx, "query_size_limits")
	defer span.Finish()
	log := spanlogger.FromContext(ctx)
	defer log.Finish()

	// Only support TSDB
	schemaCfg, err := q.getSchemaCfg(r)
	if err != nil {
		return nil, httpgrpc.Errorf(http.StatusInternalServerError, "Failed to get schema config: %s", err.Error())
	}
	if schemaCfg.IndexType != config.TSDBType {
		return q.next.Do(ctx, r)
	}

	tenantIDs, err := tenant.TenantIDs(ctx)
	if err != nil {
		return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
	}

	limitFuncCapture := func(id string) int { return q.limitFunc(ctx, id) }
	if maxBytesRead := validation.SmallestPositiveNonZeroIntPerTenant(tenantIDs, limitFuncCapture); maxBytesRead > 0 {
		bytesRead, err := q.getBytesReadForRequest(ctx, r)
		if err != nil {
			return nil, httpgrpc.Errorf(http.StatusInternalServerError, "Failed to get bytes read stats for query: %s", err.Error())
		}

		statsBytesStr := humanize.IBytes(bytesRead)
		maxBytesReadStr := humanize.IBytes(uint64(maxBytesRead))

		if bytesRead > uint64(maxBytesRead) {
			level.Warn(log).Log("msg", "Query exceeds limits", "status", "rejected", "limit_name", q.guessLimitName(), "limit_bytes", maxBytesReadStr, "resolved_bytes", statsBytesStr)
			return nil, httpgrpc.Errorf(http.StatusBadRequest, q.limitErrorTmpl, statsBytesStr, maxBytesReadStr)
		}

		level.Debug(log).Log("msg", "Query is within limits", "status", "accepted", "limit_name", q.guessLimitName(), "limit_bytes", maxBytesReadStr, "resolved_bytes", statsBytesStr)
	}

	return q.next.Do(ctx, r)
}

type seriesLimiter struct {
	hashes map[uint64]struct{}
	rw     sync.RWMutex
	buf    []byte // buf used for hashing to avoid allocations.

	maxSeries int
	next      queryrangebase.Handler
}

type seriesLimiterMiddleware int

// newSeriesLimiter creates a new series limiter middleware for use for a single request.
func newSeriesLimiter(maxSeries int) queryrangebase.Middleware {
	return seriesLimiterMiddleware(maxSeries)
}

// Wrap wraps a global handler and returns a per request limited handler.
// The handler returned is thread safe.
func (slm seriesLimiterMiddleware) Wrap(next queryrangebase.Handler) queryrangebase.Handler {
	return &seriesLimiter{
		hashes:    make(map[uint64]struct{}),
		maxSeries: int(slm),
		buf:       make([]byte, 0, 1024),
		next:      next,
	}
}

func (sl *seriesLimiter) Do(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
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
		lbs := logproto.FromLabelAdaptersToLabels(s.Labels)
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
	configs []config.PeriodConfig
	next    http.RoundTripper
	limits  Limits

	codec      queryrangebase.Codec
	middleware queryrangebase.Middleware
}

// NewLimitedRoundTripper creates a new roundtripper that enforces MaxQueryParallelism to the `next` roundtripper across `middlewares`.
func NewLimitedRoundTripper(next http.RoundTripper, codec queryrangebase.Codec, limits Limits, configs []config.PeriodConfig, middlewares ...queryrangebase.Middleware) http.RoundTripper {
	transport := limitedRoundTripper{
		configs:    configs,
		next:       next,
		codec:      codec,
		limits:     limits,
		middleware: queryrangebase.MergeMiddlewares(middlewares...),
	}
	return transport
}

type work struct {
	req    queryrangebase.Request
	ctx    context.Context
	result chan result
}

type result struct {
	response queryrangebase.Response
	err      error
}

func newWork(ctx context.Context, req queryrangebase.Request) work {
	return work{
		req:    req,
		ctx:    ctx,
		result: make(chan result, 1),
	}
}

func (rt limitedRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	var (
		wg           sync.WaitGroup
		intermediate = make(chan work)
		ctx, cancel  = context.WithCancel(r.Context())
	)
	defer func() {
		cancel()
		wg.Wait()
	}()

	// Do not forward any request header.
	request, err := rt.codec.DecodeRequest(ctx, r, nil)
	if err != nil {
		return nil, err
	}

	if span := opentracing.SpanFromContext(ctx); span != nil {
		request.LogToSpan(span)
	}
	tenantIDs, err := tenant.TenantIDs(ctx)
	if err != nil {
		return nil, httpgrpc.Errorf(http.StatusBadRequest, err.Error())
	}

	parallelism := MinWeightedParallelism(
		ctx,
		tenantIDs,
		rt.configs,
		rt.limits,
		model.Time(request.GetStart()),
		model.Time(request.GetEnd()),
	)

	if parallelism < 1 {
		return nil, httpgrpc.Errorf(http.StatusTooManyRequests, ErrMaxQueryParalellism.Error())
	}

	for i := 0; i < parallelism; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case w := <-intermediate:
					resp, err := rt.do(w.ctx, w.req)
					w.result <- result{response: resp, err: err}
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	response, err := rt.middleware.Wrap(
		queryrangebase.HandlerFunc(func(ctx context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
			w := newWork(ctx, r)
			select {
			case intermediate <- w:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
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

func (rt limitedRoundTripper) do(ctx context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
	sp, ctx := opentracing.StartSpanFromContext(ctx, "limitedRoundTripper.do")
	defer sp.Finish()

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

// WeightedParallelism will calculate the request parallelism to use
// based on the two fields:
// 1) `max_query_parallelism`:
// 2) `tsdb_max_query_parallelism`:
// For instance, if the max_query_parallelism=10,
// tsdb_max_query_parallelism=100, and the request is equally split
// between tsdb and non-tsdb period configs,
// the resulting parallelism will be
// 0.5 * 10 + 0.5 * 100 = 60
func WeightedParallelism(
	ctx context.Context,
	configs []config.PeriodConfig,
	user string,
	l Limits,
	start, end model.Time,
) int {
	logger := util_log.WithContext(ctx, util_log.Logger)

	tsdbMaxQueryParallelism := l.TSDBMaxQueryParallelism(ctx, user)
	regMaxQueryParallelism := l.MaxQueryParallelism(ctx, user)
	if tsdbMaxQueryParallelism+regMaxQueryParallelism == 0 {
		level.Info(logger).Log("msg", "querying disabled for tenant")
		return 0
	}

	// query end before start would anyways error out so just short circuit and return 1
	if end < start {
		level.Warn(logger).Log("msg", "query end time before start, letting downstream code handle it gracefully", "start", start, "end", end)
		return 1
	}

	// Return first index of desired period configs
	i := sort.Search(len(configs), func(i int) bool {
		// return true when there is no overlap with query & current
		// config because query is in future
		// or
		// there is overlap with current config
		finalOrFuture := i == len(configs)-1 || configs[i].From.After(end)
		if finalOrFuture {
			return true
		}
		// qEnd not before start && qStart not after end
		overlapCurrent := !end.Before(configs[i].From.Time) && !start.After(configs[i+1].From.Time)
		return overlapCurrent
	})

	// There was no overlapping index. This can only happen when a time
	// was requested before the first period config start. In that case, just
	// use the first period config. It should error elsewhere.
	if i == len(configs) {
		i = 0
	}

	// If start == end, this is an instant query;
	// use the appropriate parallelism type for
	// the active configuration
	if start.Equal(end) {
		switch configs[i].IndexType {
		case config.TSDBType:
			return l.TSDBMaxQueryParallelism(ctx, user)
		}
		return l.MaxQueryParallelism(ctx, user)

	}

	var tsdbDur, otherDur time.Duration

	for ; i < len(configs) && configs[i].From.Before(end); i++ {
		_, from := minMaxModelTime(start, configs[i].From.Time)
		through := end
		if i+1 < len(configs) {
			through, _ = minMaxModelTime(end, configs[i+1].From.Time)
		}

		dur := through.Sub(from)

		if i+1 < len(configs) && configs[i+1].From.Time.Before(end) {
			dur = configs[i+1].From.Time.Sub(from)
		}
		if ty := configs[i].IndexType; ty == config.TSDBType {
			tsdbDur += dur
		} else {
			otherDur += dur
		}
	}

	totalDur := int(tsdbDur + otherDur)
	// If totalDur is 0, the query likely does not overlap any of the schema configs so just use parallelism of 1 and
	// let the downstream code handle it.
	if totalDur == 0 {
		level.Warn(logger).Log("msg", "could not determine query overlaps on tsdb vs non-tsdb schemas, likely due to query not overlapping any of the schema configs,"+
			"letting downstream code handle it gracefully", "start", start, "end", end)
		return 1
	}

	tsdbPart := int(tsdbDur) * tsdbMaxQueryParallelism / totalDur
	regPart := int(otherDur) * regMaxQueryParallelism / totalDur

	if combined := regPart + tsdbPart; combined > 0 {
		return combined
	}

	// As long as the actual config is not zero,
	// ensure at least 1 parallelism to account for integer division
	// in unlikely edge cases such as two configs with parallelism of 1
	// being rounded down to zero
	if (tsdbMaxQueryParallelism > 0 && tsdbDur > 0) || (regMaxQueryParallelism > 0 && otherDur > 0) {
		return 1
	}

	return 0
}

func minMaxModelTime(a, b model.Time) (min, max model.Time) {
	if a.Before(b) {
		return a, b
	}
	return b, a
}

func MinWeightedParallelism(ctx context.Context, tenantIDs []string, configs []config.PeriodConfig, l Limits, start, end model.Time) int {
	return validation.SmallestPositiveIntPerTenant(tenantIDs, func(user string) int {
		return WeightedParallelism(
			ctx,
			configs,
			user,
			l,
			start,
			end,
		)
	})
}

// validates log entries limits
func validateMaxEntriesLimits(req *http.Request, reqLimit uint32, limits Limits) error {
	tenantIDs, err := tenant.TenantIDs(req.Context())
	if err != nil {
		return httpgrpc.Errorf(http.StatusBadRequest, err.Error())
	}

	maxEntriesCapture := func(id string) int { return limits.MaxEntriesLimitPerQuery(req.Context(), id) }
	maxEntriesLimit := validation.SmallestPositiveNonZeroIntPerTenant(tenantIDs, maxEntriesCapture)

	if int(reqLimit) > maxEntriesLimit && maxEntriesLimit != 0 {
		return fmt.Errorf(maxSeriesErrTmpl, reqLimit, maxEntriesLimit)
	}
	return nil
}

func validateMatchers(req *http.Request, limits Limits, matchers []*labels.Matcher) error {
	tenants, err := tenant.TenantIDs(req.Context())
	if err != nil {
		return err
	}

	actual := make(map[string]struct{}, len(matchers))
	var present []string
	for _, m := range matchers {
		actual[m.Name] = struct{}{}
		present = append(present, m.Name)
	}

	// Enforce RequiredLabels limit
	for _, tenant := range tenants {
		required := limits.RequiredLabels(req.Context(), tenant)
		var missing []string
		for _, label := range required {
			if _, found := actual[label]; !found {
				missing = append(missing, label)
			}
		}

		if len(missing) > 0 {
			return fmt.Errorf(requiredLabelsErrTmpl, strings.Join(missing, ", "), strings.Join(present, ", "))
		}
	}

	// Enforce RequiredNumberLabels limit.
	// The reason to enforce this one after RequiredLabels is to avoid users
	// from adding enough label matchers to pass the RequiredNumberLabels limit but then
	// having to modify them to use the ones required by RequiredLabels.
	requiredNumberLabelsCapture := func(id string) int { return limits.RequiredNumberLabels(req.Context(), id) }
	if requiredNumberLabels := validation.SmallestPositiveNonZeroIntPerTenant(tenants, requiredNumberLabelsCapture); requiredNumberLabels > 0 {
		if len(present) < requiredNumberLabels {
			return fmt.Errorf(requiredNumberLabelsErrTmpl, strings.Join(present, ", "), len(present), requiredNumberLabels)
		}
	}

	return nil
}
