package logql

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	logqlerr "github.com/grafana/loki/v3/pkg/logql/errors"
	"github.com/grafana/loki/v3/pkg/logql/model/metadata"
	"github.com/grafana/loki/v3/pkg/tracing"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/histogram"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	promql_parser "github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/dskit/tenant"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	logqlmodel "github.com/grafana/loki/v3/pkg/logql/model"
	"github.com/grafana/loki/v3/pkg/logql/model/stats"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
	logutil "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/util/server"
	"github.com/grafana/loki/v3/pkg/util/validation"
)

var tracer = otel.Tracer("pkg/logql")

const (
	DefaultBlockedQueryMessage = "blocked by policy"
)

var (
	QueryTime = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "logql",
		Name:      "query_duration_seconds",
		Help:      "LogQL query timings",
		Buckets:   prometheus.DefBuckets,
	}, []string{"query_type"})

	QueriesBlocked = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Name:      "blocked_queries",
		Help:      "Count of queries blocked by per-tenant policy",
	}, []string{"user"})

	lastEntryMinTime = time.Unix(-100, 0)
)

// seriesLimitPartialResultsWarning returns the warning of a query that returns partial results
// because it reached the series limit.
func seriesLimitPartialResultsWarning(maxSeries int) string {
	return fmt.Sprintf("maximum number of series (%d) reached for a single query; returning partial results", maxSeries)
}

type QueryParams interface {
	LogSelector() (syntax.LogSelectorExpr, error)
	GetStart() time.Time
	GetEnd() time.Time
	GetShards() []string
	GetDeletes() []*logproto.Delete
}

// SelectParams specifies parameters passed to data selections.
type SelectLogParams struct {
	*logproto.QueryRequest
}

func (s SelectLogParams) WithStoreChunks(chunkRefGroup *logproto.ChunkRefGroup) SelectLogParams {
	cpy := *s.QueryRequest
	cpy.StoreChunks = chunkRefGroup
	return SelectLogParams{&cpy}
}

// WithTimeRange returns a copy of s covering the given time range. The copy owns its request, so
// the time range of s does not change.
func (s SelectLogParams) WithTimeRange(start, end time.Time) SelectLogParams {
	cpy := *s.QueryRequest
	cpy.Start = start
	cpy.End = end
	return SelectLogParams{&cpy}
}

func (s SelectLogParams) String() string {
	if s.QueryRequest != nil {
		return fmt.Sprintf("selector=%s, direction=%s, start=%s, end=%s, limit=%d, shards=%s",
			s.Selector, logproto.Direction_name[int32(s.Direction)], s.Start, s.End, s.Limit, strings.Join(s.Shards, ","))
	}
	return ""
}

// LogSelector returns the LogSelectorExpr from the SelectParams.
// The `LogSelectorExpr` can then returns all matchers and filters to use for that request.
func (s SelectLogParams) LogSelector() (syntax.LogSelectorExpr, error) {
	if s.Plan == nil {
		return nil, errors.New("query plan is empty")
	}
	expr, ok := s.Plan.AST.(syntax.LogSelectorExpr)
	if !ok {
		return nil, errors.New("only log selector is supported")
	}
	return expr, nil
}

type SelectSampleParams struct {
	*logproto.SampleQueryRequest
}

func (s SelectSampleParams) WithStoreChunks(chunkRefGroup *logproto.ChunkRefGroup) SelectSampleParams {
	cpy := *s.SampleQueryRequest
	cpy.StoreChunks = chunkRefGroup
	return SelectSampleParams{&cpy}
}

// WithTimeRange returns a copy of s covering the given time range. The copy owns its request, so
// the time range of s does not change.
func (s SelectSampleParams) WithTimeRange(start, end time.Time) SelectSampleParams {
	cpy := *s.SampleQueryRequest
	cpy.Start = start
	cpy.End = end
	return SelectSampleParams{&cpy}
}

// Expr returns the SampleExpr from the SelectSampleParams.
// The `LogSelectorExpr` can then returns all matchers and filters to use for that request.
func (s SelectSampleParams) Expr() (syntax.SampleExpr, error) {
	if s.Plan == nil {
		return nil, errors.New("query plan is empty")
	}
	expr, ok := s.Plan.AST.(syntax.SampleExpr)
	if !ok {
		return nil, errors.New("only sample expression supported")
	}
	return expr, nil
}

