package logql

import (
	"errors"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/sketch"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
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
	_, err := MergeQuantileSketchVector(true, result, ev, LiteralParams{})
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

	selRange := (5 * time.Second).Nanoseconds()
	step := (30 * time.Second)
	offset := int64(0)
	start := time.Unix(10, 0)
	end := time.Unix(100, 0)

	// (end - start) / step == len(results)
	params := LiteralParams{
		start: start,
		end:   end,
		step:  step,
	}

	b.ReportAllocs()
	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		iter := newQuantileSketchIterator(newfakePeekingSampleIterator(samples), selRange, step.Nanoseconds(), start.UnixNano(), end.UnixNano(), offset)
		ev := &QuantileSketchStepEvaluator{
			iter: iter,
		}
		_, _, r := ev.Next()
		m, err := MergeQuantileSketchVector(true, r.QuantileSketchVec(), ev, params)
		require.NoError(b, err)
		m.(ProbabilisticQuantileMatrix).Release()
	}
}

func BenchmarkQuantileBatchRangeVectorIteratorAt(b *testing.B) {
	for _, tc := range []struct {
		numberSamples int64
	}{
		{numberSamples: 1},
		{numberSamples: 1_000},
		{numberSamples: 10_000},
		{numberSamples: 100_000},
		{numberSamples: 1_000_000},
	} {
		b.Run(fmt.Sprintf("%d-samples", tc.numberSamples), func(b *testing.B) {
			r := rand.New(rand.NewSource(42))

			key := "group"
			// similar to Benchmark_RangeVectorIterator
			it := &quantileSketchBatchRangeVectorIterator{
				batchRangeVectorIterator: &batchRangeVectorIterator{
					window: map[string]*promql.Series{
						key: {Floats: make([]promql.FPoint, tc.numberSamples)},
					},
				},
			}
			for i := int64(0); i < tc.numberSamples; i++ {
				it.window[key].Floats[i] = promql.FPoint{T: i, F: r.Float64()}
			}

			b.ResetTimer()
			b.ReportAllocs()

			for n := 0; n < b.N; n++ {
				_, r := it.At()
				r.QuantileSketchVec().Release()
			}
		})
	}
}
