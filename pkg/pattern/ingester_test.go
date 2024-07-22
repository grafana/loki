package pattern

import (
	"context"
	"fmt"
	"math"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/pattern/drain"
	"github.com/grafana/loki/v3/pkg/pattern/iter"
	"github.com/grafana/loki/v3/pkg/pattern/metric"

	"github.com/grafana/loki/pkg/push"
)

var lbls = labels.New(labels.Label{Name: "test", Value: "test"})

func setup(t *testing.T) *instance {
	writer := &mockEntryWriter{}
	writer.On("WriteEntry", mock.Anything, mock.Anything, mock.Anything)

	inst, err := newInstance(
		"foo",
		log.NewNopLogger(),
		newIngesterMetrics(nil, "test"),
		metric.NewChunkMetrics(nil, "test"),
		drain.DefaultConfig(),
		metric.AggregationConfig{
			Enabled: true,
		},
		writer,
	)
	require.NoError(t, err)

	return inst
}

func downsampleInstance(inst *instance, tts int64) {
	ts := model.TimeFromUnixNano(time.Unix(tts, 0).UnixNano())
	_ = inst.streams.ForEach(func(s *stream) (bool, error) {
		inst.streams.WithLock(func() {
			s.Downsample(ts)
		})
		return true, nil
	})
}

func TestInstancePushQuery(t *testing.T) {
	inst := setup(t)
	err := inst.Push(context.Background(), &push.PushRequest{
		Streams: []push.Stream{
			{
				Labels: lbls.String(),
				Entries: []push.Entry{
					{
						Timestamp: time.Unix(20, 0),
						Line:      "ts=1 msg=hello",
					},
				},
			},
		},
	})
	require.NoError(t, err)
	downsampleInstance(inst, 20)

	err = inst.Push(context.Background(), &push.PushRequest{
		Streams: []push.Stream{
			{
				Labels: lbls.String(),
				Entries: []push.Entry{
					{
						Timestamp: time.Unix(30, 0),
						Line:      "ts=2 msg=hello",
					},
				},
			},
		},
	})
	require.NoError(t, err)
	downsampleInstance(inst, 30)

	for i := 0; i <= 30; i++ {
		err = inst.Push(context.Background(), &push.PushRequest{
			Streams: []push.Stream{
				{
					Labels: lbls.String(),
					Entries: []push.Entry{
						{
							Timestamp: time.Unix(30, 0),
							Line:      "foo bar foo bar",
						},
					},
				},
			},
		})
		require.NoError(t, err)
	}
	downsampleInstance(inst, 30)

	it, err := inst.Iterator(context.Background(), &logproto.QueryPatternsRequest{
		Query: "{test=\"test\"}",
		Start: time.Unix(0, 0),
		End:   time.Unix(0, math.MaxInt64),
	})
	require.NoError(t, err)
	res, err := iter.ReadAll(it)
	require.NoError(t, err)
	require.Equal(t, 2, len(res.Series))
}

