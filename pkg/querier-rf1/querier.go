package querierrf1

import (
	"context"
	"flag"
	"fmt"
	"net/http"
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
	"golang.org/x/exp/slices"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/loki/v3/pkg/compactor/deletion"
	"github.com/grafana/loki/v3/pkg/indexgateway"
	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	logql_log "github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/querier"
	"github.com/grafana/loki/v3/pkg/querier-rf1/wal"
	querier_limits "github.com/grafana/loki/v3/pkg/querier/limits"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	"github.com/grafana/loki/v3/pkg/storage"
	"github.com/grafana/loki/v3/pkg/storage/stores/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/seriesvolume"
	"github.com/grafana/loki/v3/pkg/storage/stores/index/stats"
	"github.com/grafana/loki/v3/pkg/util/spanlogger"
	util_validation "github.com/grafana/loki/v3/pkg/util/validation"
)

var nowFunc = func() time.Time { return time.Now() }

type Config struct {
	Enabled                 bool
	ExtraQueryDelay         time.Duration    `yaml:"extra_query_delay,omitempty"`
	Engine                  logql.EngineOpts `yaml:"engine,omitempty"`
	MaxConcurrent           int              `yaml:"max_concurrent"`
	PerRequestLimitsEnabled bool             `yaml:"per_request_limits_enabled"`
}

// RegisterFlags register flags.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.Engine.RegisterFlagsWithPrefix("querier-rf1", f)
	f.BoolVar(&cfg.Enabled, "querier-rf1.enabled", false, "Enable the RF1 querier. If set, replaces the usual querier with an RF-1 querier.")
	f.DurationVar(&cfg.ExtraQueryDelay, "querier-rf1.extra-query-delay", 0, "Time to wait before sending more than the minimum successful query requests.")
	f.IntVar(&cfg.MaxConcurrent, "querier-rf1.max-concurrent", 4, "The maximum number of queries that can be simultaneously processed by the querier.")
	f.BoolVar(&cfg.PerRequestLimitsEnabled, "querier-rf1.per-request-limits-enabled", false, "When true, querier limits sent via a header are enforced.")
}

var _ Querier = &Rf1Querier{}

// Querier can select logs and samples and handle query requests.
type Querier interface {
	logql.Querier
	Label(ctx context.Context, req *logproto.LabelRequest) (*logproto.LabelResponse, error)
	Series(ctx context.Context, req *logproto.SeriesRequest) (*logproto.SeriesResponse, error)
	Tail(ctx context.Context, req *logproto.TailRequest, categorizedLabels bool) (*querier.Tailer, error)
	IndexStats(ctx context.Context, req *loghttp.RangeQuery) (*stats.Stats, error)
	IndexShards(ctx context.Context, req *loghttp.RangeQuery, targetBytesPerShard uint64) (*logproto.ShardsResponse, error)
	Volume(ctx context.Context, req *logproto.VolumeRequest) (*logproto.VolumeResponse, error)
	DetectedFields(ctx context.Context, req *logproto.DetectedFieldsRequest) (*logproto.DetectedFieldsResponse, error)
	Patterns(ctx context.Context, req *logproto.QueryPatternsRequest) (*logproto.QueryPatternsResponse, error)
	DetectedLabels(ctx context.Context, req *logproto.DetectedLabelsRequest) (*logproto.DetectedLabelsResponse, error)
}

type Limits querier_limits.Limits

// Store is the store interface we need on the querier.
type Store interface {
	storage.SelectStore
	index.BaseReader
	index.StatsReader
}

// Rf1Querier handles rf1 queries.
type Rf1Querier struct {
	cfg            Config
	store          Store
	limits         Limits
	deleteGetter   deleteGetter
	logger         log.Logger
	patternQuerier PatterQuerier
	walQuerier     logql.Querier
}

type deleteGetter interface {
	GetAllDeleteRequestsForUser(ctx context.Context, userID string) ([]deletion.DeleteRequest, error)
}

// New makes a new Querier for RF1 work.
func New(cfg Config, store Store, limits Limits, d deleteGetter, metastore wal.Metastore, b wal.BlockStorage, logger log.Logger) (*Rf1Querier, error) {
	querier, err := wal.New(metastore, b)
	if err != nil {
		return nil, err
	}
	return &Rf1Querier{
		cfg:          cfg,
		store:        store,
		limits:       limits,
		deleteGetter: d,
		walQuerier:   querier,
		logger:       logger,
	}, nil
}