// LogSelector returns the LogSelectorExpr from the SelectParams.
// The `LogSelectorExpr` can then returns all matchers and filters to use for that request.
func (s SelectSampleParams) LogSelector() (syntax.LogSelectorExpr, error) {
	expr, err := s.Expr()
	if err != nil {
		return nil, err
	}
	return expr.Selector()
}

// Querier allows a LogQL expression to fetch an EntryIterator for a
// set of matchers and filters
type Querier interface {
	SelectLogs(context.Context, SelectLogParams) (iter.EntryIterator, error)
	SelectSamples(context.Context, SelectSampleParams) (iter.SampleIterator, error)
}

type Engine interface {
	Query(Params) Query
}

// EngineOpts is the list of options to use with the LogQL query engine.
type EngineOpts struct {
	// MaxLookBackPeriod is the maximum amount of time to look back for log lines.
	// only used for instant log queries.
	MaxLookBackPeriod time.Duration `yaml:"max_look_back_period"`

	// LogExecutingQuery will control if we log the query when Exec is called.
	LogExecutingQuery bool `yaml:"-"`

	// MaxCountMinSketchHeapSize is the maximum number of labels the heap for a topk query using a count min sketch
	// can track. This impacts the memory usage and accuracy of a sharded probabilistic topk query.
	MaxCountMinSketchHeapSize int `yaml:"max_count_min_sketch_heap_size"`

	MaxQuerySeriesCutoff bool `yaml:"max_query_series_cutoff" category:"experimental"`

	// EnableHistograms enables the histogram functions: histogram_over_time, and the functions that read it.
	// A query that uses one fails while this is off. The functions are in development, so they are off by default.
	EnableHistograms bool `yaml:"enable_histograms" category:"experimental"`
}

func (opts *EngineOpts) RegisterFlags(f *flag.FlagSet) {
	opts.RegisterFlagsWithPrefix("", f)
}

func (opts *EngineOpts) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.DurationVar(&opts.MaxLookBackPeriod, prefix+"max-lookback-period", 30*time.Second, "The maximum amount of time to look back for log lines. Used only for instant log queries.")
	f.IntVar(&opts.MaxCountMinSketchHeapSize, prefix+"max-count-min-sketch-heap-size", 10_000, "The maximum number of labels the heap of a topk query using a count min sketch can track.")
	f.BoolVar(&opts.MaxQuerySeriesCutoff, prefix+"max-query-series-cutoff", false, "Experimental: Fail a query on max_query_series as soon as a range iterator or vector aggregation holds more series than the limit, where the query's output cannot be smaller. Logs Drilldown requests are exempt.")
	f.BoolVar(&opts.EnableHistograms, prefix+"enable-histograms", false, "Experimental: Enable the histogram functions, histogram_over_time and the functions that read its result, such as histogram_quantile. The functions are in development, so a query that uses one fails while this is disabled. Enable it on the queriers, on the query frontends, and on the rulers that evaluate rules themselves, because each of them refuses a query that uses histograms without it.")

	// Log executing query by default
	opts.LogExecutingQuery = true
}

// queryOpts is the subset of EngineOpts that a query reads at execution time. The remaining
// EngineOpts fields only matter when an engine or its evaluator is constructed.
type queryOpts struct {
	// logExecutingQuery controls if the query is logged when Exec is called.
	logExecutingQuery bool
	// maxQuerySeriesCutoff controls if max_query_series is enforced before the output is built.
	maxQuerySeriesCutoff bool
	// histogramsEnabled controls if the histogram functions are allowed. A query that uses them fails
	// without it.
	histogramsEnabled bool
}

// newQueryOpts returns the queryOpts for a query run by an engine configured with opts.
func newQueryOpts(opts EngineOpts) queryOpts {
	return queryOpts{
		logExecutingQuery:    opts.LogExecutingQuery,
		maxQuerySeriesCutoff: opts.MaxQuerySeriesCutoff,
		histogramsEnabled:    opts.EnableHistograms,
	}
}

// errHistogramsDisabled is returned for a query that uses histograms when they are not enabled. It wraps
// ErrParse so that it is served as a client error: a server error is retried by the query frontend and
// counts against the error rate.
var errHistogramsDisabled = fmt.Errorf("%w: histogram functions are experimental and not enabled, "+
	"set -querier.engine.enable-histograms to enable them", logqlerr.ErrParse)

