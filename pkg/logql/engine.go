package logql

import (
	"container/heap"
	"context"
	"math"
	"sort"
	"time"

	"github.com/grafana/loki/pkg/helpers"
	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/pkg/errors"
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
	// MaxLookBackPeriod is the maximun amount of time to look back for log lines.
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

// Engine is the LogQL engine.
type Engine struct {
	timeout           time.Duration
	maxLookBackPeriod time.Duration
}

// NewEngine creates a new LogQL engine.
func NewEngine(opts EngineOpts) *Engine {
	opts.applyDefault()
	return &Engine{
		timeout:           opts.Timeout,
		maxLookBackPeriod: opts.MaxLookBackPeriod,
	}
}

// Query is a LogQL query to be executed.
type Query interface {
	// Exec processes the query.
	Exec(ctx context.Context) (promql.Value, error)
}

type query struct {
	querier    Querier
	qs         string
	start, end time.Time
	step       time.Duration
	direction  logproto.Direction
	limit      uint32

	ng *Engine
}

func (q *query) isInstant() bool {
	return q.start == q.end && q.step == 0
}

// Exec Implements `Query`
func (q *query) Exec(ctx context.Context) (promql.Value, error) {
	var queryType string
	if q.isInstant() {
		queryType = "instant"
	} else {
		queryType = "range"
	}
	timer := prometheus.NewTimer(queryTime.WithLabelValues(queryType))
	defer timer.ObserveDuration()
	return q.ng.exec(ctx, q)
}

// NewRangeQuery creates a new LogQL range query.
func (ng *Engine) NewRangeQuery(
	q Querier,
	qs string,
	start, end time.Time, step time.Duration,
	direction logproto.Direction, limit uint32) Query {
	return &query{
		querier:   q,
		qs:        qs,
		start:     start,
		end:       end,
		step:      step,
		direction: direction,
		limit:     limit,
		ng:        ng,
	}
}

// NewInstantQuery creates a new LogQL instant query.
func (ng *Engine) NewInstantQuery(
	q Querier,
	qs string,
	ts time.Time,
	direction logproto.Direction, limit uint32) Query {
	return &query{
		querier:   q,
		qs:        qs,
		start:     ts,
		end:       ts,
		step:      0,
		direction: direction,
		limit:     limit,
		ng:        ng,
	}
}

func (ng *Engine) exec(ctx context.Context, q *query) (promql.Value, error) {
	ctx, cancel := context.WithTimeout(ctx, ng.timeout)
	defer cancel()

	if q.qs == "1+1" {
		if q.isInstant() {
			return promql.Vector{}, nil
		}
		return promql.Matrix{}, nil
	}

	expr, err := ParseExpr(q.qs)
	if err != nil {
		return nil, err
	}

	switch e := expr.(type) {
	case SampleExpr:
		if err := ng.setupIterators(ctx, e, q); err != nil {
			return nil, err
		}
		return ng.evalSample(e, q), nil

	case LogSelectorExpr:
		params := SelectParams{
			QueryRequest: &logproto.QueryRequest{
				Start:     q.start,
				End:       q.end,
				Limit:     q.limit,
				Direction: q.direction,
				Selector:  e.String(),
			},
		}
		// instant query, we look back to find logs near the requested ts.
		if q.isInstant() {
			params.Start = params.Start.Add(-ng.maxLookBackPeriod)
		}
		iter, err := q.querier.Select(ctx, params)
		if err != nil {
			return nil, err
		}
		defer helpers.LogError("closing iterator", iter.Close)
		return readStreams(iter, q.limit)
	}

	return nil, nil
}

// setupIterators walk through the AST tree and build iterators required to eval samples.
func (ng *Engine) setupIterators(ctx context.Context, expr SampleExpr, q *query) error {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *vectorAggregationExpr:
		return ng.setupIterators(ctx, e.left, q)
	case *rangeAggregationExpr:
		iter, err := q.querier.Select(ctx, SelectParams{
			&logproto.QueryRequest{
				Start:     q.start.Add(-e.left.interval),
				End:       q.end,
				Limit:     0,
				Direction: logproto.FORWARD,
				Selector:  e.Selector().String(),
			},
		})
		if err != nil {
			return err
		}
		e.iterator = newRangeVectorIterator(iter, e.left.interval.Nanoseconds(), q.step.Nanoseconds(),
			q.start.UnixNano(), q.end.UnixNano())
	}
	return nil
}

