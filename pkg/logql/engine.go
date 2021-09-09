package logql

import (
	"context"
	"errors"
	"flag"
	"math"
	"sort"
	"time"

	"github.com/cortexproject/cortex/pkg/util/spanlogger"
	"github.com/go-kit/kit/log/level"
	"github.com/grafana/dskit/tenant"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"
	promql_parser "github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logqlmodel"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/util"
)

var (
	queryTime = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "logql",
		Name:      "query_duration_seconds",
		Help:      "LogQL query timings",
		Buckets:   prometheus.DefBuckets,
	}, []string{"query_type"})
	lastEntryMinTime = time.Unix(-100, 0)
)

// EngineOpts is the list of options to use with the LogQL query engine.
type EngineOpts struct {
	// Timeout for queries execution
	Timeout time.Duration `yaml:"timeout"`
	// MaxLookBackPeriod is the maximum amount of time to look back for log lines.
	// only used for instant log queries.
	MaxLookBackPeriod time.Duration `yaml:"max_look_back_period"`
}

func (opts *EngineOpts) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.DurationVar(&opts.Timeout, prefix+".engine.timeout", 5*time.Minute, "Timeout for query execution.")
	f.DurationVar(&opts.MaxLookBackPeriod, prefix+".engine.max-lookback-period", 30*time.Second, "The maximum amount of time to look back for log lines. Used only for instant log queries.")
}

func (opts *EngineOpts) applyDefault() {
	if opts.Timeout == 0 {
		opts.Timeout = 5 * time.Minute
	}
	if opts.MaxLookBackPeriod == 0 {
		opts.MaxLookBackPeriod = 30 * time.Second
	}
}

// Engine is the LogQL engine.
type Engine struct {
	timeout   time.Duration
	evaluator Evaluator
	limits    Limits
}

// NewEngine creates a new LogQL Engine.
func NewEngine(opts EngineOpts, q Querier, l Limits) *Engine {
	opts.applyDefault()
	return &Engine{
		timeout:   opts.Timeout,
		evaluator: NewDefaultEvaluator(q, opts.MaxLookBackPeriod),
		limits:    l,
	}
}

// Query creates a new LogQL query. Instant/Range type is derived from the parameters.
func (ng *Engine) Query(params Params) Query {
	return &query{
		timeout:   ng.timeout,
		params:    params,
		evaluator: ng.evaluator,
		parse: func(_ context.Context, query string) (Expr, error) {
			return ParseExpr(query)
		},
		record: true,
		limits: ng.limits,
	}
}

// Query is a LogQL query to be executed.
type Query interface {
	// Exec processes the query.
	Exec(ctx context.Context) (logqlmodel.Result, error)
}

type query struct {
	timeout   time.Duration
	params    Params
	parse     func(context.Context, string) (Expr, error)
	limits    Limits
	evaluator Evaluator
	record    bool
}

// Exec Implements `Query`. It handles instrumentation & defers to Eval.
func (q *query) Exec(ctx context.Context) (logqlmodel.Result, error) {
	log, ctx := spanlogger.New(ctx, "query.Exec")
	defer log.Finish()

	rangeType := GetRangeType(q.params)
	timer := prometheus.NewTimer(queryTime.WithLabelValues(string(rangeType)))
	defer timer.ObserveDuration()

	// records query statistics
	var statResult stats.Result
	start := time.Now()
	ctx = stats.NewContext(ctx)

	data, err := q.Eval(ctx)

	statResult = stats.Snapshot(ctx, time.Since(start))
	statResult.Log(level.Debug(log))

	status := "200"
	if err != nil {
		status = "500"
		if errors.Is(err, logqlmodel.ErrParse) ||
			errors.Is(err, logqlmodel.ErrPipeline) ||
			errors.Is(err, logqlmodel.ErrLimit) ||
			errors.Is(err, context.Canceled) {
			status = "400"
		}
	}

	if q.record {
		RecordMetrics(ctx, q.params, status, statResult, data)
	}

	return logqlmodel.Result{
		Data:       data,
		Statistics: statResult,
	}, err
}

func (q *query) Eval(ctx context.Context) (promql_parser.Value, error) {
	ctx, cancel := context.WithTimeout(ctx, q.timeout)
	defer cancel()

	expr, err := q.parse(ctx, q.params.Query())
	if err != nil {
		return nil, err
	}

	switch e := expr.(type) {
	case SampleExpr:
		value, err := q.evalSample(ctx, e)
		return value, err

	case LogSelectorExpr:
		iter, err := q.evaluator.Iterator(ctx, e, q.params)
		if err != nil {
			return nil, err
		}

		defer util.LogErrorWithContext(ctx, "closing iterator", iter.Close)
		streams, err := readStreams(iter, q.params.Limit(), q.params.Direction(), q.params.Interval())
		return streams, err
	default:
		return nil, errors.New("Unexpected type (%T): cannot evaluate")
	}
}

