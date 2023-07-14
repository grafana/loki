package logql

import (
	"context"
	"sort"
	"time"

	"github.com/go-kit/log"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"

	"github.com/grafana/loki/pkg/logql/sketch"
	"github.com/grafana/loki/pkg/logql/syntax"
)

type ProbabilisticEvaluator struct {
	DefaultEvaluator
	logger log.Logger
}

// NewDefaultEvaluator constructs a DefaultEvaluator
func NewProbabilisticEvaluator(querier Querier, maxLookBackPeriod time.Duration) Evaluator {
	d := NewDefaultEvaluator(querier, maxLookBackPeriod)

	// TODO(karsten): The nested DefaultEvaluator and outer
	// ProbabilisticEvaluator have a circular dependency throuh the agg
	// evaluator. This should be avoided.
	p := &ProbabilisticEvaluator{DefaultEvaluator: *d}
	d.newVectorAggEvaluator = p.newProbabilisticVectorAggEvaluator
	return p
}

func (p *ProbabilisticEvaluator) newProbabilisticVectorAggEvaluator(
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
	return NewStepEvaluator(func() (bool, int64, promql.Vector) {
		next, ts, vec := nextEvaluator.Next()

		if !next {
			return false, 0, promql.Vector{}
		}
		result := map[uint64]*sketch.Topk{}
		if expr.Params < 1 {
			return next, ts, promql.Vector{}
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
				// TODO(karsten): get k and c.
				group, err = sketch.NewCMSTopkForCardinality(p.logger, 10, 100000)
				if err != nil {
					// TODO(karsten): return error
					return next, ts, promql.Vector{}
				}
				result[groupingKey] = group
			}

			// TODO(karsten): add s.F instead
			group.Observe(s.Metric.String())
		}
		r := sketch.TopKVector{
			TS: uint64(ts),
		}
		for _, aggr := range result {
			// TODO(karsten): How are we handling groups?
			r.Topk = aggr
		}
		return next, ts, vec
	}, nextEvaluator.Close, nextEvaluator.Error)
}
