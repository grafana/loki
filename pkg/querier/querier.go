package querier

import (
	"context"
	"flag"
	"net/http"
	"time"

	"github.com/prometheus/common/model"
	"github.com/weaveworks/common/httpgrpc"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/cortexproject/cortex/pkg/util/spanlogger"
	cortex_validation "github.com/cortexproject/cortex/pkg/util/validation"
	"github.com/go-kit/kit/log/level"
	"github.com/grafana/dskit/tenant"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/storage"
	listutil "github.com/grafana/loki/pkg/util"
	"github.com/grafana/loki/pkg/validation"
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
	QueryTimeout                  time.Duration    `yaml:"query_timeout"`
	TailMaxDuration               time.Duration    `yaml:"tail_max_duration"`
	ExtraQueryDelay               time.Duration    `yaml:"extra_query_delay,omitempty"`
	QueryIngestersWithin          time.Duration    `yaml:"query_ingesters_within,omitempty"`
	IngesterQueryStoreMaxLookback time.Duration    `yaml:"-"`
	Engine                        logql.EngineOpts `yaml:"engine,omitempty"`
	MaxConcurrent                 int              `yaml:"max_concurrent"`
	QueryStoreOnly                bool             `yaml:"query_store_only"`
}

// RegisterFlags register flags.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.Engine.RegisterFlagsWithPrefix("querier", f)
	f.DurationVar(&cfg.TailMaxDuration, "querier.tail-max-duration", 1*time.Hour, "Limit the duration for which live tailing request would be served")
	f.DurationVar(&cfg.QueryTimeout, "querier.query-timeout", 1*time.Minute, "Timeout when querying backends (ingesters or storage) during the execution of a query request")
	f.DurationVar(&cfg.ExtraQueryDelay, "querier.extra-query-delay", 0, "Time to wait before sending more than the minimum successful query requests.")
	f.DurationVar(&cfg.QueryIngestersWithin, "querier.query-ingesters-within", 0, "Maximum lookback beyond which queries are not sent to ingester. 0 means all queries are sent to ingester.")
	f.IntVar(&cfg.MaxConcurrent, "querier.max-concurrent", 20, "The maximum number of concurrent queries.")
	f.BoolVar(&cfg.QueryStoreOnly, "querier.query-store-only", false, "Queriers should only query the store and not try to query any ingesters")
}

// Querier handlers queries.
type Querier struct {
	cfg             Config
	store           storage.Store
	engine          *logql.Engine
	limits          *validation.Overrides
	ingesterQuerier *IngesterQuerier
}

// New makes a new Querier.
func New(cfg Config, store storage.Store, ingesterQuerier *IngesterQuerier, limits *validation.Overrides) (*Querier, error) {
	querier := Querier{
		cfg:             cfg,
		store:           store,
		ingesterQuerier: ingesterQuerier,
		limits:          limits,
	}

	querier.engine = logql.NewEngine(cfg.Engine, &querier, limits)

	return &querier, nil
}

func (q *Querier) SetQueryable(queryable logql.Querier) {
	q.engine = logql.NewEngine(q.cfg.Engine, queryable, q.limits)
}

