package logql

import (
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	promql_parser "github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/pkg/iter"
	"github.com/grafana/loki/pkg/logql/sketch"
	"github.com/grafana/loki/pkg/logqlmodel"
)

type QuantileSketchVector []quantileSketchSample
type QuantileSketchMatrix []QuantileSketchVector

func (left QuantileSketchVector) Merge(right QuantileSketchVector) QuantileSketchVector {
	/*
		var groupingKey uint64
		if e.expr.Grouping.Without {
			groupingKey, e.buf = metric.HashWithoutLabels(e.buf, e.expr.Grouping.Groups...)
		} else {
			groupingKey, e.buf = metric.HashForLabels(e.buf, e.expr.Grouping.Groups...)
		}
	*/
	// labels hash to vector index map
	groups := make(map[uint64]int)
	for i, sample := range left {
		// TODO(karsten): this might be slow.
		groups[sample.Metric.Hash()] = i
	}

	for _, sample := range right {
		i, ok := groups[sample.Metric.Hash()]
		if !ok {
			left = append(left, sample)
			continue
		}

		// TODO(karsten): handle error
		left[i].F.Merge(sample.F) //nolint:errcheck
	}

	return left
}

func (QuantileSketchMatrix) String() string {
	return "TDigestMatrix()"
}

func (QuantileSketchMatrix) Type() promql_parser.ValueType { return "TDigestMatrix" }

type TDigestStepEvaluator struct {
	iter RangeVectorIterator[QuantileSketchVector]

	err error
}

func (r *TDigestStepEvaluator) Next() (bool, int64, QuantileSketchVector) {
	next := r.iter.Next()
	if !next {
		return false, 0, QuantileSketchVector{}
	}
	ts, vec := r.iter.At()
	for _, s := range vec {
		// Errors are not allowed in metrics unless they've been specifically requested.
		if s.Metric.Has(logqlmodel.ErrorLabel) && s.Metric.Get(logqlmodel.PreserveErrorLabel) != "true" {
			r.err = logqlmodel.NewPipelineErr(s.Metric)
			return false, 0, QuantileSketchVector{}
		}
	}
	return true, ts, vec
}

func (r *TDigestStepEvaluator) Close() error { return r.iter.Close() }

func (r *TDigestStepEvaluator) Error() error {
	if r.err != nil {
		return r.err
	}
	return r.iter.Error()
}

func (r *TDigestStepEvaluator) Explain(parent Node) {
	parent.Child("T-Digest")
}

func newTDigestIterator(
	it iter.PeekingSampleIterator,
	selRange, step, start, end, offset int64) RangeVectorIterator[QuantileSketchVector] {
	inner := batchRangeVectorIterator{
		iter:     it,
		step:     step,
		end:      end,
		selRange: selRange,
		metrics:  map[string]labels.Labels{},
		window:   map[string]*promql.Series{},
		agg:      nil,
		current:  start - step, // first loop iteration will set it to start
		offset:   offset,
	}
	return &tdigestBatchRangeVectorIterator{
		batchRangeVectorIterator: inner,
	}
}

//batch

type quantileSketchSample struct {
	T int64
	F sketch.QuantileSketch

	Metric labels.Labels
}

type tdigestBatchRangeVectorIterator struct {
	batchRangeVectorIterator
	at []quantileSketchSample
}

func (r *tdigestBatchRangeVectorIterator) At() (int64, QuantileSketchVector) {
	if r.at == nil {
		r.at = make([]quantileSketchSample, 0, len(r.window))
	}
	r.at = r.at[:0]
	// convert ts from nano to milli seconds as the iterator work with nanoseconds
	ts := r.current/1e+6 + r.offset/1e+6
	for _, series := range r.window {
		r.at = append(r.at, quantileSketchSample{
			F:      r.agg(series.Floats),
			T:      ts,
			Metric: series.Metric,
		})
	}
	return ts, r.at
}

func (r *tdigestBatchRangeVectorIterator) agg(samples []promql.FPoint) sketch.QuantileSketch {
	s := sketch.NewTDigestSketch()
	//s := sketch.NewDDSketch()
	for _, v := range samples {
		s.Add(v.F)
	}
	return s
}

// JoinTDigest joins the results from stepEvaluator into a TDigestMatrix.
func JoinTDigest(stepEvaluator StepEvaluator[QuantileSketchVector], params Params) (promql_parser.Value, error) {
	// TODO(karsten): check if ts should be used
	next, _, vec := stepEvaluator.Next()
	if stepEvaluator.Error() != nil {
		return nil, stepEvaluator.Error()
	}

	result := make([]QuantileSketchVector, 0)

	for next {
		result = append(result, vec)

		next, _, vec = stepEvaluator.Next()
		if stepEvaluator.Error() != nil {
			return nil, stepEvaluator.Error()
		}
	}

	return QuantileSketchMatrix(result), stepEvaluator.Error()
}

