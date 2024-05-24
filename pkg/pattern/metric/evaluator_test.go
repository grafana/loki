package metric

import (
	"context"
	"testing"

	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
)

func Test_SampleEvaluator(t *testing.T) {
	fiveMin := int64(300000)
	stream := labels.Labels{
		labels.Label{
			Name:  "foo",
			Value: "bar",
		},
		labels.Label{
			Name:  "level",
			Value: "debug",
		},
	}

	setup := func(chunks Chunks, now int64, query string) logql.StepEvaluator {
		factory := NewDefaultEvaluatorFactory(&chunks)

		expr, err := syntax.ParseSampleExpr(query)
		require.NoError(t, err)

		typ, err := ExtractMetricType(expr)
		require.NoError(t, err)

		evaluator, err := factory.NewStepEvaluator(
			context.Background(),
			factory,
			expr.(syntax.SampleExpr),
			typ,
			model.Time(now-fiveMin), model.Time(now), model.Time(fiveMin),
		)

		require.NoError(t, err)
		return evaluator
	}

	chunks := func(now, then, beforeThen int64) Chunks {
		nowTime := model.Time(now)
		thenTime := model.Time(then)
		beforeThenTime := model.Time(beforeThen)
		return Chunks{
			chunks: []Chunk{
				{
					Samples: []MetricSample{
						{
							Timestamp: beforeThenTime,
							Bytes:     1,
							Count:     1,
						},
						{
							Timestamp: thenTime,
							Bytes:     3,
							Count:     2,
						},
						{
							Timestamp: nowTime,
							Bytes:     5,
							Count:     3,
						},
					},
					mint: thenTime.Unix(),
					maxt: nowTime.Unix(),
				},
			},
			labels: stream,
		}
	}

	t.Run("grouping", func(t *testing.T) {
		group := labels.Labels{
			labels.Label{
				Name:  "level",
				Value: "debug",
			},
		}
		t.Run("evenly aligned, non-overlapping timestamps", func(t *testing.T) {
			now := int64(1715964275000)
			then := now - fiveMin        // 1715963975000 -- 5m before n
			beforeThen := then - fiveMin // 1715963675000 -- 5m before then
			chks := chunks(now, then, beforeThen)

			t.Run("count", func(t *testing.T) {
				evaluator := setup(chks, now, `sum by (level)(count_over_time({foo="bar"}[5m]))`)

				resultTs := make([]int64, 3)
				resultVals := make([]float64, 3)

				for i := 0; i < 3; i++ {
					ok, ts, r := evaluator.Next()
					require.True(t, ok)
					require.True(t, then-fiveMin <= ts && ts <= now)

					// The []promqlSample in the Vector is reused, so we need to copy the value and timestamp.
					resultTs[i] = r.SampleVector()[0].T
					resultVals[i] = r.SampleVector()[0].F

					require.Equal(t, group, r.SampleVector()[0].Metric)
				}

				ok, _, _ := evaluator.Next()
				require.False(t, ok)

				require.Equal(t, beforeThen, resultTs[0])
				require.Equal(t, then, resultTs[1])
				require.Equal(t, now, resultTs[2])

				require.Equal(t, float64(1), resultVals[0])
				require.Equal(t, float64(2), resultVals[1])
				require.Equal(t, float64(3), resultVals[2])
			})

			t.Run("bytes", func(t *testing.T) {
				evaluator := setup(chks, now, `sum by (level)(bytes_over_time({foo="bar"}[5m]))`)

				resultTs := make([]int64, 3)
				resultVals := make([]float64, 3)

				for i := 0; i < 3; i++ {
					ok, ts, r := evaluator.Next()
					require.True(t, ok)
					require.True(t, then-fiveMin <= ts && ts <= now)

					// The []promqlSample in the Vector is reused, so we need to copy the value and timestamp.
					resultTs[i] = r.SampleVector()[0].T
					resultVals[i] = r.SampleVector()[0].F

					require.Equal(t, group, r.SampleVector()[0].Metric)
				}

				ok, _, _ := evaluator.Next()
				require.False(t, ok)

				require.Equal(t, beforeThen, resultTs[0])
				require.Equal(t, then, resultTs[1])
				require.Equal(t, now, resultTs[2])

				require.Equal(t, float64(1), resultVals[0])
				require.Equal(t, float64(3), resultVals[1]) // TODO: got 2, expected 3
				require.Equal(t, float64(5), resultVals[2])
			})
		})

		t.Run("evenly aligned, overlapping timestamps", func(t *testing.T) {
			now := int64(1715964275000)
			then := now - 150000        // 1715964125000 -- 2.5m before now
			beforeThen := then - 450000 // 1715963675000 -- 7.5m before then, 10m before now
			chks := chunks(now, then, beforeThen)

			t.Run("count", func(t *testing.T) {
				evaluator := setup(chks, now, `sum by (level)(count_over_time({foo="bar"}[5m]))`)

				resultTs := make([]int64, 3)
				resultVals := make([]float64, 3)

				start := (now - fiveMin - fiveMin) // from - step
				for i := 0; i < 3; i++ {
					ok, ts, r := evaluator.Next()
					require.True(t, ok)
					require.True(t, start <= ts && ts <= now)

					// The []promqlSample in the Vector is reused, so we need to copy the value and timestamp.
					resultTs[i] = r.SampleVector()[0].T
					resultVals[i] = r.SampleVector()[0].F

					require.Equal(t, group, r.SampleVector()[0].Metric)
				}

				ok, _, _ := evaluator.Next()
				require.False(t, ok)

				require.Equal(t, now-600000, resultTs[0])
				require.Equal(t, now-fiveMin, resultTs[1])
				require.Equal(t, now, resultTs[2])

				require.Equal(t, float64(1), resultVals[0])
				require.Equal(t, float64(0), resultVals[1])
				require.Equal(t, float64(5), resultVals[2])
			})

			t.Run("bytes", func(t *testing.T) {
				evaluator := setup(chks, now, `sum by (level)(bytes_over_time({foo="bar"}[5m]))`)

				resultTs := make([]int64, 3)
				resultVals := make([]float64, 3)

				start := (now - fiveMin - fiveMin) // from - step
				for i := 0; i < 3; i++ {
					ok, ts, r := evaluator.Next()
					require.True(t, ok)
					require.True(t, start <= ts && ts <= now)

					// The []promqlSample in the Vector is reused, so we need to copy the value and timestamp.
					resultTs[i] = r.SampleVector()[0].T
					resultVals[i] = r.SampleVector()[0].F

					require.Equal(t, group, r.SampleVector()[0].Metric)
				}

				ok, _, _ := evaluator.Next()
				require.False(t, ok)

				require.Equal(t, now-600000, resultTs[0])
				require.Equal(t, now-fiveMin, resultTs[1])
				require.Equal(t, now, resultTs[2])

				require.Equal(t, float64(1), resultVals[0])
				require.Equal(t, float64(0), resultVals[1])
				require.Equal(t, float64(8), resultVals[2])
			})
		})
	})

	t.Run("without grouping", func(t *testing.T) {
		t.Run("evenly aligned, non-overlapping timestamps", func(t *testing.T) {
			now := int64(1715964275000)
			then := now - fiveMin        // 1715963975000 -- 5m before n
			beforeThen := then - fiveMin // 1715963675000 -- 5m before then
			chks := chunks(now, then, beforeThen)

			t.Run("count", func(t *testing.T) {
				evaluator := setup(chks, now, `count_over_time({foo="bar"}[5m])`)

				resultTs := make([]int64, 3)
				resultVals := make([]float64, 3)

				for i := 0; i < 3; i++ {
					ok, ts, r := evaluator.Next()
					require.True(t, ok)
					require.True(t, then-fiveMin <= ts && ts <= now)

					// The []promqlSample in the Vector is reused, so we need to copy the value and timestamp.
					samples := r.SampleVector()
					resultTs[i] = samples[0].T
					resultVals[i] = samples[0].F

					require.Equal(t, stream, samples[0].Metric)
				}

				ok, _, _ := evaluator.Next()
				require.False(t, ok)

				require.Equal(t, beforeThen, resultTs[0])
				require.Equal(t, then, resultTs[1])
				require.Equal(t, now, resultTs[2])

				require.Equal(t, float64(1), resultVals[0])
				require.Equal(t, float64(2), resultVals[1])
				require.Equal(t, float64(3), resultVals[2])
			})

			t.Run("bytes", func(t *testing.T) {
				evaluator := setup(chks, now, `bytes_over_time({foo="bar"}[5m])`)

				resultTs := make([]int64, 3)
				resultVals := make([]float64, 3)

				for i := 0; i < 3; i++ {
					ok, ts, r := evaluator.Next()
					require.True(t, ok)
					require.True(t, then-fiveMin <= ts && ts <= now)

					// The []promqlSample in the Vector is reused, so we need to copy the value and timestamp.
					samples := r.SampleVector()
					resultTs[i] = samples[0].T
					resultVals[i] = samples[0].F

					require.Equal(t, stream, samples[0].Metric)
				}

				ok, _, _ := evaluator.Next()
				require.False(t, ok)

				require.Equal(t, beforeThen, resultTs[0])
				require.Equal(t, then, resultTs[1])
				require.Equal(t, now, resultTs[2])

				require.Equal(t, float64(1), resultVals[0])
				require.Equal(t, float64(3), resultVals[1])
				require.Equal(t, float64(5), resultVals[2])
			})
		})

		t.Run("evenly aligned, overlapping timestamps", func(t *testing.T) {
			now := int64(1715964275000)
			then := now - 150000        // 1715964125000 -- 2.5m before now
			beforeThen := then - 450000 // 1715963675000 -- 7.5m before then, 10m before now
			chks := chunks(now, then, beforeThen)

			t.Run("count", func(t *testing.T) {
				evaluator := setup(chks, now, `count_over_time({foo="bar"}[5m])`)

				resultTs := make([]int64, 3)
				resultVals := make([]float64, 3)

				start := (now - fiveMin - fiveMin) // from - step
				for i := 0; i < 3; i++ {
					ok, ts, r := evaluator.Next()
					require.True(t, ok)
					require.True(t, start <= ts && ts <= now)

					// The []promqlSample in the Vector is reused, so we need to copy the value and timestamp.
					resultTs[i] = r.SampleVector()[0].T
					resultVals[i] = r.SampleVector()[0].F

					require.Equal(t, stream, r.SampleVector()[0].Metric)
				}

				ok, _, _ := evaluator.Next()
				require.False(t, ok)

				require.Equal(t, now-600000, resultTs[0])
				require.Equal(t, now-fiveMin, resultTs[1])
				require.Equal(t, now, resultTs[2])

				require.Equal(t, float64(1), resultVals[0])
				require.Equal(t, float64(0), resultVals[1])
				require.Equal(t, float64(5), resultVals[2])
			})

			t.Run("bytes", func(t *testing.T) {
				evaluator := setup(chks, now, `bytes_over_time({foo="bar"}[5m])`)

				resultTs := make([]int64, 3)
				resultVals := make([]float64, 3)

				start := (now - fiveMin - fiveMin) // from - step
				for i := 0; i < 3; i++ {
					ok, ts, r := evaluator.Next()
					require.True(t, ok)
					require.True(t, start <= ts && ts <= now)

					// The []promqlSample in the Vector is reused, so we need to copy the value and timestamp.
					resultTs[i] = r.SampleVector()[0].T
					resultVals[i] = r.SampleVector()[0].F

					require.Equal(t, stream, r.SampleVector()[0].Metric)
				}

				ok, _, _ := evaluator.Next()
				require.False(t, ok)

				require.Equal(t, now-600000, resultTs[0])
				require.Equal(t, now-fiveMin, resultTs[1])
				require.Equal(t, now, resultTs[2])

				require.Equal(t, float64(1), resultVals[0])
				require.Equal(t, float64(0), resultVals[1])
				require.Equal(t, float64(8), resultVals[2])
			})
		})
	})
}