// evalSample evaluate a sampleExpr
func (q *query) evalSample(ctx context.Context, expr SampleExpr) (promql_parser.Value, error) {
	if lit, ok := expr.(*LiteralExpr); ok {
		return q.evalLiteral(ctx, lit)
	}

	userID, err := tenant.ID(ctx)
	if err != nil {
		return nil, err
	}

	expr, err = optimizeSampleExpr(expr)
	if err != nil {
		return nil, err
	}

	stepEvaluator, err := q.evaluator.StepEvaluator(ctx, q.evaluator, expr, q.params)
	if err != nil {
		return nil, err
	}
	defer util.LogErrorWithContext(ctx, "closing SampleExpr", stepEvaluator.Close)

	seriesIndex := map[uint64]*promql.Series{}
	maxSeries := q.limits.MaxQuerySeries(userID)

	next, ts, vec := stepEvaluator.Next()
	if stepEvaluator.Error() != nil {
		return nil, stepEvaluator.Error()
	}

	// fail fast for the first step or instant query
	if len(vec) > maxSeries {
		return nil, logqlmodel.NewSeriesLimitError(maxSeries)
	}

	if GetRangeType(q.params) == InstantType {
		sort.Slice(vec, func(i, j int) bool { return labels.Compare(vec[i].Metric, vec[j].Metric) < 0 })
		return vec, nil
	}

	stepCount := int(math.Ceil(float64(q.params.End().Sub(q.params.Start()).Nanoseconds()) / float64(q.params.Step().Nanoseconds())))
	if stepCount <= 0 {
		stepCount = 1
	}

	for next {
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
					Points: make([]promql.Point, 0, stepCount),
				}
				seriesIndex[hash] = series
			}
			series.Points = append(series.Points, promql.Point{
				T: ts,
				V: p.V,
			})
		}
		// as we slowly build the full query for each steps, make sure we don't go over the limit of unique series.
		if len(seriesIndex) > maxSeries {
			return nil, logqlmodel.NewSeriesLimitError(maxSeries)
		}
		next, ts, vec = stepEvaluator.Next()
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

func (q *query) evalLiteral(_ context.Context, expr *LiteralExpr) (promql_parser.Value, error) {
	s := promql.Scalar{
		T: q.params.Start().UnixNano() / int64(time.Millisecond),
		V: expr.value,
	}

	if GetRangeType(q.params) == InstantType {
		return s, nil
	}

	return PopulateMatrixFromScalar(s, q.params), nil
}

func PopulateMatrixFromScalar(data promql.Scalar, params Params) promql.Matrix {
	var (
		start  = params.Start()
		end    = params.End()
		step   = params.Step()
		series = promql.Series{
			Points: make(
				[]promql.Point,
				0,
				// allocate enough space for all needed entries
				int(end.Sub(start)/step)+1,
			),
		}
	)

	for ts := start; !ts.After(end); ts = ts.Add(step) {
		series.Points = append(series.Points, promql.Point{
			T: ts.UnixNano() / int64(time.Millisecond),
			V: data.V,
		})
	}
	return promql.Matrix{series}
}

func readStreams(i iter.EntryIterator, size uint32, dir logproto.Direction, interval time.Duration) (logqlmodel.Streams, error) {
	streams := map[string]*logproto.Stream{}
	respSize := uint32(0)
	// lastEntry should be a really old time so that the first comparison is always true, we use a negative
	// value here because many unit tests start at time.Unix(0,0)
	lastEntry := lastEntryMinTime
	for respSize < size && i.Next() {
		labels, entry := i.Labels(), i.Entry()
		forwardShouldOutput := dir == logproto.FORWARD &&
			(i.Entry().Timestamp.Equal(lastEntry.Add(interval)) || i.Entry().Timestamp.After(lastEntry.Add(interval)))
		backwardShouldOutput := dir == logproto.BACKWARD &&
			(i.Entry().Timestamp.Equal(lastEntry.Add(-interval)) || i.Entry().Timestamp.Before(lastEntry.Add(-interval)))
		// If step == 0 output every line.
		// If lastEntry.Unix < 0 this is the first pass through the loop and we should output the line.
		// Then check to see if the entry is equal to, or past a forward or reverse step
		if interval == 0 || lastEntry.Unix() < 0 || forwardShouldOutput || backwardShouldOutput {
			stream, ok := streams[labels]
			if !ok {
				stream = &logproto.Stream{
					Labels: labels,
				}
				streams[labels] = stream
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
