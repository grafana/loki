package logql

import (
	"context"
	"sort"
	"time"

	"github.com/cortexproject/cortex/pkg/util/spanlogger"
	"github.com/grafana/loki/pkg/helpers"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/stats"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"
)

var (
	queryTime = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "logql",
		Name:      "query_duration_seconds",
		Help:      "LogQL query timings",
		Buckets:   prometheus.DefBuckets,
	}, []string{"query_type"})
)

// ValueTypeStreams promql.ValueType for log streams
const ValueTypeStreams = "streams"

// Streams is promql.Value
type Streams []*logproto.Stream

// Type implements `promql.Value`
func (Streams) Type() promql.ValueType { return ValueTypeStreams }

// String implements `promql.Value`
func (Streams) String() string {
	return ""
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

/*
Engine interface
function/agg Nodes should have Associative method to allow ad-hoc parallelization?
limit + filter (regexp) queries can be parallelized well
non filtered queries can be executed in roughly 1/shard_factor time, but require 2x decode cost: sending all (unfiltered) data back to frontend

custom downstream impl may require it's own endpoint (in order to pass shard info not encoded in labels)
*/
type Engine interface {
	NewRangeQuery(qs string, start, end time.Time, step time.Duration, direction logproto.Direction, limit uint32) Query
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
	Exec(ctx context.Context) (promql.Value, error)
}

type query struct {
	LiteralParams

	ng *engine
}

// Exec Implements `Query`
func (q *query) Exec(ctx context.Context) (promql.Value, error) {
	var queryType string

	if IsInstant(q) {
		queryType = "instant"
	} else {
		queryType = "range"
	}
	timer := prometheus.NewTimer(queryTime.WithLabelValues(queryType))
	defer timer.ObserveDuration()
	return q.ng.exec(ctx, q)
}

// NewRangeQuery creates a new LogQL range query.
func (ng *engine) NewRangeQuery(
	qs string,
	start, end time.Time, step time.Duration,
	direction logproto.Direction, limit uint32) Query {
	return &query{
		LiteralParams: LiteralParams{
			qs:        qs,
			start:     start,
			end:       end,
			step:      step,
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
			direction: direction,
			limit:     limit,
		},
		ng: ng,
	}
}

func (ng *engine) exec(ctx context.Context, q *query) (promql.Value, error) {
	log, ctx := spanlogger.New(ctx, "Engine.exec")
	defer log.Finish()
	ctx, cancel := context.WithTimeout(ctx, ng.timeout)
	defer cancel()

	qs := q.String()
	if qs == "1+1" {
		if IsInstant(q) {
			return promql.Vector{}, nil
		}
		return promql.Matrix{}, nil
	}

	expr, err := ParseExpr(qs)
	if err != nil {
		return nil, err
	}

	ctx = stats.NewContext(ctx)
	start := time.Now()
	defer func() {
		resultStats := stats.Snapshot(ctx, time.Since(start))
		stats.Log(log, resultStats)
	}()

	switch e := expr.(type) {
	case SampleExpr:
		return ng.evalSample(ctx, e, q)

	case LogSelectorExpr:
		iter, err := ng.evaluator.Iterator(ctx, e, q)
		if err != nil {
			return nil, err
		}
		defer helpers.LogError("closing iterator", iter.Close)
		return readStreams(iter, q.limit)
	}

	return nil, nil
}

// evalSample evaluate a sampleExpr
func (ng *engine) evalSample(ctx context.Context, expr SampleExpr, q *query) (promql.Value, error) {

	stepEvaluator, err := ng.evaluator.Evaluator(ctx, expr, q)
	defer helpers.LogError("closing SampleExpr", stepEvaluator.Close)
	if err != nil {
		return nil, err
	}
	seriesIndex := map[uint64]*promql.Series{}

	next, ts, vec := stepEvaluator.Next()
	if IsInstant(q) {
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

func readStreams(i iter.EntryIterator, size uint32) (Streams, error) {
	streams := map[string]*logproto.Stream{}
	respSize := uint32(0)
	for ; respSize < size && i.Next(); respSize++ {
		labels, entry := i.Labels(), i.Entry()
		stream, ok := streams[labels]
		if !ok {
			stream = &logproto.Stream{
				Labels: labels,
			}
			streams[labels] = stream
		}
		stream.Entries = append(stream.Entries, entry)
	}

	result := make([]*logproto.Stream, 0, len(streams))
	for _, stream := range streams {
		result = append(result, stream)
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
