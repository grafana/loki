package querier

import (
	"context"
	"flag"
	"net/http"
	"time"

	"github.com/opentracing/opentracing-go"

	"github.com/grafana/loki/pkg/storage/stores/index/seriesvolume"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/tenant"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/weaveworks/common/httpgrpc"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/stores/index/stats"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper/compactor/deletion"
	listutil "github.com/grafana/loki/pkg/util"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/util/spanlogger"
	util_validation "github.com/grafana/loki/pkg/util/validation"
)

const (
	// How long the Tailer should wait - once there are no entries to read from ingesters -
	// before checking if a new entry is available (to avoid spinning the CPU in a continuous
	// check loop)
	tailerWaitEntryThrottle = time.Second / 2
)

var nowFunc = func() time.Time { return time.Now() }

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
	QueryTimeout                  time.Duration    `yaml:"query_timeout" doc:"hidden"`
	PerRequestLimitsEnabled       bool             `yaml:"per_request_limits_enabled"`
}

// RegisterFlags register flags.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.Engine.RegisterFlagsWithPrefix("querier", f)
	f.DurationVar(&cfg.TailMaxDuration, "querier.tail-max-duration", 1*time.Hour, "Maximum duration for which the live tailing requests are served.")
	f.DurationVar(&cfg.ExtraQueryDelay, "querier.extra-query-delay", 0, "Time to wait before sending more than the minimum successful query requests.")
	f.DurationVar(&cfg.QueryIngestersWithin, "querier.query-ingesters-within", 3*time.Hour, "Maximum lookback beyond which queries are not sent to ingester. 0 means all queries are sent to ingester.")
	f.IntVar(&cfg.MaxConcurrent, "querier.max-concurrent", 10, "The maximum number of concurrent queries allowed.")
	f.BoolVar(&cfg.QueryStoreOnly, "querier.query-store-only", false, "Only query the store, and not attempt any ingesters. This is useful for running a standalone querier pool operating only against stored data.")
	f.BoolVar(&cfg.QueryIngesterOnly, "querier.query-ingester-only", false, "When true, queriers only query the ingesters, and not stored data. This is useful when the object store is unavailable.")
	f.BoolVar(&cfg.MultiTenantQueriesEnabled, "querier.multi-tenant-queries-enabled", false, "When true, allow queries to span multiple tenants.")
	f.BoolVar(&cfg.PerRequestLimitsEnabled, "querier.per-request-limits-enabled", false, "When true, querier limits sent via a header are enforced.")
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
	Tail(ctx context.Context, req *logproto.TailRequest) (*Tailer, error)
	IndexStats(ctx context.Context, req *loghttp.RangeQuery) (*stats.Stats, error)
	SeriesVolume(ctx context.Context, req *logproto.VolumeRequest) (*logproto.VolumeResponse, error)
}

type Limits interface {
	logql.Limits
	timeRangeLimits
	QueryTimeout(context.Context, string) time.Duration
	MaxStreamsMatchersPerQuery(context.Context, string) int
	MaxConcurrentTailRequests(context.Context, string) int
	MaxEntriesLimitPerQuery(context.Context, string) int
}

// SingleTenantQuerier handles single tenant queries.
type SingleTenantQuerier struct {
	cfg             Config
	store           storage.Store
	limits          Limits
	ingesterQuerier *IngesterQuerier
	deleteGetter    deleteGetter
	metrics         *Metrics
}

type deleteGetter interface {
	GetAllDeleteRequestsForUser(ctx context.Context, userID string) ([]deletion.DeleteRequest, error)
}

// New makes a new Querier.
func New(cfg Config, store storage.Store, ingesterQuerier *IngesterQuerier, limits Limits, d deleteGetter, r prometheus.Registerer) (*SingleTenantQuerier, error) {
	return &SingleTenantQuerier{
		cfg:             cfg,
		store:           store,
		ingesterQuerier: ingesterQuerier,
		limits:          limits,
		deleteGetter:    d,
		metrics:         NewMetrics(r),
	}, nil
}