// Select Implements logql.Querier which select logs via matchers and regex filters.
func (q *Rf1Querier) SelectLogs(ctx context.Context, params logql.SelectLogParams) (iter.EntryIterator, error) {
	var err error
	params.Start, params.End, err = q.validateQueryRequest(ctx, params)
	if err != nil {
		return nil, err
	}

	params.QueryRequest.Deletes, err = q.deletesForUser(ctx, params.Start, params.End)
	if err != nil {
		level.Error(spanlogger.FromContext(ctx)).Log("msg", "failed loading deletes for user", "err", err)
	}

	sp := opentracing.SpanFromContext(ctx)
	iters := []iter.EntryIterator{}
	if sp != nil {
		sp.LogKV(
			"msg", "querying rf1 store",
			"params", params)
	}
	storeIter, err := q.walQuerier.SelectLogs(ctx, params)
	if err != nil {
		return nil, err
	}

	iters = append(iters, storeIter)
	if len(iters) == 1 {
		return iters[0], nil
	}
	return iter.NewMergeEntryIterator(ctx, iters, params.Direction), nil
}

func (q *Rf1Querier) SelectSamples(ctx context.Context, params logql.SelectSampleParams) (iter.SampleIterator, error) {
	var err error
	params.Start, params.End, err = q.validateQueryRequest(ctx, params)
	if err != nil {
		return nil, err
	}

	params.SampleQueryRequest.Deletes, err = q.deletesForUser(ctx, params.Start, params.End)
	if err != nil {
		level.Error(spanlogger.FromContext(ctx)).Log("msg", "failed loading deletes for user", "err", err)
	}

	sp := opentracing.SpanFromContext(ctx)
	if sp != nil {
		sp.LogKV(
			"msg", "querying rf1 store for samples",
			"params", params)
	}
	storeIter, err := q.walQuerier.SelectSamples(ctx, params)
	if err != nil {
		return nil, err
	}

	iters := []iter.SampleIterator{}
	iters = append(iters, storeIter)
	return iter.NewMergeSampleIterator(ctx, iters), nil
}

