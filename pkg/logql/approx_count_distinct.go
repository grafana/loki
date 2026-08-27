package logql

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/axiomhq/hyperloglog"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	promql_parser "github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/v3/pkg/iter"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
)

const (
	CountDistinctSketchVectorType = "CountDistinctSketchVector"
	CountDistinctSketchMatrixType = "CountDistinctSketchMatrix"
)

// CountDistinctSketchVector is a list of HyperLogLog sketches keyed by metric labels.
type CountDistinctSketchVector []CountDistinctSketchSample

// CountDistinctSketchMatrix is one CountDistinctSketchVector per evaluation step.
type CountDistinctSketchMatrix []CountDistinctSketchVector

// CountDistinctSketchSample is one HyperLogLog sketch keyed by metric labels.
type CountDistinctSketchSample struct {
	T      int64
	F      *hyperloglog.Sketch
	Metric labels.Labels
}

var countDistinctKeyPool = sync.Pool{
	New: func() interface{} { return make(map[string]int) },
}

// Merge unions sketches that share exact grouping labels.
func (v CountDistinctSketchVector) Merge(right CountDistinctSketchVector) (CountDistinctSketchVector, error) {
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

func (CountDistinctSketchVector) SampleVector() promql.Vector {
	return promql.Vector{}
}

func (CountDistinctSketchVector) QuantileSketchVec() ProbabilisticQuantileVector {
	return ProbabilisticQuantileVector{}
}

func (CountDistinctSketchVector) CountMinSketchVec() CountMinSketchVector {
	return CountMinSketchVector{}
}

func (v CountDistinctSketchVector) CountDistinctSketchVec() CountDistinctSketchVector {
	return v
}

func (CountDistinctSketchVector) String() string {
	return "CountDistinctSketchVector()"
}

func (CountDistinctSketchVector) Type() promql_parser.ValueType { return CountDistinctSketchVectorType }

// ToProto serializes the vector for frontend/querier transport.
func (v CountDistinctSketchVector) ToProto() (*logproto.CountDistinctSketchVector, error) {
	samples := make([]*logproto.CountDistinctSketchSample, len(v))
	for i, sample := range v {
		p, err := sample.ToProto()
		if err != nil {
			return nil, err
		}
		samples[i] = p
	}
	return &logproto.CountDistinctSketchVector{Samples: samples}, nil
}

// CountDistinctSketchVectorFromProto deserializes a CountDistinctSketchVector.
func CountDistinctSketchVectorFromProto(proto *logproto.CountDistinctSketchVector) (CountDistinctSketchVector, error) {
	if proto == nil {
		return CountDistinctSketchVector{}, nil
	}
	out := make(CountDistinctSketchVector, len(proto.Samples))
	for i, sample := range proto.Samples {
		s, err := CountDistinctSketchSampleFromProto(sample)
		if err != nil {
			return nil, err
		}
		out[i] = s
	}
	return out, nil
}

// Estimate converts sketches into a numeric sample vector.
func (v CountDistinctSketchVector) Estimate() SampleVector {
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

func (CountDistinctSketchMatrix) String() string {
	return "CountDistinctSketchMatrix()"
}

func (CountDistinctSketchMatrix) Type() promql_parser.ValueType { return CountDistinctSketchMatrixType }

// Merge unions each step's sketches. Lengths must match so the same evaluation
// time is never combined with another.
func (m CountDistinctSketchMatrix) Merge(right CountDistinctSketchMatrix) (CountDistinctSketchMatrix, error) {
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

func (m CountDistinctSketchMatrix) ToProto() (*logproto.CountDistinctSketchMatrix, error) {
	values := make([]*logproto.CountDistinctSketchVector, len(m))
	for i, vec := range m {
		p, err := vec.ToProto()
		if err != nil {
			return nil, err
		}
		values[i] = p
	}
	return &logproto.CountDistinctSketchMatrix{Values: values}, nil
}

// CountDistinctSketchMatrixFromProto deserializes a CountDistinctSketchMatrix.
func CountDistinctSketchMatrixFromProto(proto *logproto.CountDistinctSketchMatrix) (CountDistinctSketchMatrix, error) {
	if proto == nil {
		return CountDistinctSketchMatrix{}, nil
	}
	out := make(CountDistinctSketchMatrix, len(proto.Values))
	for i, vec := range proto.Values {
		v, err := CountDistinctSketchVectorFromProto(vec)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

// ToProto serializes one sketch sample.
func (s CountDistinctSketchSample) ToProto() (*logproto.CountDistinctSketchSample, error) {
	metric := make([]*logproto.LabelPair, 0, s.Metric.Len())
	s.Metric.Range(func(l labels.Label) {
		metric = append(metric, &logproto.LabelPair{Name: l.Name, Value: l.Value})
	})
	hllBytes, err := s.F.MarshalBinary()
	if err != nil {
		return nil, err
	}
	return &logproto.CountDistinctSketchSample{
		Hyperloglog: hllBytes,
		TimestampMs: s.T,
		Metric:      metric,
	}, nil
}

// CountDistinctSketchSampleFromProto deserializes one sketch sample.
func CountDistinctSketchSampleFromProto(proto *logproto.CountDistinctSketchSample) (CountDistinctSketchSample, error) {
	if proto == nil {
		return CountDistinctSketchSample{}, fmt.Errorf("nil CountDistinctSketchSample")
	}
	hll := hyperloglog.New14()
	if err := hll.UnmarshalBinary(proto.Hyperloglog); err != nil {
		return CountDistinctSketchSample{}, err
	}
	builder := labels.NewScratchBuilder(len(proto.Metric))
	for _, pair := range proto.Metric {
		builder.Add(pair.Name, pair.Value)
	}
	return CountDistinctSketchSample{
		T:      proto.TimestampMs,
		F:      hll,
		Metric: builder.Labels(),
	}, nil
}

// JoinCountDistinctSketchVector joins step sketches into a CountDistinctSketchMatrix.
func JoinCountDistinctSketchVector(next bool, r StepResult, stepEvaluator StepEvaluator, params Params) (promql_parser.Value, error) {
	vec := r.CountDistinctSketchVec()
	if stepEvaluator.Error() != nil {
		return nil, stepEvaluator.Error()
	}

	if GetRangeType(params) == InstantType {
		return CountDistinctSketchMatrix{vec}, nil
	}

	stepCount := int(math.Ceil(float64(params.End().Sub(params.Start()).Nanoseconds()) / float64(params.Step().Nanoseconds())))
	if stepCount <= 0 {
		stepCount = 1
	}

	result := make(CountDistinctSketchMatrix, 0, stepCount)
	for next {
		result = append(result, vec)
		next, _, r = stepEvaluator.Next()
		vec = r.CountDistinctSketchVec()
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
		at := make(CountDistinctSketchVector, 0, len(r.window))
		for _, series := range r.window {
			at = append(at, CountDistinctSketchSample{
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
		return false, 0, CountDistinctSketchVector{}
	}
	ts, r := e.iter.At()
	vec := r.CountDistinctSketchVec()
	for _, s := range vec {
		if s.Metric.Has(logqlmodel.ErrorLabel) && s.Metric.Get(logqlmodel.PreserveErrorLabel) != trueString {
			e.err = logqlmodel.NewPipelineErr(s.Metric)
			return false, 0, CountDistinctSketchVector{}
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
	parent.Child("CountDistinctSketch")
}

// CountDistinctSketchMergeExpr concatenates sharded CountDistinctSketchExpr children.
type CountDistinctSketchMergeExpr struct {
	syntax.SampleExpr
	downstreams []DownstreamSampleExpr
}

func (e *CountDistinctSketchMergeExpr) String() string {
	var sb strings.Builder
	for i, d := range e.downstreams {
		if i >= defaultMaxDepth {
			break
		}

		if i > 0 {
			sb.WriteString(" ++ ")
		}
		sb.WriteString(d.String())
	}
	return fmt.Sprintf("CountDistinctSketchMerge<%s>", sb.String())
}

func (e *CountDistinctSketchMergeExpr) Walk(f syntax.WalkFn) {
	if !f(e) {
		return
	}
	for _, d := range e.downstreams {
		d.Walk(f)
	}
}

// CountDistinctSketchEvalExpr merges sharded sketches then estimates.
type CountDistinctSketchEvalExpr struct {
	syntax.SampleExpr
	mergeExpr *CountDistinctSketchMergeExpr
}

func (e *CountDistinctSketchEvalExpr) String() string {
	if e.mergeExpr == nil {
		return "CountDistinctSketchEval<>"
	}
	return fmt.Sprintf("CountDistinctSketchEval<%s>", e.mergeExpr.String())
}

func (e *CountDistinctSketchEvalExpr) Walk(f syntax.WalkFn) {
	if !f(e) {
		return
	}
	if e.mergeExpr != nil {
		e.mergeExpr.Walk(f)
	}
}

// CountDistinctSketchMatrixStepEvaluator steps through a matrix of count-distinct sketches.
type CountDistinctSketchMatrixStepEvaluator struct {
	end, ts time.Time
	step    time.Duration
	m       CountDistinctSketchMatrix
}

func NewCountDistinctSketchMatrixStepEvaluator(m CountDistinctSketchMatrix, params Params) *CountDistinctSketchMatrixStepEvaluator {
	return &CountDistinctSketchMatrixStepEvaluator{
		end:  params.End(),
		ts:   params.Start().Add(-params.Step()), // corrected on first Next()
		step: params.Step(),
		m:    m,
	}
}

func (m *CountDistinctSketchMatrixStepEvaluator) Next() (bool, int64, StepResult) {
	m.ts = m.ts.Add(m.step)
	if m.ts.After(m.end) {
		return false, 0, nil
	}
	ts := m.ts.UnixNano() / int64(time.Millisecond)
	if len(m.m) == 0 {
		return false, 0, nil
	}
	vec := m.m[0]
	m.m = m.m[1:]
	return true, ts, vec
}

func (*CountDistinctSketchMatrixStepEvaluator) Close() error { return nil }
func (*CountDistinctSketchMatrixStepEvaluator) Error() error { return nil }
func (m *CountDistinctSketchMatrixStepEvaluator) Explain(parent Node) {
	parent.Child("CountDistinctSketchMatrix")
}

// CountDistinctSketchVectorStepEvaluator estimates one sketch vector per step.
type CountDistinctSketchVectorStepEvaluator struct {
	inner StepEvaluator
}

var _ StepEvaluator = NewCountDistinctSketchVectorStepEvaluator(nil)

func NewCountDistinctSketchVectorStepEvaluator(inner StepEvaluator) *CountDistinctSketchVectorStepEvaluator {
	return &CountDistinctSketchVectorStepEvaluator{inner: inner}
}

func (e *CountDistinctSketchVectorStepEvaluator) Next() (bool, int64, StepResult) {
	ok, ts, r := e.inner.Next()
	if !ok {
		return false, 0, SampleVector{}
	}
	return ok, ts, r.CountDistinctSketchVec().Estimate()
}

func (*CountDistinctSketchVectorStepEvaluator) Close() error { return nil }
func (*CountDistinctSketchVectorStepEvaluator) Error() error { return nil }
func (e *CountDistinctSketchVectorStepEvaluator) Explain(parent Node) {
	parent.Child("CountDistinctSketchVector")
}
