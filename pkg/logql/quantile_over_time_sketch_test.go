package logql

import (
	"errors"
	"testing"

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
	_, err := JoinQuantileSketchVector(true, result, ev)
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
