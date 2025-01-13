package logql

import (
	"container/heap"
	"fmt"
	"slices"
	"strings"

	"github.com/axiomhq/hyperloglog"
	"github.com/cespare/xxhash/v2"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	promql_parser "github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/sketch"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

const (
	CountMinSketchVectorType = "CountMinSketchVector"

	epsilon = 0.0001
	// delta of 0.01 results in a sketch size of 27183 * 7 * 4 bytes = 761,124 bytes, 0.05 would yield 543,660 bytes
	delta = 0.01
)

// CountMinSketchVector tracks the count or sum of values of a metric, ie list of label value pairs. It's storage for
// the values is upper bound bu delta and epsilon. To limit the storage for labels see HeapCountMinSketchVector.
// The main use case is for a topk approximation.
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
	// The underlying CMS implementation already merges the HLL sketches that are part of that structure.
	err := v.F.Merge(right.F)
	if err != nil {
		return v, err
	}

	// Merge labels without duplication. Note: the CMS does not limit the number of labels as the
	// HeapCountMinSketchVector does.
	processed := map[string]struct{}{}
	for _, l := range v.Metrics {
		processed[l.String()] = struct{}{}
	}

	for _, r := range right.Metrics {
		if _, duplicate := processed[r.String()]; !duplicate {
			processed[r.String()] = struct{}{}
			v.Metrics = append(v.Metrics, r)
		}
	}

	return v, nil
}

func (CountMinSketchVector) String() string {
	return "CountMinSketchVector()"
}

func (CountMinSketchVector) Type() promql_parser.ValueType { return CountMinSketchVectorType }

func (v CountMinSketchVector) ToProto() (*logproto.CountMinSketchVector, error) {
	p := &logproto.CountMinSketchVector{
		TimestampMs: v.T,
		Metrics:     make([]*logproto.Labels, len(v.Metrics)),
		Sketch: &logproto.CountMinSketch{
			Depth: v.F.Depth,
			Width: v.F.Width,
		},
	}

	// insert the hll sketch
	hllBytes, err := v.F.HyperLogLog.MarshalBinary()
	if err != nil {
		return nil, err
	}
	p.Sketch.Hyperloglog = hllBytes

	// Serialize CMS
	p.Sketch.Counters = make([]float64, 0, v.F.Depth*v.F.Width)
	for row := uint32(0); row < v.F.Depth; row++ {
		p.Sketch.Counters = append(p.Sketch.Counters, v.F.Counters[row]...)
	}

	// Serialize metric labels
	for i, metric := range v.Metrics {
		p.Metrics[i] = &logproto.Labels{
			Metric: make([]*logproto.LabelPair, len(metric)),
		}
		for j, pair := range metric {
			p.Metrics[i].Metric[j] = &logproto.LabelPair{
				Name:  pair.Name,
				Value: pair.Value,
			}
		}
	}

	return p, nil
}

func CountMinSketchVectorFromProto(p *logproto.CountMinSketchVector) (CountMinSketchVector, error) {
	vec := CountMinSketchVector{
		T:       p.TimestampMs,
		Metrics: make([]labels.Labels, len(p.Metrics)),
	}

	// Deserialize CMS
	var err error
	vec.F, err = sketch.NewCountMinSketch(p.Sketch.Width, p.Sketch.Depth)
	if err != nil {
		return vec, err
	}

	hll := hyperloglog.New()
	if err := hll.UnmarshalBinary(p.Sketch.Hyperloglog); err != nil {
		return vec, err
	}
	vec.F.HyperLogLog = hll

	for row := 0; row < int(vec.F.Depth); row++ {
		s := row * int(vec.F.Width)
		e := s + int(vec.F.Width)
		copy(vec.F.Counters[row], p.Sketch.Counters[s:e])
	}

	// Deserialize metric labels
	for i, in := range p.Metrics {
		lbls := make(labels.Labels, len(in.Metric))
		for j, labelPair := range in.Metric {
			lbls[j].Name = labelPair.Name
			lbls[j].Value = labelPair.Value
		}
		vec.Metrics[i] = lbls
	}

	return vec, nil
}

// HeapCountMinSketchVector is a CountMinSketchVector that keeps the number of metrics to a defined maximum.
type HeapCountMinSketchVector struct {
	CountMinSketchVector

	// internal set of observed events
	observed  map[uint64]struct{}
	maxLabels int

	// The buffers are used by `labels.Bytes` similar to `series.Hash` in `codec.MergeResponse`. They are alloccated
	// outside of the method in order to reuse them for the next `Add` call. This saves a lot of allocations.
	// 1KB is used for `b` after some experimentation. Reusing the buffer is not thread safe.
	buffer []byte
}