// Select Implements logql.Querier which select logs via matchers and regex filters.
func (q *Querier) SelectLogs(ctx context.Context, params logql.SelectLogParams) (iter.EntryIterator, error) {
	var err error
	params.Start, params.End, err = q.validateQueryRequest(ctx, params)
	if err != nil {
		return nil, err
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

	if storeQueryInterval != nil {
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

	return iter.NewHeapIterator(ctx, iters, params.Direction), nil
}

func (q *Querier) SelectSamples(ctx context.Context, params logql.SelectSampleParams) (iter.SampleIterator, error) {
	var err error
	params.Start, params.End, err = q.validateQueryRequest(ctx, params)
	if err != nil {
		return nil, err
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

	if storeQueryInterval != nil {
		params.Start = storeQueryInterval.start
		params.End = storeQueryInterval.end

		storeIter, err := q.store.SelectSamples(ctx, params)
		if err != nil {
			return nil, err
		}

		iters = append(iters, storeIter)
	}
	return iter.NewHeapSampleIterator(ctx, iters), nil
}

func (q *Querier) buildQueryIntervals(queryStart, queryEnd time.Time) (*interval, *interval) {
	// limitQueryInterval is a flag for whether store queries should be limited to start time of ingester queries.
	limitQueryInterval := false
	// ingesterMLB having -1 means query ingester for whole duration.
	ingesterMLB := time.Duration(-1)
	if q.cfg.IngesterQueryStoreMaxLookback != 0 {
		// IngesterQueryStoreMaxLookback takes the precedence over QueryIngestersWithin while also limiting the store query range.
		limitQueryInterval = true
		ingesterMLB = q.cfg.IngesterQueryStoreMaxLookback
	} else if q.cfg.QueryIngestersWithin != 0 {
		ingesterMLB = q.cfg.QueryIngestersWithin
	}

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

	// see if there is an overlap between ingester query interval and actual query interval, if not just do the store query.
	ingesterOldestStartTime := time.Now().Add(-ingesterMLB)
	if queryEnd.Before(ingesterOldestStartTime) {
		return nil, &interval{
			start: queryStart,
			end:   queryEnd,
		}
	}

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
func (q *Querier) Label(ctx context.Context, req *logproto.LabelRequest) (*logproto.LabelResponse, error) {
	userID, err := tenant.ID(ctx)
	if err != nil {
		return nil, err
	}

	if *req.Start, *req.End, err = validateQueryTimeRangeLimits(ctx, userID, q.limits, *req.Start, *req.End); err != nil {
		return nil, err
	}

	// Enforce the query timeout while querying backends
	ctx, cancel := context.WithDeadline(ctx, time.Now().Add(q.cfg.QueryTimeout))
	defer cancel()

	var ingesterValues [][]string
	if !q.cfg.QueryStoreOnly {
		ingesterValues, err = q.ingesterQuerier.Label(ctx, req)
		if err != nil {
			return nil, err
		}
	}

	from, through := model.TimeFromUnixNano(req.Start.UnixNano()), model.TimeFromUnixNano(req.End.UnixNano())
	var storeValues []string
	if req.Values {
		storeValues, err = q.store.LabelValuesForMetricName(ctx, userID, from, through, "logs", req.Name)
		if err != nil {
			return nil, err
		}
	} else {
		storeValues, err = q.store.LabelNamesForMetricName(ctx, userID, from, through, "logs")
		if err != nil {
			return nil, err
		}
	}

	results := append(ingesterValues, storeValues)

	return &logproto.LabelResponse{
		Values: listutil.MergeStringLists(results...),
	}, nil
}

// Check implements the grpc healthcheck
func (*Querier) Check(_ context.Context, _ *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	return &grpc_health_v1.HealthCheckResponse{Status: grpc_health_v1.HealthCheckResponse_SERVING}, nil
}

// Tail keeps getting matching logs from all ingesters for given query
func (q *Querier) Tail(ctx context.Context, req *logproto.TailRequest) (*Tailer, error) {
	err := q.checkTailRequestLimit(ctx)
	if err != nil {
		return nil, err
	}

	histReq := logql.SelectLogParams{
		QueryRequest: &logproto.QueryRequest{
			Selector:  req.Query,
			Start:     req.Start,
			End:       time.Now(),
			Limit:     req.Limit,
			Direction: logproto.BACKWARD,
		},
	}

	histReq.Start, histReq.End, err = q.validateQueryRequest(ctx, histReq)
	if err != nil {
		return nil, err
	}

	// Enforce the query timeout except when tailing, otherwise the tailing
	// will be terminated once the query timeout is reached
	tailCtx := ctx
	queryCtx, cancelQuery := context.WithDeadline(ctx, time.Now().Add(q.cfg.QueryTimeout))
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
	), nil
}

// Series fetches any matching series for a list of matcher sets
func (q *Querier) Series(ctx context.Context, req *logproto.SeriesRequest) (*logproto.SeriesResponse, error) {
	userID, err := tenant.ID(ctx)
	if err != nil {
		return nil, err
	}

	if req.Start, req.End, err = validateQueryTimeRangeLimits(ctx, userID, q.limits, req.Start, req.End); err != nil {
		return nil, err
	}

	// Enforce the query timeout while querying backends
	ctx, cancel := context.WithDeadline(ctx, time.Now().Add(q.cfg.QueryTimeout))
	defer cancel()

	return q.awaitSeries(ctx, req)
}

func (q *Querier) awaitSeries(ctx context.Context, req *logproto.SeriesRequest) (*logproto.SeriesResponse, error) {
	// buffer the channels to the # of calls they're expecting su
	series := make(chan [][]logproto.SeriesIdentifier, 2)
	errs := make(chan error, 2)

	// fetch series from ingesters and store concurrently
	if q.cfg.QueryStoreOnly {
		series <- [][]logproto.SeriesIdentifier{}
	} else {
		go func() {
			// fetch series identifiers from ingesters
			resps, err := q.ingesterQuerier.Series(ctx, req)
			if err != nil {
				errs <- err
				return
			}

			series <- resps
		}()
	}

	go func() {
		storeValues, err := q.seriesForMatchers(ctx, req.Start, req.End, req.GetGroups(), req.Shards)
		if err != nil {
			errs <- err
			return
		}
		series <- [][]logproto.SeriesIdentifier{storeValues}
	}()

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
func (q *Querier) seriesForMatchers(
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
func (q *Querier) seriesForMatcher(ctx context.Context, from, through time.Time, matcher string, shards []string) ([]logproto.SeriesIdentifier, error) {
	ids, err := q.store.GetSeries(ctx, logql.SelectLogParams{
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

func (q *Querier) validateQueryRequest(ctx context.Context, req logql.QueryParams) (time.Time, time.Time, error) {
	userID, err := tenant.ID(ctx)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}

	selector, err := req.LogSelector()
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	matchers := selector.Matchers()

	maxStreamMatchersPerQuery := q.limits.MaxStreamsMatchersPerQuery(userID)
	if len(matchers) > maxStreamMatchersPerQuery {
		return time.Time{}, time.Time{}, httpgrpc.Errorf(http.StatusBadRequest,
			"max streams matchers per query exceeded, matchers-count > limit (%d > %d)", len(matchers), maxStreamMatchersPerQuery)
	}

	return validateQueryTimeRangeLimits(ctx, userID, q.limits, req.GetStart(), req.GetEnd())
}

type timeRangeLimits interface {
	MaxQueryLookback(string) time.Duration
	MaxQueryLength(string) time.Duration
}

func validateQueryTimeRangeLimits(ctx context.Context, userID string, limits timeRangeLimits, from, through time.Time) (time.Time, time.Time, error) {
	now := nowFunc()
	// Clamp the time range based on the max query lookback.
	if maxQueryLookback := limits.MaxQueryLookback(userID); maxQueryLookback > 0 && from.Before(now.Add(-maxQueryLookback)) {
		origStartTime := from
		from = now.Add(-maxQueryLookback)

		level.Debug(spanlogger.FromContext(ctx)).Log(
			"msg", "the start time of the query has been manipulated because of the 'max query lookback' setting",
			"original", origStartTime,
			"updated", from)

	}
	if maxQueryLength := limits.MaxQueryLength(userID); maxQueryLength > 0 && (through).Sub(from) > maxQueryLength {
		return time.Time{}, time.Time{}, httpgrpc.Errorf(http.StatusBadRequest, cortex_validation.ErrQueryTooLong, (through).Sub(from), maxQueryLength)
	}
	if through.Before(from) {
		return time.Time{}, time.Time{}, httpgrpc.Errorf(http.StatusBadRequest, "invalid query, through < from (%s < %s)", through, from)
	}
	return from, through, nil
}

func (q *Querier) checkTailRequestLimit(ctx context.Context) error {
	userID, err := tenant.ID(ctx)
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
	l := uint32(q.limits.MaxConcurrentTailRequests(userID))
	if maxCnt >= l {
		return httpgrpc.Errorf(http.StatusBadRequest,
			"max concurrent tail requests limit exceeded, count > limit (%d > %d)", maxCnt+1, l)
	}

	return nil
}