// checkNoHistograms fails when a sample of vec holds a histogram. The result of a query is a vector or
// a matrix of floats, and a histogram that reached it would be dropped, which leaves a series with the
// value zero. The parser does not allow a query that returns a histogram, so this is a second check.
//
// Like the other guards of the histograms, it wraps ErrParse: what is wrong is the query, and it is the same
// on every retry, so it is a client error and not one that the query frontend retries.
func checkNoHistograms(vec promql.Vector) error {
	for _, s := range vec {
		if s.H != nil {
			return fmt.Errorf("%w: a query cannot return a histogram, read it with a histogram_* function", logqlerr.ErrParse)
		}
	}
	return nil
}

func (opts *EngineOpts) applyDefault() {
	if opts.MaxLookBackPeriod == 0 {
		opts.MaxLookBackPeriod = 30 * time.Second
	}
}

// QueryEngine is the LogQL engine.
type QueryEngine struct {
	logger           log.Logger
	evaluatorFactory EvaluatorFactory
	limits           Limits
	opts             EngineOpts
}

// NewEngine creates a new LogQL [QueryEngine].
func NewEngine(opts EngineOpts, q Querier, l Limits, logger log.Logger) *QueryEngine {
	opts.applyDefault()
	if logger == nil {
		logger = log.NewNopLogger()
	}
	return &QueryEngine{
		logger:           logger,
		evaluatorFactory: NewDefaultEvaluator(q, opts.MaxLookBackPeriod, opts.MaxCountMinSketchHeapSize, l),
		limits:           l,
		opts:             opts,
	}
}

// Query creates a new LogQL query. Instant/Range type is derived from the parameters.
func (qe *QueryEngine) Query(params Params) Query {
	return &query{
		logger:    qe.logger,
		params:    params,
		evaluator: qe.evaluatorFactory,
		record:    true,
		opts:      newQueryOpts(qe.opts),
		limits:    qe.limits,
	}
}

// Query is a LogQL query to be executed.
type Query interface {
	// Exec processes the query.
	Exec(ctx context.Context) (logqlmodel.Result, error)
}

type query struct {
	logger    log.Logger
	params    Params
	limits    Limits
	evaluator EvaluatorFactory
	record    bool
	opts      queryOpts
}

func (q *query) resultLength(res promql_parser.Value) int {
	switch r := res.(type) {
	case promql.Vector:
		return len(r)
	case promql.Matrix:
		return r.TotalSamples()
	case logqlmodel.Streams:
		return int(r.Lines())
	case ProbabilisticQuantileMatrix:
		return len(r)
	default:
		// for `scalar` or `string` or any other return type, we just return `0` as result length.
		return 0
	}
}

// Exec Implements `Query`. It handles instrumentation & defers to Eval.
func (q *query) Exec(ctx context.Context) (logqlmodel.Result, error) {
	ctx, sp := tracer.Start(ctx, "query.Exec")
	defer sp.End()

	sp.SetAttributes(
		attribute.String("type", string(GetRangeType(q.params))),
		attribute.String("query", q.params.QueryString()),
		attribute.String("start", q.params.Start().String()),
		attribute.String("end", q.params.End().String()),
		attribute.String("step", q.params.Step().String()),
		attribute.String("length", q.params.End().Sub(q.params.Start()).String()),
	)

	if q.opts.logExecutingQuery {
		queryHash := util.HashedQuery(q.params.QueryString())

		logValues := []interface{}{
			"msg", "executing query",
			"query", q.params.QueryString(),
			"query_hash", queryHash,
		}
		tags := httpreq.ExtractQueryTagsFromContext(ctx)
		tagValues := httpreq.TagsToKeyValues(tags)
		if GetRangeType(q.params) == InstantType {
			logValues = append(logValues, "type", "instant")
		} else {
			logValues = append(logValues, "type", "range", "length", q.params.End().Sub(q.params.Start()), "step", q.params.Step())
		}
		logValues = append(logValues, tagValues...)
		level.Info(logutil.WithContext(ctx, q.logger)).Log(logValues...)
	}

	rangeType := GetRangeType(q.params)
	timer := prometheus.NewTimer(QueryTime.WithLabelValues(string(rangeType)))
	defer timer.ObserveDuration()

	// records query statistics
	start := time.Now()
	statsCtx, ctx := stats.NewContext(ctx)
	metadataCtx, ctx := metadata.NewContext(ctx)

	data, err := q.Eval(ctx)

	queueTime, _ := ctx.Value(httpreq.QueryQueueTimeHTTPHeader).(time.Duration)

	execTime := time.Since(start)
	statsCtx.AddQuerierExecTime(execTime)
	statResult := statsCtx.Result(execTime, queueTime, q.resultLength(data))
	sp.SetAttributes(tracing.KeyValuesToOTelAttributes(statResult.KVList())...)

	status, _ := server.ClientHTTPStatusAndError(err)

	if q.record {
		RecordRangeAndInstantQueryMetrics(ctx, q.logger, q.params, strconv.Itoa(status), statResult, data, err)
	}

	return logqlmodel.Result{
		Data:       data,
		Statistics: statResult,
		Headers:    metadataCtx.Headers(),
		Warnings:   metadataCtx.Warnings(),
	}, err
}