// TDigestMatrixStepEvaluator steps through a matrix of tdigest vectors, ie
// sketches per time step.
type TDigestMatrixStepEvaluator struct {
	start, end, ts time.Time
	step           time.Duration
	m              QuantileSketchMatrix
}

func NewTDigestMatrixStepEvaluator(m QuantileSketchMatrix, params Params) *TDigestMatrixStepEvaluator {
	var (
		start = params.Start()
		end   = params.End()
		step  = params.Step()
	)
	return &TDigestMatrixStepEvaluator{
		start: start,
		end:   end,
		ts:    start.Add(-step), // will be corrected on first Next() call
		step:  step,
		m:     m,
	}
}

func (m *TDigestMatrixStepEvaluator) Next() (bool, int64, QuantileSketchVector) {
	m.ts = m.ts.Add(m.step)
	if m.ts.After(m.end) {
		return false, 0, nil
	}

	ts := m.ts.UnixNano() / int64(time.Millisecond)

	// TODO(karsten): test for empty matrix
	vec := m.m[0]

	// Reset for next step
	m.m = m.m[1:]

	return true, ts, vec
}

func (*TDigestMatrixStepEvaluator) Close() error { return nil }

func (*TDigestMatrixStepEvaluator) Error() error { return nil }

func (*TDigestMatrixStepEvaluator) Explain(parent Node) {
	parent.Child("TDigestMatrix")
}

// TDigestMergeStepEvaluator merges multiple tdigest sketches into one for each
// step.
type TDigestMergeStepEvaluator struct {
	evaluators []StepEvaluator[QuantileSketchVector]
}

func NewTDigestMergeStepEvaluator(evaluators []StepEvaluator[QuantileSketchVector]) *TDigestMergeStepEvaluator {
	return &TDigestMergeStepEvaluator{
		evaluators: evaluators,
	}
}

func (e *TDigestMergeStepEvaluator) Next() (bool, int64, QuantileSketchVector) {
	// TODO(karsten): check that we have more than one
	ok, ts, cur := e.evaluators[0].Next()
	for _, eval := range e.evaluators[1:] {
		// TODO(karsten): check ok and ts.
		_, _, vec := eval.Next()
		cur.Merge(vec)
	}

	return ok, ts, cur
}

func (*TDigestMergeStepEvaluator) Close() error { return nil }

func (*TDigestMergeStepEvaluator) Error() error { return nil }

func (e *TDigestMergeStepEvaluator) Explain(parent Node) {
	b := parent.Child("TDigestMerge")
	if len(e.evaluators) < 3 {
		for _, child := range e.evaluators {
			child.Explain(b)
		}
	} else {
		e.evaluators[0].Explain(b)
		b.Child("...")
		e.evaluators[len(e.evaluators)-1].Explain(b)
	}
}

// TDigestVectorStepEvaluator evaluates a tdigest qunatile sketch into a
// promql.Vector.
type TDigestVectorStepEvaluator struct {
	inner    StepEvaluator[QuantileSketchVector]
	quantile float64
}

var _ StepEvaluator[promql.Vector] = NewTDigestVectorStepEvaluator(nil, 0)

func NewTDigestVectorStepEvaluator(inner StepEvaluator[QuantileSketchVector], quantile float64) *TDigestVectorStepEvaluator {
	return &TDigestVectorStepEvaluator{
		inner:    inner,
		quantile: quantile,
	}
}

func (e *TDigestVectorStepEvaluator) Next() (bool, int64, promql.Vector) {
	ok, ts, quantileSketchVec := e.inner.Next()

	vec := make(promql.Vector, len(quantileSketchVec))

	for i, quantileSketch := range quantileSketchVec {
		// TODO(karsten): check error
		f, _ := quantileSketch.F.Quantile(e.quantile)

		vec[i] = promql.Sample{
			T:      quantileSketch.T,
			F:      f,
			Metric: quantileSketch.Metric,
		}
	}

	return ok, ts, vec
}

func (*TDigestVectorStepEvaluator) Close() error { return nil }

func (*TDigestVectorStepEvaluator) Error() error { return nil }

func (e *TDigestVectorStepEvaluator) Explain(parent Node) {
	b := parent.Child("TDigestVector")
	e.inner.Explain(b)
}
