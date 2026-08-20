package logql

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/axiomhq/hyperloglog"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	promql_parser "github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
)

const CountDistinctVectorType = "CountDistinctVector"

// CountDistinctVector is a list of HyperLogLog sketches keyed by metric labels.
type CountDistinctVector []CountDistinctSample

// CountDistinctSample is one HyperLogLog sketch keyed by metric labels.
type CountDistinctSample struct {
	T      int64
	F      *hyperloglog.Sketch
	Metric labels.Labels
}

var countDistinctKeyPool = sync.Pool{
	New: func() interface{} { return make(map[string]int) },
}

// Merge unions sketches that share exact grouping labels.
func (v CountDistinctVector) Merge(right CountDistinctVector) (CountDistinctVector, error) {
	groups := countDistinctKeyPool.Get().(map[string]int)
	defer func() {
		clear(groups)
		countDistinctKeyPool.Put(groups)
	}()
	for i, sample := range v {
		groups[sample.Metric.String()] = i
	}

	for _, sample := range right {
		key := sample.Metric.String()
		i, ok := groups[key]
		if !ok {
			groups[key] = len(v)
			v = append(v, sample)
			continue
		}
		if err := v[i].F.Merge(sample.F); err != nil {
			return v, err
		}
	}
	return v, nil
}

func (CountDistinctVector) SampleVector() promql.Vector {
	return promql.Vector{}
}

func (CountDistinctVector) QuantileSketchVec() ProbabilisticQuantileVector {
	return ProbabilisticQuantileVector{}
}

func (CountDistinctVector) CountMinSketchVec() CountMinSketchVector {
	return CountMinSketchVector{}
}

func (v CountDistinctVector) CountDistinctVec() CountDistinctVector {
	return v
}

func (CountDistinctVector) String() string {
	return "CountDistinctVector()"
}

func (CountDistinctVector) Type() promql_parser.ValueType { return CountDistinctVectorType }

// Estimate converts sketches into a numeric sample vector.
func (v CountDistinctVector) Estimate() SampleVector {
	out := make(promql.Vector, 0, len(v))
	for _, sample := range v {
		out = append(out, promql.Sample{
			T:      sample.T,
			F:      float64(sample.F.Estimate()),
			Metric: sample.Metric,
		})
	}
	return SampleVector(out)
}

// JoinCountDistinctVector materializes an instant sketch vector result.
func JoinCountDistinctVector(_ bool, r StepResult, stepEvaluator StepEvaluator, params Params) (promql_parser.Value, error) {
	vec := r.CountDistinctVec()
	if GetRangeType(params) != InstantType {
		return nil, fmt.Errorf("approx_count_distinct is only supported on instant queries")
	}
	return vec, stepEvaluator.Error()
}

type countDistinctStepEvaluator struct {
	it         iter.PeekingSampleIterator
	params     Params
	interval   time.Duration
	offset     time.Duration
	emitSketch bool
	exhausted  bool
	err        error
}

func newCountDistinctStepEvaluator(
	it iter.PeekingSampleIterator,
	params Params,
	interval, offset time.Duration,
	emitSketch bool,
) (StepEvaluator, error) {
	if GetRangeType(params) != InstantType {
		return nil, fmt.Errorf("approx_count_distinct is only supported on instant queries")
	}
	return &countDistinctStepEvaluator{
		it:         it,
		params:     params,
		interval:   interval,
		offset:     offset,
		emitSketch: emitSketch,
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

	out := make(CountDistinctVector, 0, len(groups))
	for _, g := range groups {
		out = append(out, CountDistinctSample{
			T:      ts,
			F:      g.hll,
			Metric: g.metric,
		})
	}

	if e.emitSketch {
		return true, ts, out
	}
	return true, ts, out.Estimate()
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