// Select Implements logql.Querier which select logs via matchers and regex filters.
func (q *SingleTenantQuerier) SelectLogs(ctx context.Context, params logql.SelectLogParams) (iter.EntryIterator, error) {
	var err error
	params.Start, params.End, err = q.validateQueryRequest(ctx, params)
	if err != nil {
		return nil, err
	}

	params.QueryRequest.Deletes, err = q.deletesForUser(ctx, params.Start, params.End)
	if err != nil {
		level.Error(spanlogger.FromContext(ctx)).Log("msg", "failed loading deletes for user", "err", err)
	}

	ingesterQueryInterval, storeQueryInterval := q.buildQueryIntervals(params.Start, params.End)

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
		level.Debug(spanlogger.FromContext(ctx)).Log(
			"msg", "querying ingester",
			"params", newParams)
		ingesterIters, err := q.ingesterQuerier.SelectLogs(ctx, newParams)
		if err != nil {
			return nil, err
		}

		iters = append(iters, ingesterIters...)
	}

	if !q.cfg.QueryIngesterOnly && storeQueryInterval != nil {
		params.Start = storeQueryInterval.start
		params.End = storeQueryInterval.end
		level.Debug(spanlogger.FromContext(ctx)).Log(
			"msg", "querying store",
			"params", params)
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
	var err error
	params.Start, params.End, err = q.validateQueryRequest(ctx, params)
	if err != nil {
		return nil, err
	}

	params.SampleQueryRequest.Deletes, err = q.deletesForUser(ctx, params.Start, params.End)
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

func (q *SingleTenantQuerier) deletesForUser(ctx context.Context, startT, endT time.Time) ([]*logproto.Delete, error) {
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	d, err := q.deleteGetter.GetAllDeleteRequestsForUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	start := startT.UnixNano()
	end := endT.UnixNano()

	var deletes []*logproto.Delete
	for _, del := range d {
		if del.StartTime.UnixNano() <= end && del.EndTime.UnixNano() >= start {
			deletes = append(deletes, &logproto.Delete{
				Selector: del.Query,
				Start:    del.StartTime.UnixNano(),
				End:      del.EndTime.UnixNano(),
			})
		}
	}

	return deletes, nil
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

	if *req.Start, *req.End, err = validateQueryTimeRangeLimits(ctx, userID, q.limits, *req.Start, *req.End); err != nil {
		return nil, err
	}

	var matchers []*labels.Matcher
	if req.Query != "" {
		matchers, err = syntax.ParseMatchers(req.Query)
		if err != nil {
			return nil, err
		}
	}

	// Enforce the query timeout while querying backends
	queryTimeout := q.limits.QueryTimeout(ctx, userID)
	// TODO: remove this clause once we remove the deprecated query-timeout flag.
	if q.cfg.QueryTimeout != 0 { // querier YAML configuration.
		level.Warn(util_log.Logger).Log("msg", "deprecated querier:query_timeout YAML configuration identified. Please migrate to limits:query_timeout instead.", "err", err, "call", "Label")
		queryTimeout = q.cfg.QueryTimeout
	}
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
				storeValues, err = q.store.LabelNamesForMetricName(ctx, userID, from, through, "logs")
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

// Tail keeps getting matching logs from all ingesters for given query
func (q *SingleTenantQuerier) Tail(ctx context.Context, req *logproto.TailRequest) (*Tailer, error) {
	err := q.checkTailRequestLimit(ctx)
	if err != nil {
		return nil, err
	}

	deletes, err := q.deletesForUser(ctx, req.Start, time.Now())
	if err != nil {
		level.Error(spanlogger.FromContext(ctx)).Log("msg", "failed loading deletes for user", "err", err)
	}

	histReq := logql.SelectLogParams{
		QueryRequest: &logproto.QueryRequest{
			Selector:  req.Query,
			Start:     req.Start,
			End:       time.Now(),
			Limit:     req.Limit,
			Direction: logproto.BACKWARD,
			Deletes:   deletes,
		},
	}

	histReq.Start, histReq.End, err = q.validateQueryRequest(ctx, histReq)
	if err != nil {
		return nil, err
	}

	// Enforce the query timeout except when tailing, otherwise the tailing
	// will be terminated once the query timeout is reached
	tailCtx := ctx
	tenantID, err := tenant.TenantID(tailCtx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to load tenant")
	}
	queryTimeout := q.limits.QueryTimeout(tailCtx, tenantID)
	// TODO: remove this clause once we remove the deprecated query-timeout flag.
	if q.cfg.QueryTimeout != 0 { // querier YAML configuration.
		level.Warn(util_log.Logger).Log("msg", "deprecated querier:query_timeout YAML configuration identified. Please migrate to limits:query_timeout instead.", "call", "SingleTenantQuerier/Tail")
		queryTimeout = q.cfg.QueryTimeout
	}
	queryCtx, cancelQuery := context.WithDeadline(ctx, time.Now().Add(queryTimeout))
	defer cancelQuery()

	tailClients, err := q.ingesterQuerier.Tail(tailCtx, req)
	if err != nil {
		return nil, err
	}

	histIterators, err := q.SelectLogs(queryCtx, histReq)
	if err != nil {
		return nil, err
	}

	reversedIterator, err := iter.NewReversedIter(histIterators, req.Limit, true)
	if err != nil {
		return nil, err
	}

	return newTailer(
		time.Duration(req.DelayFor)*time.Second,
		tailClients,
		reversedIterator,
		func(connectedIngestersAddr []string) (map[string]logproto.Querier_TailClient, error) {
			return q.ingesterQuerier.TailDisconnectedIngesters(tailCtx, req, connectedIngestersAddr)
		},
		q.cfg.TailMaxDuration,
		tailerWaitEntryThrottle,
		q.metrics,
	), nil
}

// Series fetches any matching series for a list of matcher sets
func (q *SingleTenantQuerier) Series(ctx context.Context, req *logproto.SeriesRequest) (*logproto.SeriesResponse, error) {
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	if req.Start, req.End, err = validateQueryTimeRangeLimits(ctx, userID, q.limits, req.Start, req.End); err != nil {
		return nil, err
	}

	// Enforce the query timeout while querying backends
	queryTimeout := q.limits.QueryTimeout(ctx, userID)
	// TODO: remove this clause once we remove the deprecated query-timeout flag.
	if q.cfg.QueryTimeout != 0 { // querier YAML configuration.
		level.Warn(util_log.Logger).Log("msg", "deprecated querier:query_timeout YAML configuration identified. Please migrate to limits:query_timeout instead.", "call", "SingleTenantQuerier/Series")
		queryTimeout = q.cfg.QueryTimeout
	}
	ctx, cancel := context.WithDeadline(ctx, time.Now().Add(queryTimeout))
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

	deduped := make(map[string]logproto.SeriesIdentifier)
	for _, set := range sets {
		for _, s := range set {
			key := loghttp.LabelSet(s.Labels).String()
			if _, exists := deduped[key]; !exists {
				deduped[key] = s
			}
		}
	}

	response := &logproto.SeriesResponse{
		Series: make([]logproto.SeriesIdentifier, 0, len(deduped)),
	}

	for _, s := range deduped {
		response.Series = append(response.Series, s)
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
	ids, err := q.store.Series(ctx, logql.SelectLogParams{
		QueryRequest: &logproto.QueryRequest{
			Selector:  matcher,
			Limit:     1,
			Start:     from,
			End:       through,
			Direction: logproto.FORWARD,
			Shards:    shards,
		},
	})
	if err != nil {
		return nil, err
	}
	return ids, nil
}

func (q *SingleTenantQuerier) validateQueryRequest(ctx context.Context, req logql.QueryParams) (time.Time, time.Time, error) {
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}

	selector, err := req.LogSelector()
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	matchers := selector.Matchers()

	maxStreamMatchersPerQuery := q.limits.MaxStreamsMatchersPerQuery(ctx, userID)
	if len(matchers) > maxStreamMatchersPerQuery {
		return time.Time{}, time.Time{}, httpgrpc.Errorf(http.StatusBadRequest,
			"max streams matchers per query exceeded, matchers-count > limit (%d > %d)", len(matchers), maxStreamMatchersPerQuery)
	}

	return validateQueryTimeRangeLimits(ctx, userID, q.limits, req.GetStart(), req.GetEnd())
}

type timeRangeLimits interface {
	MaxQueryLookback(context.Context, string) time.Duration
	MaxQueryLength(context.Context, string) time.Duration
}

func validateQueryTimeRangeLimits(ctx context.Context, userID string, limits timeRangeLimits, from, through time.Time) (time.Time, time.Time, error) {
	now := nowFunc()
	// Clamp the time range based on the max query lookback.
	maxQueryLookback := limits.MaxQueryLookback(ctx, userID)
	if maxQueryLookback > 0 && from.Before(now.Add(-maxQueryLookback)) {
		origStartTime := from
		from = now.Add(-maxQueryLookback)

		level.Debug(spanlogger.FromContext(ctx)).Log(
			"msg", "the start time of the query has been manipulated because of the 'max query lookback' setting",
			"original", origStartTime,
			"updated", from)

	}
	maxQueryLength := limits.MaxQueryLength(ctx, userID)
	if maxQueryLength > 0 && (through).Sub(from) > maxQueryLength {
		return time.Time{}, time.Time{}, httpgrpc.Errorf(http.StatusBadRequest, util_validation.ErrQueryTooLong, (through).Sub(from), model.Duration(maxQueryLength))
	}
	if through.Before(from) {
		return time.Time{}, time.Time{}, httpgrpc.Errorf(http.StatusBadRequest, util_validation.ErrQueryTooOld, model.Duration(maxQueryLookback))
	}
	return from, through, nil
}

func (q *SingleTenantQuerier) checkTailRequestLimit(ctx context.Context) error {
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return err
	}

	responses, err := q.ingesterQuerier.TailersCount(ctx)
	// We are only checking active ingesters, and any error returned stops checking other ingesters
	// so return that error here as well.
	if err != nil {
		return err
	}

	var maxCnt uint32
	maxCnt = 0
	for _, resp := range responses {
		if resp > maxCnt {
			maxCnt = resp
		}
	}
	l := uint32(q.limits.MaxConcurrentTailRequests(ctx, userID))
	if maxCnt >= l {
		return httpgrpc.Errorf(http.StatusBadRequest,
			"max concurrent tail requests limit exceeded, count > limit (%d > %d)", maxCnt+1, l)
	}

	return nil
}

func (q *SingleTenantQuerier) IndexStats(ctx context.Context, req *loghttp.RangeQuery) (*stats.Stats, error) {
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	start, end, err := validateQueryTimeRangeLimits(ctx, userID, q.limits, req.Start, req.End)
	if err != nil {
		return nil, err
	}

	matchers, err := syntax.ParseMatchers(req.Query)
	if err != nil {
		return nil, err
	}

	// Enforce the query timeout while querying backends
	queryTimeout := q.limits.QueryTimeout(ctx, userID)
	// TODO: remove this clause once we remove the deprecated query-timeout flag.
	if q.cfg.QueryTimeout != 0 { // querier YAML configuration.
		level.Warn(util_log.Logger).Log("msg", "deprecated querier:query_timeout YAML configuration identified. Please migrate to limits:query_timeout instead.", "call", "SingleTenantQuerier/IndexStats")
		queryTimeout = q.cfg.QueryTimeout
	}
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

func (q *SingleTenantQuerier) SeriesVolume(ctx context.Context, req *logproto.VolumeRequest) (*logproto.VolumeResponse, error) {
	sp, ctx := opentracing.StartSpanFromContext(ctx, "Querier.SeriesVolume")
	defer sp.Finish()

	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	matchers, err := syntax.ParseMatchers(req.Matchers)
	if err != nil && req.Matchers != seriesvolume.MatchAny {
		return nil, err
	}

	// Enforce the query timeout while querying backends
	queryTimeout := q.limits.QueryTimeout(ctx, userID)

	// TODO: remove this clause once we remove the deprecated query-timeout flag.
	if q.cfg.QueryTimeout != 0 { // querier YAML configuration.
		level.Warn(util_log.Logger).Log("msg", "deprecated querier:query_timeout YAML configuration identified. Please migrate to limits:query_timeout instead.", "call", "SingleTenantQuerier/Volume")
		queryTimeout = q.cfg.QueryTimeout
	}
	ctx, cancel := context.WithDeadline(ctx, time.Now().Add(queryTimeout))
	defer cancel()

	sp.LogKV(
		"user", userID,
		"from", req.From.Time(),
		"through", req.Through.Time(),
		"matchers", syntax.MatchersString(matchers),
		"limit", req.Limit,
	)

	return q.store.SeriesVolume(
		ctx,
		userID,
		req.From,
		req.Through,
		req.Limit,
		matchers...,
	)
}