func (q *query) Eval(ctx context.Context) (promql_parser.Value, error) {
	tenants, _ := tenant.TenantIDs(ctx)
	timeoutCapture := func(id string) time.Duration { return q.limits.QueryTimeout(ctx, id) }
	queryTimeout := validation.SmallestPositiveNonZeroDurationPerTenant(tenants, timeoutCapture)
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	if q.checkBlocked(ctx, tenants) {
		return nil, logqlerr.ErrBlocked
	}

	switch e := q.params.GetExpression().(type) {
	case syntax.SampleExpr:
		value, err := q.evalSample(ctx, e)
		return value, err

	case syntax.LogSelectorExpr:
		itr, err := q.evaluator.NewIterator(ctx, e, q.params)
		if err != nil {
			return nil, err
		}

		encodingFlags := httpreq.ExtractEncodingFlagsFromCtx(ctx)
		if encodingFlags.Has(httpreq.FlagCategorizeLabels) {
			itr = iter.NewCategorizeLabelsIterator(itr)
		}

		defer util.LogErrorWithContext(ctx, "closing iterator", itr.Close)
		streams, err := readStreams(itr, q.params.Limit(), q.params.Direction(), q.params.Interval())
		return streams, err
	default:
		return nil, fmt.Errorf("unexpected type (%T): cannot evaluate", e)
	}
}

func (q *query) checkBlocked(ctx context.Context, tenants []string) bool {
	blocker := newQueryBlocker(ctx, q)

	for _, tenant := range tenants {
		if blocker.isBlocked(ctx, tenant) {
			QueriesBlocked.WithLabelValues(tenant).Inc()
			return true
		}
	}

	return false
}

// evalSample evaluate a sampleExpr
func (q *query) evalSample(ctx context.Context, expr syntax.SampleExpr) (promql_parser.Value, error) {
	if lit, ok := expr.(*syntax.LiteralExpr); ok {
		return q.evalLiteral(ctx, lit)
	}
	if vec, ok := expr.(*syntax.VectorExpr); ok {
		return q.evalVector(ctx, vec)
	}

	if !q.opts.histogramsEnabled && syntax.UsesHistograms(expr) {
		return nil, errHistogramsDisabled
	}

	tenantIDs, err := tenant.TenantIDs(ctx)
	if err != nil {
		return nil, err
	}

	maxIntervalCapture := func(id string) time.Duration { return q.limits.MaxQueryRange(ctx, id) }
	maxQueryInterval := validation.SmallestPositiveNonZeroDurationPerTenant(tenantIDs, maxIntervalCapture)
	if maxQueryInterval != 0 {
		err = q.checkIntervalLimit(expr, maxQueryInterval)
		if err != nil {
			return nil, err
		}
	}

	expr, err = optimizeSampleExpr(expr)
	if err != nil {
		return nil, err
	}

	// Only count the real query evaluation, not the *DownstreamEvaluator the query-frontend uses
	// to dispatch a sharded or astmapper-rewritten query. It never reads a sample itself.
	//
	// A query counts as stream-first when any part of it runs in stream-first order.
	if _, ok := q.evaluator.(*DefaultEvaluator); ok {
		if sampleOrderForQuery(ctx, expr, q.limits) == logproto.SAMPLE_ORDER_BY_STREAM {
			stats.FromContext(ctx).AddStreamFirstQueries(1)
		} else {
			stats.FromContext(ctx).AddTimestampFirstQueries(1)
		}
	}

	maxSeries := seriesLimitFor(tenantIDs, q.limits)

	stepEvaluator, err := q.evaluator.NewStepEvaluator(ctx, q.evaluator, expr, q.params, RootContext(expr, q.maxQuerySeriesCutoffLimit(ctx, maxSeries)))
	if err != nil {
		return nil, err
	}
	defer util.LogErrorWithContext(ctx, "closing SampleExpr", stepEvaluator.Close)

	next, _, r := stepEvaluator.Next()
	if stepEvaluator.Error() != nil {
		return nil, stepEvaluator.Error()
	}

	if next && r != nil {
		switch vec := r.(type) {
		case SampleVector:
			mfl := false
			if rae, ok := expr.(*syntax.RangeAggregationExpr); ok && (rae.Operation == syntax.OpRangeTypeFirstWithTimestamp || rae.Operation == syntax.OpRangeTypeLastWithTimestamp) {
				mfl = true
			}
			return q.JoinSampleVector(ctx, next, vec, stepEvaluator, maxSeries, mfl)
		case ProbabilisticQuantileVector:
			return JoinQuantileSketchVector(next, vec, stepEvaluator, q.params)
		case CountMinSketchVector:
			return JoinCountMinSketchVector(next, vec, stepEvaluator, q.params)
		case HeapCountMinSketchVector:
			return JoinCountMinSketchVector(next, vec.CountMinSketchVector, stepEvaluator, q.params)
		case CountDistinctSketchVector:
			return JoinCountDistinctSketchVector(next, vec, stepEvaluator, q.params)
		default:
			return nil, fmt.Errorf("unsupported result type: %T", r)
		}
	}
	return nil, errors.New("unexpected empty result")
}