func TestInstancePushQuerySamples(t *testing.T) {
	t.Run("test count_over_time samples", func(t *testing.T) {
		inst := setup(t)
		err := inst.Push(context.Background(), &push.PushRequest{
			Streams: []push.Stream{
				{
					Labels: lbls.String(),
					Entries: []push.Entry{
						{
							Timestamp: time.Unix(0, 0),
							Line:      "ts=1 msg=hello",
						},
					},
				},
			},
		})
		require.NoError(t, err)
		downsampleInstance(inst, 0)

		for i := 1; i <= 30; i++ {
			err = inst.Push(context.Background(), &push.PushRequest{
				Streams: []push.Stream{
					{
						Labels: lbls.String(),
						Entries: []push.Entry{
							{
								Timestamp: time.Unix(int64(20*i), 0),
								Line:      "foo bar foo bar",
							},
						},
					},
				},
			})
			require.NoError(t, err)
			downsampleInstance(inst, int64(20*i))
		}

		expr, err := syntax.ParseSampleExpr(`count_over_time({test="test"}[20s])`)
		require.NoError(t, err)

		it, err := inst.QuerySample(context.Background(), expr, &logproto.QuerySamplesRequest{
			Query: expr.String(),
			Start: time.Unix(0, 0),
			End:   time.Unix(int64(20*30), 0),
			Step:  10000,
		})
		require.NoError(t, err)
		res, err := iter.ReadAllSamples(it)
		require.NoError(t, err)
		require.Equal(t, 1, len(res.Series))

		lblsWithLevel := addUnknownLevelLabel(lbls)
		require.Equal(t, lblsWithLevel.String(), res.Series[0].GetLabels())

		// end - start / step -- (start is 0, step is 10s, downsampling at 20s intervals)
		expectedDataPoints := ((20 * 30) / 10)
		require.Equal(t, expectedDataPoints, len(res.Series[0].Samples))
		require.Equal(t, float64(1), res.Series[0].Samples[0].Value)
		require.Equal(t, float64(1), res.Series[0].Samples[expectedDataPoints-1].Value)

		expr, err = syntax.ParseSampleExpr(`count_over_time({test="test"}[80s])`)
		require.NoError(t, err)

		it, err = inst.QuerySample(context.Background(), expr, &logproto.QuerySamplesRequest{
			Query: expr.String(),
			Start: time.Unix(0, 0),
			End:   time.Unix(int64(20*30), 0),
			Step:  10000,
		})
		require.NoError(t, err)
		res, err = iter.ReadAllSamples(it)
		require.NoError(t, err)
		require.Equal(t, 1, len(res.Series))

		lblsWithLevel = addUnknownLevelLabel(lbls)
		require.Equal(t, lblsWithLevel.String(), res.Series[0].GetLabels())

		// end - start / step -- (start is 0, step is 10s, downsampling at 20s intervals)
		expectedDataPoints = ((20 * 30) / 10)
		require.Equal(t, expectedDataPoints, len(res.Series[0].Samples))

		// with a larger selection range of 80s, we expect to eventually get up to 4 per datapoint
		// our pushes are spaced 20s apart, and there's 10s step, so we ecpect to see the value increase
		// every 2 samples, maxing out and staying at 4 after 6 samples (since it starts a 1, not 0)
		require.Equal(t, float64(1), res.Series[0].Samples[0].Value)
		require.Equal(t, float64(1), res.Series[0].Samples[1].Value)
		require.Equal(t, float64(2), res.Series[0].Samples[2].Value)
		require.Equal(t, float64(2), res.Series[0].Samples[3].Value)
		require.Equal(t, float64(3), res.Series[0].Samples[4].Value)
		require.Equal(t, float64(3), res.Series[0].Samples[5].Value)
		require.Equal(t, float64(4), res.Series[0].Samples[6].Value)
		require.Equal(t, float64(4), res.Series[0].Samples[expectedDataPoints-1].Value)
	})

	t.Run("test count_over_time samples with downsampling", func(t *testing.T) {
		inst := setup(t)
		err := inst.Push(context.Background(), &push.PushRequest{
			Streams: []push.Stream{
				{
					Labels: lbls.String(),
					Entries: []push.Entry{
						{
							Timestamp: time.Unix(0, 0),
							Line:      "ts=1 msg=hello",
						},
					},
				},
			},
		})
		require.NoError(t, err)
		downsampleInstance(inst, 0)

		for i := 1; i <= 30; i++ {
			err = inst.Push(context.Background(), &push.PushRequest{
				Streams: []push.Stream{
					{
						Labels: lbls.String(),
						Entries: []push.Entry{
							{
								Timestamp: time.Unix(int64(10*i), 0),
								Line:      "foo bar foo bar",
							},
						},
					},
				},
			})
			require.NoError(t, err)

			// downsample every 20s
			if i%2 == 0 {
				downsampleInstance(inst, int64(10*i))
			}
		}

		expr, err := syntax.ParseSampleExpr(`count_over_time({test="test"}[20s])`)
		require.NoError(t, err)

		it, err := inst.QuerySample(context.Background(), expr, &logproto.QuerySamplesRequest{
			Query: expr.String(),
			Start: time.Unix(0, 0),
			End:   time.Unix(int64(10*30), 0),
			Step:  20000,
		})
		require.NoError(t, err)
		res, err := iter.ReadAllSamples(it)
		require.NoError(t, err)
		require.Equal(t, 1, len(res.Series))

		lblsWithLevel := addUnknownLevelLabel(lbls)
		require.Equal(t, lblsWithLevel.String(), res.Series[0].GetLabels())

		// end - start / step -- (start is 0, step is 10s, downsampling at 20s intervals)
		expectedDataPoints := ((10 * 30) / 20)
		require.Equal(t, expectedDataPoints, len(res.Series[0].Samples))
		require.Equal(t, float64(1), res.Series[0].Samples[0].Value)

		// after the first push there's 2 pushes per sample due to downsampling
		require.Equal(t, float64(2), res.Series[0].Samples[expectedDataPoints-1].Value)

		expr, err = syntax.ParseSampleExpr(`count_over_time({test="test"}[80s])`)
		require.NoError(t, err)

		it, err = inst.QuerySample(context.Background(), expr, &logproto.QuerySamplesRequest{
			Query: expr.String(),
			Start: time.Unix(0, 0),
			End:   time.Unix(int64(10*30), 0),
			Step:  20000,
		})
		require.NoError(t, err)
		res, err = iter.ReadAllSamples(it)
		require.NoError(t, err)
		require.Equal(t, 1, len(res.Series))

		lblsWithLevel = addUnknownLevelLabel(lbls)
		require.Equal(t, lblsWithLevel.String(), res.Series[0].GetLabels())

		// end - start / step -- (start is 0, step is 10s, downsampling at 20s intervals)
		expectedDataPoints = ((10 * 30) / 20)
		require.Equal(t, expectedDataPoints, len(res.Series[0].Samples))

		// with a larger selection range of 80s, we expect to eventually get up to 8 per datapoint
		// our pushes are spaced 10s apart, downsampled every 20s, and there's 10s step,
		// so we expect to see the value increase by 2 every samples, maxing out and staying at 8 after 5 samples
		require.Equal(t, float64(1), res.Series[0].Samples[0].Value)
		require.Equal(t, float64(3), res.Series[0].Samples[1].Value)
		require.Equal(t, float64(5), res.Series[0].Samples[2].Value)
		require.Equal(t, float64(7), res.Series[0].Samples[3].Value)
		require.Equal(t, float64(8), res.Series[0].Samples[4].Value)
		require.Equal(t, float64(8), res.Series[0].Samples[expectedDataPoints-1].Value)
	})

	t.Run("test bytes_over_time samples", func(t *testing.T) {
		inst := setup(t)
		err := inst.Push(context.Background(), &push.PushRequest{
			Streams: []push.Stream{
				{
					Labels: lbls.String(),
					Entries: []push.Entry{
						{
							Timestamp: time.Unix(0, 0),
							Line:      "foo bar foo bars",
						},
					},
				},
			},
		})
		require.NoError(t, err)

		downsampleInstance(inst, 0)
		for i := 1; i <= 30; i++ {
			err = inst.Push(context.Background(), &push.PushRequest{
				Streams: []push.Stream{
					{
						Labels: lbls.String(),
						Entries: []push.Entry{
							{
								Timestamp: time.Unix(int64(20*i), 0),
								Line:      "foo bar foo bars",
							},
						},
					},
				},
			})
			require.NoError(t, err)
			downsampleInstance(inst, int64(20*i))
		}

		expr, err := syntax.ParseSampleExpr(`bytes_over_time({test="test"}[20s])`)
		require.NoError(t, err)

		it, err := inst.QuerySample(context.Background(), expr, &logproto.QuerySamplesRequest{
			Query: expr.String(),
			Start: time.Unix(0, 0),
			End:   time.Unix(int64(20*30), 0),
			Step:  10000,
		})
		require.NoError(t, err)
		res, err := iter.ReadAllSamples(it)
		require.NoError(t, err)
		require.Equal(t, 1, len(res.Series))

		lblsWithLevel := addUnknownLevelLabel(lbls)
		require.Equal(t, lblsWithLevel.String(), res.Series[0].GetLabels())

		// end - start / step -- (start is 0, step is 10s)
		expectedDataPoints := ((20 * 30) / 10)
		require.Equal(t, expectedDataPoints, len(res.Series[0].Samples))
		require.Equal(t, float64(16), res.Series[0].Samples[0].Value)

		expr, err = syntax.ParseSampleExpr(`bytes_over_time({test="test"}[80s])`)
		require.NoError(t, err)

		it, err = inst.QuerySample(context.Background(), expr, &logproto.QuerySamplesRequest{
			Query: expr.String(),
			Start: time.Unix(0, 0),
			End:   time.Unix(int64(20*30), 0),
			Step:  10000,
		})
		require.NoError(t, err)
		res, err = iter.ReadAllSamples(it)
		require.NoError(t, err)
		require.Equal(t, 1, len(res.Series))

		lblsWithLevel = addUnknownLevelLabel(lbls)
		require.Equal(t, lblsWithLevel.String(), res.Series[0].GetLabels())

		// end - start / step -- (start is 0, step is 10s)
		expectedDataPoints = ((20 * 30) / 10)
		require.Equal(t, expectedDataPoints, len(res.Series[0].Samples))

		// with a larger selection range of 80s, we expect to eventually get up to 64 bytes
		// as each pushe is 16 bytes and are spaced 20s apart. We query with 10s step,
		// so we ecpect to see the value increase by 16 bytes every 2 samples,
		// maxing out and staying at 64 after 6 samples (since it starts a 1, not 0)
		require.Equal(t, float64(16), res.Series[0].Samples[0].Value)
		require.Equal(t, float64(16), res.Series[0].Samples[1].Value)
		require.Equal(t, float64(32), res.Series[0].Samples[2].Value)
		require.Equal(t, float64(32), res.Series[0].Samples[3].Value)
		require.Equal(t, float64(48), res.Series[0].Samples[4].Value)
		require.Equal(t, float64(48), res.Series[0].Samples[5].Value)
		require.Equal(t, float64(64), res.Series[0].Samples[6].Value)
		require.Equal(t, float64(64), res.Series[0].Samples[expectedDataPoints-1].Value)
	})

	t.Run("test count_over_time samples, multiple streams", func(t *testing.T) {
		lbls2 := labels.New(
			labels.Label{Name: "fizz", Value: "buzz"},
			labels.Label{Name: "test", Value: "test"},
			labels.Label{Name: "foo", Value: "bar"},
		)
		lbls3 := labels.New(
			labels.Label{Name: "fizz", Value: "buzz"},
			labels.Label{Name: "test", Value: "test"},
		)
		lbls4 := labels.New(
			labels.Label{Name: "fizz", Value: "buzz"},
			labels.Label{Name: "foo", Value: "baz"},
		)

		inst := setup(t)
		err := inst.Push(context.Background(), &push.PushRequest{
			Streams: []push.Stream{
				{
					Labels: lbls.String(),
					Entries: []push.Entry{
						{
							Timestamp: time.Unix(0, 0),
							Line:      "ts=1 msg=hello",
						},
					},
				},
				{
					Labels: lbls2.String(),
					Entries: []push.Entry{
						{
							Timestamp: time.Unix(0, 0),
							Line:      "ts=1 msg=goodbye",
						},
					},
				},
				{
					Labels: lbls3.String(),
					Entries: []push.Entry{
						{
							Timestamp: time.Unix(0, 0),
							Line:      "ts=1 msg=hello",
						},
						{
							Timestamp: time.Unix(0, 0),
							Line:      "ts=1 msg=goodbye",
						},
					},
				},
				{
					Labels: lbls4.String(),
					Entries: []push.Entry{
						{
							Timestamp: time.Unix(0, 0),
							Line:      "ts=1 msg=hello",
						},
						{
							Timestamp: time.Unix(0, 0),
							Line:      "ts=1 msg=goodbye",
						},
						{
							Timestamp: time.Unix(0, 0),
							Line:      "ts=1 msg=shalom",
						},
					},
				},
			},
		})
		require.NoError(t, err)
		downsampleInstance(inst, 0)

		for i := 1; i <= 30; i++ {
			err = inst.Push(context.Background(), &push.PushRequest{
				Streams: []push.Stream{
					{
						Labels: lbls.String(),
						Entries: []push.Entry{
							{
								Timestamp: time.Unix(int64(20*i), 0),
								Line:      "foo bar foo bar",
							},
						},
					},
					{
						Labels: lbls2.String(),
						Entries: []push.Entry{
							{
								Timestamp: time.Unix(int64(20*i), 0),
								Line:      "foo",
							},
						},
					},
					{
						Labels: lbls3.String(),
						Entries: []push.Entry{
							{
								Timestamp: time.Unix(int64(20*i), 0),
								Line:      "foo",
							},
							{
								Timestamp: time.Unix(int64(20*i), 0),
								Line:      "bar",
							},
						},
					},
					{
						Labels: lbls4.String(),
						Entries: []push.Entry{
							{
								Timestamp: time.Unix(int64(20*i), 0),
								Line:      "foo",
							},
							{
								Timestamp: time.Unix(int64(20*i), 0),
								Line:      "bar",
							},
							{
								Timestamp: time.Unix(int64(20*i), 0),
								Line:      "baz",
							},
						},
					},
				},
			})
			require.NoError(t, err)
			downsampleInstance(inst, int64(20*i))
		}

		for _, tt := range []struct {
			name                string
			expr                func(string) syntax.SampleExpr
			req                 func(syntax.SampleExpr) logproto.QuerySamplesRequest
			verifySeries20sStep func([]logproto.Series)
			verifySeries80sStep func([]logproto.Series)
		}{
			{
				// test="test" will capture lbls - lbls3
				name: `{test="test"}`,
				expr: func(selRange string) syntax.SampleExpr {
					expr, err := syntax.ParseSampleExpr(fmt.Sprintf(`count_over_time({test="test"}[%s])`, selRange))
					require.NoError(t, err)
					return expr
				},
				req: func(expr syntax.SampleExpr) logproto.QuerySamplesRequest {
					return logproto.QuerySamplesRequest{
						Query: expr.String(),
						Start: time.Unix(0, 0),
						End:   time.Unix(int64(20*30), 0),
						Step:  10000,
					}
				},
				verifySeries20sStep: func(series []logproto.Series) {
					require.Equal(t, 3, len(series))

					lbls2WithLevel := addUnknownLevelLabel(lbls2)
					require.Equal(t, lbls2WithLevel.String(), series[0].GetLabels())

					lbls3WithLevel := addUnknownLevelLabel(lbls3)
					require.Equal(t, lbls3WithLevel.String(), series[1].GetLabels())

					lblsWithLevel := addUnknownLevelLabel(lbls)
					require.Equal(t, lblsWithLevel.String(), series[2].GetLabels())

					// end - start / step -- (start is 0, step is 10s)
					expectedDataPoints := ((20 * 30) / 10)
					require.Equal(t, expectedDataPoints, len(series[0].Samples))
					require.Equal(t, expectedDataPoints, len(series[1].Samples))
					require.Equal(t, expectedDataPoints, len(series[2].Samples))

					// pushes are spaced 20s apart, and there's 10s step,
					// so we expect to see a single line per step for lbls2
					require.Equal(t, float64(1), series[0].Samples[0].Value, fmt.Sprintf("series: %v, sample: %v", series[0].Labels, series[0].Samples[0]))
					require.Equal(t, float64(1), series[0].Samples[expectedDataPoints-1].Value, fmt.Sprintf("series: %v, sample: %v", series[0].Labels, series[0].Samples[expectedDataPoints-1]))

					// pushes are spaced 20s apart, and there's 10s step,
					// so we expect to see a 2 lines per step for lbls3
					require.Equal(t, float64(2), series[1].Samples[0].Value, fmt.Sprintf("series: %v, sample: %v", series[1].Labels, series[1].Samples[0]))
					require.Equal(t, float64(2), series[1].Samples[expectedDataPoints-1].Value, fmt.Sprintf("series: %v, sample: %v", series[1].Labels, series[1].Samples[expectedDataPoints-1]))

					// pushes are spaced 20s apart, and there's 10s step,
					// so we expect to see a 1 linee per step for lbls3
					require.Equal(t, float64(1), series[2].Samples[0].Value, fmt.Sprintf("series: %v, samples: %v", series[2].Labels, series[2].Samples))
					require.Equal(t, float64(1), series[2].Samples[expectedDataPoints-1].Value, fmt.Sprintf("series: %v, samples: %v", series[2].Labels, series[2].Samples[expectedDataPoints-1]))
				},
				verifySeries80sStep: func(series []logproto.Series) {
					require.Equal(t, 3, len(series))

					lbls2WithLevel := addUnknownLevelLabel(lbls2)
					require.Equal(t, lbls2WithLevel.String(), series[0].GetLabels())

					lbls3WithLevel := addUnknownLevelLabel(lbls3)
					require.Equal(t, lbls3WithLevel.String(), series[1].GetLabels())

					lblsWithLevel := addUnknownLevelLabel(lbls)
					require.Equal(t, lblsWithLevel.String(), series[2].GetLabels())

					// end - start / step -- (start is 0, step is 10s)
					expectedDataPoints := ((20 * 30) / 10)
					require.Equal(t, expectedDataPoints, len(series[0].Samples))
					require.Equal(t, expectedDataPoints, len(series[1].Samples))
					require.Equal(t, expectedDataPoints, len(series[2].Samples))

					// pushes are spaced 80s apart, and there's 10s step,
					// so we expect to see a single line for the first step
					// and 4 lines for the last for lbls2
					require.Equal(t, float64(1), series[0].Samples[0].Value, fmt.Sprintf("series: %v, sample: %v", series[0].Labels, series[0].Samples[0]))
					require.Equal(t, float64(4), series[0].Samples[expectedDataPoints-1].Value, fmt.Sprintf("series: %v, sample: %v", series[0].Labels, series[0].Samples[expectedDataPoints-1]))

					// pushes are spaced 20s apart, and there's 10s step,
					// so we expect to see a 2 lines for the first step and
					// 8 lines for the last for lbls3
					require.Equal(t, float64(2), series[1].Samples[0].Value, fmt.Sprintf("series: %v, sample: %v", series[1].Labels, series[1].Samples[0]))
					require.Equal(t, float64(8), series[1].Samples[expectedDataPoints-1].Value, fmt.Sprintf("series: %v, sample: %v", series[1].Labels, series[1].Samples[expectedDataPoints-1]))

					// pushes are spaced 20s apart, and there's 10s step,
					// so we expect to see a 1 linee for the first step
					// and 4 lintes for the last step for lbls3
					require.Equal(t, float64(1), series[2].Samples[0].Value, fmt.Sprintf("series: %v, samples: %v", series[2].Labels, series[2].Samples))
					require.Equal(t, float64(4), series[2].Samples[expectedDataPoints-1].Value, fmt.Sprintf("series: %v, samples: %v", series[2].Labels, series[2].Samples[expectedDataPoints-1]))
				},
			},
			{
				// fizz="buzz" will capture lbls2 - lbls4
				name: `{fizz="buzz"}`,
				expr: func(selRange string) syntax.SampleExpr {
					expr, err := syntax.ParseSampleExpr(fmt.Sprintf(`count_over_time({fizz="buzz"}[%s])`, selRange))
					require.NoError(t, err)
					return expr
				},
				req: func(expr syntax.SampleExpr) logproto.QuerySamplesRequest {
					return logproto.QuerySamplesRequest{
						Query: expr.String(),
						Start: time.Unix(0, 0),
						End:   time.Unix(int64(20*30), 0),
						Step:  10000,
					}
				},
				verifySeries20sStep: func(series []logproto.Series) {
					require.Equal(t, 3, len(series))
					seriesLabels := make([]string, 0, len(series))
					for _, s := range series {
						seriesLabels = append(seriesLabels, s.GetLabels())
					}

					lbls2WithLevel := addUnknownLevelLabel(lbls2)
					lbls3WithLevel := addUnknownLevelLabel(lbls3)
					lbls4WithLevel := addUnknownLevelLabel(lbls4)

					require.Equal(t, lbls2WithLevel.String(), series[0].GetLabels(), fmt.Sprintf("series: %v", seriesLabels))
					require.Equal(t, lbls4WithLevel.String(), series[1].GetLabels(), fmt.Sprintf("series: %v", seriesLabels))
					require.Equal(t, lbls3WithLevel.String(), series[2].GetLabels(), fmt.Sprintf("series: %v", seriesLabels))

					// end - start / step -- (start is 0, step is 10s)
					expectedDataPoints := ((20 * 30) / 10)
					require.Equal(t, expectedDataPoints, len(series[0].Samples))
					require.Equal(t, expectedDataPoints, len(series[1].Samples))
					require.Equal(t, expectedDataPoints, len(series[2].Samples))

					// pushes are spaced 20s apart, and there's 10s step,
					// so we expect to see a single line per step for lbls2
					require.Equal(t, float64(1), series[0].Samples[0].Value, fmt.Sprintf("series: %v, sample: %v", series[0].Labels, series[0].Samples[0]))
					require.Equal(t, float64(1), series[0].Samples[expectedDataPoints-1].Value, fmt.Sprintf("series: %v, sample: %v", series[0].Labels, series[0].Samples[expectedDataPoints-1]))

					// pushes are spaced 20s apart, and there's 10s step,
					// so we expect to see a 3 lines per step for lbls4
					require.Equal(t, float64(3), series[1].Samples[0].Value, fmt.Sprintf("series: %v, sample: %v", series[1].Labels, series[1].Samples[0]))
					require.Equal(t, float64(3), series[1].Samples[expectedDataPoints-1].Value, fmt.Sprintf("series: %v, sample: %v", series[1].Labels, series[1].Samples[expectedDataPoints-1]))

					// pushes are spaced 20s apart, and there's 10s step,
					// so we expect to see a 2 lines per step for lbls3
					require.Equal(t, float64(2), series[2].Samples[0].Value, fmt.Sprintf("series: %v, sample: %v", series[2].Labels, series[2].Samples[0]))
					require.Equal(t, float64(2), series[2].Samples[expectedDataPoints-1].Value, fmt.Sprintf("series: %v, sample: %v", series[2].Labels, series[2].Samples[expectedDataPoints-1]))
				},
				verifySeries80sStep: func(series []logproto.Series) {
					require.Equal(t, 3, len(series))
					sereisLabels := make([]string, 0, len(series))
					for _, s := range series {
						sereisLabels = append(sereisLabels, s.GetLabels())
					}

					lbls2WithLevel := addUnknownLevelLabel(lbls2)
					lbls3WithLevel := addUnknownLevelLabel(lbls3)
					lbls4WithLevel := addUnknownLevelLabel(lbls4)

					require.Equal(t, lbls2WithLevel.String(), series[0].GetLabels(), fmt.Sprintf("series: %v", sereisLabels))
					require.Equal(t, lbls4WithLevel.String(), series[1].GetLabels(), fmt.Sprintf("series: %v", sereisLabels))
					require.Equal(t, lbls3WithLevel.String(), series[2].GetLabels(), fmt.Sprintf("series: %v", sereisLabels))

					// end - start / step -- (start is 0, step is 10s)
					expectedDataPoints := ((20 * 30) / 10)
					require.Equal(t, expectedDataPoints, len(series[0].Samples))
					require.Equal(t, expectedDataPoints, len(series[1].Samples))
					require.Equal(t, expectedDataPoints, len(series[2].Samples))

					// pushes are spaced 20s apart, and there's 10s step,
					// so we expect to see a single line for the first step
					// and 4 lines for the last step for lbls2
					require.Equal(t, float64(1), series[0].Samples[0].Value, fmt.Sprintf("series: %v, sample: %v", series[0].Labels, series[0].Samples[0]))
					require.Equal(t, float64(4), series[0].Samples[expectedDataPoints-1].Value, fmt.Sprintf("series: %v, sample: %v", series[0].Labels, series[0].Samples[expectedDataPoints-1]))

					// pushes are spaced 20s apart, and there's 10s step,
					// so we expect to see a 3 lines for the first step
					// and 12 lines for the last step for lbls4
					require.Equal(t, float64(3), series[1].Samples[0].Value, fmt.Sprintf("series: %v, sample: %v", series[1].Labels, series[1].Samples[0]))
					require.Equal(t, float64(12), series[1].Samples[expectedDataPoints-1].Value, fmt.Sprintf("series: %v, sample: %v", series[1].Labels, series[1].Samples[expectedDataPoints-1]))

					// pushes are spaced 20s apart, and there's 10s step,
					// so we expect to see a 2 lines for the first step
					// and 8 lines for the last step for lbls3
					require.Equal(t, float64(2), series[2].Samples[0].Value, fmt.Sprintf("series: %v, sample: %v", series[2].Labels, series[2].Samples[0]))
					require.Equal(t, float64(8), series[2].Samples[expectedDataPoints-1].Value, fmt.Sprintf("series: %v, sample: %v", series[2].Labels, series[2].Samples[expectedDataPoints-1]))
				},
			},
			{
				// foo="bar" will capture only lbls2
				name: `{foo="bar"}`,
				expr: func(selRange string) syntax.SampleExpr {
					expr, err := syntax.ParseSampleExpr(fmt.Sprintf(`count_over_time({foo="bar"}[%s])`, selRange))
					require.NoError(t, err)
					return expr
				},
				req: func(expr syntax.SampleExpr) logproto.QuerySamplesRequest {
					return logproto.QuerySamplesRequest{
						Query: expr.String(),
						Start: time.Unix(0, 0),
						End:   time.Unix(int64(20*30), 0),
						Step:  10000,
					}
				},
				verifySeries20sStep: func(series []logproto.Series) {
					require.Equal(t, 1, len(series))

					sereisLabels := make([]string, 0, len(series))
					for _, s := range series {
						sereisLabels = append(sereisLabels, s.GetLabels())
					}

					lbls2WithLevel := addUnknownLevelLabel(lbls2)
					require.Equal(t, lbls2WithLevel.String(), series[0].GetLabels(), fmt.Sprintf("series: %v", sereisLabels))

					// end - start / step -- (start is 0, step is 10s)
					expectedDataPoints := ((20 * 30) / 10)
					require.Equal(t, expectedDataPoints, len(series[0].Samples))

					// pushes are spaced 20s apart, and there's 10s step,
					// so we expect to see a single line per step for lbls2
					require.Equal(t, float64(1), series[0].Samples[0].Value, fmt.Sprintf("series: %v, sample: %v", series[0].Labels, series[0].Samples[0]))
					require.Equal(t, float64(1), series[0].Samples[expectedDataPoints-1].Value, fmt.Sprintf("series: %v, sample: %v", series[0].Labels, series[0].Samples[expectedDataPoints-1]))
				},
				verifySeries80sStep: func(series []logproto.Series) {
					require.Equal(t, 1, len(series))

					sereisLabels := make([]string, 0, len(series))
					for _, s := range series {
						sereisLabels = append(sereisLabels, s.GetLabels())
					}

					lbls2WithLevel := addUnknownLevelLabel(lbls2)
					require.Equal(t, lbls2WithLevel.String(), series[0].GetLabels(), fmt.Sprintf("series: %v", sereisLabels))

					// end - start / step -- (start is 0, step is 10s)
					expectedDataPoints := ((20 * 30) / 10)
					require.Equal(t, expectedDataPoints, len(series[0].Samples))

					// pushes are spaced 20s apart, and there's 10s step,
					// so we expect to see a single line for the first step
					// and 4 lines for the last step for lbls2
					require.Equal(t, float64(1), series[0].Samples[0].Value, fmt.Sprintf("series: %v, sample: %v", series[0].Labels, series[0].Samples[0]))
					require.Equal(t, float64(4), series[0].Samples[expectedDataPoints-1].Value, fmt.Sprintf("series: %v, sample: %v", series[0].Labels, series[0].Samples[expectedDataPoints-1]))
				},
			},
			{
				// foo=~".+" will capture lbls2 and lbls4
				name: `{foo=~".+"}`,
				expr: func(selRange string) syntax.SampleExpr {
					expr, err := syntax.ParseSampleExpr(fmt.Sprintf(`count_over_time({foo=~".+"}[%s])`, selRange))
					require.NoError(t, err)
					return expr
				},
				req: func(expr syntax.SampleExpr) logproto.QuerySamplesRequest {
					return logproto.QuerySamplesRequest{
						Query: expr.String(),
						Start: time.Unix(0, 0),
						End:   time.Unix(int64(20*30), 0),
						Step:  10000,
					}
				},
				verifySeries20sStep: func(series []logproto.Series) {
					require.Equal(t, 2, len(series))

					sereisLabels := make([]string, 0, len(series))
					for _, s := range series {
						sereisLabels = append(sereisLabels, s.GetLabels())
					}

					lbls2WithLevel := addUnknownLevelLabel(lbls2)
					lbls4WithLevel := addUnknownLevelLabel(lbls4)
					require.Equal(t, lbls2WithLevel.String(), series[0].GetLabels(), fmt.Sprintf("series: %v", sereisLabels))
					require.Equal(t, lbls4WithLevel.String(), series[1].GetLabels(), fmt.Sprintf("series: %v", sereisLabels))

					// end - start / step -- (start is 0, step is 10s)
					expectedDataPoints := ((20 * 30) / 10)
					require.Equal(t, expectedDataPoints, len(series[0].Samples))
					require.Equal(t, expectedDataPoints, len(series[1].Samples))

					// pushes are spaced 20s apart, and there's 10s step,
					// so we expect to see a single line per step for lbls2
					require.Equal(t, float64(1), series[0].Samples[0].Value, fmt.Sprintf("series: %v, sample: %v", series[0].Labels, series[0].Samples[0]))
					require.Equal(t, float64(1), series[0].Samples[expectedDataPoints-1].Value, fmt.Sprintf("series: %v, sample: %v", series[0].Labels, series[0].Samples[expectedDataPoints-1]))

					// pushes are spaced 20s apart, and there's 10s step,
					// so we expect to see 3 lines per step for lbls4
					require.Equal(t, float64(3), series[1].Samples[0].Value, fmt.Sprintf("series: %v, sample: %v", series[1].Labels, series[1].Samples[0]))
					require.Equal(t, float64(3), series[1].Samples[expectedDataPoints-1].Value, fmt.Sprintf("series: %v, sample: %v", series[1].Labels, series[1].Samples[expectedDataPoints-1]))
				},
				verifySeries80sStep: func(series []logproto.Series) {
					require.Equal(t, 2, len(series))

					sereisLabels := make([]string, 0, len(series))
					for _, s := range series {
						sereisLabels = append(sereisLabels, s.GetLabels())
					}

					lbls2WithLevel := addUnknownLevelLabel(lbls2)
					lbls4WithLevel := addUnknownLevelLabel(lbls4)
					require.Equal(t, lbls2WithLevel.String(), series[0].GetLabels(), fmt.Sprintf("series: %v", sereisLabels))
					require.Equal(t, lbls4WithLevel.String(), series[1].GetLabels(), fmt.Sprintf("series: %v", sereisLabels))

					// end - start / step -- (start is 0, step is 10s)
					expectedDataPoints := ((20 * 30) / 10)
					require.Equal(t, expectedDataPoints, len(series[0].Samples))
					require.Equal(t, expectedDataPoints, len(series[1].Samples))

					// pushes are spaced 20s apart, and there's 10s step,
					// so we expect to see a single line for the first step
					// and 4 lines for the last step for lbls2
					require.Equal(t, float64(1), series[0].Samples[0].Value, fmt.Sprintf("series: %v, sample: %v", series[0].Labels, series[0].Samples[0]))
					require.Equal(t, float64(4), series[0].Samples[expectedDataPoints-1].Value, fmt.Sprintf("series: %v, sample: %v", series[0].Labels, series[0].Samples[expectedDataPoints-1]))

					// pushes are spaced 20s apart, and there's 10s step,
					// so we expect to see 3 lines for the first step
					// and 12 lines for the last step for lbls4
					require.Equal(t, float64(3), series[1].Samples[0].Value, fmt.Sprintf("series: %v, sample: %v", series[1].Labels, series[1].Samples[0]))
					require.Equal(t, float64(12), series[1].Samples[expectedDataPoints-1].Value, fmt.Sprintf("series: %v, sample: %v", series[1].Labels, series[1].Samples[expectedDataPoints-1]))
				},
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				expr := tt.expr("20s")
				req := tt.req(expr)

				it, err := inst.QuerySample(context.Background(), expr, &req)
				require.NoError(t, err)
				res, err := iter.ReadAllSamples(it)
				require.NoError(t, err)

				ss := make([]logproto.Series, 0, len(res.Series))
				ss = append(ss, res.Series...)

				sort.Slice(ss, func(i, j int) bool {
					return ss[i].Labels < ss[j].Labels
				})

				tt.verifySeries20sStep(ss)

				expr = tt.expr("80s")
				req = tt.req(expr)

				it, err = inst.QuerySample(context.Background(), expr, &req)
				require.NoError(t, err)
				res, err = iter.ReadAllSamples(it)
				require.NoError(t, err)

				ss = make([]logproto.Series, 0, len(res.Series))
				ss = append(ss, res.Series...)

				sort.Slice(ss, func(i, j int) bool {
					return ss[i].Labels < ss[j].Labels
				})

				if tt.verifySeries80sStep != nil {
					tt.verifySeries80sStep(ss)
				}
			})
		}
	})
}

func addUnknownLevelLabel(lbls labels.Labels) labels.Labels {
	lblsWithLevel := append(lbls, labels.Label{Name: "level", Value: "unknown"})
	slices.SortFunc(lblsWithLevel, func(i, j labels.Label) int {
		if i.Name < j.Name {
			return -1
		}

		if i.Name > j.Name {
			return 1
		}

		return 0
	})

	return lblsWithLevel
}