func NewHeapCountMinSketchVector(ts int64, metricsLength, maxLabels int) HeapCountMinSketchVector {
	f, _ := sketch.NewCountMinSketchFromErrorAndProbability(epsilon, delta)

	if metricsLength >= maxLabels {
		metricsLength = maxLabels
	}

	return HeapCountMinSketchVector{
		CountMinSketchVector: CountMinSketchVector{
			T:       ts,
			F:       f,
			Metrics: make([]labels.Labels, 0, metricsLength+1),
		},
		observed:  make(map[uint64]struct{}),
		maxLabels: maxLabels,
		buffer:    make([]byte, 0, 1024),
	}
}

func (v *HeapCountMinSketchVector) Add(metric labels.Labels, value float64) {
	slices.SortFunc(metric, func(a, b labels.Label) int { return strings.Compare(a.Name, b.Name) })
	v.buffer = metric.Bytes(v.buffer)

	v.F.Add(v.buffer, value)

	// TODO(karsten): There is a chance that the ids match but not the labels due to hash collision. Ideally there's
	// an else block the compares the series labels. However, that's not trivial. Besides, instance.Series has the
	// same issue in its deduping logic.
	id := xxhash.Sum64(v.buffer)

	// Add our metric if we haven't seen it
	if _, ok := v.observed[id]; !ok {
		heap.Push(v, metric)
		v.observed[id] = struct{}{}
	} else if labels.Equal(v.Metrics[0], metric) {
		// The smallest element has been updated to fix the heap.
		heap.Fix(v, 0)
	}

	// The maximum number of labels has been reached, so drop the smallest element.
	if len(v.Metrics) > v.maxLabels {
		metric := heap.Pop(v).(labels.Labels)
		v.buffer = metric.Bytes(v.buffer)
		id := xxhash.Sum64(v.buffer)
		delete(v.observed, id)
	}
}

func (v HeapCountMinSketchVector) Len() int {
	return len(v.Metrics)
}

func (v HeapCountMinSketchVector) Less(i, j int) bool {
	v.buffer = v.Metrics[i].Bytes(v.buffer)
	left := v.F.Count(v.buffer)

	v.buffer = v.Metrics[j].Bytes(v.buffer)
	right := v.F.Count(v.buffer)
	return left < right
}

func (v HeapCountMinSketchVector) Swap(i, j int) {
	v.Metrics[i], v.Metrics[j] = v.Metrics[j], v.Metrics[i]
}

func (v *HeapCountMinSketchVector) Push(x any) {
	v.Metrics = append(v.Metrics, x.(labels.Labels))
}

func (v *HeapCountMinSketchVector) Pop() any {
	old := v.Metrics
	n := len(old)
	x := old[n-1]
	v.Metrics = old[0 : n-1]
	return x
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

func newCountMinSketchVectorAggEvaluator(nextEvaluator StepEvaluator, expr *syntax.VectorAggregationExpr, maxLabels int) (*countMinSketchVectorAggEvaluator, error) {
	if expr.Grouping.Groups != nil {
		return nil, fmt.Errorf("count min sketch vector aggregation does not support any grouping")
	}

	return &countMinSketchVectorAggEvaluator{
		nextEvaluator: nextEvaluator,
		expr:          expr,
		buf:           make([]byte, 0, 1024),
		lb:            labels.NewBuilder(nil),
		maxLabels:     maxLabels,
	}, nil
}

// countMinSketchVectorAggEvaluator processes sample vectors and aggregates them in a count min sketch with a heap.
type countMinSketchVectorAggEvaluator struct {
	nextEvaluator StepEvaluator
	expr          *syntax.VectorAggregationExpr
	buf           []byte
	lb            *labels.Builder
	maxLabels     int
}

func (e *countMinSketchVectorAggEvaluator) Next() (bool, int64, StepResult) {
	next, ts, r := e.nextEvaluator.Next()

	if !next {
		return false, 0, CountMinSketchVector{}
	}
	vec := r.SampleVector()

	result := NewHeapCountMinSketchVector(ts, len(vec), e.maxLabels)
	for _, s := range vec {
		result.Add(s.Metric, s.F)
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

	// The buffers are used by `labels.Bytes` similar to `series.Hash` in `codec.MergeResponse`. They are alloccated
	// outside of the method in order to reuse them for the next `Next` call. This saves a lot of allocations.
	// 1KB is used for `b` after some experimentation. Reusing the buffer is not thread safe.
	buffer []byte
}

var _ StepEvaluator = NewQuantileSketchVectorStepEvaluator(nil, 0)

func NewCountMinSketchVectorStepEvaluator(vec *CountMinSketchVector) *CountMinSketchVectorStepEvaluator {
	return &CountMinSketchVectorStepEvaluator{
		exhausted: false,
		vec:       vec,
		buffer:    make([]byte, 0, 1024),
	}
}

func (e *CountMinSketchVectorStepEvaluator) Next() (bool, int64, StepResult) {
	if e.exhausted {
		return false, 0, SampleVector{}
	}

	vec := make(promql.Vector, len(e.vec.Metrics))

	for i, labels := range e.vec.Metrics {

		e.buffer = labels.Bytes(e.buffer)
		f := e.vec.F.Count(e.buffer)

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