func (q *Rf1Querier) deletesForUser(ctx context.Context, startT, endT time.Time) ([]*logproto.Delete, error) {
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

// Label does the heavy lifting for a Label query.
func (q *Rf1Querier) Label(ctx context.Context, req *logproto.LabelRequest) (*logproto.LabelResponse, error) {
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	if *req.Start, *req.End, err = validateQueryTimeRangeLimits(ctx, userID, q.limits, *req.Start, *req.End); err != nil {
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

	var storeValues []string
	g.Go(func() error {
		var (
			err     error
			from    = model.TimeFromUnixNano(req.Start.UnixNano())
			through = model.TimeFromUnixNano(req.End.UnixNano())
		)

		if req.Values {
			storeValues, err = q.store.LabelValuesForMetricName(ctx, userID, from, through, "logs", req.Name, matchers...)
		} else {
			storeValues, err = q.store.LabelNamesForMetricName(ctx, userID, from, through, "logs", matchers...)
		}
		return err
	})

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return &logproto.LabelResponse{
		Values: storeValues,
	}, nil
}

// Check implements the grpc healthcheck
func (*Rf1Querier) Check(_ context.Context, _ *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	return &grpc_health_v1.HealthCheckResponse{Status: grpc_health_v1.HealthCheckResponse_SERVING}, nil
}

// Tail keeps getting matching logs from all ingesters for given query
func (q *Rf1Querier) Tail(_ context.Context, _ *logproto.TailRequest, _ bool) (*querier.Tailer, error) {
	return nil, errors.New("not implemented")
}

// Series fetches any matching series for a list of matcher sets
func (q *Rf1Querier) Series(ctx context.Context, req *logproto.SeriesRequest) (*logproto.SeriesResponse, error) {
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	if req.Start, req.End, err = validateQueryTimeRangeLimits(ctx, userID, q.limits, req.Start, req.End); err != nil {
		return nil, err
	}

	// Enforce the query timeout while querying backends
	queryTimeout := q.limits.QueryTimeout(ctx, userID)
	ctx, cancel := context.WithDeadlineCause(ctx, time.Now().Add(queryTimeout), errors.New("query timeout reached"))
	defer cancel()

	return q.awaitSeries(ctx, req)
}

func (q *Rf1Querier) awaitSeries(ctx context.Context, req *logproto.SeriesRequest) (*logproto.SeriesResponse, error) {
	// buffer the channels to the # of calls they're expecting su
	series := make(chan [][]logproto.SeriesIdentifier, 1)
	errs := make(chan error, 1)

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
func (q *Rf1Querier) seriesForMatchers(
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
func (q *Rf1Querier) seriesForMatcher(ctx context.Context, from, through time.Time, matcher string, shards []string) ([]logproto.SeriesIdentifier, error) {
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

func (q *Rf1Querier) validateQueryRequest(ctx context.Context, req logql.QueryParams) (time.Time, time.Time, error) {
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

type TimeRangeLimits querier_limits.TimeRangeLimits

func validateQueryTimeRangeLimits(ctx context.Context, userID string, limits TimeRangeLimits, from, through time.Time) (time.Time, time.Time, error) {
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

func (q *Rf1Querier) IndexStats(ctx context.Context, req *loghttp.RangeQuery) (*stats.Stats, error) {
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	start, end, err := validateQueryTimeRangeLimits(ctx, userID, q.limits, req.Start, req.End)
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

func (q *Rf1Querier) IndexShards(
	ctx context.Context,
	req *loghttp.RangeQuery,
	targetBytesPerShard uint64,
) (*logproto.ShardsResponse, error) {
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	start, end, err := validateQueryTimeRangeLimits(ctx, userID, q.limits, req.Start, req.End)
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

func (q *Rf1Querier) Volume(ctx context.Context, req *logproto.VolumeRequest) (*logproto.VolumeResponse, error) {
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

	numResponses := 1
	responses := make([]*logproto.VolumeResponse, 0, numResponses)

	resp, err := q.store.Volume(
		ctx,
		userID,
		model.TimeFromUnix(req.From.Unix()),
		model.TimeFromUnix(req.Through.Unix()),
		req.Limit,
		req.TargetLabels,
		req.AggregateBy,
		matchers...,
	)
	if err != nil {
		return nil, err
	}

	responses = append(responses, resp)

	return seriesvolume.Merge(responses, req.Limit), nil
}

// DetectedLabels fetches labels and values from store and ingesters and filters them by relevance criteria as per logs app.
func (q *Rf1Querier) DetectedLabels(ctx context.Context, req *logproto.DetectedLabelsRequest) (*logproto.DetectedLabelsResponse, error) {
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

	if req.Start, req.End, err = validateQueryTimeRangeLimits(ctx, userID, q.limits, req.Start, req.End); err != nil {
		return nil, err
	}

	// Fetch labels from the store
	storeLabelsMap := make(map[string][]string)
	var matchers []*labels.Matcher
	if req.Query != "" {
		matchers, err = syntax.ParseMatchers(req.Query, true)
		if err != nil {
			return nil, err
		}
	}
	g.Go(func() error {
		var err error
		start := model.TimeFromUnixNano(req.Start.UnixNano())
		end := model.TimeFromUnixNano(req.End.UnixNano())
		storeLabels, err := q.store.LabelNamesForMetricName(ctx, userID, start, end, "logs")
		for _, label := range storeLabels {
			values, err := q.store.LabelValuesForMetricName(ctx, userID, start, end, "logs", label, matchers...)
			if err != nil {
				return err
			}
			storeLabelsMap[label] = values
		}
		return err
	})

	if err := g.Wait(); err != nil {
		return nil, err
	}

	if len(storeLabelsMap) == 0 {
		return &logproto.DetectedLabelsResponse{
			DetectedLabels: []*logproto.DetectedLabel{},
		}, nil
	}

	return &logproto.DetectedLabelsResponse{
		DetectedLabels: countLabelsAndCardinality(storeLabelsMap, staticLabels),
	}, nil
}

func countLabelsAndCardinality(storeLabelsMap map[string][]string, staticLabels map[string]struct{}) []*logproto.DetectedLabel {
	dlMap := make(map[string]*parsedFields)

	for label, values := range storeLabelsMap {
		if _, isStatic := staticLabels[label]; isStatic || !containsAllIDTypes(values) {
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

func (q *Rf1Querier) DetectedFields(ctx context.Context, req *logproto.DetectedFieldsRequest) (*logproto.DetectedFieldsResponse, error) {
	expr, err := syntax.ParseLogSelector(req.Query, true)
	if err != nil {
		return nil, err
	}
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

	detectedFields := parseDetectedFields(ctx, req.FieldLimit, streams)

	fields := make([]*logproto.DetectedField, len(detectedFields))
	fieldCount := 0
	for k, v := range detectedFields {
		sketch, err := v.sketch.MarshalBinary()
		if err != nil {
			level.Warn(q.logger).Log("msg", "failed to marshal hyperloglog sketch", "err", err)
			continue
		}

		fields[fieldCount] = &logproto.DetectedField{
			Label:       k,
			Type:        v.fieldType,
			Cardinality: v.Estimate(),
			Sketch:      sketch,
			Parsers:     v.parsers,
		}

		fieldCount++
	}

	return &logproto.DetectedFieldsResponse{
		Fields:     fields,
		FieldLimit: req.GetFieldLimit(),
	}, nil
}

type parsedFields struct {
	sketch    *hyperloglog.Sketch
	fieldType logproto.DetectedFieldType
	parsers   []string
}

func newParsedFields(parser *string) *parsedFields {
	p := ""
	if parser != nil {
		p = *parser
	}
	return &parsedFields{
		sketch:    hyperloglog.New(),
		fieldType: logproto.DetectedFieldString,
		parsers:   []string{p},
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

func parseDetectedFields(ctx context.Context, limit uint32, streams logqlmodel.Streams) map[string]*parsedFields {
	detectedFields := make(map[string]*parsedFields, limit)
	fieldCount := uint32(0)

	for _, stream := range streams {
		level.Debug(spanlogger.FromContext(ctx)).Log(
			"detected_fields", "true",
			"msg", fmt.Sprintf("looking for detected fields in stream %d with %d lines", stream.Hash, len(stream.Entries)))

		for _, entry := range stream.Entries {
			detected, parser := parseLine(entry.Line)
			for k, vals := range detected {
				df, ok := detectedFields[k]
				if !ok && fieldCount < limit {
					df = newParsedFields(parser)
					detectedFields[k] = df
					fieldCount++
				}

				if df == nil {
					continue
				}

				if !slices.Contains(df.parsers, *parser) {
					df.parsers = append(df.parsers, *parser)
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

				level.Debug(spanlogger.FromContext(ctx)).Log(
					"detected_fields", "true",
					"msg", fmt.Sprintf("detected field %s with %d values", k, len(vals)))
			}
		}
	}

	return detectedFields
}

func parseLine(line string) (map[string][]string, *string) {
	parser := "logfmt"
	logFmtParser := logql_log.NewLogfmtParser(true, false)

	lbls := logql_log.NewBaseLabelsBuilder().ForLabels(labels.EmptyLabels(), 0)
	_, logfmtSuccess := logFmtParser.Process(0, []byte(line), lbls)
	if !logfmtSuccess || lbls.HasErr() {
		parser = "json"
		jsonParser := logql_log.NewJSONParser()
		lbls.Reset()
		_, jsonSuccess := jsonParser.Process(0, []byte(line), lbls)
		if !jsonSuccess || lbls.HasErr() {
			return map[string][]string{}, nil
		}
	}

	parsedLabels := map[string]map[string]struct{}{}
	for _, lbl := range lbls.LabelsResult().Labels() {
		if values, ok := parsedLabels[lbl.Name]; ok {
			values[lbl.Value] = struct{}{}
		} else {
			parsedLabels[lbl.Name] = map[string]struct{}{lbl.Value: {}}
		}
	}

	result := make(map[string][]string, len(parsedLabels))
	for lbl, values := range parsedLabels {
		vals := make([]string, 0, len(values))
		for v := range values {
			vals = append(vals, v)
		}
		result[lbl] = vals
	}

	return result, &parser
}

// streamsForFieldDetection reads the streams from the iterator and returns them sorted.
// If categorizeLabels is true, the stream labels contains just the stream labels and entries inside each stream have their
// structuredMetadata and parsed fields populated with structured metadata labels plus the parsed labels respectively.
// Otherwise, the stream labels are the whole series labels including the stream labels, structured metadata labels and parsed labels.
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
			stream, ok := streams[streamLabels]
			if !ok {
				stream = &logproto.Stream{
					Labels: streamLabels,
				}
				streams[streamLabels] = stream
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

type PatterQuerier interface {
	Patterns(ctx context.Context, req *logproto.QueryPatternsRequest) (*logproto.QueryPatternsResponse, error)
}

func (q *Rf1Querier) WithPatternQuerier(pq querier.PatterQuerier) {
	q.patternQuerier = pq
}

func (q *Rf1Querier) Patterns(ctx context.Context, req *logproto.QueryPatternsRequest) (*logproto.QueryPatternsResponse, error) {
	if q.patternQuerier == nil {
		return nil, httpgrpc.Errorf(http.StatusNotFound, "")
	}
	res, err := q.patternQuerier.Patterns(ctx, req)
	if err != nil {
		return nil, httpgrpc.Errorf(http.StatusBadRequest, "%s", err.Error())
	}

	return res, err
}
