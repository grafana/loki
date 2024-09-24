package logql

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/sketch"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	promql_parser "github.com/prometheus/prometheus/promql/parser"
)

const (
	CountMinSketchVectorType = "CountMinSketchVector"

	epsilon = 0.0001
	delta = 0.05
)

type CountMinSketchVector struct {
	T int64
	F *sketch.CountMinSketch

	Metrics []labels.Labels
}

func (CountMinSketchVector) SampleVector() promql.Vector {
	return promql.Vector{}
}

func (CountMinSketchVector) QuantileSketchVec() ProbabilisticQuantileVector {
	return ProbabilisticQuantileVector{}
}

func (v CountMinSketchVector) CountMinSketchVec() CountMinSketchVector {
	return v
}

func (v *CountMinSketchVector) Merge(right *CountMinSketchVector) (*CountMinSketchVector, error) {
	err := v.F.Merge(right.F)
	if err != nil {
		return v, err
	}

	// TODO: merge labels

	return v, nil
}

func (CountMinSketchVector) String() string {
	return "CountMinSketchVector()"
}

func (CountMinSketchVector) Type() promql_parser.ValueType { return CountMinSketchVectorType }

func (v CountMinSketchVector) ToProto() *logproto.CountMinSketchVector {
	p := &logproto.CountMinSketchVector{
		TimestampMs: v.T,
		Metrics:     make([]*logproto.Labels, len(v.Metrics)),
		Sketch: &logproto.CountMinSketch{
			Depth: v.F.Depth,
			Width: v.F.Width,
		},
	}

	// Serialize CMs
	p.Sketch.Counters = make([]uint32, 0, v.F.Depth*v.F.Width)
	for row := uint32(0); row < v.F.Depth; row++ {
		p.Sketch.Counters = append(p.Sketch.Counters, v.F.Counters[row]...)
	}

	// Serialize metric labels
	for i, metric := range v.Metrics {
		p.Metrics[i] = &logproto.Labels{
			Metric: make([]*logproto.LabelPair, len(metric)),
		}
		for j, pair := range metric {
			p.Metrics[i].Metric[j].Name = pair.Name
			p.Metrics[i].Metric[j].Value = pair.Value
		}
	}

	return p
}

// JoinCountMinSketchVector joins the results from stepEvaluator into a CountMinSketchVector.
func JoinCountMinSketchVector(_ bool, r StepResult, stepEvaluator StepEvaluator, params Params) (promql_parser.Value, error) {
	vec := r.CountMinSketchVec()
	if stepEvaluator.Error() != nil {
		return nil, stepEvaluator.Error()
	}

	if GetRangeType(params) != InstantType {
		return nil, fmt.Errorf("count min sketches are only supported on instant queries")
	}

	return vec, nil
}

func newCountMinSketchVectorAggEvaluator(nextEvaluator StepEvaluator, expr *syntax.VectorAggregationExpr) (*countMinSketchVectorAggEvaluator, error) {
	if expr.Grouping.Groups != nil {
		return nil, fmt.Errorf("count min sketch vector aggregation does not support any grouping")
	}

	return &countMinSketchVectorAggEvaluator{
		nextEvaluator: nextEvaluator,
		expr:          expr,
		buf:           make([]byte, 0, 1024),
		lb:            labels.NewBuilder(nil),
	}, nil
}

type countMinSketchVectorAggEvaluator struct {
	nextEvaluator StepEvaluator
	expr          *syntax.VectorAggregationExpr
	buf           []byte
	lb            *labels.Builder
}

func (e *countMinSketchVectorAggEvaluator) Next() (bool, int64, StepResult) {
	next, ts, r := e.nextEvaluator.Next()

	if !next {
		return false, 0, CountMinSketchVector{}
	}
	vec := r.SampleVector()

	f, _ := sketch.NewCountMinSketchFromErroAndProbability(epsilon, delta)

	result := CountMinSketchVector{
		T:       ts,
		F:       f,
		Metrics: make([]labels.Labels, 0, len(vec)),
	}
	for _, s := range vec {
		metric := s.Metric

		result.F.Add(metric.String(), int(s.F))
		result.Metrics = append(result.Metrics, metric) // TODO: this should be sorted and unique
	}
	return next, ts, result
}

func (e *countMinSketchVectorAggEvaluator) Explain(parent Node) {
	b := parent.Child("CountMinSketchVectorAgg")
	e.nextEvaluator.Explain(b)
}

func (e *countMinSketchVectorAggEvaluator) Close() error {
	return e.nextEvaluator.Close()
}

func (e *countMinSketchVectorAggEvaluator) Error() error {
	return e.nextEvaluator.Error()
}

// CountMinSketchVectorStepEvaluator evaluates a count min sketch into a promql.Vector.
type CountMinSketchVectorStepEvaluator struct {
	exhausted bool
	vec       *CountMinSketchVector
}

var _ StepEvaluator = NewQuantileSketchVectorStepEvaluator(nil, 0)

func NewCountMinSketchVectorStepEvaluator(vec *CountMinSketchVector) *CountMinSketchVectorStepEvaluator {
	return &CountMinSketchVectorStepEvaluator{
		exhausted: false,
		vec:       vec,
	}
}

func (e *CountMinSketchVectorStepEvaluator) Next() (bool, int64, StepResult) {
	if e.exhausted {
		return false, 0, SampleVector{}
	}

	vec := make(promql.Vector, len(e.vec.Metrics))

	for i, labels := range e.vec.Metrics {

		f := e.vec.F.Count(labels.String())

		vec[i] = promql.Sample{
			T:      e.vec.T,
			F:      float64(f),
			Metric: labels,
		}
	}

	return true, e.vec.T, SampleVector(vec)
}

func (*CountMinSketchVectorStepEvaluator) Close() error { return nil }

func (*CountMinSketchVectorStepEvaluator) Error() error { return nil }
