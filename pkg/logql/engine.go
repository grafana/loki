package logql

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/opentracing/opentracing-go"

	"github.com/grafana/loki/v3/pkg/logqlmodel/metadata"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	promql_parser "github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/dskit/tenant"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
	logutil "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/util/server"
	"github.com/grafana/loki/v3/pkg/util/validation"
)

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

type QueryParams interface {
	LogSelector() (syntax.LogSelectorExpr, error)
	GetStart() time.Time
	GetEnd() time.Time
	GetShards() []string
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
	if s.QueryRequest.Plan == nil {
		return nil, errors.New("query plan is empty")
	}
	expr, ok := s.QueryRequest.Plan.AST.(syntax.LogSelectorExpr)
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

// Expr returns the SampleExpr from the SelectSampleParams.
// The `LogSelectorExpr` can then returns all matchers and filters to use for that request.
func (s SelectSampleParams) Expr() (syntax.SampleExpr, error) {
	if s.SampleQueryRequest.Plan == nil {
		return nil, errors.New("query plan is empty")
	}
	expr, ok := s.SampleQueryRequest.Plan.AST.(syntax.SampleExpr)
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

// EngineOpts is the list of options to use with the LogQL query engine.
type EngineOpts struct {
	// MaxLookBackPeriod is the maximum amount of time to look back for log lines.
	// only used for instant log queries.
	MaxLookBackPeriod time.Duration `yaml:"max_look_back_period"`

	// LogExecutingQuery will control if we log the query when Exec is called.
	LogExecutingQuery bool `yaml:"-"`
}

func (opts *EngineOpts) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.DurationVar(&opts.MaxLookBackPeriod, prefix+".engine.max-lookback-period", 30*time.Second, "The maximum amount of time to look back for log lines. Used only for instant log queries.")
	// Log executing query by default
	opts.LogExecutingQuery = true
}

func (opts *EngineOpts) applyDefault() {
	if opts.MaxLookBackPeriod == 0 {
		opts.MaxLookBackPeriod = 30 * time.Second
	}
}

// Engine is the LogQL engine.
type Engine struct {
	logger           log.Logger
	evaluatorFactory EvaluatorFactory
	limits           Limits
	opts             EngineOpts
}

// NewEngine creates a new LogQL Engine.
func NewEngine(opts EngineOpts, q Querier, l Limits, logger log.Logger) *Engine {
	opts.applyDefault()
	if logger == nil {
		logger = log.NewNopLogger()
	}
	return &Engine{
		logger:           logger,
		evaluatorFactory: NewDefaultEvaluator(q, opts.MaxLookBackPeriod),
		limits:           l,
		opts:             opts,
	}
}

// Query creates a new LogQL query. Instant/Range type is derived from the parameters.
func (ng *Engine) Query(params Params) Query {
	return &query{
		logger:       ng.logger,
		params:       params,
		evaluator:    ng.evaluatorFactory,
		record:       true,
		logExecQuery: ng.opts.LogExecutingQuery,
		limits:       ng.limits,
	}
}

// Query is a LogQL query to be executed.
type Query interface {
	// Exec processes the query.
	Exec(ctx context.Context) (logqlmodel.Result, error)
}

type query struct {
	logger       log.Logger
	params       Params
	limits       Limits
	evaluator    EvaluatorFactory
	record       bool
	logExecQuery bool
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
	sp, ctx := opentracing.StartSpanFromContext(ctx, "query.Exec")
	defer sp.Finish()

	sp.LogKV(
		"type", GetRangeType(q.params),
		"query", q.params.QueryString(),
		"start", q.params.Start(),
		"end", q.params.End(),
		"step", q.params.Step(),
		"length", q.params.End().Sub(q.params.Start()),
	)

	if q.logExecQuery {
		queryHash := util.HashedQuery(q.params.QueryString())
		if GetRangeType(q.params) == InstantType {
			level.Info(logutil.WithContext(ctx, q.logger)).Log("msg", "executing query", "type", "instant", "query", q.params.QueryString(), "query_hash", queryHash)
		} else {
			level.Info(logutil.WithContext(ctx, q.logger)).Log("msg", "executing query", "type", "range", "query", q.params.QueryString(), "length", q.params.End().Sub(q.params.Start()), "step", q.params.Step(), "query_hash", queryHash)
		}
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

	statResult := statsCtx.Result(time.Since(start), queueTime, q.resultLength(data))
	sp.LogKV(statResult.KVList()...)

	status, _ := server.ClientHTTPStatusAndError(err)

	if q.record {
		RecordRangeAndInstantQueryMetrics(ctx, q.logger, q.params, strconv.Itoa(status), statResult, data)
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
		return nil, logqlmodel.ErrBlocked
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

	stepEvaluator, err := q.evaluator.NewStepEvaluator(ctx, q.evaluator, expr, q.params)
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
			maxSeriesCapture := func(id string) int { return q.limits.MaxQuerySeries(ctx, id) }
			maxSeries := validation.SmallestPositiveIntPerTenant(tenantIDs, maxSeriesCapture)
			return q.JoinSampleVector(next, vec, stepEvaluator, maxSeries)
		case ProbabilisticQuantileVector:
			return MergeQuantileSketchVector(next, vec, stepEvaluator, q.params)
		default:
			return nil, fmt.Errorf("unsupported result type: %T", r)
		}
	}
	return nil, nil
}

func (q *query) JoinSampleVector(next bool, r StepResult, stepEvaluator StepEvaluator, maxSeries int) (promql_parser.Value, error) {

	seriesIndex := map[uint64]*promql.Series{}

	vec := promql.Vector{}
	if next {
		vec = r.SampleVector()
	}

	// fail fast for the first step or instant query
	if len(vec) > maxSeries {
		return nil, logqlmodel.NewSeriesLimitError(maxSeries)
	}

	if GetRangeType(q.params) == InstantType {
		sortByValue, err := Sortable(q.params)
		if err != nil {
			return nil, fmt.Errorf("fail to check Sortable, logql: %s ,err: %s", q.params.QueryString(), err)
		}
		if !sortByValue {
			sort.Slice(vec, func(i, j int) bool { return labels.Compare(vec[i].Metric, vec[j].Metric) < 0 })
		}
		return vec, nil
	}

	stepCount := int(math.Ceil(float64(q.params.End().Sub(q.params.Start()).Nanoseconds()) / float64(q.params.Step().Nanoseconds())))
	if stepCount <= 0 {
		stepCount = 1
	}

	for next {
		vec = r.SampleVector()
		for _, p := range vec {
			var (
				series *promql.Series
				hash   = p.Metric.Hash()
				ok     bool
			)

			series, ok = seriesIndex[hash]
			if !ok {
				series = &promql.Series{
					Metric: p.Metric,
					Floats: make([]promql.FPoint, 0, stepCount),
				}
				seriesIndex[hash] = series
			}
			series.Floats = append(series.Floats, promql.FPoint{
				T: p.T,
				F: p.F,
			})
		}
		// as we slowly build the full query for each steps, make sure we don't go over the limit of unique series.
		if len(seriesIndex) > maxSeries {
			return nil, logqlmodel.NewSeriesLimitError(maxSeries)
		}
		next, _, r = stepEvaluator.Next()
		if stepEvaluator.Error() != nil {
			return nil, stepEvaluator.Error()
		}
	}

	series := make([]promql.Series, 0, len(seriesIndex))
	for _, s := range seriesIndex {
		series = append(series, *s)
	}
	result := promql.Matrix(series)
	sort.Sort(result)

	return result, stepEvaluator.Error()
}

func (q *query) checkIntervalLimit(expr syntax.SampleExpr, limit time.Duration) error {
	var err error
	expr.Walk(func(e syntax.Expr) {
		switch e := e.(type) {
		case *syntax.RangeAggregationExpr:
			if e.Left == nil || e.Left.Interval <= limit {
				return
			}
			err = fmt.Errorf("%w: [%s] > [%s]", logqlmodel.ErrIntervalLimit, model.Duration(e.Left.Interval), model.Duration(limit))
		}
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
		streamLabels, entry := i.Labels(), i.Entry()

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
			lastEntry = i.Entry().Timestamp
			respSize++
		}
	}

	result := make(logqlmodel.Streams, 0, len(streams))
	for _, stream := range streams {
		result = append(result, *stream)
	}
	sort.Sort(result)
	return result, i.Error()
}

type groupedAggregation struct {
	labels      labels.Labels
	value       float64
	mean        float64
	groupCount  int
	heap        vectorByValueHeap
	reverseHeap vectorByReverseValueHeap
}
