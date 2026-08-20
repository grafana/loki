package logql

import (
	"fmt"
	"math"
	"time"

	"github.com/axiomhq/hyperloglog"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	promql_parser "github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
)

type countDistinctStepEvaluator struct {
	it        iter.PeekingSampleIterator
	params    Params
	interval  time.Duration
	offset    time.Duration
	exhausted bool
	err       error
}

func newCountDistinctStepEvaluator(
	it iter.PeekingSampleIterator,
	params Params,
	interval, offset time.Duration,
) (StepEvaluator, error) {
	if GetRangeType(params) != InstantType {
		return nil, fmt.Errorf("approx_count_distinct is only supported on instant queries")
	}
	return &countDistinctStepEvaluator{
		it:       it,
		params:   params,
		interval: interval,
		offset:   offset,
	}, nil
}

func (e *countDistinctStepEvaluator) Next() (bool, int64, StepResult) {
	if e.exhausted {
		return false, 0, SampleVector{}
	}
	e.exhausted = true

	// Match RangeAggregationExpr: (T-D-O, T-O]
	start := e.params.Start().Add(-e.interval).Add(-e.offset).UnixNano()
	end := e.params.End().Add(-e.offset).UnixNano()
	ts := e.params.Start().UnixMilli()

	type group struct {
		hll    *hyperloglog.Sketch
		metric labels.Labels
	}
	groups := make(map[string]*group)

	for lbs, sample, ok := e.it.Peek(); ok; lbs, sample, ok = e.it.Peek() {
		if sample.Timestamp > end {
			break
		}
		if sample.Timestamp <= start {
			_ = e.it.Next()
			continue
		}

		g, exists := groups[lbs]
		if !exists {
			metric, err := promql_parser.NewParser(promql_parser.Options{}).ParseMetric(lbs)
			if err != nil {
				_ = e.it.Next()
				continue
			}
			if metric.Has(logqlmodel.ErrorLabel) && metric.Get(logqlmodel.PreserveErrorLabel) != trueString {
				e.err = logqlmodel.NewPipelineErr(metric)
				return false, 0, SampleVector{}
			}
			g = &group{
				hll:    hyperloglog.New14(),
				metric: metric,
			}
			groups[lbs] = g
		}
		g.hll.InsertHash(math.Float64bits(sample.Value))
		_ = e.it.Next()
	}

	out := make(promql.Vector, 0, len(groups))
	for _, g := range groups {
		out = append(out, promql.Sample{
			T:      ts,
			F:      float64(g.hll.Estimate()),
			Metric: g.metric,
		})
	}
	return true, ts, SampleVector(out)
}

func (e *countDistinctStepEvaluator) Close() error { return e.it.Close() }

func (e *countDistinctStepEvaluator) Error() error {
	if e.err != nil {
		return e.err
	}
	return e.it.Err()
}

func (e *countDistinctStepEvaluator) Explain(parent Node) {
	parent.Child("CountDistinct")
}
