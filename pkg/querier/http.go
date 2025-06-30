package querier

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/middleware"
	"github.com/grafana/dskit/tenant"
	"github.com/grafana/dskit/user"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/thanos-io/objstore"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/engine"
	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	querier_limits "github.com/grafana/loki/v3/pkg/querier/limits"
	"github.com/grafana/loki/v3/pkg/querier/pattern"
	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	index_stats "github.com/grafana/loki/v3/pkg/storage/stores/index/stats"
	"github.com/grafana/loki/v3/pkg/tracing"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
	utillog "github.com/grafana/loki/v3/pkg/util/log"
	serverutil "github.com/grafana/loki/v3/pkg/util/server"
	"github.com/grafana/loki/v3/pkg/util/spanlogger"
	util_validation "github.com/grafana/loki/v3/pkg/util/validation"
)

type QueryResponse struct {
	ResultType parser.ValueType `json:"resultType"`
	Result     parser.Value     `json:"result"`
}

// nolint // QuerierAPI defines HTTP handler functions for the querier.
type QuerierAPI struct {
	querier  Querier
	cfg      Config
	limits   querier_limits.Limits
	engineV1 logql.Engine // Loki's current query engine
	engineV2 logql.Engine // Loki's next generation query engine
	logger   log.Logger
}

// NewQuerierAPI returns an instance of the QuerierAPI.
func NewQuerierAPI(cfg Config, querier Querier, limits querier_limits.Limits, store objstore.Bucket, reg prometheus.Registerer, logger log.Logger) *QuerierAPI {
	return &QuerierAPI{
		cfg:      cfg,
		limits:   limits,
		querier:  querier,
		engineV1: logql.NewEngine(cfg.Engine, querier, limits, logger),
		engineV2: engine.New(cfg.Engine, store, limits, reg, logger),
		logger:   logger,
	}
}

// RangeQueryHandler is a http.HandlerFunc for range queries and legacy log queries
func (q *QuerierAPI) RangeQueryHandler(ctx context.Context, req *queryrange.LokiRequest) (logqlmodel.Result, error) {
	var result logqlmodel.Result
	logger := utillog.WithContext(ctx, q.logger)

	if err := q.validateMaxEntriesLimits(ctx, req.Plan.AST, req.Limit); err != nil {
		return result, err
	}

	params, err := queryrange.ParamsFromRequest(req)
	if err != nil {
		return result, err
	}

	if q.cfg.Engine.EnableV2Engine && hasDataObjectsAvailable(params.Start(), params.End()) {
		query := q.engineV2.Query(params)
		result, err = query.Exec(ctx)
		if err == nil {
			return result, err
		}
		if !errors.Is(err, engine.ErrNotSupported) {
			level.Error(logger).Log("msg", "query execution failed with new query engine", "err", err)
			return result, errors.Wrap(err, "failed with new execution engine")
		}
		level.Warn(logger).Log("msg", "falling back to legacy query engine", "err", err)
	}

	query := q.engineV1.Query(params)
	return query.Exec(ctx)
}

func hasDataObjectsAvailable(_, end time.Time) bool {
	// Data objects in object storage lag behind 20-30 minutes.
	// We are generous and only enable v2 engine queries that end earlier than 1 hour ago, to ensure data objects are available.
	return end.Before(time.Now().Add(-1 * time.Hour))
}

// InstantQueryHandler is a http.HandlerFunc for instant queries.
func (q *QuerierAPI) InstantQueryHandler(ctx context.Context, req *queryrange.LokiInstantRequest) (logqlmodel.Result, error) {
	// do not allow log selector expression (aka log query) as instant query
	if _, ok := req.Plan.AST.(syntax.SampleExpr); !ok {
		return logqlmodel.Result{}, logqlmodel.ErrUnsupportedSyntaxForInstantQuery
	}

	if err := q.validateMaxEntriesLimits(ctx, req.Plan.AST, req.Limit); err != nil {
		return logqlmodel.Result{}, err
	}

	params, err := queryrange.ParamsFromRequest(req)
	if err != nil {
		return logqlmodel.Result{}, err
	}
	query := q.engineV1.Query(params)
	return query.Exec(ctx)
}

