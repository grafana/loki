package logql

import (
	"container/heap"
	"context"
	"math"
	"sort"
	"time"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"
)

type Qry interface {
	String() string
	Start() time.Time
	End() time.Time
	Step() time.Duration
	Limit() uint32
}

func IsInstant(q Qry) bool {
	return q.Start() == q.End() && q.Step() == 0
}

type Evaluator interface {
	Evaluator(context.Context, SampleExpr, Qry) (StepEvaluator, error)
	Iterator(context.Context, LogSelectorExpr, Qry) (iter.EntryIterator, error)
}

type defaultEvaluator struct {
	querier Querier
}

func (ev *defaultEvaluator) Evaluator(ctx context.Context, expr SampleExpr, q Qry) (StepEvaluator, error) {
	switch e := expr.(type) {
	case *vectorAggregationExpr:
		return ev.vectorAggEvaluator(ctx, e, q)
	case *rangeAggregationExpr:
		return ev.rangeAggEvaluator(ctx, e, q)

	default:
		return nil, errors.Errorf("unexpected type (%T): %v", e, e)
	}
}

func (ev *defaultEvaluator) vectorAggEvaluator(ctx context.Context, expr *vectorAggregationExpr, q Qry) (StepEvaluator, error) {
	nextEvaluator, err := ev.Evaluator(ctx, expr.left, q)
	if err != nil {
		return nil, err
	}

	return StepEvaluatorFn(func() (bool, int64, promql.Vector) {
		next, ts, vec := nextEvaluator.Next()
		if !next {
			return false, 0, promql.Vector{}
		}
		result := map[uint64]*groupedAggregation{}
		if expr.operation == OpTypeTopK || expr.operation == OpTypeBottomK {
			if expr.params < 1 {
				return next, ts, promql.Vector{}
			}

		}
		for _, s := range vec {
			metric := s.Metric

			var (
				groupingKey uint64
			)
			if expr.grouping.without {
				groupingKey, _ = metric.HashWithoutLabels(make([]byte, 0, 1024), expr.grouping.groups...)
			} else {
				groupingKey, _ = metric.HashForLabels(make([]byte, 0, 1024), expr.grouping.groups...)
			}
			group, ok := result[groupingKey]
			// Add a new group if it doesn't exist.
			if !ok {
				var m labels.Labels

				if expr.grouping.without {
					lb := labels.NewBuilder(metric)
					lb.Del(expr.grouping.groups...)
					lb.Del(labels.MetricName)
					m = lb.Labels()
				} else {
					m = make(labels.Labels, 0, len(expr.grouping.groups))
					for _, l := range metric {
						for _, n := range expr.grouping.groups {
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
				resultSize := expr.params
				if expr.params > inputVecLen {
					resultSize = inputVecLen
				}
				if expr.operation == OpTypeStdvar || expr.operation == OpTypeStddev {
					result[groupingKey].value = 0.0
				} else if expr.operation == OpTypeTopK {
					result[groupingKey].heap = make(vectorByValueHeap, 0, resultSize)
					heap.Push(&result[groupingKey].heap, &promql.Sample{
						Point:  promql.Point{V: s.V},
						Metric: s.Metric,
					})
				} else if expr.operation == OpTypeBottomK {
					result[groupingKey].reverseHeap = make(vectorByReverseValueHeap, 0, resultSize)
					heap.Push(&result[groupingKey].reverseHeap, &promql.Sample{
						Point:  promql.Point{V: s.V},
						Metric: s.Metric,
					})
				}
				continue
			}
			switch expr.operation {
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
				if len(group.heap) < expr.params || group.heap[0].V < s.V || math.IsNaN(group.heap[0].V) {
					if len(group.heap) == expr.params {
						heap.Pop(&group.heap)
					}
					heap.Push(&group.heap, &promql.Sample{
						Point:  promql.Point{V: s.V},
						Metric: s.Metric,
					})
				}

			case OpTypeBottomK:
				if len(group.reverseHeap) < expr.params || group.reverseHeap[0].V > s.V || math.IsNaN(group.reverseHeap[0].V) {
					if len(group.reverseHeap) == expr.params {
						heap.Pop(&group.reverseHeap)
					}
					heap.Push(&group.reverseHeap, &promql.Sample{
						Point:  promql.Point{V: s.V},
						Metric: s.Metric,
					})
				}
			default:
				panic(errors.Errorf("expected aggregation operator but got %q", expr.operation))
			}
		}
		vec = vec[:0]
		for _, aggr := range result {
			switch expr.operation {
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
	}), nil
}

func (ev *defaultEvaluator) rangeAggEvaluator(ctx context.Context, expr *rangeAggregationExpr, q Qry) (StepEvaluator, error) {
	entryIter, err := ev.querier.Select(ctx, SelectParams{
		&logproto.QueryRequest{
			Start:     q.Start().Add(-expr.left.interval),
			End:       q.End(),
			Limit:     0,
			Direction: logproto.FORWARD,
			Selector:  expr.Selector().String(),
		},
	})

	if err != nil {
		return nil, err
	}

	vecIter := newRangeVectorIterator(entryIter, expr.left.interval.Nanoseconds(), q.Step().Nanoseconds(),
		q.Start().UnixNano(), q.End().UnixNano())

	var fn RangeVectorAggregator
	switch expr.operation {
	case OpTypeRate:
		fn = rate(expr.left.interval)
	case OpTypeCountOverTime:
		fn = count
	}
	return StepEvaluatorFn(func() (bool, int64, promql.Vector) {
		next := vecIter.Next()
		if !next {
			return false, 0, promql.Vector{}
		}
		ts, vec := vecIter.At(fn)
		return true, ts, vec
	}), nil
}
