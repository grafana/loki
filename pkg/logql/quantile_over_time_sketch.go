package logql

import (
	"fmt"
	"math"
	"sync"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	promql_parser "github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/sketch"
)

const (
	QuantileSketchMatrixType = "QuantileSketchMatrix"
)

type (
	ProbabilisticQuantileVector []ProbabilisticQuantileSample
	ProbabilisticQuantileMatrix []ProbabilisticQuantileVector
)

var streamHashPool = sync.Pool{
	New: func() interface{} { return make(map[uint64]int) },
}

func (q ProbabilisticQuantileVector) Merge(right ProbabilisticQuantileVector) (ProbabilisticQuantileVector, error) {
	// labels hash to vector index map
	groups := streamHashPool.Get().(map[uint64]int)
	defer func() {
		clear(groups)
		streamHashPool.Put(groups)
	}()
	for i, sample := range q {
		groups[labels.StableHash(sample.Metric)] = i
	}

	for _, sample := range right {
		i, ok := groups[labels.StableHash(sample.Metric)]
		if !ok {
			q = append(q, sample)
			continue
		}

		_, err := q[i].F.Merge(sample.F)
		if err != nil {
			return q, err
		}
	}

	return q, nil
}

func (ProbabilisticQuantileVector) SampleVector() promql.Vector {
	return promql.Vector{}
}

func (q ProbabilisticQuantileVector) QuantileSketchVec() ProbabilisticQuantileVector {
	return q
}

func (ProbabilisticQuantileVector) CountMinSketchVec() CountMinSketchVector {
	return CountMinSketchVector{}
}

func (ProbabilisticQuantileVector) CountDistinctSketchVec() CountDistinctSketchVector {
	return CountDistinctSketchVector{}
}

func (q ProbabilisticQuantileVector) ToProto() *logproto.QuantileSketchVector {
	samples := make([]*logproto.QuantileSketchSample, len(q))
	for i, sample := range q {
		samples[i] = sample.ToProto()
	}
	return &logproto.QuantileSketchVector{Samples: samples}
}

func (q ProbabilisticQuantileVector) Release() {
	for _, s := range q {
		s.F.Release()
	}
}

func ProbabilisticQuantileVectorFromProto(proto *logproto.QuantileSketchVector) (ProbabilisticQuantileVector, error) {
	out := make([]ProbabilisticQuantileSample, len(proto.Samples))
	var s ProbabilisticQuantileSample
	var err error
	for i, sample := range proto.Samples {
		s, err = probabilisticQuantileSampleFromProto(sample)
		if err != nil {
			return ProbabilisticQuantileVector{}, err
		}
		out[i] = s
	}
	return out, nil
}

func (ProbabilisticQuantileMatrix) String() string {
	return "QuantileSketchMatrix()"
}

func (m ProbabilisticQuantileMatrix) Merge(right ProbabilisticQuantileMatrix) (ProbabilisticQuantileMatrix, error) {
	if len(m) != len(right) {
		return nil, fmt.Errorf("failed to merge probabilistic quantile matrix: lengths differ %d!=%d", len(m), len(right))
	}
	var err error
	for i, vec := range m {
		m[i], err = vec.Merge(right[i])
		if err != nil {
			return nil, fmt.Errorf("failed to merge probabilistic quantile matrix: %w", err)
		}
	}

	return m, nil
}

func (ProbabilisticQuantileMatrix) Type() promql_parser.ValueType { return QuantileSketchMatrixType }

func (m ProbabilisticQuantileMatrix) Release() {
	for _, vec := range m {
		vec.Release()
	}
}

func (m ProbabilisticQuantileMatrix) ToProto() *logproto.QuantileSketchMatrix {
	values := make([]*logproto.QuantileSketchVector, len(m))
	for i, vec := range m {
		values[i] = vec.ToProto()
	}
	return &logproto.QuantileSketchMatrix{Values: values}
}

func ProbabilisticQuantileMatrixFromProto(proto *logproto.QuantileSketchMatrix) (ProbabilisticQuantileMatrix, error) {
	out := make([]ProbabilisticQuantileVector, len(proto.Values))
	var s ProbabilisticQuantileVector
	var err error
	for i, v := range proto.Values {
		s, err = ProbabilisticQuantileVectorFromProto(v)
		if err != nil {
			return ProbabilisticQuantileMatrix{}, err
		}
		out[i] = s
	}
	return out, nil
}

type QuantileSketchStepEvaluator struct {
	iter RangeVectorIterator

	// keepsErroredLines reports whether the query asked to keep the samples that carry __error__,
	// so a kept errored sample does not fail the query.
	keepsErroredLines bool

	err error
}

func (e *QuantileSketchStepEvaluator) Next() (bool, int64, StepResult) {
	if e.err != nil {
		return false, 0, ProbabilisticQuantileVector{}
	}

	next := e.iter.Next()
	e.err = e.iter.Error()
	if !next || e.err != nil {
		return false, 0, ProbabilisticQuantileVector{}
	}
	ts, r := e.iter.At()
	vec := r.QuantileSketchVec()
	if err := pipelineErr(e.keepsErroredLines, vec, func(s ProbabilisticQuantileSample) labels.Labels { return s.Metric }); err != nil {
		e.err = err
		return false, 0, ProbabilisticQuantileVector{}
	}
	return true, ts, vec
}

func (e *QuantileSketchStepEvaluator) Close() error { return e.iter.Close() }

func (e *QuantileSketchStepEvaluator) Error() error { return e.err }

func (e *QuantileSketchStepEvaluator) Explain(parent Node) {
	parent.Child("QuantileSketch")
}

