package logql

import (
	"context"
	"sort"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"

	"github.com/grafana/loki/pkg/logql/syntax"
)

type ProbabilisticEvaluator struct {
	DefaultEvaluator
}

// NewDefaultEvaluator constructs a DefaultEvaluator
func NewProbabilisticEvaluator(querier Querier, maxLookBackPeriod time.Duration) Evaluator {
	d := NewDefaultEvaluator(querier, maxLookBackPeriod)
	d.newVectorAggEvaluator = newProbabilisticVectorAggEvaluator
	p := &ProbabilisticEvaluator{DefaultEvaluator: *d}
	return p
}

func newProbabilisticVectorAggEvaluator(
	ctx context.Context,
	ev SampleEvaluator,
	expr *syntax.VectorAggregationExpr,
	q Params,
) (StepEvaluator, error) {

	if expr.Operation != syntax.OpTypeTopK {
		return newVectorAggEvaluator(ctx, ev, expr, q)
	}

	// TODO(karsten): Below is just copy-pasta from newVectorAggEvaluator.
	// We should find better abstractions.

	if expr.Grouping == nil {
		return nil, errors.Errorf("aggregation operator '%q' without grouping", expr.Operation)
	}
	nextEvaluator, err := ev.StepEvaluator(ctx, ev, expr.Left, q)
	if err != nil {
		return nil, err
	}
	lb := labels.NewBuilder(nil)
	buf := make([]byte, 0, 1024)
	sort.Strings(expr.Grouping.Groups)
	return newStepEvaluator(func() (bool, int64, promql.Vector) {
		next, ts, vec := nextEvaluator.Next()

		if !next {
			return false, 0, promql.Vector{}
		}
		result := map[uint64]*groupedAggregation{}
		if expr.Operation == syntax.OpTypeTopK || expr.Operation == syntax.OpTypeBottomK {
			if expr.Params < 1 {
				return next, ts, promql.Vector{}
			}
		}
		for _, s := range vec {
			metric := s.Metric

			var groupingKey uint64
			if expr.Grouping.Without {
				groupingKey, buf = metric.HashWithoutLabels(buf, expr.Grouping.Groups...)
			} else {
				groupingKey, buf = metric.HashForLabels(buf, expr.Grouping.Groups...)
			}
			group, ok := result[groupingKey]
			// Add a new group if it doesn't exist.
			if !ok {
				var m labels.Labels

				if expr.Grouping.Without {
					lb.Reset(metric)
					lb.Del(expr.Grouping.Groups...)
					lb.Del(labels.MetricName)
					m = lb.Labels()
				} else {
					m = make(labels.Labels, 0, len(expr.Grouping.Groups))
					for _, l := range metric {
						for _, n := range expr.Grouping.Groups {
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
					value:      s.F,
					mean:       s.F,
					groupCount: 1,
				}

				inputVecLen := len(vec)
				resultSize := expr.Params
				if expr.Params > inputVecLen {
					resultSize = inputVecLen
				}
				if expr.Operation == syntax.OpTypeStdvar || expr.Operation == syntax.OpTypeStddev {
					result[groupingKey].value = 0.0
				} else if expr.Operation == syntax.OpTypeTopK {
					result[groupingKey].heap = make(vectorByValueHeap, 0, resultSize)
					heap.Push(&result[groupingKey].heap, &promql.Sample{
						F:      s.F,
						Metric: s.Metric,
					})
				} else if expr.Operation == syntax.OpTypeBottomK {
					result[groupingKey].reverseHeap = make(vectorByReverseValueHeap, 0, resultSize)
					heap.Push(&result[groupingKey].reverseHeap, &promql.Sample{
						F:      s.F,
						Metric: s.Metric,
					})
				} else if expr.Operation == syntax.OpTypeSortDesc {
					result[groupingKey].heap = make(vectorByValueHeap, 0)
					heap.Push(&result[groupingKey].heap, &promql.Sample{
						F:      s.F,
						Metric: s.Metric,
					})
				} else if expr.Operation == syntax.OpTypeSort {
					result[groupingKey].reverseHeap = make(vectorByReverseValueHeap, 0)
					heap.Push(&result[groupingKey].reverseHeap, &promql.Sample{
						F:      s.F,
						Metric: s.Metric,
					})
				}
				continue
			}
			switch expr.Operation {
			case syntax.OpTypeSum:
				group.value += s.F

			case syntax.OpTypeAvg:
				group.groupCount++
				group.mean += (s.F - group.mean) / float64(group.groupCount)

			case syntax.OpTypeMax:
				if group.value < s.F || math.IsNaN(group.value) {
					group.value = s.F
				}

			case syntax.OpTypeMin:
				if group.value > s.F || math.IsNaN(group.value) {
					group.value = s.F
				}

			case syntax.OpTypeCount:
				group.groupCount++

			case syntax.OpTypeStddev, syntax.OpTypeStdvar:
				group.groupCount++
				delta := s.F - group.mean
				group.mean += delta / float64(group.groupCount)
				group.value += delta * (s.F - group.mean)

			case syntax.OpTypeTopK:
				if len(group.heap) < expr.Params || group.heap[0].F < s.F || math.IsNaN(group.heap[0].F) {
					if len(group.heap) == expr.Params {
						heap.Pop(&group.heap)
					}
					heap.Push(&group.heap, &promql.Sample{
						F:      s.F,
						Metric: s.Metric,
					})
				}

			case syntax.OpTypeBottomK:
				if len(group.reverseHeap) < expr.Params || group.reverseHeap[0].F > s.F || math.IsNaN(group.reverseHeap[0].F) {
					if len(group.reverseHeap) == expr.Params {
						heap.Pop(&group.reverseHeap)
					}
					heap.Push(&group.reverseHeap, &promql.Sample{
						F:      s.F,
						Metric: s.Metric,
					})
				}
			case syntax.OpTypeSortDesc:
				heap.Push(&group.heap, &promql.Sample{
					F:      s.F,
					Metric: s.Metric,
				})
			case syntax.OpTypeSort:
				heap.Push(&group.reverseHeap, &promql.Sample{
					F:      s.F,
					Metric: s.Metric,
				})
			default:
				panic(errors.Errorf("expected aggregation operator but got %q", expr.Operation))
			}
		}
		vec = vec[:0]
		for _, aggr := range result {
			switch expr.Operation {
			case syntax.OpTypeAvg:
				aggr.value = aggr.mean

			case syntax.OpTypeCount:
				aggr.value = float64(aggr.groupCount)

			case syntax.OpTypeStddev:
				aggr.value = math.Sqrt(aggr.value / float64(aggr.groupCount))

			case syntax.OpTypeStdvar:
				aggr.value = aggr.value / float64(aggr.groupCount)

			case syntax.OpTypeTopK, syntax.OpTypeSortDesc:
				// The heap keeps the lowest value on top, so reverse it.
				sort.Sort(sort.Reverse(aggr.heap))
				for _, v := range aggr.heap {
					vec = append(vec, promql.Sample{
						Metric: v.Metric,
						T:      ts,
						F:      v.F,
					})
				}
				continue // Bypass default append.

			case syntax.OpTypeBottomK, syntax.OpTypeSort:
				// The heap keeps the lowest value on top, so reverse it.
				sort.Sort(sort.Reverse(aggr.reverseHeap))
				for _, v := range aggr.reverseHeap {
					vec = append(vec, promql.Sample{
						Metric: v.Metric,
						T:      ts,
						F:      v.F,
					})
				}
				continue // Bypass default append.
			default:
			}
			vec = append(vec, promql.Sample{
				Metric: aggr.labels,
				T:      ts,
				F:      aggr.value,
			})
		}
		return next, ts, vec
	}, nextEvaluator.Close, nextEvaluator.Error)
}
