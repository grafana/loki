package querier

import (
	"context"
	"flag"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"time"

	"github.com/axiomhq/hyperloglog"
	"github.com/dustin/go-humanize"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/google/uuid"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/tenant"
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/loki/v3/pkg/indexgateway"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	logql_log "github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/querier/deletion"
	querier_limits "github.com/grafana/loki/v3/pkg/querier/limits"
	"github.com/grafana/loki/v3/pkg/querier/pattern"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/seriesvolume"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/stats"
	listutil "github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
	"github.com/grafana/loki/v3/pkg/util/spanlogger"

	"github.com/grafana/loki/pkg/push"
)

type interval struct {
	start, end time.Time
}

// Config for a querier.
type Config struct {
	TailMaxDuration               time.Duration    `yaml:"tail_max_duration"`
	ExtraQueryDelay               time.Duration    `yaml:"extra_query_delay,omitempty"`
	QueryIngestersWithin          time.Duration    `yaml:"query_ingesters_within,omitempty"`
	IngesterQueryStoreMaxLookback time.Duration    `yaml:"-"`
	Engine                        logql.EngineOpts `yaml:"engine,omitempty"`
	MaxConcurrent                 int              `yaml:"max_concurrent"`
	QueryStoreOnly                bool             `yaml:"query_store_only"`
	QueryIngesterOnly             bool             `yaml:"query_ingester_only"`
	MultiTenantQueriesEnabled     bool             `yaml:"multi_tenant_queries_enabled"`
	PerRequestLimitsEnabled       bool             `yaml:"per_request_limits_enabled"`
	QueryPartitionIngesters       bool             `yaml:"query_partition_ingesters" category:"experimental"`
}

// RegisterFlags register flags.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.Engine.RegisterFlagsWithPrefix("querier", f)
	f.DurationVar(&cfg.TailMaxDuration, "querier.tail-max-duration", 1*time.Hour, "Maximum duration for which the live tailing requests are served.")
	f.DurationVar(&cfg.ExtraQueryDelay, "querier.extra-query-delay", 0, "Time to wait before sending more than the minimum successful query requests.")
	f.DurationVar(&cfg.QueryIngestersWithin, "querier.query-ingesters-within", 3*time.Hour, "Maximum lookback beyond which queries are not sent to ingester. 0 means all queries are sent to ingester.")
	f.IntVar(&cfg.MaxConcurrent, "querier.max-concurrent", 4, "The maximum number of queries that can be simultaneously processed by the querier.")
	f.BoolVar(&cfg.QueryStoreOnly, "querier.query-store-only", false, "Only query the store, and not attempt any ingesters. This is useful for running a standalone querier pool operating only against stored data.")
	f.BoolVar(&cfg.QueryIngesterOnly, "querier.query-ingester-only", false, "When true, queriers only query the ingesters, and not stored data. This is useful when the object store is unavailable.")
	f.BoolVar(&cfg.MultiTenantQueriesEnabled, "querier.multi-tenant-queries-enabled", false, "When true, allow queries to span multiple tenants.")
	f.BoolVar(&cfg.PerRequestLimitsEnabled, "querier.per-request-limits-enabled", false, "When true, querier limits sent via a header are enforced.")
	f.BoolVar(&cfg.QueryPartitionIngesters, "querier.query-partition-ingesters", false, "When true, querier directs ingester queries to the partition-ingesters instead of the normal ingesters.")
}

// Validate validates the config.
func (cfg *Config) Validate() error {
	if cfg.QueryStoreOnly && cfg.QueryIngesterOnly {
		return errors.New("querier.query_store_only and querier.query_ingester_only cannot both be true")
	}
	return nil
}

// Querier can select logs and samples and handle query requests.
type Querier interface {
	logql.Querier
	Label(ctx context.Context, req *logproto.LabelRequest) (*logproto.LabelResponse, error)
	Series(ctx context.Context, req *logproto.SeriesRequest) (*logproto.SeriesResponse, error)
	IndexStats(ctx context.Context, req *loghttp.RangeQuery) (*stats.Stats, error)
	IndexShards(ctx context.Context, req *loghttp.RangeQuery, targetBytesPerShard uint64) (*logproto.ShardsResponse, error)
	Volume(ctx context.Context, req *logproto.VolumeRequest) (*logproto.VolumeResponse, error)
	DetectedFields(ctx context.Context, req *logproto.DetectedFieldsRequest) (*logproto.DetectedFieldsResponse, error)
	Patterns(ctx context.Context, req *logproto.QueryPatternsRequest) (*logproto.QueryPatternsResponse, error)
	DetectedLabels(ctx context.Context, req *logproto.DetectedLabelsRequest) (*logproto.DetectedLabelsResponse, error)
	WithPatternQuerier(patternQuerier pattern.PatterQuerier)
}

// Store is the store interface we need on the querier.
type Store interface {
	SelectSamples(ctx context.Context, req logql.SelectSampleParams) (iter.SampleIterator, error)
	SelectLogs(ctx context.Context, req logql.SelectLogParams) (iter.EntryIterator, error)
	SelectSeries(ctx context.Context, req logql.SelectLogParams) ([]logproto.SeriesIdentifier, error)
	LabelValuesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string, labelName string, matchers ...*labels.Matcher) ([]string, error)
	LabelNamesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string, matchers ...*labels.Matcher) ([]string, error)
	Stats(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) (*stats.Stats, error)
	Volume(ctx context.Context, userID string, from, through model.Time, limit int32, targetLabels []string, aggregateBy string, matchers ...*labels.Matcher) (*logproto.VolumeResponse, error)
	GetShards(
		ctx context.Context,
		userID string,
		from, through model.Time,
		targetBytesPerShard uint64,
		predicate chunk.Predicate,
	) (*logproto.ShardsResponse, error)
}

