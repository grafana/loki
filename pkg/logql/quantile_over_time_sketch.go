package logql

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	promql_parser "github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/sketch"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
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
		groups[sample.Metric.Hash()] = i
	}

	for _, sample := range right {
		i, ok := groups[sample.Metric.Hash()]
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

	err error
}

func (e *QuantileSketchStepEvaluator) Next() (bool, int64, StepResult) {
	next := e.iter.Next()
	if !next {
		return false, 0, ProbabilisticQuantileVector{}
	}
	ts, r := e.iter.At()
	vec := r.QuantileSketchVec()
	for _, s := range vec {
		// Errors are not allowed in metrics unless they've been specifically requested.
		if s.Metric.Has(logqlmodel.ErrorLabel) && s.Metric.Get(logqlmodel.PreserveErrorLabel) != "true" {
			e.err = logqlmodel.NewPipelineErr(s.Metric)
			return false, 0, ProbabilisticQuantileVector{}
		}
	}
	return true, ts, vec
}

func (e *QuantileSketchStepEvaluator) Close() error { return e.iter.Close() }

func (e *QuantileSketchStepEvaluator) Error() error {
	if e.err != nil {
		return e.err
	}
	return e.iter.Error()
}

func (e *QuantileSketchStepEvaluator) Explain(parent Node) {
	parent.Child("QuantileSketch")
}

func newQuantileSketchIterator(
	it iter.PeekingSampleIterator,
	selRange, step, start, end, offset int64,
) RangeVectorIterator {
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
	metric := make([]*logproto.LabelPair, len(q.Metric))
	for i, m := range q.Metric {
		metric[i] = &logproto.LabelPair{Name: m.Name, Value: m.Value}
	}

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
		T:      proto.TimestampMs,
		F:      s,
		Metric: make(labels.Labels, len(proto.Metric)),
	}

	for i, p := range proto.Metric {
		out.Metric[i] = labels.Label{Name: p.Name, Value: p.Value}
	}

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
type QuantileSketchMatrixStepEvaluator struct {
	start, end, ts time.Time
	step           time.Duration
	m              ProbabilisticQuantileMatrix
}

func NewQuantileSketchMatrixStepEvaluator(m ProbabilisticQuantileMatrix, params Params) *QuantileSketchMatrixStepEvaluator {
	var (
		start = params.Start()
		end   = params.End()
		step  = params.Step()
	)
	return &QuantileSketchMatrixStepEvaluator{
		start: start,
		end:   end,
		ts:    start.Add(-step), // will be corrected on first Next() call
		step:  step,
		m:     m,
	}
}

func (m *QuantileSketchMatrixStepEvaluator) Next() (bool, int64, StepResult) {
	m.ts = m.ts.Add(m.step)
	if m.ts.After(m.end) {
		return false, 0, nil
	}

	ts := m.ts.UnixNano() / int64(time.Millisecond)

	if len(m.m) == 0 {
		return false, 0, nil
	}

	vec := m.m[0]

	// Reset for next step
	m.m = m.m[1:]

	return true, ts, vec
}

func (*QuantileSketchMatrixStepEvaluator) Close() error { return nil }

func (*QuantileSketchMatrixStepEvaluator) Error() error { return nil }

func (*QuantileSketchMatrixStepEvaluator) Explain(parent Node) {
	parent.Child("QuantileSketchMatrix")
}

// QuantileSketchVectorStepEvaluator evaluates a quantile sketch into a
// promql.Vector.
type QuantileSketchVectorStepEvaluator struct {
	inner    StepEvaluator
	quantile float64
}

var _ StepEvaluator = NewQuantileSketchVectorStepEvaluator(nil, 0)

func NewQuantileSketchVectorStepEvaluator(inner StepEvaluator, quantile float64) *QuantileSketchVectorStepEvaluator {
	return &QuantileSketchVectorStepEvaluator{
		inner:    inner,
		quantile: quantile,
	}
}

func (e *QuantileSketchVectorStepEvaluator) Next() (bool, int64, StepResult) {
	ok, ts, r := e.inner.Next()
	if !ok {
		return false, 0, SampleVector{}
	}
	quantileSketchVec := r.QuantileSketchVec()

	vec := make(promql.Vector, len(quantileSketchVec))

	for i, quantileSketch := range quantileSketchVec {
		f, _ := quantileSketch.F.Quantile(e.quantile)

		vec[i] = promql.Sample{
			T:      quantileSketch.T,
			F:      f,
			Metric: quantileSketch.Metric,
		}
	}

	return ok, ts, SampleVector(vec)
}

func (*QuantileSketchVectorStepEvaluator) Close() error { return nil }

func (*QuantileSketchVectorStepEvaluator) Error() error { return nil }