func vectorsToSeries(vec promql.Vector, sm map[uint64]promql.Series) {
	vectorsToSeriesWithLimit(vec, sm, 0) // 0 means no limit
}

func vectorsToSeriesWithLimit(vec promql.Vector, sm map[uint64]promql.Series, maxSeries int) bool {
	limitExceeded := false
	for _, p := range vec {
		var (
			series promql.Series
			hash   = labels.StableHash(p.Metric)
			ok     bool
		)

		series, ok = sm[hash]

		// create a new series if under the limit
		if !ok && !limitExceeded {
			// Check if adding a new series would exceed the limit
			if maxSeries > 0 && len(sm) >= maxSeries {
				// We've reached the series limit, skip adding new series
				limitExceeded = true
				continue
			}
			series = promql.Series{
				Metric: p.Metric,
				Floats: make([]promql.FPoint, 0, 1),
			}
			sm[hash] = series
		}
		series.Floats = append(series.Floats, promql.FPoint{
			T: p.T,
			F: p.F,
		})
		sm[hash] = series
	}
	return limitExceeded
}

// maxQuerySeriesCutoffLimit returns the limit that preserving expressions may fail on before the
// query's output is built, or zero when the early cut-off is off. Logs Drilldown requests are
// exempt, so they still get partial results and a warning from JoinSampleVector.
func (q *query) maxQuerySeriesCutoffLimit(ctx context.Context, maxSeries int) int {
	if !q.opts.maxQuerySeriesCutoff || httpreq.IsLogsDrilldownRequest(ctx) {
		return 0
	}
	// vector() steps in whole milliseconds and the range iterators in nanoseconds, so below a
	// millisecond a range leg of a binary operation can run steps past the end of a vector() leg.
	// The binary operation discards those steps, so they are not a lower bound on the output.
	if step := q.params.Step(); step > 0 && step < time.Millisecond {
		return 0
	}
	return maxSeries
}

