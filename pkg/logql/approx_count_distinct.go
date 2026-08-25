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

const (
	CountDistinctVectorType = "CountDistinctVector"
	CountDistinctMatrixType = "CountDistinctMatrix"
)

// CountDistinctVector is a list of HyperLogLog sketches keyed by metric labels.
type CountDistinctVector []CountDistinctSample

// CountDistinctMatrix is one CountDistinctVector per evaluation step.
type CountDistinctMatrix []CountDistinctVector

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

func (CountDistinctMatrix) String() string {
	return "CountDistinctMatrix()"
}

func (CountDistinctMatrix) Type() promql_parser.ValueType { return CountDistinctMatrixType }

// Merge unions each step's sketches. Lengths must match so the same evaluation
// time is never combined with another.
func (m CountDistinctMatrix) Merge(right CountDistinctMatrix) (CountDistinctMatrix, error) {
	if len(m) != len(right) {
		return nil, fmt.Errorf("failed to merge count distinct matrix: lengths differ %d!=%d", len(m), len(right))
	}
	var err error
	for i, vec := range m {
		m[i], err = vec.Merge(right[i])
		if err != nil {
			return nil, fmt.Errorf("failed to merge count distinct matrix: %w", err)
		}
	}
	return m, nil
}

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

// JoinCountDistinctVector joins step sketches into a CountDistinctMatrix.
func JoinCountDistinctVector(next bool, r StepResult, stepEvaluator StepEvaluator, params Params) (promql_parser.Value, error) {
	vec := r.CountDistinctVec()
	if stepEvaluator.Error() != nil {
		return nil, stepEvaluator.Error()
	}

	if GetRangeType(params) == InstantType {
		return CountDistinctMatrix{vec}, nil
	}

	stepCount := int(math.Ceil(float64(params.End().Sub(params.Start()).Nanoseconds()) / float64(params.Step().Nanoseconds())))
	if stepCount <= 0 {
		stepCount = 1
	}

	result := make(CountDistinctMatrix, 0, stepCount)
	for next {
		result = append(result, vec)
		next, _, r = stepEvaluator.Next()
		vec = r.CountDistinctVec()
		if stepEvaluator.Error() != nil {
			return nil, stepEvaluator.Error()
		}
	}

	return result, stepEvaluator.Error()
}

func newCountDistinctStepEvaluator(
	it iter.PeekingSampleIterator,
	params Params,
	interval, offset time.Duration,
	emitSketch bool,
) StepEvaluator {
	iter := newCountDistinctIterator(
		it,
		interval.Nanoseconds(),
		params.Step().Nanoseconds(),
		params.Start().UnixNano(),
		params.End().UnixNano(),
		offset.Nanoseconds(),
		emitSketch,
	)
	if emitSketch {
		return &countDistinctSketchEvaluator{iter: iter}
	}
	return &RangeVectorEvaluator{iter: iter}
}

func newCountDistinctIterator(
	it iter.PeekingSampleIterator,
	selRange, step, start, end, offset int64,
	emitSketch bool,
) RangeVectorIterator {
	// forces at least one step.
	if step == 0 {
		step = 1
	}
	if offset != 0 {
		start = start - offset
		end = end - offset
	}

	return &countDistinctBatchRangeVectorIterator{
		batchRangeVectorIterator: &batchRangeVectorIterator{
			iter:     it,
			step:     step,
			end:      end,
			selRange: selRange,
			metrics:  map[string]labels.Labels{},
			window:   map[string]*promql.Series{},
			current:  start - step, // first loop iteration will set it to start
			offset:   offset,
		},
		emitSketch: emitSketch,
	}
}

type countDistinctBatchRangeVectorIterator struct {
	*batchRangeVectorIterator
	emitSketch bool
}

func (r *countDistinctBatchRangeVectorIterator) At() (int64, StepResult) {
	// convert ts from nano to milli seconds as the iterator work with nanoseconds
	ts := r.current/1e+6 + r.offset/1e+6
	if r.emitSketch {
		at := make(CountDistinctVector, 0, len(r.window))
		for _, series := range r.window {
			at = append(at, CountDistinctSample{
				F:      countDistinctSketch(series.Floats),
				T:      ts,
				Metric: series.Metric,
			})
		}
		return ts, at
	}
	if r.at == nil {
		r.at = make([]promql.Sample, 0, len(r.window))
	}
	r.at = r.at[:0]
	for _, series := range r.window {
		r.at = append(r.at, promql.Sample{
			F:      float64(countDistinctSketch(series.Floats).Estimate()),
			T:      ts,
			Metric: series.Metric,
		})
	}
	return ts, SampleVector(r.at)
}

func countDistinctSketch(samples []promql.FPoint) *hyperloglog.Sketch {
	hll := hyperloglog.New14()
	for _, sample := range samples {
		hll.InsertHash(math.Float64bits(sample.F))
	}
	return hll
}

// countDistinctSketchEvaluator is RangeVectorEvaluator for HLL sketches.
// SampleVector is empty on that StepResult, so pipeline errors are checked here.
type countDistinctSketchEvaluator struct {
	iter RangeVectorIterator
	err  error
}

func (e *countDistinctSketchEvaluator) Next() (bool, int64, StepResult) {
	next := e.iter.Next()
	if !next {
		return false, 0, CountDistinctVector{}
	}
	ts, r := e.iter.At()
	vec := r.CountDistinctVec()
	for _, s := range vec {
		if s.Metric.Has(logqlmodel.ErrorLabel) && s.Metric.Get(logqlmodel.PreserveErrorLabel) != trueString {
			e.err = logqlmodel.NewPipelineErr(s.Metric)
			return false, 0, CountDistinctVector{}
		}
	}
	return true, ts, vec
}

func (e *countDistinctSketchEvaluator) Close() error { return e.iter.Close() }

func (e *countDistinctSketchEvaluator) Error() error {
	if e.err != nil {
		return e.err
	}
	return e.iter.Error()
}

func (e *countDistinctSketchEvaluator) Explain(parent Node) {
	parent.Child("CountDistinct")
}
