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

// ToProto serializes the vector for frontend/querier transport.
func (v CountDistinctVector) ToProto() (*logproto.CountDistinctVector, error) {
	samples := make([]*logproto.CountDistinctSample, len(v))
	for i, sample := range v {
		p, err := sample.ToProto()
		if err != nil {
			return nil, err
		}
		samples[i] = p
	}
	return &logproto.CountDistinctVector{Samples: samples}, nil
}

// ToProto serializes one sketch sample.
func (s CountDistinctSample) ToProto() (*logproto.CountDistinctSample, error) {
	metric := make([]*logproto.LabelPair, 0, s.Metric.Len())
	s.Metric.Range(func(l labels.Label) {
		metric = append(metric, &logproto.LabelPair{Name: l.Name, Value: l.Value})
	})
	hllBytes, err := s.F.MarshalBinary()
	if err != nil {
		return nil, err
	}
	return &logproto.CountDistinctSample{
		Hyperloglog: hllBytes,
		TimestampMs: s.T,
		Metric:      metric,
	}, nil
}

// CountDistinctVectorFromProto deserializes a CountDistinctVector.
func CountDistinctVectorFromProto(proto *logproto.CountDistinctVector) (CountDistinctVector, error) {
	if proto == nil {
		return CountDistinctVector{}, nil
	}
	out := make(CountDistinctVector, len(proto.Samples))
	for i, sample := range proto.Samples {
		s, err := CountDistinctSampleFromProto(sample)
		if err != nil {
			return nil, err
		}
		out[i] = s
	}
	return out, nil
}

// CountDistinctSampleFromProto deserializes one sketch sample.
func CountDistinctSampleFromProto(proto *logproto.CountDistinctSample) (CountDistinctSample, error) {
	if proto == nil {
		return CountDistinctSample{}, fmt.Errorf("nil CountDistinctSample")
	}
	hll := hyperloglog.New14()
	if err := hll.UnmarshalBinary(proto.Hyperloglog); err != nil {
		return CountDistinctSample{}, err
	}
	builder := labels.NewScratchBuilder(len(proto.Metric))
	for _, pair := range proto.Metric {
		builder.Add(pair.Name, pair.Value)
	}
	return CountDistinctSample{
		T:      proto.TimestampMs,
		F:      hll,
		Metric: builder.Labels(),
	}, nil
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

// CountDistinctMergeExpr concatenates sharded CountDistinctSketchExpr children.
type CountDistinctMergeExpr struct {
	syntax.SampleExpr
	downstreams []DownstreamSampleExpr
}

func (e *CountDistinctMergeExpr) String() string {
	var sb strings.Builder
	for i, d := range e.downstreams {
		if i > 0 {
			sb.WriteString(" ++ ")
		}
		sb.WriteString(d.String())
	}
	return fmt.Sprintf("CountDistinctMerge<%s>", sb.String())
}

func (e *CountDistinctMergeExpr) Walk(f syntax.WalkFn) {
	if !f(e) {
		return
	}
	for _, d := range e.downstreams {
		d.Walk(f)
	}
}

// CountDistinctEvalExpr merges sharded sketches then estimates.
type CountDistinctEvalExpr struct {
	syntax.SampleExpr
	mergeExpr *CountDistinctMergeExpr
}

func (e *CountDistinctEvalExpr) String() string {
	if e.mergeExpr == nil {
		return "CountDistinctEval<>"
	}
	return fmt.Sprintf("CountDistinctEval<%s>", e.mergeExpr.String())
}

func (e *CountDistinctEvalExpr) Walk(f syntax.WalkFn) {
	if !f(e) {
		return
	}
	if e.mergeExpr != nil {
		e.mergeExpr.Walk(f)
	}
}

// CountDistinctVectorStepEvaluator estimates merged sketches into a sample vector.
type CountDistinctVectorStepEvaluator struct {
	vec  CountDistinctVector
	done bool
}

func NewCountDistinctVectorStepEvaluator(vec CountDistinctVector) *CountDistinctVectorStepEvaluator {
	return &CountDistinctVectorStepEvaluator{vec: vec}
}

func (e *CountDistinctVectorStepEvaluator) Next() (bool, int64, StepResult) {
	if e.done {
		return false, 0, SampleVector{}
	}
	e.done = true
	ts := int64(0)
	if len(e.vec) > 0 {
		ts = e.vec[0].T
	}
	return true, ts, e.vec.Estimate()
}

func (*CountDistinctVectorStepEvaluator) Close() error { return nil }
func (*CountDistinctVectorStepEvaluator) Error() error { return nil }
func (e *CountDistinctVectorStepEvaluator) Explain(parent Node) {
	parent.Child("CountDistinctVector")
}
