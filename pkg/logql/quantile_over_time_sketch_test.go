package logql

import (
	"errors"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/sketch"
	"github.com/grafana/loki/pkg/logqlmodel"
)

func TestProbabilisticMQuantileMatrixSerialization(t *testing.T) {
	emptySketch := sketch.NewDDSketch()
	ddsketchBytes := make([]byte, 0)
	emptySketch.Encode(&ddsketchBytes, false)

	matrix := ProbabilisticQuantileMatrix([]ProbabilisticQuantileVector{
		[]ProbabilisticQuantileSample{
			{T: 0, F: emptySketch, Metric: []labels.Label{{Name: "foo", Value: "bar"}}},
		},
	})

	proto := &logproto.QuantileSketchMatrix{
		Values: []*logproto.QuantileSketchVector{
			{
				Samples: []*logproto.QuantileSketchSample{
					{
						TimestampMs: 0,
						F:           &logproto.QuantileSketch{Sketch: &logproto.QuantileSketch_Ddsketch{Ddsketch: ddsketchBytes}},
						Metric:      []*logproto.LabelPair{{Name: "foo", Value: "bar"}},
					},
				},
			},
		},
	}

	actual := matrix.ToProto()
	require.Equal(t, proto, actual)

	_, err := ProbabilisticQuantileMatrixFromProto(actual)
	require.NoError(t, err)
}

func TestQuantileSketchStepEvaluatorError(t *testing.T) {
	iter := errorRangeVectorIterator{
		result: ProbabilisticQuantileVector([]ProbabilisticQuantileSample{
			{T: 43, F: nil, Metric: labels.Labels{{Name: logqlmodel.ErrorLabel, Value: "my error"}}},
		}),
	}
	ev := QuantileSketchStepEvaluator{
		iter: iter,
	}
	ok, _, _ := ev.Next()
	require.False(t, ok)

	err := ev.Error()
	require.ErrorContains(t, err, "my error")
}

func TestJoinQuantileSketchVectorError(t *testing.T) {
	result := ProbabilisticQuantileVector{}
	ev := errorStepEvaluator{
		err: errors.New("could not evaluate"),
	}
	_, err := JoinQuantileSketchVector(true, result, ev, LiteralParams{})
	require.ErrorContains(t, err, "could not evaluate")
}

type errorRangeVectorIterator struct {
	err    error
	result StepResult
}

func (e errorRangeVectorIterator) Next() bool {
	return e.result != nil
}

func (e errorRangeVectorIterator) At() (int64, StepResult) {
	return 0, e.result
}

func (errorRangeVectorIterator) Close() error {
	return nil
}

func (e errorRangeVectorIterator) Error() error {
	return e.err
}

type errorStepEvaluator struct {
	err error
}

func (errorStepEvaluator) Next() (ok bool, ts int64, r StepResult) {
	return false, 0, nil
}

func (errorStepEvaluator) Close() error {
	return nil
}

func (e errorStepEvaluator) Error() error {
	return e.err
}

func (e errorStepEvaluator) Explain(Node) {}

func BenchmarkJoinQuantileSketchVector(b *testing.B) {
	results := make([]ProbabilisticQuantileVector, 100)
	for i := range results {
		results[i] = make(ProbabilisticQuantileVector, 10)
		for j := range results[i] {
			results[i][j] = ProbabilisticQuantileSample{
				T:      int64(i),
				F:      newRandomSketch(),
				Metric: []labels.Label{{Name: "foo", Value: fmt.Sprintf("bar-%d", j)}},
			}
		}
	}

	ev := &sliceStepEvaluator{
		slice: results,
		cur:   1,
	}

	// (end - start) / step == len(results)
	params := LiteralParams{
		start: time.Unix(0, 0),
		end:   time.Unix(int64(len(results)), 0),
		step:  time.Second,
	}

	b.ReportAllocs()
	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		// Reset step evaluator
		ev.cur = 1
		r, err := JoinQuantileSketchVector(true, results[0], ev, params)
		require.NoError(b, err)
		r.(ProbabilisticQuantileMatrix).Release()
	}
}

func newRandomSketch() sketch.QuantileSketch {
	r := rand.New(rand.NewSource(42))
	s := sketch.NewDDSketch()
	for i := 0; i < 1000; i++ {
		_ = s.Add(r.Float64())
	}
	return s
}

type sliceStepEvaluator struct {
	err   error
	slice []ProbabilisticQuantileVector
	cur   int
}

// Close implements StepEvaluator.
func (*sliceStepEvaluator) Close() error {
	return nil
}

// Error implements StepEvaluator.
func (ev *sliceStepEvaluator) Error() error {
	return ev.err
}

// Explain implements StepEvaluator.
func (*sliceStepEvaluator) Explain(Node) {}

// Next implements StepEvaluator.
func (ev *sliceStepEvaluator) Next() (ok bool, ts int64, r StepResult) {
	if ev.cur >= len(ev.slice) {
		return false, 0, nil
	}

	r = ev.slice[ev.cur]
	ts = ev.slice[ev.cur][0].T
	ev.cur++
	ok = ev.cur < len(ev.slice)
	return
}
