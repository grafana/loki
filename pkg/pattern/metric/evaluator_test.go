package metric

import (
	"context"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

func Test_SampleEvaluator(t *testing.T) {
	fiveMinMs := int64(300000)
	tenSecMs := int64(1e4)
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

	setup := func(chunks Chunks, from, through, step int64, query string) logql.StepEvaluator {
		factory := NewDefaultEvaluatorFactory(&chunks)

		expr, err := syntax.ParseSampleExpr(query)
		require.NoError(t, err)

		evaluator, err := factory.NewStepEvaluator(
			context.Background(),
			factory,
			expr,
			// add 10s to the end to include chunks @ now
			model.Time(from), model.Time(through), model.Time(fiveMinMs),
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
					Samples: []Sample{
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
			now := int64(600000)           // 10 minutes after 0
			then := now - fiveMinMs        // 300000, 5m before now
			beforeThen := then - fiveMinMs // 0, 5m before then
			chks := chunks(now, then, beforeThen)

			t.Run("count", func(t *testing.T) {
				evaluator := setup(chks, beforeThen, now+1e4, fiveMinMs, `sum by (level)(count_over_time({foo="bar"}[5m]))`)

				resultTs := make([]int64, 3)
				resultVals := make([]float64, 3)

				for i := 0; i < 3; i++ {
					ok, ts, r := evaluator.Next()
					require.True(t, ok)
					require.True(t, then-fiveMinMs <= ts && ts <= now)

					// The []promqlSample in the Vector is reused, so we need to copy the value and timestamp.
					vec := r.SampleVector()
					require.Equal(t, 1, len(vec))
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
				evaluator := setup(chks, beforeThen, now+tenSecMs, fiveMinMs, `sum by (level)(bytes_over_time({foo="bar"}[5m]))`)

				resultTs := make([]int64, 3)
				resultVals := make([]float64, 3)

				for i := 0; i < 3; i++ {
					ok, ts, r := evaluator.Next()
					require.True(t, ok)
					require.True(t, then-fiveMinMs <= ts && ts <= now)

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
			now := int64(150000 + 450000) // 600000, 10m after 0
			then := now - 150000          // 450000 -- 2.5m before now
			beforeThen := then - 450000   // 0 -- 7.5m before then, 10m before now
			chks := chunks(now, then, beforeThen)

			t.Run("count", func(t *testing.T) {
				evaluator := setup(chks, beforeThen, now+1e4, fiveMinMs, `sum by (level)(count_over_time({foo="bar"}[5m]))`)

				resultTs := make([]int64, 0, 2)
				resultVals := make([]float64, 0, 2)

				start := (beforeThen - fiveMinMs) // from - step

				// Datapoints(ts -> val):
				// 0 -> 1
				// 450000 -> 2
				// 600000 -> 3
				//
				// the selection range logic is (start, end] so we expect the
				// second window of 0 to 300000 to be empty
				//
				// 0 -> range -30000 to 0, ts: 0, val: 1
				// 1 -> range 0 to 300000, val: empty
				// 2 -> range 30000 to 610000, val: 5 (includes last 2 datapoints)
				for i := 0; i < 3; i++ {
					ok, ts, r := evaluator.Next()
					require.True(t, ok)
					require.True(t, start <= ts && ts < now+tenSecMs)

					if i == 1 {
						require.Equal(t, 0, len(r.SampleVector()))
					} else {
						// The []promqlSample in the Vector is reused, so we need to copy the value and timestamp.
						resultTs = append(resultTs, r.SampleVector()[0].T)
						resultVals = append(resultVals, r.SampleVector()[0].F)
						require.Equal(t, group, r.SampleVector()[0].Metric)
					}
				}

				ok, _, _ := evaluator.Next()
				require.False(t, ok)

				// Because of the 5m step and 5m lookback, the first window will be
				// -300000 to 0, the second 0 to 300000, and the third 300000 to 600000.
				// We don't expect the 2nd to have any data, so below we check results of the 1st and 3rd.
				require.Equal(t, beforeThen, resultTs[0])
				require.Equal(t, beforeThen+fiveMinMs+fiveMinMs, resultTs[1])

				require.Equal(t, float64(1), resultVals[0])
				require.Equal(t, float64(5), resultVals[1])
			})

			t.Run("bytes", func(t *testing.T) {
				evaluator := setup(chks, beforeThen, now+1e4, fiveMinMs, `sum by (level)(bytes_over_time({foo="bar"}[5m]))`)

				resultTs := make([]int64, 0, 2)
				resultVals := make([]float64, 0, 2)

				start := (beforeThen - fiveMinMs) // from - step

				// Datapoints(ts -> val):
				// 0 -> 1
				// 450000 -> 3
				// 600000 -> 5
				//
				// the selection range logic is (start, end] so we expect the
				// second window of 0 to 300000 to be empty
				//
				// 0 -> range -30000 to 0, ts: 0, val: 1
				// 1 -> range 0 to 300000, val: empty
				// 2 -> range 30000 to 610000, val: 8 (includes last 2 datapoints)
				for i := 0; i < 3; i++ {
					ok, ts, r := evaluator.Next()
					require.True(t, ok)
					require.True(t, start <= ts && ts <= now)

					if i == 1 {
						require.Equal(t, 0, len(r.SampleVector()))
					} else {
						// The []promqlSample in the Vector is reused, so we need to copy the value and timestamp.
						resultTs = append(resultTs, r.SampleVector()[0].T)
						resultVals = append(resultVals, r.SampleVector()[0].F)
						require.Equal(t, group, r.SampleVector()[0].Metric)
					}
				}

				ok, _, _ := evaluator.Next()
				require.False(t, ok)

				// Because of the 5m step and 5m lookback, the first window will be
				// -300000 to 0, the second 0 to 300000, and the third 300000 to 600000.
				// We don't expect the 2nd to have any data, so below we check results of the 1st and 3rd.
				require.Equal(t, now-600000, resultTs[0])
				require.Equal(t, now, resultTs[1])

				require.Equal(t, float64(1), resultVals[0])
				require.Equal(t, float64(8), resultVals[1])
			})
		})
	})

	t.Run("without grouping", func(t *testing.T) {
		t.Run("evenly aligned, non-overlapping timestamps", func(t *testing.T) {
			now := int64(600000)           // 10 minutes after 0
			then := now - fiveMinMs        // 300000, 5m before now
			beforeThen := then - fiveMinMs // 0, 5m before then
			chks := chunks(now, then, beforeThen)

			t.Run("count", func(t *testing.T) {
				evaluator := setup(chks, beforeThen, now+tenSecMs, fiveMinMs, `count_over_time({foo="bar"}[5m])`)

				resultTs := make([]int64, 3)
				resultVals := make([]float64, 3)

				for i := 0; i < 3; i++ {
					ok, ts, r := evaluator.Next()
					require.True(t, ok)
					require.True(t, then-fiveMinMs <= ts && ts <= now)

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
				evaluator := setup(chks, beforeThen, now+1e4, fiveMinMs, `bytes_over_time({foo="bar"}[5m])`)

				resultTs := make([]int64, 3)
				resultVals := make([]float64, 3)

				start := beforeThen - fiveMinMs // from - step
				for i := 0; i < 3; i++ {
					ok, ts, r := evaluator.Next()
					require.True(t, ok)
					require.True(t, start <= ts && ts < now+fiveMinMs)

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
			now := int64(150000 + 450000) // 600000
			then := now - 150000          // 450000 -- 2.5m before now
			beforeThen := then - 450000   // 0 -- 7.5m before then, 10m before now
			chks := chunks(now, then, beforeThen)

			t.Run("count", func(t *testing.T) {
				evaluator := setup(chks, beforeThen, now+tenSecMs, fiveMinMs, `count_over_time({foo="bar"}[5m])`)

				resultTs := make([]int64, 0, 2)
				resultVals := make([]float64, 0, 2)

				start := beforeThen - fiveMinMs // from - step
				// Datapoints(ts -> val):
				// 0 -> 1
				// 450000 -> 2
				// 600000 -> 3
				//
				// the selection range logic is (start, end] so we expect the
				// second window of 0 to 300000 to be empty
				//
				// 0 -> range -30000 to 0, ts: 0, val: 1
				// 1 -> range 0 to 300000, val: empty
				// 2 -> range 30000 to 610000, val: 5 (includes last 2 datapoints)
				for i := 0; i < 3; i++ {
					ok, ts, r := evaluator.Next()
					require.True(t, ok)
					require.True(t, start <= ts && ts < now+tenSecMs)

					if i == 1 {
						require.Equal(t, 0, len(r.SampleVector()))
					} else {
						// The []promqlSample in the Vector is reused, so we need to copy the value and timestamp.
						resultTs = append(resultTs, r.SampleVector()[0].T)
						resultVals = append(resultVals, r.SampleVector()[0].F)
						require.Equal(t, stream, r.SampleVector()[0].Metric)
					}
				}

				ok, _, _ := evaluator.Next()
				require.False(t, ok)

				// Because of the 5m step and 5m lookback, the first window will be
				// -300000 to 0, the second 0 to 300000, and the third 300000 to 600000.
				// We don't expect the 2nd to have any data, so below we check results of the 1st and 3rd.
				require.Equal(t, now-600000, resultTs[0])
				require.Equal(t, now, resultTs[1])

				require.Equal(t, float64(1), resultVals[0])
				require.Equal(t, float64(5), resultVals[1])
			})

			t.Run("bytes", func(t *testing.T) {
				evaluator := setup(chks, beforeThen, now+tenSecMs, fiveMinMs, `bytes_over_time({foo="bar"}[5m])`)

				resultTs := make([]int64, 0, 2)
				resultVals := make([]float64, 0, 2)

				start := beforeThen - fiveMinMs // from - step
				// Datapoints(ts -> val):
				// 0 -> 1
				// 450000 -> 3
				// 600000 -> 5
				//
				// the selection range logic is (start, end] so we expect the
				// second window of 0 to 300000 to be empty
				//
				// 0 -> range -30000 to 0, ts: 0, val: 1
				// 1 -> range 0 to 300000, val: empty
				// 2 -> range 30000 to 610000, val: 8 (includes last 2 datapoints)
				for i := 0; i < 3; i++ {
					ok, ts, r := evaluator.Next()
					require.True(t, ok)
					require.True(t, start <= ts && ts < now+tenSecMs)

					if i == 1 {
						require.Equal(t, 0, len(r.SampleVector()))
					} else {
						// The []promqlSample in the Vector is reused, so we need to copy the value and timestamp.
						resultTs = append(resultTs, r.SampleVector()[0].T)
						resultVals = append(resultVals, r.SampleVector()[0].F)
						require.Equal(t, stream, r.SampleVector()[0].Metric)
					}
				}

				ok, _, _ := evaluator.Next()
				require.False(t, ok)

				// Because of the 5m step and 5m lookback, the first window will be
				// -300000 to 0, the second 0 to 300000, and the third 300000 to 600000.
				// We don't expect the 2nd to have any data, so below we check results of the 1st and 3rd.
				require.Equal(t, now-600000, resultTs[0])
				require.Equal(t, now, resultTs[1])

				require.Equal(t, float64(1), resultVals[0])
				require.Equal(t, float64(8), resultVals[1])
			})
		})
	})
}