// LabelHandler is a http.HandlerFunc for handling label queries.
func (q *QuerierAPI) LabelHandler(ctx context.Context, req *logproto.LabelRequest) (*logproto.LabelResponse, error) {
	timer := prometheus.NewTimer(logql.QueryTime.WithLabelValues(logql.QueryTypeLabels))
	defer timer.ObserveDuration()

	start := time.Now()
	statsCtx, ctx := stats.NewContext(ctx)

	resp, err := q.querier.Label(ctx, req)
	if err != nil {
		return nil, err
	}

	if resp != nil && q.metricAggregationEnabled(ctx) {
		resp.Values = q.filterAggregatedMetricsLabel(resp.Values)
	}
	queueTime, _ := ctx.Value(httpreq.QueryQueueTimeHTTPHeader).(time.Duration)

	resLength := 0
	if resp != nil {
		resLength = len(resp.Values)
	}
	statResult := statsCtx.Result(time.Since(start), queueTime, resLength)
	sp := trace.SpanFromContext(ctx)
	sp.SetAttributes(tracing.KeyValuesToOTelAttributes(statResult.KVList())...)

	status, _ := serverutil.ClientHTTPStatusAndError(err)
	logql.RecordLabelQueryMetrics(ctx, utillog.Logger, *req.Start, *req.End, req.Name, req.Query, strconv.Itoa(status), statResult)

	return resp, err
}

func (q *QuerierAPI) metricAggregationEnabled(ctx context.Context) bool {
	orgID, _ := user.ExtractOrgID(ctx)
	return q.limits.MetricAggregationEnabled(orgID)
}

// SeriesHandler returns the list of time series that match a certain label set.
// See https://prometheus.io/docs/prometheus/latest/querying/api/#finding-series-by-label-matchers
func (q *QuerierAPI) SeriesHandler(ctx context.Context, req *logproto.SeriesRequest) (*logproto.SeriesResponse, stats.Result, error) {
	timer := prometheus.NewTimer(logql.QueryTime.WithLabelValues(logql.QueryTypeSeries))
	defer timer.ObserveDuration()

	start := time.Now()
	statsCtx, ctx := stats.NewContext(ctx)

	// filtering out aggregated metrics is quicker if we had a matcher for it, ie. __aggregated__metric__=""
	// however, that only works fo non-empty matchers, so we still need the filter the response
	aggMetricsRequestedInAnyGroup := false
	if q.metricAggregationEnabled(ctx) {
		var grpsWithAggMetricsFilter []string
		var err error
		grpsWithAggMetricsFilter, aggMetricsRequestedInAnyGroup, err = q.filterAggregatedMetrics(req.GetGroups())
		if err != nil {
			return nil, stats.Result{}, err
		}
		req.Groups = grpsWithAggMetricsFilter
	}

	resp, err := q.querier.Series(ctx, req)
	queueTime, _ := ctx.Value(httpreq.QueryQueueTimeHTTPHeader).(time.Duration)

	resLength := 0
	if resp != nil {
		resLength = len(resp.Series)

		// filter the response to catch the empty matcher case
		if !aggMetricsRequestedInAnyGroup && q.metricAggregationEnabled(ctx) {
			resp = q.filterAggregatedMetricsFromSeriesResp(resp)
		}
	}

	statResult := statsCtx.Result(time.Since(start), queueTime, resLength)
	sp := trace.SpanFromContext(ctx)
	sp.SetAttributes(tracing.KeyValuesToOTelAttributes(statResult.KVList())...)

	status, _ := serverutil.ClientHTTPStatusAndError(err)
	logql.RecordSeriesQueryMetrics(ctx, utillog.Logger, req.Start, req.End, req.Groups, strconv.Itoa(status), req.GetShards(), statResult)

	return resp, statResult, err
}