func (q *query) JoinSampleVector(ctx context.Context, next bool, r StepResult, stepEvaluator StepEvaluator, maxSeries int, mergeFirstLast bool) (promql_parser.Value, error) {
	vec := promql.Vector{}
	if next {
		vec = r.SampleVector()
		if err := checkNoHistograms(vec); err != nil {
			return nil, err
		}
	}
	seriesIndex := map[uint64]promql.Series{}

	// fail fast for the first step or instant query
	if len(vec) > maxSeries {
		if httpreq.IsLogsDrilldownRequest(ctx) {
			// For Logs Drilldown requests, return partial results with warning
			vec = vec[:maxSeries]
			metadata.FromContext(ctx).AddWarning(seriesLimitPartialResultsWarning(maxSeries))
			// Since we've already reached the series limit, skip processing additional steps and add the initial vector to seriesIndex
			next = false
			vectorsToSeries(vec, seriesIndex)
		} else {
			return nil, logqlerr.NewSeriesLimitError(maxSeries)
		}
	}

	if GetRangeType(q.params) == InstantType {
		// an instant query sharded first/last_over_time can return a single vector
		if mergeFirstLast {
			vectorsToSeries(vec, seriesIndex)
			series := make([]promql.Series, 0, len(seriesIndex))
			for _, s := range seriesIndex {
				series = append(series, s)
			}
			result := promql.Matrix(series)
			sort.Sort(result)
			return result, stepEvaluator.Error()
		}

		sortByValue, err := Sortable(q.params)
		if err != nil {
			return nil, fmt.Errorf("fail to check Sortable, logql: %s ,err: %s", q.params.QueryString(), err)
		}
		if !sortByValue {
			sort.Slice(vec, func(i, j int) bool { return labels.Compare(vec[i].Metric, vec[j].Metric) < 0 })
		}
		return vec, nil
	}

	for next {
		vec = r.SampleVector()

		// vectorsToSeriesWithLimit stops accumulating once maxSeries distinct series
		// exist and reports whether the limit was hit, so seriesIndex never grows past
		// maxSeries regardless of request type.
		if vectorsToSeriesWithLimit(vec, seriesIndex, maxSeries) {
			if !httpreq.IsLogsDrilldownRequest(ctx) {
				return nil, logqlerr.NewSeriesLimitError(maxSeries)
			}
			// Logs Drilldown returns partial results with a warning instead of failing.
			metadata.FromContext(ctx).AddWarning(seriesLimitPartialResultsWarning(maxSeries))
			break
		}

		next, _, r = stepEvaluator.Next()
		if stepEvaluator.Error() != nil {
			return nil, stepEvaluator.Error()
		}
		// The first vector was checked above, so this is each of the other ones, once.
		if next {
			if err := checkNoHistograms(r.SampleVector()); err != nil {
				return nil, err
			}
		}
	}

	series := make([]promql.Series, 0, len(seriesIndex))
	for _, s := range seriesIndex {
		series = append(series, s)
	}
	result := promql.Matrix(series)
	sort.Sort(result)

	return result, stepEvaluator.Error()
}

func (q *query) checkIntervalLimit(expr syntax.SampleExpr, limit time.Duration) error {
	var err error
	expr.Walk(func(e syntax.Expr) bool {
		switch e := e.(type) {
		case *syntax.LogRangeExpr:
			if e.Interval > limit {
				err = fmt.Errorf("%w: [%s] > [%s]", logqlerr.ErrIntervalLimit, model.Duration(e.Interval), model.Duration(limit))
			}
		}
		return true
	})
	return err
}

func (q *query) evalLiteral(_ context.Context, expr *syntax.LiteralExpr) (promql_parser.Value, error) {
	value, err := expr.Value()
	if err != nil {
		return nil, err
	}
	s := promql.Scalar{
		T: q.params.Start().UnixNano() / int64(time.Millisecond),
		V: value,
	}

	if GetRangeType(q.params) == InstantType {
		return s, nil
	}

	return PopulateMatrixFromScalar(s, q.params), nil
}

func (q *query) evalVector(_ context.Context, expr *syntax.VectorExpr) (promql_parser.Value, error) {
	value, err := expr.Value()
	if err != nil {
		return nil, err
	}
	s := promql.Scalar{
		T: q.params.Start().UnixNano() / int64(time.Millisecond),
		V: value,
	}

	if GetRangeType(q.params) == InstantType {
		return promql.Vector{promql.Sample{
			T:      q.params.Start().UnixMilli(),
			F:      value,
			Metric: labels.Labels{},
		}}, nil
	}

	return PopulateMatrixFromScalar(s, q.params), nil
}

func PopulateMatrixFromScalar(data promql.Scalar, params Params) promql.Matrix {
	var (
		start  = params.Start()
		end    = params.End()
		step   = params.Step()
		series = promql.Series{
			Floats: make(
				[]promql.FPoint,
				0,
				// allocate enough space for all needed entries
				int(end.Sub(start)/step)+1,
			),
		}
	)

	for ts := start; !ts.After(end); ts = ts.Add(step) {
		series.Floats = append(series.Floats, promql.FPoint{
			T: ts.UnixNano() / int64(time.Millisecond),
			F: data.V,
		})
	}
	return promql.Matrix{series}
}