// SingleTenantQuerier handles single tenant queries.
type SingleTenantQuerier struct {
	cfg             Config
	store           Store
	limits          querier_limits.Limits
	ingesterQuerier *IngesterQuerier
	patternQuerier  pattern.PatterQuerier
	deleteGetter    deletion.DeleteGetter
	logger          log.Logger
}

// New makes a new Querier.
func New(cfg Config, store Store, ingesterQuerier *IngesterQuerier, limits querier_limits.Limits, d deletion.DeleteGetter, logger log.Logger) (*SingleTenantQuerier, error) {
	q := &SingleTenantQuerier{
		cfg:             cfg,
		store:           store,
		ingesterQuerier: ingesterQuerier,
		limits:          limits,
		deleteGetter:    d,
		logger:          logger,
	}

	return q, nil
}

// Select Implements logql.Querier which select logs via matchers and regex filters.
func (q *SingleTenantQuerier) SelectLogs(ctx context.Context, params logql.SelectLogParams) (iter.EntryIterator, error) {
	// Create a new partition context for the query
	// This is used to track which ingesters were used in the query and reuse the same ingesters for consecutive queries
	ctx = NewPartitionContext(ctx)
	var err error
	params.Start, params.End, err = querier_limits.ValidateQueryRequest(ctx, params, q.limits)
	if err != nil {
		return nil, err
	}

	err = querier_limits.ValidateAggregatedMetricQuery(ctx, params)
	if err != nil {
		if errors.Is(err, querier_limits.ErrAggMetricsDrilldownOnly) {
			return iter.NoopEntryIterator, nil
		}
		return nil, err
	}

	params.QueryRequest.Deletes, err = deletion.DeletesForUserQuery(ctx, params.Start, params.End, q.deleteGetter)
	if err != nil {
		level.Error(spanlogger.FromContext(ctx)).Log("msg", "failed loading deletes for user", "err", err)
	}

	ingesterQueryInterval, storeQueryInterval := q.buildQueryIntervals(params.Start, params.End)

	sp := opentracing.SpanFromContext(ctx)
	iters := []iter.EntryIterator{}
	if !q.cfg.QueryStoreOnly && ingesterQueryInterval != nil {
		// Make a copy of the request before modifying
		// because the initial request is used below to query stores
		queryRequestCopy := *params.QueryRequest
		newParams := logql.SelectLogParams{
			QueryRequest: &queryRequestCopy,
		}
		newParams.Start = ingesterQueryInterval.start
		newParams.End = ingesterQueryInterval.end
		if sp != nil {
			sp.LogKV(
				"msg", "querying ingester",
				"params", newParams)
		}
		ingesterIters, err := q.ingesterQuerier.SelectLogs(ctx, newParams)
		if err != nil {
			return nil, err
		}

		iters = append(iters, ingesterIters...)
	}

	if !q.cfg.QueryIngesterOnly && storeQueryInterval != nil {
		params.Start = storeQueryInterval.start
		params.End = storeQueryInterval.end
		if sp != nil {
			sp.LogKV(
				"msg", "querying store",
				"params", params)
		}
		storeIter, err := q.store.SelectLogs(ctx, params)
		if err != nil {
			return nil, err
		}

		iters = append(iters, storeIter)
	}
	if len(iters) == 1 {
		return iters[0], nil
	}
	return iter.NewMergeEntryIterator(ctx, iters, params.Direction), nil
}

func (q *SingleTenantQuerier) SelectSamples(ctx context.Context, params logql.SelectSampleParams) (iter.SampleIterator, error) {
	// Create a new partition context for the query
	// This is used to track which ingesters were used in the query and reuse the same ingesters for consecutive queries
	ctx = NewPartitionContext(ctx)
	var err error
	params.Start, params.End, err = querier_limits.ValidateQueryRequest(ctx, params, q.limits)
	if err != nil {
		return nil, err
	}

	params.SampleQueryRequest.Deletes, err = deletion.DeletesForUserQuery(ctx, params.Start, params.End, q.deleteGetter)
	if err != nil {
		level.Error(spanlogger.FromContext(ctx)).Log("msg", "failed loading deletes for user", "err", err)
	}

	ingesterQueryInterval, storeQueryInterval := q.buildQueryIntervals(params.Start, params.End)

	iters := []iter.SampleIterator{}
	if !q.cfg.QueryStoreOnly && ingesterQueryInterval != nil {
		// Make a copy of the request before modifying
		// because the initial request is used below to query stores
		queryRequestCopy := *params.SampleQueryRequest
		newParams := logql.SelectSampleParams{
			SampleQueryRequest: &queryRequestCopy,
		}
		newParams.Start = ingesterQueryInterval.start
		newParams.End = ingesterQueryInterval.end

		ingesterIters, err := q.ingesterQuerier.SelectSample(ctx, newParams)
		if err != nil {
			return nil, err
		}

		iters = append(iters, ingesterIters...)
	}

	if !q.cfg.QueryIngesterOnly && storeQueryInterval != nil {
		params.Start = storeQueryInterval.start
		params.End = storeQueryInterval.end

		storeIter, err := q.store.SelectSamples(ctx, params)
		if err != nil {
			return nil, err
		}

		iters = append(iters, storeIter)
	}
	return iter.NewMergeSampleIterator(ctx, iters), nil
}