// IndexStatsHandler queries the index for the data statistics related to a query
func (q *QuerierAPI) IndexStatsHandler(ctx context.Context, req *loghttp.RangeQuery) (*logproto.IndexStatsResponse, error) {
	timer := prometheus.NewTimer(logql.QueryTime.WithLabelValues(logql.QueryTypeStats))
	defer timer.ObserveDuration()

	start := time.Now()
	statsCtx, ctx := stats.NewContext(ctx)

	// TODO(karsten): we might want to change IndexStats to receive a logproto.IndexStatsRequest instead
	resp, err := q.querier.IndexStats(ctx, req)
	if resp == nil {
		// Some stores don't implement this
		resp = &index_stats.Stats{}
	}

	queueTime, _ := ctx.Value(httpreq.QueryQueueTimeHTTPHeader).(time.Duration)
	statResult := statsCtx.Result(time.Since(start), queueTime, 1)
	sp := trace.SpanFromContext(ctx)
	sp.SetAttributes(tracing.KeyValuesToOTelAttributes(statResult.KVList())...)

	status, _ := serverutil.ClientHTTPStatusAndError(err)
	logql.RecordStatsQueryMetrics(ctx, utillog.Logger, req.Start, req.End, req.Query, strconv.Itoa(status), statResult)

	return resp, err
}

func (q *QuerierAPI) IndexShardsHandler(ctx context.Context, req *loghttp.RangeQuery, targetBytesPerShard uint64) (*logproto.ShardsResponse, error) {
	timer := prometheus.NewTimer(logql.QueryTime.WithLabelValues(logql.QueryTypeShards))
	defer timer.ObserveDuration()

	start := time.Now()
	statsCtx, ctx := stats.NewContext(ctx)

	resp, err := q.querier.IndexShards(ctx, req, targetBytesPerShard)
	queueTime, _ := ctx.Value(httpreq.QueryQueueTimeHTTPHeader).(time.Duration)

	resLength := 0
	if resp != nil {
		resLength = len(resp.Shards)
		stats.JoinResults(ctx, resp.Statistics)
	}

	statResult := statsCtx.Result(time.Since(start), queueTime, resLength)

	sp := trace.SpanFromContext(ctx)
	sp.SetAttributes(tracing.KeyValuesToOTelAttributes(statResult.KVList())...)

	status, _ := serverutil.ClientHTTPStatusAndError(err)
	logql.RecordShardsQueryMetrics(
		ctx, utillog.Logger, req.Start, req.End, req.Query, targetBytesPerShard, strconv.Itoa(status), resLength, statResult,
	)

	return resp, err
}

// TODO(trevorwhitney): add test for the handler split

