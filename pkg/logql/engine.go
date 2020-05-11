package logql

import (
	"context"
	"sort"
	"time"

	"github.com/go-kit/kit/log/level"

	"github.com/cortexproject/cortex/pkg/util/spanlogger"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"

	"github.com/grafana/loki/pkg/helpers"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/stats"
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

// ValueTypeStreams promql.ValueType for log streams
const ValueTypeStreams = "streams"

// Streams is promql.Value
type Streams []logproto.Stream

// Type implements `promql.Value`
func (Streams) Type() promql.ValueType { return ValueTypeStreams }

// String implements `promql.Value`
func (Streams) String() string {
	return ""
}

// Result is the result of a query execution.
type Result struct {
	Data       promql.Value
	Statistics stats.Result
}

// EngineOpts is the list of options to use with the LogQL query engine.
type EngineOpts struct {
	// Timeout for queries execution
	Timeout time.Duration `yaml:"timeout"`
	// MaxLookBackPeriod is the maximum amount of time to look back for log lines.
	// only used for instant log queries.
	MaxLookBackPeriod time.Duration `yaml:"max_look_back_period"`
}

func (opts *EngineOpts) applyDefault() {
	if opts.Timeout == 0 {
		opts.Timeout = 3 * time.Minute
	}
	if opts.MaxLookBackPeriod == 0 {
		opts.MaxLookBackPeriod = 30 * time.Second
	}
}

// Engine interface used to construct queries
type Engine interface {
	NewRangeQuery(qs string, start, end time.Time, step, interval time.Duration, direction logproto.Direction, limit uint32) Query
	NewInstantQuery(qs string, ts time.Time, direction logproto.Direction, limit uint32) Query
}

// engine is the LogQL engine.
type engine struct {
	timeout   time.Duration
	evaluator Evaluator
}

// NewEngine creates a new LogQL engine.
func NewEngine(opts EngineOpts, q Querier) Engine {
	if q == nil {
		panic("nil Querier")
	}

	opts.applyDefault()

	return &engine{
		timeout: opts.Timeout,
		evaluator: &defaultEvaluator{
			querier:           q,
			maxLookBackPeriod: opts.MaxLookBackPeriod,
		},
	}
}

// Query is a LogQL query to be executed.
type Query interface {
	// Exec processes the query.
	Exec(ctx context.Context) (Result, error)
}

type query struct {
	LiteralParams

	ng *engine
}

// Exec Implements `Query`
func (q *query) Exec(ctx context.Context) (Result, error) {
	log, ctx := spanlogger.New(ctx, "Engine.Exec")
	defer log.Finish()

	rangeType := GetRangeType(q)
	timer := prometheus.NewTimer(queryTime.WithLabelValues(string(rangeType)))
	defer timer.ObserveDuration()

	// records query statistics
	var statResult stats.Result
	start := time.Now()
	ctx = stats.NewContext(ctx)

	data, err := q.ng.exec(ctx, q)

	statResult = stats.Snapshot(ctx, time.Since(start))
	statResult.Log(level.Debug(log))

	status := "200"
	if err != nil {
		status = "500"
		if IsParseError(err) {
			status = "400"
		}
	}
	RecordMetrics(ctx, q, status, statResult)

	return Result{
		Data:       data,
		Statistics: statResult,
	}, err
}

// NewRangeQuery creates a new LogQL range query.
func (ng *engine) NewRangeQuery(
	qs string,
	start, end time.Time, step time.Duration, interval time.Duration,
	direction logproto.Direction, limit uint32) Query {
	return &query{
		LiteralParams: LiteralParams{
			qs:        qs,
			start:     start,
			end:       end,
			step:      step,
			interval:  interval,
			direction: direction,
			limit:     limit,
		},
		ng: ng,
	}
}

// NewInstantQuery creates a new LogQL instant query.
func (ng *engine) NewInstantQuery(
	qs string,
	ts time.Time,
	direction logproto.Direction, limit uint32) Query {
	return &query{
		LiteralParams: LiteralParams{
			qs:        qs,
			start:     ts,
			end:       ts,
			step:      0,
			interval:  0,
			direction: direction,
			limit:     limit,
		},
		ng: ng,
	}
}

func (ng *engine) exec(ctx context.Context, q *query) (promql.Value, error) {
	ctx, cancel := context.WithTimeout(ctx, ng.timeout)
	defer cancel()

	qs := q.Query()

	expr, err := ParseExpr(qs)
	if err != nil {
		return nil, err
	}

	switch e := expr.(type) {
	case SampleExpr:
		value, err := ng.evalSample(ctx, e, q)
		return value, err

	case LogSelectorExpr:
		iter, err := ng.evaluator.Iterator(ctx, e, q)
		if err != nil {
			return nil, err
		}
		defer helpers.LogErrorWithContext(ctx, "closing iterator", iter.Close)
		streams, err := readStreams(iter, q.limit, q.direction, q.interval)
		return streams, err
	}

	return nil, nil
}

// evalSample evaluate a sampleExpr
func (ng *engine) evalSample(ctx context.Context, expr SampleExpr, q *query) (promql.Value, error) {
	if lit, ok := expr.(*literalExpr); ok {
		return ng.evalLiteral(ctx, lit, q)
	}

	stepEvaluator, err := ng.evaluator.StepEvaluator(ctx, ng.evaluator, expr, q)
	if err != nil {
		return nil, err
	}
	defer helpers.LogErrorWithContext(ctx, "closing SampleExpr", stepEvaluator.Close)

	seriesIndex := map[uint64]*promql.Series{}

	next, ts, vec := stepEvaluator.Next()
	if GetRangeType(q) == InstantType {
		sort.Slice(vec, func(i, j int) bool { return labels.Compare(vec[i].Metric, vec[j].Metric) < 0 })
		return vec, nil
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
				}
				seriesIndex[hash] = series
			}
			series.Points = append(series.Points, promql.Point{
				T: ts,
				V: p.V,
			})
		}
		next, ts, vec = stepEvaluator.Next()
	}

	series := make([]promql.Series, 0, len(seriesIndex))
	for _, s := range seriesIndex {
		series = append(series, *s)
	}
	result := promql.Matrix(series)
	sort.Sort(result)
	return result, nil
}

func (ng *engine) evalLiteral(_ context.Context, expr *literalExpr, q *query) (promql.Value, error) {
	s := promql.Scalar{
		T: q.Start().UnixNano() / int64(time.Millisecond),
		V: expr.value,
	}

	if GetRangeType(q) == InstantType {
		return s, nil
	}

	return PopulateMatrixFromScalar(s, q.LiteralParams), nil

}

func PopulateMatrixFromScalar(data promql.Scalar, params LiteralParams) promql.Matrix {
	var (
		start  = params.Start()
		end    = params.End()
		step   = params.Step()
		series = promql.Series{
			Points: make(
				[]promql.Point,
				0,
				// allocate enough space for all needed entries
				int(params.End().Sub(params.Start())/params.Step())+1,
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

func readStreams(i iter.EntryIterator, size uint32, dir logproto.Direction, interval time.Duration) (Streams, error) {
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

	result := make([]logproto.Stream, 0, len(streams))
	for _, stream := range streams {
		result = append(result, *stream)
	}
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

// rate calculate the per-second rate of log lines.
func rate(selRange time.Duration) func(ts int64, samples []promql.Point) float64 {
	return func(ts int64, samples []promql.Point) float64 {
		return float64(len(samples)) / selRange.Seconds()
	}
}

// count counts the amount of log lines.
func count(ts int64, samples []promql.Point) float64 {
	return float64(len(samples))
}
