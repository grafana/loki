package logql

import (
	"context"
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
	"github.com/grafana/loki/v3/pkg/querier/plan"
)

const CountDistinctVectorType = "CountDistinctVector"

// CountDistinctVector is a list of HLL sketches keyed by metric labels.
type CountDistinctVector []CountDistinctSample

// CountDistinctSample is one HyperLogLog sketch keyed by metric labels.
type CountDistinctSample struct {
	T      int64
	F      *hyperloglog.Sketch
	Metric labels.Labels
}

var countDistinctHashPool = sync.Pool{
	New: func() interface{} { return make(map[uint64]int) },
}

func (v CountDistinctVector) Merge(right CountDistinctVector) (CountDistinctVector, error) {
	groups := countDistinctHashPool.Get().(map[uint64]int)
	defer func() {
		clear(groups)
		countDistinctHashPool.Put(groups)
	}()
	for i, sample := range v {
		groups[labels.StableHash(sample.Metric)] = i
	}

	for _, sample := range right {
		i, ok := groups[labels.StableHash(sample.Metric)]
		if !ok {
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
		s, err := countDistinctSampleFromProto(sample)
		if err != nil {
			return nil, err
		}
		out[i] = s
	}
	return out, nil
}

func countDistinctSampleFromProto(proto *logproto.CountDistinctSample) (CountDistinctSample, error) {
	sk := hyperloglog.New14()
	if err := sk.UnmarshalBinary(proto.Hyperloglog); err != nil {
		return CountDistinctSample{}, err
	}
	b := labels.NewScratchBuilder(len(proto.Metric))
	for _, p := range proto.Metric {
		b.Add(p.Name, p.Value)
	}
	return CountDistinctSample{
		T:      proto.TimestampMs,
		F:      sk,
		Metric: b.Labels(),
	}, nil
}

// JoinCountDistinctVector joins step results into a CountDistinctVector.
// Instant queries only.
func JoinCountDistinctVector(_ bool, r StepResult, stepEvaluator StepEvaluator, params Params) (promql_parser.Value, error) {
	vec := r.CountDistinctVec()
	if stepEvaluator.Error() != nil {
		return nil, stepEvaluator.Error()
	}
	if GetRangeType(params) != InstantType {
		return nil, fmt.Errorf("approx_count_distinct is only supported on instant queries")
	}
	return vec, nil
}

// countDistinctSketchEvaluator builds one HLL per series while scanning samples.
type countDistinctSketchEvaluator struct {
	it         iter.SampleIterator
	ts         int64
	vec        CountDistinctVector
	err        error
	done       bool
	sketchOnly bool
	groups     map[uint64]*hyperloglog.Sketch
	metrics    map[uint64]labels.Labels
}

func newCountDistinctSketchEvaluator(it iter.SampleIterator, params Params, sketchOnly bool) *countDistinctSketchEvaluator {
	return &countDistinctSketchEvaluator{
		it:         it,
		ts:         params.End().UnixMilli(),
		sketchOnly: sketchOnly,
		groups:     make(map[uint64]*hyperloglog.Sketch),
		metrics:    make(map[uint64]labels.Labels),
	}
}

func (e *countDistinctSketchEvaluator) Next() (bool, int64, StepResult) {
	if e.done {
		return false, 0, CountDistinctVector{}
	}
	e.done = true

	for e.it.Next() {
		sample := e.it.At()
		lbs, err := syntax.ParseLabels(e.it.Labels())
		if err != nil {
			e.err = err
			return false, 0, CountDistinctVector{}
		}
		h := labels.StableHash(lbs)
		sk, ok := e.groups[h]
		if !ok {
			sk = hyperloglog.New14()
			e.groups[h] = sk
			e.metrics[h] = lbs
		}
		sk.InsertHash(math.Float64bits(sample.Value))
	}
	if err := e.it.Err(); err != nil {
		e.err = err
		return false, 0, CountDistinctVector{}
	}

	e.vec = make(CountDistinctVector, 0, len(e.groups))
	for h, sk := range e.groups {
		e.vec = append(e.vec, CountDistinctSample{
			T:      e.ts,
			F:      sk,
			Metric: e.metrics[h],
		})
	}
	if e.sketchOnly {
		return true, e.ts, e.vec
	}
	ts, out := countDistinctVectorToSampleVector(e.vec)
	return true, ts, out
}

func (e *countDistinctSketchEvaluator) Close() error {
	return e.it.Close()
}

func (e *countDistinctSketchEvaluator) Error() error { return e.err }

func (e *countDistinctSketchEvaluator) Explain(parent Node) {
	parent.Child("CountDistinctSketch")
}

// CountDistinctVectorStepEvaluator materializes HLL estimates into a promql.Vector.
type CountDistinctVectorStepEvaluator struct {
	exhausted bool
	vec       CountDistinctVector
}

// NewCountDistinctVectorStepEvaluator creates a step evaluator that emits estimates.
func NewCountDistinctVectorStepEvaluator(vec CountDistinctVector) *CountDistinctVectorStepEvaluator {
	return &CountDistinctVectorStepEvaluator{vec: vec}
}

func countDistinctVectorToSampleVector(vec CountDistinctVector) (int64, SampleVector) {
	out := make(promql.Vector, len(vec))
	var ts int64
	for i, s := range vec {
		ts = s.T
		out[i] = promql.Sample{
			T:      s.T,
			F:      float64(s.F.Estimate()),
			Metric: s.Metric,
		}
	}
	return ts, SampleVector(out)
}

func (e *CountDistinctVectorStepEvaluator) Next() (bool, int64, StepResult) {
	if e.exhausted {
		return false, 0, SampleVector{}
	}
	e.exhausted = true
	ts, out := countDistinctVectorToSampleVector(e.vec)
	return true, ts, out
}

func (*CountDistinctVectorStepEvaluator) Close() error { return nil }
func (*CountDistinctVectorStepEvaluator) Error() error { return nil }
func (e *CountDistinctVectorStepEvaluator) Explain(parent Node) {
	parent.Child("CountDistinctVector")
}

// CountDistinctEvalExpr materializes merged HLL sketches into a vector of estimates.
type CountDistinctEvalExpr struct {
	syntax.SampleExpr
	mergeExpr *CountDistinctMergeExpr
}

func (e CountDistinctEvalExpr) String() string {
	if e.mergeExpr != nil {
		return fmt.Sprintf("CountDistinctEval<%s>", e.mergeExpr.String())
	}
	if e.SampleExpr != nil {
		return fmt.Sprintf("CountDistinctEval<%s>", e.SampleExpr.String())
	}
	return "CountDistinctEval<>"
}

func (e *CountDistinctEvalExpr) Walk(f syntax.WalkFn) {
	if !f(e) {
		return
	}
	if e.SampleExpr != nil {
		e.SampleExpr.Walk(f)
	}
	if e.mergeExpr != nil {
		e.mergeExpr.Walk(f)
	}
}

// CountDistinctMergeExpr merges per-shard ApproxCountDistinctExpr results.
type CountDistinctMergeExpr struct {
	syntax.SampleExpr
	downstreams []DownstreamSampleExpr
}

func (e CountDistinctMergeExpr) String() string {
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

// CountDistinctEvalStepEvaluator evaluates a local ApproxCountDistinctExpr and
// materializes HLL estimates.
type CountDistinctEvalStepEvaluator struct {
	ctx           context.Context
	nextEvFactory SampleEvaluatorFactory
	expr          *CountDistinctEvalExpr
	params        Params
	err           error
}

// NewCountDistinctEvalStepEvaluator creates a CountDistinctEvalStepEvaluator.
func NewCountDistinctEvalStepEvaluator(ctx context.Context, nextEvFactory SampleEvaluatorFactory, expr *CountDistinctEvalExpr, params Params) (*CountDistinctEvalStepEvaluator, error) {
	if GetRangeType(params) != InstantType {
		return nil, fmt.Errorf("approx_count_distinct is only supported on instant queries")
	}
	return &CountDistinctEvalStepEvaluator{
		ctx:           ctx,
		nextEvFactory: nextEvFactory,
		expr:          expr,
		params:        params,
	}, nil
}

func (e *CountDistinctEvalStepEvaluator) Next() (bool, int64, StepResult) {
	if e.expr.SampleExpr == nil {
		return false, 0, SampleVector{}
	}
	nextEv, err := e.nextEvFactory.NewStepEvaluator(e.ctx, e.nextEvFactory, e.expr.SampleExpr, e.params)
	if err != nil {
		e.err = err
		return false, 0, SampleVector{}
	}
	ok, _, results := nextEv.Next()
	if !ok {
		e.err = nextEv.Error()
		return false, 0, SampleVector{}
	}
	return NewCountDistinctVectorStepEvaluator(results.CountDistinctVec()).Next()
}

func (*CountDistinctEvalStepEvaluator) Close() error { return nil }
func (e *CountDistinctEvalStepEvaluator) Error() error { return e.err }
func (*CountDistinctEvalStepEvaluator) Explain(parent Node) {
	parent.Child("CountDistinctEval")
}

func newApproxCountDistinctStepEvaluator(ctx context.Context, querier Querier, expr *syntax.ApproxCountDistinctExpr, q Params, maxLookBackPeriod time.Duration) (StepEvaluator, error) {
	if GetRangeType(q) != InstantType {
		return nil, fmt.Errorf("approx_count_distinct is only supported on instant queries")
	}
	start := q.Start().Add(-maxLookBackPeriod)
	it, err := querier.SelectSamples(ctx, SelectSampleParams{
		&logproto.SampleQueryRequest{
			Start:    start,
			End:      q.End().Add(time.Nanosecond),
			Selector: expr.String(),
			Shards:   q.Shards(),
			Plan: &plan.QueryPlan{
				AST: expr,
			},
			StoreChunks: q.GetStoreChunks(),
		},
	})
	if err != nil {
		return nil, err
	}
	return newCountDistinctSketchEvaluator(it, q, expr.SketchOnly), nil
}