// evalSample evaluate a sampleExpr
func (ng *Engine) evalSample(expr SampleExpr, q *query) promql.Value {
	defer helpers.LogError("closing SampleExpr", expr.Close)

	stepEvaluator := expr.Evaluator()
	seriesIndex := map[uint64]*promql.Series{}

	next, ts, vec := stepEvaluator.Next()
	if q.isInstant() {
		sort.Slice(vec, func(i, j int) bool { return labels.Compare(vec[i].Metric, vec[j].Metric) < 0 })
		return vec
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
	return result
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

// Evaluator implements `SampleExpr` for a vectorAggregationExpr
// this is copied and adapted from Prometheus vector aggregation code.
func (v *vectorAggregationExpr) Evaluator() StepEvaluator {
	return StepEvaluatorFn(func() (bool, int64, promql.Vector) {
		next, ts, vec := v.left.Evaluator().Next()
		if !next {
			return false, 0, promql.Vector{}
		}
		result := map[uint64]*groupedAggregation{}
		if v.operation == OpTypeTopK || v.operation == OpTypeBottomK {
			if v.params < 1 {
				return next, ts, promql.Vector{}
			}

		}
		for _, s := range vec {
			metric := s.Metric

			var (
				groupingKey uint64
			)
			if v.grouping.without {
				groupingKey, _ = metric.HashWithoutLabels(make([]byte, 0, 1024), v.grouping.groups...)
			} else {
				groupingKey, _ = metric.HashForLabels(make([]byte, 0, 1024), v.grouping.groups...)
			}
			group, ok := result[groupingKey]
			// Add a new group if it doesn't exist.
			if !ok {
				var m labels.Labels

				if v.grouping.without {
					lb := labels.NewBuilder(metric)
					lb.Del(v.grouping.groups...)
					lb.Del(labels.MetricName)
					m = lb.Labels()
				} else {
					m = make(labels.Labels, 0, len(v.grouping.groups))
					for _, l := range metric {
						for _, n := range v.grouping.groups {
							if l.Name == n {
								m = append(m, l)
								break
							}
						}
					}
					sort.Sort(m)
				}
				result[groupingKey] = &groupedAggregation{
					labels:     m,
					value:      s.V,
					mean:       s.V,
					groupCount: 1,
				}

				inputVecLen := len(vec)
				resultSize := v.params
				if v.params > inputVecLen {
					resultSize = inputVecLen
				}
				if v.operation == OpTypeStdvar || v.operation == OpTypeStddev {
					result[groupingKey].value = 0.0
				} else if v.operation == OpTypeTopK {
					result[groupingKey].heap = make(vectorByValueHeap, 0, resultSize)
					heap.Push(&result[groupingKey].heap, &promql.Sample{
						Point:  promql.Point{V: s.V},
						Metric: s.Metric,
					})
				} else if v.operation == OpTypeBottomK {
					result[groupingKey].reverseHeap = make(vectorByReverseValueHeap, 0, resultSize)
					heap.Push(&result[groupingKey].reverseHeap, &promql.Sample{
						Point:  promql.Point{V: s.V},
						Metric: s.Metric,
					})
				}
				continue
			}
			switch v.operation {
			case OpTypeSum:
				group.value += s.V

			case OpTypeAvg:
				group.groupCount++
				group.mean += (s.V - group.mean) / float64(group.groupCount)

			case OpTypeMax:
				if group.value < s.V || math.IsNaN(group.value) {
					group.value = s.V
				}

			case OpTypeMin:
				if group.value > s.V || math.IsNaN(group.value) {
					group.value = s.V
				}

			case OpTypeCount:
				group.groupCount++

			case OpTypeStddev, OpTypeStdvar:
				group.groupCount++
				delta := s.V - group.mean
				group.mean += delta / float64(group.groupCount)
				group.value += delta * (s.V - group.mean)

			case OpTypeTopK:
				if len(group.heap) < v.params || group.heap[0].V < s.V || math.IsNaN(group.heap[0].V) {
					if len(group.heap) == v.params {
						heap.Pop(&group.heap)
					}
					heap.Push(&group.heap, &promql.Sample{
						Point:  promql.Point{V: s.V},
						Metric: s.Metric,
					})
				}

			case OpTypeBottomK:
				if len(group.reverseHeap) < v.params || group.reverseHeap[0].V > s.V || math.IsNaN(group.reverseHeap[0].V) {
					if len(group.reverseHeap) == v.params {
						heap.Pop(&group.reverseHeap)
					}
					heap.Push(&group.reverseHeap, &promql.Sample{
						Point:  promql.Point{V: s.V},
						Metric: s.Metric,
					})
				}
			default:
				panic(errors.Errorf("expected aggregation operator but got %q", v.operation))
			}
		}
		vec = vec[:0]
		for _, aggr := range result {
			switch v.operation {
			case OpTypeAvg:
				aggr.value = aggr.mean

			case OpTypeCount:
				aggr.value = float64(aggr.groupCount)

			case OpTypeStddev:
				aggr.value = math.Sqrt(aggr.value / float64(aggr.groupCount))

			case OpTypeStdvar:
				aggr.value = aggr.value / float64(aggr.groupCount)

			case OpTypeTopK:
				// The heap keeps the lowest value on top, so reverse it.
				sort.Sort(sort.Reverse(aggr.heap))
				for _, v := range aggr.heap {
					vec = append(vec, promql.Sample{
						Metric: v.Metric,
						Point: promql.Point{
							T: ts,
							V: v.V,
						},
					})
				}
				continue // Bypass default append.

			case OpTypeBottomK:
				// The heap keeps the lowest value on top, so reverse it.
				sort.Sort(sort.Reverse(aggr.reverseHeap))
				for _, v := range aggr.reverseHeap {
					vec = append(vec, promql.Sample{
						Metric: v.Metric,
						Point: promql.Point{
							T: ts,
							V: v.V,
						},
					})
				}
				continue // Bypass default append.
			default:
			}
			vec = append(vec, promql.Sample{
				Metric: aggr.labels,
				Point: promql.Point{
					T: ts,
					V: aggr.value,
				},
			})
		}
		return next, ts, vec
	})
}

// Evaluator implements `SampleExpr` for a rangeAggregationExpr
func (e *rangeAggregationExpr) Evaluator() StepEvaluator {
	var fn RangeVectorAggregator
	switch e.operation {
	case OpTypeRate:
		fn = rate(e.left.interval)
	case OpTypeCountOverTime:
		fn = count
	}
	return StepEvaluatorFn(func() (bool, int64, promql.Vector) {
		next := e.iterator.Next()
		if !next {
			return false, 0, promql.Vector{}
		}
		ts, vec := e.iterator.At(fn)
		return true, ts, vec
	})
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
