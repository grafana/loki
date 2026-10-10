package logql

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/sketch"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

func TestHeapCountMinSketchVectorHeap(t *testing.T) {
	v := NewHeapCountMinSketchVector(0, 0, 3)

	a := labels.FromStrings("event", "a")
	b := labels.FromStrings("event", "b")
	c := labels.FromStrings("event", "c")
	d := labels.FromStrings("event", "d")

	v.Add(a, 2.0)
	v.Add(b, 4.0)
	v.Add(d, 5.5)
	require.Equal(t, "a", v.Metrics[0].Get("event"))

	// Adding c drops a
	v.Add(c, 3.0)
	require.Equal(t, "c", v.Metrics[0].Get("event"))
	require.Len(t, v.Metrics, v.maxLabels)
	require.NotContains(t, v.observed, a.String())

	// Increasing c to 6.0 should make b with 4,0 the smallest
	v.Add(c, 3.0)
	require.Equal(t, "b", v.Metrics[0].Get("event"))

	// Increasing a to 5.0 drops b because it's the smallest
	v.Add(a, 3.0)
	require.Equal(t, "a", v.Metrics[0].Get("event"))
	require.Len(t, v.Metrics, v.maxLabels)
	require.NotContains(t, v.observed, b.String())

	// Verify final list
	final := make([]string, v.maxLabels)
	for i, metric := range v.Metrics {
		final[i] = metric.Get("event")
	}
	require.ElementsMatch(t, []string{"a", "d", "c"}, final)
}

func TestCountMinSketchSerialization(t *testing.T) {
	metric := labels.FromStrings("foo", "bar")
	cms, err := sketch.NewCountMinSketch(4, 2)
	require.NoError(t, err)
	vec := HeapCountMinSketchVector{
		CountMinSketchVector: CountMinSketchVector{
			T: 42,
			F: cms,
		},
		observed:  make(map[uint64]struct{}, 0),
		maxLabels: 10_000,
		buffer:    make([]byte, 0, 1024),
	}
	vec.Add(metric, 42.0)

	hllBytes, _ := vec.F.HyperLogLog.MarshalBinary()
	proto := &logproto.CountMinSketchVector{
		TimestampMs: 42,
		Sketch: &logproto.CountMinSketch{
			Depth:       2,
			Width:       4,
			Counters:    []float64{0, 42, 0, 0, 0, 42, 0, 0},
			Hyperloglog: hllBytes,
		},
		Metrics: []*logproto.Labels{
			{Metric: []*logproto.LabelPair{{Name: "foo", Value: "bar"}}},
		},
	}

	actual, err := vec.ToProto()
	require.NoError(t, err)
	require.Equal(t, proto, actual)

	round, err := CountMinSketchVectorFromProto(actual)
	require.NoError(t, err)

	// The HeapCountMinSketchVector is serialized to a CountMinSketchVector.
	require.Equal(t, round, vec.CountMinSketchVector)
}

func BenchmarkHeapCountMinSketchVectorAdd(b *testing.B) {
	maxLabels := 10_000
	v := NewHeapCountMinSketchVector(0, maxLabels, maxLabels)
	if len(v.Metrics) > maxLabels || cap(v.Metrics) > maxLabels+1 {
		b.Errorf("Length or capcity of metrics is too high: len=%d cap=%d", len(v.Metrics), cap(v.Metrics))
	}

	eventsCount := 100_000
	uniqueEventsCount := 20_000
	events := make([]labels.Labels, eventsCount)
	for i := range events {
		events[i] = labels.FromStrings("event", fmt.Sprintf("%d", i%uniqueEventsCount))
	}

	b.ResetTimer()
	b.ReportAllocs()

	for n := 0; n < b.N; n++ {
		for _, event := range events {
			v.Add(event, rand.Float64())
			if len(v.Metrics) > maxLabels || cap(v.Metrics) > maxLabels+1 {
				b.Errorf("Length or capcity of metrics is too high: len=%d cap=%d", len(v.Metrics), cap(v.Metrics))
			}
		}
	}
}