func (q *SingleTenantQuerier) isWithinIngesterMaxLookbackPeriod(maxLookback time.Duration, queryEnd time.Time) bool {
	// if no lookback limits are configured, always consider this within the range of the lookback period
	if maxLookback <= 0 {
		return true
	}

	// find the first instance that we would want to query the ingester from...
	ingesterOldestStartTime := time.Now().Add(-maxLookback)

	// ...and if the query range ends before that, don't query the ingester
	return queryEnd.After(ingesterOldestStartTime)
}

func (q *SingleTenantQuerier) calculateIngesterMaxLookbackPeriod() time.Duration {
	mlb := time.Duration(-1)
	if q.cfg.IngesterQueryStoreMaxLookback != 0 {
		// IngesterQueryStoreMaxLookback takes the precedence over QueryIngestersWithin while also limiting the store query range.
		mlb = q.cfg.IngesterQueryStoreMaxLookback
	} else if q.cfg.QueryIngestersWithin != 0 {
		mlb = q.cfg.QueryIngestersWithin
	}

	return mlb
}

func (q *SingleTenantQuerier) buildQueryIntervals(queryStart, queryEnd time.Time) (*interval, *interval) {
	// limitQueryInterval is a flag for whether store queries should be limited to start time of ingester queries.
	limitQueryInterval := false
	// ingesterMLB having -1 means query ingester for whole duration.
	if q.cfg.IngesterQueryStoreMaxLookback != 0 {
		// IngesterQueryStoreMaxLookback takes the precedence over QueryIngestersWithin while also limiting the store query range.
		limitQueryInterval = true
	}

	ingesterMLB := q.calculateIngesterMaxLookbackPeriod()

	// query ingester for whole duration.
	if ingesterMLB == -1 {
		i := &interval{
			start: queryStart,
			end:   queryEnd,
		}

		if limitQueryInterval {
			// query only ingesters.
			return i, nil
		}

		// query both stores and ingesters without limiting the query interval.
		return i, i
	}

	ingesterQueryWithinRange := q.isWithinIngesterMaxLookbackPeriod(ingesterMLB, queryEnd)

	// see if there is an overlap between ingester query interval and actual query interval, if not just do the store query.
	if !ingesterQueryWithinRange {
		return nil, &interval{
			start: queryStart,
			end:   queryEnd,
		}
	}

	ingesterOldestStartTime := time.Now().Add(-ingesterMLB)

	// if there is an overlap and we are not limiting the query interval then do both store and ingester query for whole query interval.
	if !limitQueryInterval {
		i := &interval{
			start: queryStart,
			end:   queryEnd,
		}
		return i, i
	}

	// since we are limiting the query interval, check if the query touches just the ingesters, if yes then query just the ingesters.
	if ingesterOldestStartTime.Before(queryStart) {
		return &interval{
			start: queryStart,
			end:   queryEnd,
		}, nil
	}

	// limit the start of ingester query interval to ingesterOldestStartTime.
	ingesterQueryInterval := &interval{
		start: ingesterOldestStartTime,
		end:   queryEnd,
	}

	// limit the end of ingester query interval to ingesterOldestStartTime.
	storeQueryInterval := &interval{
		start: queryStart,
		end:   ingesterOldestStartTime,
	}

	// query touches only ingester query interval so do not do store query.
	if storeQueryInterval.start.After(storeQueryInterval.end) {
		storeQueryInterval = nil
	}

	return ingesterQueryInterval, storeQueryInterval
}