// readStreams reads the streams from the iterator and returns them sorted.
// If categorizeLabels is true, the stream labels contains just the stream labels and entries inside each stream have their
// structuredMetadata and parsed fields populated with structured metadata labels plus the parsed labels respectively.
// Otherwise, the stream labels are the whole series labels including the stream labels, structured metadata labels and parsed labels.
func readStreams(i iter.EntryIterator, size uint32, dir logproto.Direction, interval time.Duration) (logqlmodel.Streams, error) {
	streams := map[string]*logproto.Stream{}
	respSize := uint32(0)
	// lastEntry should be a really old time so that the first comparison is always true, we use a negative
	// value here because many unit tests start at time.Unix(0,0)
	lastEntry := lastEntryMinTime
	for respSize < size && i.Next() {
		streamLabels, entry := i.Labels(), i.At()

		forwardShouldOutput := dir == logproto.FORWARD &&
			(entry.Timestamp.Equal(lastEntry.Add(interval)) || entry.Timestamp.After(lastEntry.Add(interval)))
		backwardShouldOutput := dir == logproto.BACKWARD &&
			(entry.Timestamp.Equal(lastEntry.Add(-interval)) || entry.Timestamp.Before(lastEntry.Add(-interval)))

		// If step == 0 output every line.
		// If lastEntry.Unix < 0 this is the first pass through the loop and we should output the line.
		// Then check to see if the entry is equal to, or past a forward or reverse step
		if interval == 0 || lastEntry.Unix() < 0 || forwardShouldOutput || backwardShouldOutput {
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

type groupedAggregation struct {
	labels labels.Labels
	value  float64
	// mean is the running mean of OpTypeStddev and OpTypeStdvar.
	mean float64
	// sum is the accumulation state of OpTypeSum. avg is the accumulation state of OpTypeAvg.
	// Neither operation accumulates in value, mean or groupCount.
	sum kahanSum
	avg kahanAvg
	// hasFloat reports whether OpTypeSum added a float to the group. histogram is the sum of the
	// histograms that it added instead, and histogramC the Kahan compensation of that sum. A group
	// sums floats or histograms, and not both.
	hasFloat    bool
	histogram   *histogram.FloatHistogram
	histogramC  *histogram.FloatHistogram
	groupCount  int
	heap        vectorByValueHeap
	reverseHeap vectorByReverseValueHeap
}

// addToSum adds a sample to the sum of the group. A group sums floats or histograms. Mixing them is an
// error: there is no sum of a float and a histogram.
func (g *groupedAggregation) addToSum(s promql.Sample) error {
	if s.H == nil {
		if g.histogram != nil {
			return errMixedHistogramsAndFloats
		}
		g.hasFloat = true
		g.sum.addValue(s.F)
		return nil
	}

	if g.hasFloat {
		return errMixedHistogramsAndFloats
	}
	if g.histogram == nil {
		// The histogram of the sample is not changed, because the sum is, and a histogram may be
		// shared with whoever produced it.
		g.histogram = s.H.Copy()
		return nil
	}

	var err error
	g.histogramC, _, _, err = g.histogram.KahanAdd(s.H, g.histogramC)
	return err
}

// histogramSum returns the sum of the histograms of the group, with the rounding that the Kahan
// summation kept aside added back in.
//
// It fails when the sum has more buckets than a histogram may have. The buckets of the sum are those of
// all the histograms that were added, so if the sum of a part of them is over the limit, the sum of all
// of them is too. A query therefore fails in the same cases whether it is sharded or not.
func (g *groupedAggregation) histogramSum() (*histogram.FloatHistogram, error) {
	h := g.histogram
	if g.histogramC != nil {
		var err error
		h, _, _, err = h.Add(g.histogramC)
		if err != nil {
			return nil, err
		}
	}
	h.Compact(0)
	if err := checkHistogramBuckets(h); err != nil {
		return nil, err
	}
	return h, nil
}

// errMixedHistogramsAndFloats wraps ErrParse, as the other guards of the histograms do, see checkNoHistograms.
var errMixedHistogramsAndFloats = fmt.Errorf("%w: cannot sum histograms and floats together", logqlerr.ErrParse)