func TestErrCountMinSketchInstantOnly(t *testing.T) {
	require.EqualError(t, errCountMinSketchInstantOnly(""), "count min sketches are only supported on instant queries")
	require.EqualError(t, errCountMinSketchInstantOnly(syntax.OpTypeApproxTopK), "approx_topk error: count min sketches are only supported on instant queries")
	require.EqualError(t, errCountMinSketchInstantOnly("approx_foo"), "approx_foo error: count min sketches are only supported on instant queries")
}

func TestCountMinSketchEvalStepEvaluator_Next(t *testing.T) {
	instantParams, err := NewLiteralParams(
		`approx_topk(3, count_over_time({foo="bar"}[5m]))`,
		time.Unix(0, 0), time.Unix(0, 0), 0, 0,
		logproto.FORWARD, 1000, nil, nil,
	)
	require.NoError(t, err)

	sampleExpr, err := syntax.ParseSampleExpr(`count_over_time({foo="bar"}[5m])`)
	require.NoError(t, err)
	expr := &CountMinSketchEvalExpr{SampleExpr: sampleExpr}

	t.Run("reports the error when the next step evaluator factory fails", func(t *testing.T) {
		factoryErr := errors.New("failed to build next step evaluator")
		factory := SampleEvaluatorFunc(func(context.Context, SampleEvaluatorFactory, syntax.SampleExpr, Params, bool) (StepEvaluator, error) {
			return nil, factoryErr
		})

		ev, err := NewCountMinSketchEvalStepEvaluator(context.Background(), factory, expr, instantParams)
		require.NoError(t, err)

		ok, ts, _ := ev.Next()
		require.False(t, ok)
		require.Equal(t, int64(0), ts)
		require.ErrorIs(t, ev.Error(), factoryErr)
		require.NoError(t, ev.Close())
	})

	t.Run("reports the error when the next step evaluator is exhausted with an error", func(t *testing.T) {
		nextEvErr := errors.New("next step evaluator failed mid-iteration")
		factory := SampleEvaluatorFunc(func(context.Context, SampleEvaluatorFactory, syntax.SampleExpr, Params, bool) (StepEvaluator, error) {
			return &fakeEvaluator{err: nextEvErr}, nil
		})

		ev, err := NewCountMinSketchEvalStepEvaluator(context.Background(), factory, expr, instantParams)
		require.NoError(t, err)

		ok, _, _ := ev.Next()
		require.False(t, ok)
		require.ErrorIs(t, ev.Error(), nextEvErr)
		require.NoError(t, ev.Close())
	})

	t.Run("reports the error when the next step evaluator succeeds but also reports an error", func(t *testing.T) {
		nextEvErr := errors.New("next step evaluator loaded a partial result")
		factory := SampleEvaluatorFunc(func(context.Context, SampleEvaluatorFactory, syntax.SampleExpr, Params, bool) (StepEvaluator, error) {
			return &fakeEvaluator{ok: true, result: CountMinSketchVector{T: 42}, err: nextEvErr}, nil
		})

		ev, err := NewCountMinSketchEvalStepEvaluator(context.Background(), factory, expr, instantParams)
		require.NoError(t, err)

		ok, _, _ := ev.Next()
		require.False(t, ok)
		require.ErrorIs(t, ev.Error(), nextEvErr)
		require.NoError(t, ev.Close())
	})

	t.Run("reports no error when the next step evaluator is exhausted cleanly", func(t *testing.T) {
		factory := SampleEvaluatorFunc(func(context.Context, SampleEvaluatorFactory, syntax.SampleExpr, Params, bool) (StepEvaluator, error) {
			return &fakeEvaluator{}, nil
		})

		ev, err := NewCountMinSketchEvalStepEvaluator(context.Background(), factory, expr, instantParams)
		require.NoError(t, err)

		ok, _, _ := ev.Next()
		require.False(t, ok)
		require.NoError(t, ev.Error())
		require.NoError(t, ev.Close())
	})

	t.Run("a second call after a factory error stays exhausted and does not rebuild the next step evaluator", func(t *testing.T) {
		factoryErr := errors.New("failed to build next step evaluator")
		calls := 0
		factory := SampleEvaluatorFunc(func(context.Context, SampleEvaluatorFactory, syntax.SampleExpr, Params, bool) (StepEvaluator, error) {
			calls++
			return nil, factoryErr
		})

		ev, err := NewCountMinSketchEvalStepEvaluator(context.Background(), factory, expr, instantParams)
		require.NoError(t, err)

		ok, _, _ := ev.Next()
		require.False(t, ok)
		require.Equal(t, 1, calls)

		ok, ts, _ := ev.Next()
		require.False(t, ok)
		require.Equal(t, int64(0), ts)
		require.Equal(t, 1, calls)
		require.ErrorIs(t, ev.Error(), factoryErr)
	})

	t.Run("a second call after a successful step reports exhaustion instead of recomputing it", func(t *testing.T) {
		calls := 0
		factory := SampleEvaluatorFunc(func(context.Context, SampleEvaluatorFactory, syntax.SampleExpr, Params, bool) (StepEvaluator, error) {
			calls++
			return &fakeEvaluator{ok: true, result: CountMinSketchVector{T: 42}}, nil
		})

		ev, err := NewCountMinSketchEvalStepEvaluator(context.Background(), factory, expr, instantParams)
		require.NoError(t, err)

		ok, ts, _ := ev.Next()
		require.True(t, ok)
		require.Equal(t, int64(42), ts)
		require.Equal(t, 1, calls)

		ok, ts, _ = ev.Next()
		require.False(t, ok)
		require.Equal(t, int64(0), ts)
		require.Equal(t, 1, calls)
		require.NoError(t, ev.Error())
	})

	t.Run("Next closes the next step evaluator exactly once, and Close reports the stored result afterwards", func(t *testing.T) {
		closes := 0
		factory := SampleEvaluatorFunc(func(context.Context, SampleEvaluatorFactory, syntax.SampleExpr, Params, bool) (StepEvaluator, error) {
			return &fakeEvaluator{onClose: func() { closes++ }}, nil
		})

		ev, err := NewCountMinSketchEvalStepEvaluator(context.Background(), factory, expr, instantParams)
		require.NoError(t, err)

		ev.Next()
		require.Equal(t, 1, closes)

		require.NoError(t, ev.Close())
		require.NoError(t, ev.Close())
		require.Equal(t, 1, closes)
	})

	t.Run("Close reports the error from closing the next step evaluator", func(t *testing.T) {
		closeErr := errors.New("failed to close next step evaluator")
		factory := SampleEvaluatorFunc(func(context.Context, SampleEvaluatorFactory, syntax.SampleExpr, Params, bool) (StepEvaluator, error) {
			return &fakeEvaluator{closeErr: closeErr}, nil
		})

		ev, err := NewCountMinSketchEvalStepEvaluator(context.Background(), factory, expr, instantParams)
		require.NoError(t, err)

		ev.Next()
		require.ErrorIs(t, ev.Close(), closeErr)
		require.ErrorIs(t, ev.Close(), closeErr)
	})
}

// fakeEvaluator returns ok/result from Next(), and err, if any, from Error().
type fakeEvaluator struct {
	ok       bool
	result   StepResult
	err      error
	closeErr error
	onClose  func()

	// onNext, if set, runs on every Next() call. A non-nil return becomes err,
	// simulating an evaluator whose error is a side effect of stepping it,
	// rather than one already known beforehand.
	onNext func() error
}

func (e *fakeEvaluator) Next() (bool, int64, StepResult) {
	if e.onNext != nil {
		if err := e.onNext(); err != nil {
			e.err = err
		}
	}
	return e.ok, 0, e.result
}

func (e *fakeEvaluator) Close() error {
	if e.onClose != nil {
		e.onClose()
	}
	return e.closeErr
}

func (e *fakeEvaluator) Error() error { return e.err }
func (e *fakeEvaluator) Explain(Node) {}