// Label does the heavy lifting for a Label query.
func (q *SingleTenantQuerier) Label(ctx context.Context, req *logproto.LabelRequest) (*logproto.LabelResponse, error) {
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	if *req.Start, *req.End, err = querier_limits.ValidateQueryTimeRangeLimits(ctx, userID, q.limits, *req.Start, *req.End); err != nil {
		return nil, err
	}

	var matchers []*labels.Matcher
	if req.Query != "" {
		matchers, err = syntax.ParseMatchers(req.Query, true)
		if err != nil {
			return nil, err
		}
	}

	// Enforce the query timeout while querying backends
	queryTimeout := q.limits.QueryTimeout(ctx, userID)
	ctx, cancel := context.WithDeadline(ctx, time.Now().Add(queryTimeout))
	defer cancel()

	g, ctx := errgroup.WithContext(ctx)

	ingesterQueryInterval, storeQueryInterval := q.buildQueryIntervals(*req.Start, *req.End)

	var ingesterValues [][]string
	if !q.cfg.QueryStoreOnly && ingesterQueryInterval != nil {
		g.Go(func() error {
			var err error
			timeFramedReq := *req
			timeFramedReq.Start = &ingesterQueryInterval.start
			timeFramedReq.End = &ingesterQueryInterval.end

			ingesterValues, err = q.ingesterQuerier.Label(ctx, &timeFramedReq)
			return err
		})
	}

	var storeValues []string
	if !q.cfg.QueryIngesterOnly && storeQueryInterval != nil {
		g.Go(func() error {
			var (
				err     error
				from    = model.TimeFromUnixNano(storeQueryInterval.start.UnixNano())
				through = model.TimeFromUnixNano(storeQueryInterval.end.UnixNano())
			)

			if req.Values {
				storeValues, err = q.store.LabelValuesForMetricName(ctx, userID, from, through, "logs", req.Name, matchers...)
			} else {
				storeValues, err = q.store.LabelNamesForMetricName(ctx, userID, from, through, "logs", matchers...)
			}
			return err
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	results := append(ingesterValues, storeValues)
	return &logproto.LabelResponse{
		Values: listutil.MergeStringLists(results...),
	}, nil
}

// Check implements the grpc healthcheck
func (*SingleTenantQuerier) Check(_ context.Context, _ *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	return &grpc_health_v1.HealthCheckResponse{Status: grpc_health_v1.HealthCheckResponse_SERVING}, nil
}

// Series fetches any matching series for a list of matcher sets
func (q *SingleTenantQuerier) Series(ctx context.Context, req *logproto.SeriesRequest) (*logproto.SeriesResponse, error) {
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	if req.Start, req.End, err = querier_limits.ValidateQueryTimeRangeLimits(ctx, userID, q.limits, req.Start, req.End); err != nil {
		return nil, err
	}

	// Enforce the query timeout while querying backends
	queryTimeout := q.limits.QueryTimeout(ctx, userID)
	ctx, cancel := context.WithDeadlineCause(ctx, time.Now().Add(queryTimeout), errors.New("query timeout reached"))
	defer cancel()

	return q.awaitSeries(ctx, req)
}

func (q *SingleTenantQuerier) awaitSeries(ctx context.Context, req *logproto.SeriesRequest) (*logproto.SeriesResponse, error) {
	// buffer the channels to the # of calls they're expecting su
	series := make(chan [][]logproto.SeriesIdentifier, 2)
	errs := make(chan error, 2)

	ingesterQueryInterval, storeQueryInterval := q.buildQueryIntervals(req.Start, req.End)

	// fetch series from ingesters and store concurrently
	if !q.cfg.QueryStoreOnly && ingesterQueryInterval != nil {
		timeFramedReq := *req
		timeFramedReq.Start = ingesterQueryInterval.start
		timeFramedReq.End = ingesterQueryInterval.end

		go func() {
			// fetch series identifiers from ingesters
			resps, err := q.ingesterQuerier.Series(ctx, &timeFramedReq)
			if err != nil {
				errs <- err
				return
			}

			series <- resps
		}()
	} else {
		// If only queriying the store or the query range does not overlap with the ingester max lookback period (defined by `query_ingesters_within`)
		// then don't call out to the ingesters, and send an empty result back to the channel
		series <- [][]logproto.SeriesIdentifier{}
	}

	if !q.cfg.QueryIngesterOnly && storeQueryInterval != nil {
		go func() {
			storeValues, err := q.seriesForMatchers(ctx, storeQueryInterval.start, storeQueryInterval.end, req.GetGroups(), req.Shards)
			if err != nil {
				errs <- err
				return
			}
			series <- [][]logproto.SeriesIdentifier{storeValues}
		}()
	} else {
		// If we are not querying the store, send an empty result back to the channel
		series <- [][]logproto.SeriesIdentifier{}
	}

	var sets [][]logproto.SeriesIdentifier
	for i := 0; i < 2; i++ {
		select {
		case err := <-errs:
			return nil, err
		case s := <-series:
			sets = append(sets, s...)
		}
	}

	response := &logproto.SeriesResponse{
		Series: make([]logproto.SeriesIdentifier, 0),
	}
	seen := make(map[uint64]struct{})
	b := make([]byte, 0, 1024)
	for _, set := range sets {
		for _, s := range set {
			key := s.Hash(b)
			if _, exists := seen[key]; !exists {
				seen[key] = struct{}{}
				response.Series = append(response.Series, s)
			}
		}
	}

	return response, nil
}

// seriesForMatchers fetches series from the store for each matcher set
// TODO: make efficient if/when the index supports labels so we don't have to read chunks
func (q *SingleTenantQuerier) seriesForMatchers(
	ctx context.Context,
	from, through time.Time,
	groups []string,
	shards []string,
) ([]logproto.SeriesIdentifier, error) {
	var results []logproto.SeriesIdentifier
	// If no matchers were specified for the series query,
	// we send a query with an empty matcher which will match every series.
	if len(groups) == 0 {
		var err error
		results, err = q.seriesForMatcher(ctx, from, through, "", shards)
		if err != nil {
			return nil, err
		}
	} else {
		for _, group := range groups {
			ids, err := q.seriesForMatcher(ctx, from, through, group, shards)
			if err != nil {
				return nil, err
			}
			results = append(results, ids...)
		}
	}
	return results, nil
}

// seriesForMatcher fetches series from the store for a given matcher
func (q *SingleTenantQuerier) seriesForMatcher(ctx context.Context, from, through time.Time, matcher string, shards []string) ([]logproto.SeriesIdentifier, error) {
	var parsed syntax.Expr
	var err error
	if matcher != "" {
		parsed, err = syntax.ParseExpr(matcher)
		if err != nil {
			return nil, err
		}
	}

	ids, err := q.store.SelectSeries(ctx, logql.SelectLogParams{
		QueryRequest: &logproto.QueryRequest{
			Selector:  matcher,
			Limit:     1,
			Start:     from,
			End:       through,
			Direction: logproto.FORWARD,
			Shards:    shards,
			Plan: &plan.QueryPlan{
				AST: parsed,
			},
		},
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

func (q *SingleTenantQuerier) IndexStats(ctx context.Context, req *loghttp.RangeQuery) (*stats.Stats, error) {
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	start, end, err := querier_limits.ValidateQueryTimeRangeLimits(ctx, userID, q.limits, req.Start, req.End)
	if err != nil {
		return nil, err
	}

	matchers, err := syntax.ParseMatchers(req.Query, true)
	if err != nil {
		return nil, err
	}

	// Enforce the query timeout while querying backends
	queryTimeout := q.limits.QueryTimeout(ctx, userID)
	ctx, cancel := context.WithDeadline(ctx, time.Now().Add(queryTimeout))
	defer cancel()

	return q.store.Stats(
		ctx,
		userID,
		model.TimeFromUnixNano(start.UnixNano()),
		model.TimeFromUnixNano(end.UnixNano()),
		matchers...,
	)
}

func (q *SingleTenantQuerier) IndexShards(
	ctx context.Context,
	req *loghttp.RangeQuery,
	targetBytesPerShard uint64,
) (*logproto.ShardsResponse, error) {
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	start, end, err := querier_limits.ValidateQueryTimeRangeLimits(ctx, userID, q.limits, req.Start, req.End)
	if err != nil {
		return nil, err
	}

	// Enforce the query timeout while querying backends
	queryTimeout := q.limits.QueryTimeout(ctx, userID)
	ctx, cancel := context.WithDeadline(ctx, time.Now().Add(queryTimeout))
	defer cancel()

	p, err := indexgateway.ExtractShardRequestMatchersAndAST(req.Query)
	if err != nil {
		return nil, err
	}

	shards, err := q.store.GetShards(
		ctx,
		userID,
		model.TimeFromUnixNano(start.UnixNano()),
		model.TimeFromUnixNano(end.UnixNano()),
		targetBytesPerShard,
		p,
	)
	if err != nil {
		return nil, err
	}
	return shards, nil
}

func (q *SingleTenantQuerier) Volume(ctx context.Context, req *logproto.VolumeRequest) (*logproto.VolumeResponse, error) {
	sp, ctx := opentracing.StartSpanFromContext(ctx, "Querier.Volume")
	defer sp.Finish()

	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	matchers, err := syntax.ParseMatchers(req.Matchers, true)
	if err != nil && req.Matchers != seriesvolume.MatchAny {
		return nil, err
	}

	// Enforce the query timeout while querying backends
	queryTimeout := q.limits.QueryTimeout(ctx, userID)
	ctx, cancel := context.WithDeadline(ctx, time.Now().Add(queryTimeout))
	defer cancel()

	sp.LogKV(
		"user", userID,
		"from", req.From.Time(),
		"through", req.Through.Time(),
		"matchers", syntax.MatchersString(matchers),
		"limit", req.Limit,
		"targetLabels", req.TargetLabels,
		"aggregateBy", req.AggregateBy,
	)

	ingesterQueryInterval, storeQueryInterval := q.buildQueryIntervals(req.From.Time(), req.Through.Time())

	queryIngesters := !q.cfg.QueryStoreOnly && ingesterQueryInterval != nil
	queryStore := !q.cfg.QueryIngesterOnly && storeQueryInterval != nil

	numResponses := 0
	if queryIngesters {
		numResponses++
	}
	if queryStore {
		numResponses++
	}
	responses := make([]*logproto.VolumeResponse, 0, numResponses)

	if queryIngesters {
		// Make a copy of the request before modifying
		// because the initial request is used below to query stores

		resp, err := q.ingesterQuerier.Volume(
			ctx,
			userID,
			model.TimeFromUnix(ingesterQueryInterval.start.Unix()),
			model.TimeFromUnix(ingesterQueryInterval.end.Unix()),
			req.Limit,
			req.TargetLabels,
			req.AggregateBy,
			matchers...,
		)
		if err != nil {
			return nil, err
		}

		responses = append(responses, resp)
	}

	if queryStore {
		resp, err := q.store.Volume(
			ctx,
			userID,
			model.TimeFromUnix(storeQueryInterval.start.Unix()),
			model.TimeFromUnix(storeQueryInterval.end.Unix()),
			req.Limit,
			req.TargetLabels,
			req.AggregateBy,
			matchers...,
		)
		if err != nil {
			return nil, err
		}

		responses = append(responses, resp)
	}

	return seriesvolume.Merge(responses, req.Limit), nil
}

// DetectedLabels fetches labels and values from store and ingesters and filters them by relevance criteria as per logs app.
func (q *SingleTenantQuerier) DetectedLabels(ctx context.Context, req *logproto.DetectedLabelsRequest) (*logproto.DetectedLabelsResponse, error) {
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}
	staticLabels := map[string]struct{}{"cluster": {}, "namespace": {}, "instance": {}, "pod": {}}

	// Enforce the query timeout while querying backends
	queryTimeout := q.limits.QueryTimeout(ctx, userID)
	ctx, cancel := context.WithDeadline(ctx, time.Now().Add(queryTimeout))
	defer cancel()
	g, ctx := errgroup.WithContext(ctx)

	if req.Start, req.End, err = querier_limits.ValidateQueryTimeRangeLimits(ctx, userID, q.limits, req.Start, req.End); err != nil {
		return nil, err
	}
	ingesterQueryInterval, storeQueryInterval := q.buildQueryIntervals(req.Start, req.End)

	// Fetch labels from ingesters
	var ingesterLabels *logproto.LabelToValuesResponse
	if !q.cfg.QueryStoreOnly && ingesterQueryInterval != nil {
		g.Go(func() error {
			var err error
			splitReq := *req
			splitReq.Start = ingesterQueryInterval.start
			splitReq.End = ingesterQueryInterval.end

			ingesterLabels, err = q.ingesterQuerier.DetectedLabel(ctx, &splitReq)
			return err
		})
	}

	// Fetch labels from the store
	storeLabelsMap := make(map[string][]string)
	if !q.cfg.QueryIngesterOnly && storeQueryInterval != nil {
		var matchers []*labels.Matcher
		if req.Query != "" {
			matchers, err = syntax.ParseMatchers(req.Query, true)
			if err != nil {
				return nil, err
			}
		}
		g.Go(func() error {
			var err error
			start := model.TimeFromUnixNano(storeQueryInterval.start.UnixNano())
			end := model.TimeFromUnixNano(storeQueryInterval.end.UnixNano())
			storeLabels, err := q.store.LabelNamesForMetricName(ctx, userID, start, end, "logs", matchers...)
			for _, label := range storeLabels {
				values, err := q.store.LabelValuesForMetricName(ctx, userID, start, end, "logs", label, matchers...)
				if err != nil {
					return err
				}
				storeLabelsMap[label] = values
			}
			return err
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	if ingesterLabels == nil && len(storeLabelsMap) == 0 {
		return &logproto.DetectedLabelsResponse{
			DetectedLabels: []*logproto.DetectedLabel{},
		}, nil
	}

	return &logproto.DetectedLabelsResponse{
		DetectedLabels: countLabelsAndCardinality(storeLabelsMap, ingesterLabels, staticLabels),
	}, nil
}

func countLabelsAndCardinality(storeLabelsMap map[string][]string, ingesterLabels *logproto.LabelToValuesResponse, staticLabels map[string]struct{}) []*logproto.DetectedLabel {
	dlMap := make(map[string]*parsedFields)

	if ingesterLabels != nil {
		for label, val := range ingesterLabels.Labels {
			if _, isStatic := staticLabels[label]; (isStatic && val.Values != nil) || !containsAllIDTypes(val.Values) {
				_, ok := dlMap[label]
				if !ok {
					dlMap[label] = newParsedLabels()
				}

				parsedFields := dlMap[label]
				for _, v := range val.Values {
					parsedFields.Insert(v)
				}
			}
		}
	}

	for label, values := range storeLabelsMap {
		if _, isStatic := staticLabels[label]; (isStatic && values != nil) || !containsAllIDTypes(values) {
			_, ok := dlMap[label]
			if !ok {
				dlMap[label] = newParsedLabels()
			}

			parsedFields := dlMap[label]
			for _, v := range values {
				parsedFields.Insert(v)
			}
		}
	}

	var detectedLabels []*logproto.DetectedLabel
	for k, v := range dlMap {
		sketch, err := v.sketch.MarshalBinary()
		if err != nil {
			// TODO: add log here
			continue
		}
		detectedLabels = append(detectedLabels, &logproto.DetectedLabel{
			Label:       k,
			Cardinality: v.Estimate(),
			Sketch:      sketch,
		})
	}
	return detectedLabels
}

func (q *SingleTenantQuerier) WithPatternQuerier(pq pattern.PatterQuerier) {
	q.patternQuerier = pq
}

func (q *SingleTenantQuerier) Patterns(ctx context.Context, req *logproto.QueryPatternsRequest) (*logproto.QueryPatternsResponse, error) {
	if q.patternQuerier == nil {
		return nil, httpgrpc.Errorf(http.StatusNotFound, "")
	}
	res, err := q.patternQuerier.Patterns(ctx, req)
	return res, err
}

// containsAllIDTypes filters out all UUID, GUID and numeric types. Returns false if even one value is not of the type
func containsAllIDTypes(values []string) bool {
	for _, v := range values {
		_, err := strconv.ParseFloat(v, 64)
		if err != nil {
			_, err = uuid.Parse(v)
			if err != nil {
				return false
			}
		}
	}

	return true
}

// TODO(twhitney): Delete this method and the GRPC service signature. This is now handled in the query frontend.
func (q *SingleTenantQuerier) DetectedFields(ctx context.Context, req *logproto.DetectedFieldsRequest) (*logproto.DetectedFieldsResponse, error) {
	expr, err := syntax.ParseLogSelector(req.Query, true)
	if err != nil {
		return nil, err
	}
	// just incject the header to categorize labels
	ctx = httpreq.InjectHeader(ctx, httpreq.LokiEncodingFlagsHeader, (string)(httpreq.FlagCategorizeLabels))
	params := logql.SelectLogParams{
		QueryRequest: &logproto.QueryRequest{
			Start:     req.Start,
			End:       req.End,
			Limit:     req.LineLimit,
			Direction: logproto.BACKWARD,
			Selector:  expr.String(),
			Plan: &plan.QueryPlan{
				AST: expr,
			},
		},
	}

	iters, err := q.SelectLogs(ctx, params)
	if err != nil {
		return nil, err
	}

	// TODO(twhitney): converting from a step to a duration should be abstracted and reused,
	// doing this in a few places now.
	streams, err := streamsForFieldDetection(iters, req.LineLimit)
	if err != nil {
		return nil, err
	}

	detectedFields := parseDetectedFields(req.Limit, streams)

	fields := make([]*logproto.DetectedField, len(detectedFields))
	fieldCount := 0
	for k, v := range detectedFields {
		sketch, err := v.sketch.MarshalBinary()
		if err != nil {
			level.Warn(q.logger).Log("msg", "failed to marshal hyperloglog sketch", "err", err)
			continue
		}
		p := v.parsers
		if len(p) == 0 {
			p = nil
		}
		fields[fieldCount] = &logproto.DetectedField{
			Label:       k,
			Type:        v.fieldType,
			Cardinality: v.Estimate(),
			Sketch:      sketch,
			Parsers:     p,
		}

		fieldCount++
	}

	return &logproto.DetectedFieldsResponse{
		Fields: fields,
		Limit:  req.GetLimit(),
	}, nil
}

type parsedFields struct {
	sketch    *hyperloglog.Sketch
	fieldType logproto.DetectedFieldType
	parsers   []string
}

func newParsedFields(parsers []string) *parsedFields {
	return &parsedFields{
		sketch:    hyperloglog.New(),
		fieldType: logproto.DetectedFieldString,
		parsers:   parsers,
	}
}

func newParsedLabels() *parsedFields {
	return &parsedFields{
		sketch:    hyperloglog.New(),
		fieldType: logproto.DetectedFieldString,
	}
}

func (p *parsedFields) Insert(value string) {
	p.sketch.Insert([]byte(value))
}

func (p *parsedFields) Estimate() uint64 {
	return p.sketch.Estimate()
}

func (p *parsedFields) Marshal() ([]byte, error) {
	return p.sketch.MarshalBinary()
}

func (p *parsedFields) DetermineType(value string) {
	p.fieldType = determineType(value)
}

func determineType(value string) logproto.DetectedFieldType {
	if _, err := strconv.ParseInt(value, 10, 64); err == nil {
		return logproto.DetectedFieldInt
	}

	if _, err := strconv.ParseFloat(value, 64); err == nil {
		return logproto.DetectedFieldFloat
	}

	if _, err := strconv.ParseBool(value); err == nil {
		return logproto.DetectedFieldBoolean
	}

	if _, err := time.ParseDuration(value); err == nil {
		return logproto.DetectedFieldDuration
	}

	if _, err := humanize.ParseBytes(value); err == nil {
		return logproto.DetectedFieldBytes
	}

	return logproto.DetectedFieldString
}

func parseDetectedFields(limit uint32, streams logqlmodel.Streams) map[string]*parsedFields {
	detectedFields := make(map[string]*parsedFields, limit)
	fieldCount := uint32(0)
	emtpyparsers := []string{}

	for _, stream := range streams {
		streamLbls, err := syntax.ParseLabels(stream.Labels)
		if err != nil {
			streamLbls = labels.EmptyLabels()
		}

		for _, entry := range stream.Entries {
			structuredMetadata := getStructuredMetadata(entry)
			for k, vals := range structuredMetadata {
				df, ok := detectedFields[k]
				if !ok && fieldCount < limit {
					df = newParsedFields(emtpyparsers)
					detectedFields[k] = df
					fieldCount++
				}

				if df == nil {
					continue
				}

				detectType := true
				for _, v := range vals {
					parsedFields := detectedFields[k]
					if detectType {
						// we don't want to determine the type for every line, so we assume the type in each stream will be the same, and re-detect the type for the next stream
						parsedFields.DetermineType(v)
						detectType = false
					}

					parsedFields.Insert(v)
				}
			}

			streamLbls := logql_log.NewBaseLabelsBuilder().ForLabels(streamLbls, streamLbls.Hash())
			parsedLabels, parsers := parseEntry(entry, streamLbls)
			for k, vals := range parsedLabels {
				df, ok := detectedFields[k]
				if !ok && fieldCount < limit {
					df = newParsedFields(parsers)
					detectedFields[k] = df
					fieldCount++
				}

				if df == nil {
					continue
				}

				for _, parser := range parsers {
					if !slices.Contains(df.parsers, parser) {
						df.parsers = append(df.parsers, parser)
					}
				}

				detectType := true
				for _, v := range vals {
					parsedFields := detectedFields[k]
					if detectType {
						// we don't want to determine the type for every line, so we assume the type in each stream will be the same, and re-detect the type for the next stream
						parsedFields.DetermineType(v)
						detectType = false
					}

					parsedFields.Insert(v)
				}
			}
		}
	}

	return detectedFields
}

func getStructuredMetadata(entry push.Entry) map[string][]string {
	labels := map[string]map[string]struct{}{}
	for _, lbl := range entry.StructuredMetadata {
		if values, ok := labels[lbl.Name]; ok {
			values[lbl.Value] = struct{}{}
		} else {
			labels[lbl.Name] = map[string]struct{}{lbl.Value: {}}
		}
	}

	result := make(map[string][]string, len(labels))
	for lbl, values := range labels {
		vals := make([]string, 0, len(values))
		for v := range values {
			vals = append(vals, v)
		}
		result[lbl] = vals
	}

	return result
}

func parseEntry(entry push.Entry, lbls *logql_log.LabelsBuilder) (map[string][]string, []string) {
	origParsed := getParsedLabels(entry)
	parsed := make(map[string][]string, len(origParsed))

	for lbl, values := range origParsed {
		if lbl == logqlmodel.ErrorLabel || lbl == logqlmodel.ErrorDetailsLabel ||
			lbl == logqlmodel.PreserveErrorLabel {
			continue
		}

		parsed[lbl] = values
	}

	line := entry.Line
	parser := "json"
	jsonParser := logql_log.NewJSONParser()
	_, jsonSuccess := jsonParser.Process(0, []byte(line), lbls)
	if !jsonSuccess || lbls.HasErr() {
		lbls.Reset()

		logFmtParser := logql_log.NewLogfmtParser(false, false)
		parser = "logfmt"
		_, logfmtSuccess := logFmtParser.Process(0, []byte(line), lbls)
		if !logfmtSuccess || lbls.HasErr() {
			return parsed, nil
		}
	}

	parsedLabels := map[string]map[string]struct{}{}
	for lbl, values := range parsed {
		if vals, ok := parsedLabels[lbl]; ok {
			for _, value := range values {
				vals[value] = struct{}{}
			}
		} else {
			parsedLabels[lbl] = map[string]struct{}{}
			for _, value := range values {
				parsedLabels[lbl][value] = struct{}{}
			}
		}
	}

	lblsResult := lbls.LabelsResult().Parsed()
	for _, lbl := range lblsResult {
		if values, ok := parsedLabels[lbl.Name]; ok {
			values[lbl.Value] = struct{}{}
		} else {
			parsedLabels[lbl.Name] = map[string]struct{}{lbl.Value: {}}
		}
	}

	result := make(map[string][]string, len(parsedLabels))
	for lbl, values := range parsedLabels {
		if lbl == logqlmodel.ErrorLabel || lbl == logqlmodel.ErrorDetailsLabel ||
			lbl == logqlmodel.PreserveErrorLabel {
			continue
		}
		vals := make([]string, 0, len(values))
		for v := range values {
			vals = append(vals, v)
		}
		result[lbl] = vals
	}

	return result, []string{parser}
}

func getParsedLabels(entry push.Entry) map[string][]string {
	labels := map[string]map[string]struct{}{}
	for _, lbl := range entry.Parsed {
		if values, ok := labels[lbl.Name]; ok {
			values[lbl.Value] = struct{}{}
		} else {
			labels[lbl.Name] = map[string]struct{}{lbl.Value: {}}
		}
	}

	result := make(map[string][]string, len(labels))
	for lbl, values := range labels {
		vals := make([]string, 0, len(values))
		for v := range values {
			vals = append(vals, v)
		}
		result[lbl] = vals
	}

	return result
}

// streamsForFieldDetection reads the streams from the iterator and returns them sorted.
func streamsForFieldDetection(i iter.EntryIterator, size uint32) (logqlmodel.Streams, error) {
	streams := map[string]*logproto.Stream{}
	respSize := uint32(0)
	// lastEntry should be a really old time so that the first comparison is always true, we use a negative
	// value here because many unit tests start at time.Unix(0,0)
	lastEntry := time.Unix(-100, 0)
	for respSize < size && i.Next() {
		streamLabels, entry := i.Labels(), i.At()

		// Always going backward as the direction for field detection is hard-coded to BACKWARD
		shouldOutput := entry.Timestamp.Equal(lastEntry) || entry.Timestamp.Before(lastEntry)

		// If lastEntry.Unix < 0 this is the first pass through the loop and we should output the line.
		// Then check to see if the entry is equal to, or past a forward step
		if lastEntry.Unix() < 0 || shouldOutput {
			allLbls, err := syntax.ParseLabels(streamLabels)
			if err != nil {
				continue
			}

			parsedLbls := logproto.FromLabelAdaptersToLabels(entry.Parsed)
			structuredMetadata := logproto.FromLabelAdaptersToLabels(entry.StructuredMetadata)

			onlyStreamLbls := logql_log.NewBaseLabelsBuilder().ForLabels(allLbls, 0)
			allLbls.Range(func(l labels.Label) {
				if parsedLbls.Has(l.Name) || structuredMetadata.Has(l.Name) {
					onlyStreamLbls.Del(l.Name)
				}
			})

			lblStr := onlyStreamLbls.LabelsResult().String()
			stream, ok := streams[lblStr]
			if !ok {
				stream = &logproto.Stream{
					Labels: lblStr,
				}
				streams[lblStr] = stream
			}
			stream.Entries = append(stream.Entries, entry)
			lastEntry = i.At().Timestamp
			respSize++
		}
	}

	result := make(logqlmodel.Streams, 0, len(streams))
	for _, stream := range streams {
		result = append(result, *stream)
	}
	sort.Sort(result)
	return result, i.Err()
}