func newQuantileSketchIterator(
	it iter.PeekingSampleIterator,
	selRange, step, start, end, offset int64,
) RangeVectorIterator {
	// forces at least one step.
	if step == 0 {
		step = 1
	}
	if offset != 0 {
		start = start - offset
		end = end - offset
	}

	inner := &batchRangeVectorIterator{
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
	return &quantileSketchBatchRangeVectorIterator{
		batchRangeVectorIterator: inner,
	}
}

type ProbabilisticQuantileSample struct {
	T int64
	F sketch.QuantileSketch

	Metric labels.Labels
}

func (q ProbabilisticQuantileSample) ToProto() *logproto.QuantileSketchSample {
	metric := make([]*logproto.LabelPair, 0, q.Metric.Len())
	q.Metric.Range(func(l labels.Label) {
		metric = append(metric, &logproto.LabelPair{Name: l.Name, Value: l.Value})
	})

	sketch := q.F.ToProto()

	return &logproto.QuantileSketchSample{
		F:           sketch,
		TimestampMs: q.T,
		Metric:      metric,
	}
}

func probabilisticQuantileSampleFromProto(proto *logproto.QuantileSketchSample) (ProbabilisticQuantileSample, error) {
	s, err := sketch.QuantileSketchFromProto(proto.F)
	if err != nil {
		return ProbabilisticQuantileSample{}, err
	}
	out := ProbabilisticQuantileSample{
		T: proto.TimestampMs,
		F: s,
	}

	b := labels.NewScratchBuilder(len(proto.Metric))
	for _, p := range proto.Metric {
		b.Add(p.Name, p.Value)
	}
	out.Metric = b.Labels()

	return out, nil
}

type quantileSketchBatchRangeVectorIterator struct {
	*batchRangeVectorIterator
}

func (r *quantileSketchBatchRangeVectorIterator) At() (int64, StepResult) {
	at := make([]ProbabilisticQuantileSample, 0, len(r.window))
	// convert ts from nano to milli seconds as the iterator work with nanoseconds
	ts := r.current/1e+6 + r.offset/1e+6
	for _, series := range r.window {
		at = append(at, ProbabilisticQuantileSample{
			F:      r.agg(series.Floats),
			T:      ts,
			Metric: series.Metric,
		})
	}
	return ts, ProbabilisticQuantileVector(at)
}

func (r *quantileSketchBatchRangeVectorIterator) agg(samples []promql.FPoint) sketch.QuantileSketch {
	s := sketch.NewDDSketch()
	for _, v := range samples {
		// The sketch from the underlying sketch package we are using
		// cannot return an error when calling Add.
		s.Add(v.F) //nolint:errcheck
	}
	return s
}

// JoinQuantileSketchVector joins the results from stepEvaluator into a ProbabilisticQuantileMatrix.
func JoinQuantileSketchVector(next bool, r StepResult, stepEvaluator StepEvaluator, params Params) (promql_parser.Value, error) {
	vec := r.QuantileSketchVec()
	if stepEvaluator.Error() != nil {
		return nil, stepEvaluator.Error()
	}

	if GetRangeType(params) == InstantType {
		return ProbabilisticQuantileMatrix{vec}, nil
	}

	stepCount := int(math.Ceil(float64(params.End().Sub(params.Start()).Nanoseconds()) / float64(params.Step().Nanoseconds())))
	if stepCount <= 0 {
		stepCount = 1
	}

	result := make(ProbabilisticQuantileMatrix, 0, stepCount)

	for next {
		result = append(result, vec)
		next, _, r = stepEvaluator.Next()
		vec = r.QuantileSketchVec()
		if stepEvaluator.Error() != nil {
			return nil, stepEvaluator.Error()
		}
	}

	return result, stepEvaluator.Error()
}

// QuantileSketchMatrixStepEvaluator steps through a matrix of quantile sketch
// vectors, ie t-digest or DDSketch structures per time step.
type QuantileSketchMatrixStepEvaluator = SketchMatrixStepEvaluator[ProbabilisticQuantileVector]

func NewQuantileSketchMatrixStepEvaluator(m ProbabilisticQuantileMatrix, params Params) *QuantileSketchMatrixStepEvaluator {
	return newSketchMatrixStepEvaluator(m, params, "QuantileSketchMatrix")
}

// QuantileSketchVectorStepEvaluator evaluates a quantile sketch into a
// promql.Vector.
type QuantileSketchVectorStepEvaluator struct {
	inner    StepEvaluator
	quantile float64
	err      error
}

var _ StepEvaluator = NewQuantileSketchVectorStepEvaluator(nil, 0)

func NewQuantileSketchVectorStepEvaluator(inner StepEvaluator, quantile float64) *QuantileSketchVectorStepEvaluator {
	return &QuantileSketchVectorStepEvaluator{
		inner:    inner,
		quantile: quantile,
	}
}

func (e *QuantileSketchVectorStepEvaluator) Next() (bool, int64, StepResult) {
	if e.err != nil {
		return false, 0, SampleVector{}
	}

	ok, ts, r := e.inner.Next()
	e.err = e.inner.Error()
	if !ok || e.err != nil {
		return false, 0, SampleVector{}
	}
	quantileSketchVec := r.QuantileSketchVec()

	vec := make(promql.Vector, len(quantileSketchVec))

	for i, quantileSketch := range quantileSketchVec {
		f, err := quantileSketch.F.Quantile(e.quantile)
		if err != nil {
			e.err = err
			return false, 0, SampleVector{}
		}

		vec[i] = promql.Sample{
			T:      quantileSketch.T,
			F:      f,
			Metric: quantileSketch.Metric,
		}
	}

	return true, ts, SampleVector(vec)
}

func (e *QuantileSketchVectorStepEvaluator) Close() error { return e.inner.Close() }

func (e *QuantileSketchVectorStepEvaluator) Error() error { return e.err }