// VolumeHandler queries the index label volumes related to the passed matchers and given time range.
// Returns either N values where N is the time range / step and a single value for a time range depending on the request.
func (q *QuerierAPI) VolumeHandler(ctx context.Context, req *logproto.VolumeRequest) (*logproto.VolumeResponse, error) {
	timer := prometheus.NewTimer(logql.QueryTime.WithLabelValues(logql.QueryTypeVolume))
	defer timer.ObserveDuration()

	start := time.Now()
	statsCtx, ctx := stats.NewContext(ctx)

	resp, err := q.querier.Volume(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp == nil { // Some stores don't implement this
		return &logproto.VolumeResponse{Volumes: []logproto.Volume{}}, nil
	}

	queueTime, _ := ctx.Value(httpreq.QueryQueueTimeHTTPHeader).(time.Duration)
	statResult := statsCtx.Result(time.Since(start), queueTime, 1)
	sp := trace.SpanFromContext(ctx)
	sp.SetAttributes(tracing.KeyValuesToOTelAttributes(statResult.KVList())...)

	status, _ := serverutil.ClientHTTPStatusAndError(err)
	logql.RecordVolumeQueryMetrics(ctx, utillog.Logger, req.From.Time(), req.Through.Time(), req.GetQuery(), uint32(req.GetLimit()), time.Duration(req.GetStep()), strconv.Itoa(status), statResult)

	return resp, nil
}

// filterAggregatedMetrics adds a matcher to exclude aggregated metrics and patterns unless explicitly requested
func (q *QuerierAPI) filterAggregatedMetrics(groups []string) ([]string, bool, error) {
	// cannot add filter to an empty matcher set
	if len(groups) == 0 {
		return groups, false, nil
	}

	noAggMetrics, err := labels.NewMatcher(
		labels.MatchEqual,
		constants.AggregatedMetricLabel,
		"",
	)
	if err != nil {
		return nil, false, err
	}

	noPatterns, err := labels.NewMatcher(
		labels.MatchEqual,
		constants.PatternLabel,
		"",
	)
	if err != nil {
		return nil, false, err
	}

	newGroups := make([]string, 0, len(groups)+1)

	aggMetricsRequestedInAnyGroup := false
	for _, group := range groups {
		grp, err := syntax.ParseMatchers(group, false)
		if err != nil {
			return nil, false, err
		}

		aggMetricsRequested := false
		patternsRequested := false
		for _, m := range grp {
			if m.Name == constants.AggregatedMetricLabel {
				aggMetricsRequested = true
				aggMetricsRequestedInAnyGroup = true
			}
			if m.Name == constants.PatternLabel {
				patternsRequested = true
				aggMetricsRequestedInAnyGroup = true
			}
		}

		if !aggMetricsRequested {
			grp = append(grp, noAggMetrics)
		}
		if !patternsRequested {
			grp = append(grp, noPatterns)
		}

		newGroups = append(newGroups, syntax.MatchersString(grp))
	}
	return newGroups, aggMetricsRequestedInAnyGroup, nil
}

func (q *QuerierAPI) filterAggregatedMetricsFromSeriesResp(resp *logproto.SeriesResponse) *logproto.SeriesResponse {
	for i := 0; i < len(resp.Series); i++ {
		keys := make([]string, 0, len(resp.Series[i].Labels))
		for _, label := range resp.Series[i].Labels {
			keys = append(keys, label.Key)
		}

		if slices.Contains(keys, constants.AggregatedMetricLabel) || slices.Contains(keys, constants.PatternLabel) {
			resp.Series = slices.Delete(resp.Series, i, i+1)
			i--
		}
	}

	return resp
}

func (q *QuerierAPI) filterAggregatedMetricsLabel(labels []string) []string {
	newLabels := make([]string, 0, len(labels))
	for _, label := range labels {
		if label == constants.AggregatedMetricLabel || label == constants.PatternLabel {
			continue
		}
		newLabels = append(newLabels, label)
	}

	return newLabels
}

func (q *QuerierAPI) DetectedFieldsHandler(ctx context.Context, req *logproto.DetectedFieldsRequest) (*logproto.DetectedFieldsResponse, error) {
	resp, err := q.querier.DetectedFields(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp == nil { // Some stores don't implement this
		level.Debug(spanlogger.FromContext(ctx, q.logger)).Log(
			"msg", "queried store for detected fields that does not support it, no response from querier.DetectedFields",
		)
		return &logproto.DetectedFieldsResponse{
			Fields: []*logproto.DetectedField{},
			Limit:  req.GetLimit(),
		}, nil
	}
	return resp, nil
}

type asyncPatternResponses struct {
	responses []*logproto.QueryPatternsResponse
	lock      sync.Mutex
}

// currently, the only contention is when adding, however if that behavior changes we may need to lock in the read methods as well
func (r *asyncPatternResponses) add(resp *logproto.QueryPatternsResponse) {
	if resp == nil {
		return
	}

	r.lock.Lock()
	defer r.lock.Unlock()
	r.responses = append(r.responses, resp)
}

func (r *asyncPatternResponses) get() []*logproto.QueryPatternsResponse {
	return r.responses
}

func (r *asyncPatternResponses) len() int {
	return len(r.responses)
}

func (q *QuerierAPI) PatternsHandler(ctx context.Context, req *logproto.QueryPatternsRequest) (*logproto.QueryPatternsResponse, error) {
	// Calculate query intervals for ingester vs store
	ingesterQueryInterval, storeQueryInterval := BuildQueryIntervalsWithLookback(q.cfg, req.Start, req.End, q.cfg.QueryPatternIngestersWithin)

	responses := asyncPatternResponses{}
	g, ctx := errgroup.WithContext(ctx)

	// Query pattern ingesters for recent data
	if ingesterQueryInterval != nil && !q.cfg.QueryStoreOnly && q.querier != nil {
		g.Go(func() error {
			splitReq := *req
			splitReq.Start = ingesterQueryInterval.start
			splitReq.End = ingesterQueryInterval.end

			resp, err := q.querier.Patterns(ctx, &splitReq)
			if err != nil {
				return err
			}
			responses.add(resp)
			return nil
		})
	}

	// Query store for older data by converting to LogQL query
	// Only query the store if pattern persistence is enabled for this tenant
	if storeQueryInterval != nil && !q.cfg.QueryIngesterOnly && q.engineV1 != nil {
		tenantID, err := tenant.TenantID(ctx)
		if err != nil {
			return nil, err
		}

		// Only query the store if pattern persistence is enabled for this tenant
		if q.limits.PatternPersistenceEnabled(tenantID) {
			g.Go(func() error {
				storeReq := *req
				storeReq.Start = storeQueryInterval.start
				storeReq.End = storeQueryInterval.end
				resp, err := q.queryStoreForPatterns(ctx, &storeReq)
				if err != nil {
					return err
				}
				responses.add(resp)
				return nil
			})
		}
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	// Merge responses
	if responses.len() == 0 {
		return &logproto.QueryPatternsResponse{
			Series: []*logproto.PatternSeries{},
		}, nil
	}

	return pattern.MergePatternResponses(responses.get()), nil
}

func (q *QuerierAPI) queryStoreForPatterns(ctx context.Context, req *logproto.QueryPatternsRequest) (*logproto.QueryPatternsResponse, error) {
	params, err := queryrange.ParamsFromRequest(req)
	if err != nil {
		return nil, err
	}

	// Patterns are persisted as logfmt'd strings, so we need to to run the query using the LogQL engine
	// in order to extract the metric values from them.
	query := q.engineV1.Query(params)
	res, err := query.Exec(ctx)
	if err != nil {
		return nil, err
	}

	result := make(map[string]map[string]map[int64]float64) // level -> pattern -> timestamp -> value
	switch v := res.Data.(type) {
	case promql.Vector:
		for _, s := range v {
			lvl, pattern := getLevelAndPattern(s.Metric)
			if pattern == "" {
				continue
			}

			if result[lvl] == nil {
				result[lvl] = make(map[string]map[int64]float64)
			}

			if result[lvl][pattern] == nil {
				result[lvl][pattern] = make(map[int64]float64)
			}
			result[lvl][pattern][s.T] = s.F
		}
	case promql.Matrix:
		for _, s := range v {
			lvl, pattern := getLevelAndPattern(s.Metric)
			if pattern == "" {
				continue
			}

			if result[lvl] == nil {
				result[lvl] = make(map[string]map[int64]float64)
			}

			if result[lvl][pattern] == nil {
				result[lvl][pattern] = make(map[int64]float64)
			}
			for _, f := range s.Floats {
				result[lvl][pattern][f.T] = f.F
			}
		}
	default:
		return nil, fmt.Errorf("unexpected type (%s) when querying store for patterns. Expected %s or %s", res.Data.Type(), parser.ValueTypeMatrix, parser.ValueTypeVector)
	}

	// Convert to pattern response format
	resp := &logproto.QueryPatternsResponse{
		Series: make([]*logproto.PatternSeries, 0, len(result)),
	}

	for lvl, patterns := range result {
		for pattern, samples := range patterns {
			series := &logproto.PatternSeries{
				Pattern: pattern,
				Level:   lvl,
				Samples: make([]*logproto.PatternSample, 0, len(samples)),
			}

			// Convert samples map to slice and sort by timestamp
			timestamps := make([]int64, 0, len(samples))
			for ts := range samples {
				timestamps = append(timestamps, ts)
			}
			slices.Sort(timestamps)

			level.Debug(q.logger).Log(
				"msg", "earliest pattern sample from store",
				"timestamp", timestamps[0],
			)
			for _, ts := range timestamps {
				series.Samples = append(series.Samples, &logproto.PatternSample{
					Timestamp: model.Time(ts),
					Value:     int64(samples[ts]),
				})
			}

			resp.Series = append(resp.Series, series)
		}
	}

	return resp, nil
}

func getLevelAndPattern(metric labels.Labels) (string, string) {
	lvl := metric.Get(constants.LevelLabel)
	if lvl == "" {
		lvl = constants.LogLevelUnknown
	}

	pattern := metric.Get("decoded_pattern")
	return lvl, pattern
}

func (q *QuerierAPI) validateMaxEntriesLimits(ctx context.Context, expr syntax.Expr, limit uint32) error {
	tenantIDs, err := tenant.TenantIDs(ctx)
	if err != nil {
		return httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
	}

	// entry limit does not apply to metric queries.
	if _, ok := expr.(syntax.SampleExpr); ok {
		return nil
	}

	maxEntriesCapture := func(id string) int { return q.limits.MaxEntriesLimitPerQuery(ctx, id) }
	maxEntriesLimit := util_validation.SmallestPositiveNonZeroIntPerTenant(tenantIDs, maxEntriesCapture)
	if int(limit) > maxEntriesLimit && maxEntriesLimit != 0 {
		return httpgrpc.Errorf(http.StatusBadRequest,
			"max entries limit per query exceeded, limit > max_entries_limit_per_query (%d > %d)", limit, maxEntriesLimit)
	}
	return nil
}

// DetectedLabelsHandler returns a response for detected labels
func (q *QuerierAPI) DetectedLabelsHandler(ctx context.Context, req *logproto.DetectedLabelsRequest) (*logproto.DetectedLabelsResponse, error) {
	resp, err := q.querier.DetectedLabels(ctx, req)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// WrapQuerySpanAndTimeout applies a context deadline and a span logger to a query call.
//
// The timeout is based on the per-tenant query timeout configuration.
func WrapQuerySpanAndTimeout(call string, limits querier_limits.Limits) middleware.Interface {
	return middleware.Func(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ctx, sp := tracer.Start(req.Context(), call)
			defer sp.End()

			log := spanlogger.FromContext(req.Context(), utillog.Logger)
			defer log.Finish()

			tenants, err := tenant.TenantIDs(ctx)
			if err != nil {
				level.Error(log).Log("msg", "couldn't fetch tenantID", "err", err)
				serverutil.WriteError(httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error()), w)
				return
			}

			timeoutCapture := func(id string) time.Duration { return limits.QueryTimeout(ctx, id) }
			timeout := util_validation.SmallestPositiveNonZeroDurationPerTenant(tenants, timeoutCapture)
			newCtx, cancel := context.WithTimeoutCause(ctx, timeout, errors.New("query timeout reached"))
			defer cancel()

			newReq := req.WithContext(newCtx)
			next.ServeHTTP(w, newReq)
		})
	})
}
