package pattern

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/pattern/iter"
	"github.com/grafana/loki/v3/pkg/pattern/metric"

	"github.com/grafana/loki/pkg/push"
)

func TestInstancePushQuery(t *testing.T) {
	lbs := labels.New(labels.Label{Name: "test", Value: "test"})
	setup := func() *instance {
		inst, err := newInstance("foo", log.NewNopLogger(), newIngesterMetrics(nil, "test"), metric.AggregationConfig{
			Enabled: true,
		})
		require.NoError(t, err)

		return inst
	}
	t.Run("test pattern samples", func(t *testing.T) {
		inst := setup()
		err := inst.Push(context.Background(), &push.PushRequest{
			Streams: []push.Stream{
				{
					Labels: lbs.String(),
					Entries: []push.Entry{
						{
							Timestamp: time.Unix(20, 0),
							Line:      "ts=1 msg=hello",
						},
					},
				},
			},
		})

		err = inst.Push(context.Background(), &push.PushRequest{
			Streams: []push.Stream{
				{
					Labels: lbs.String(),
					Entries: []push.Entry{
						{
							Timestamp: time.Unix(30, 0),
							Line:      "ts=2 msg=hello",
						},
					},
				},
			},
		})
		for i := 0; i <= 30; i++ {
			err = inst.Push(context.Background(), &push.PushRequest{
				Streams: []push.Stream{
					{
						Labels: lbs.String(),
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
		require.NoError(t, err)
		it, err := inst.Iterator(context.Background(), &logproto.QueryPatternsRequest{
			Query: "{test=\"test\"}",
			Start: time.Unix(0, 0),
			End:   time.Unix(0, math.MaxInt64),
		})
		require.NoError(t, err)
		res, err := iter.ReadAllWithPatterns(it)
		require.NoError(t, err)
		require.Equal(t, 2, len(res.Series))

		it, err = inst.Iterator(context.Background(), &logproto.QueryPatternsRequest{
			Query: "{test=\"test\"}",
			Start: time.Unix(0, 0),
			End:   time.Unix(30, 0),
		})
		require.NoError(t, err)
		res, err = iter.ReadAllWithPatterns(it)
		require.NoError(t, err)
		// query should be inclusive of end time to match our
		// existing metric query behavior
		require.Equal(t, 2, len(res.Series))
		require.Equal(t, 2, len(res.Series[0].Samples))
	})

	t.Run("test count_over_time samples", func(t *testing.T) {
		inst := setup()
		err := inst.Push(context.Background(), &push.PushRequest{
			Streams: []push.Stream{
				{
					Labels: lbs.String(),
					Entries: []push.Entry{
						{
							Timestamp: time.Unix(0, 0),
							Line:      "ts=1 msg=hello",
						},
					},
				},
			},
		})
		for i := 1; i <= 30; i++ {
			err = inst.Push(context.Background(), &push.PushRequest{
				Streams: []push.Stream{
					{
						Labels: lbs.String(),
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
		}
		require.NoError(t, err)

		expr, err := syntax.ParseSampleExpr(`count_over_time({test="test"}[20s])`)
		require.NoError(t, err)

		it, err := inst.QuerySample(context.Background(), expr, &logproto.QueryPatternsRequest{
			Query: expr.String(),
			Start: time.Unix(0, 0),
			End:   time.Unix(int64(20*30), 0),
			Step:  10000,
		})
		require.NoError(t, err)
		res, err := iter.ReadAllWithLabels(it)
		require.NoError(t, err)
		require.Equal(t, 1, len(res.Series))

		require.Equal(t, lbs.String(), res.Series[0].GetLabels())

		// end - start / step -- (start is 0, step is 10s)
		// plus one because end is actually inclusive for metric queries
		expectedDataPoints := ((20 * 30) / 10) + 1
		require.Equal(t, expectedDataPoints, len(res.Series[0].Samples))
		require.Equal(t, int64(1), res.Series[0].Samples[0].Value)

		expr, err = syntax.ParseSampleExpr(`count_over_time({test="test"}[80s])`)
		require.NoError(t, err)

		it, err = inst.QuerySample(context.Background(), expr, &logproto.QueryPatternsRequest{
			Query: expr.String(),
			Start: time.Unix(0, 0),
			End:   time.Unix(int64(20*30), 0),
			Step:  10000,
		})
		require.NoError(t, err)
		res, err = iter.ReadAllWithLabels(it)
		require.NoError(t, err)
		require.Equal(t, 1, len(res.Series))

		require.Equal(t, lbs.String(), res.Series[0].GetLabels())

		// end - start / step -- (start is 0, step is 10s)
		// plus one because end is actually inclusive for metric queries
		expectedDataPoints = ((20 * 30) / 10) + 1
		require.Equal(t, expectedDataPoints, len(res.Series[0].Samples))

		// with a larger selection range of 80s, we expect to eventually get up to 4 per datapoint
		// our pushes are spaced 20s apart, and there's 10s step, so we ecpect to see the value increase
		// every 2 samples, maxing out and staying at 4 after 6 samples (since it starts a 1, not 0)
		require.Equal(t, int64(1), res.Series[0].Samples[0].Value)
		require.Equal(t, int64(1), res.Series[0].Samples[1].Value)
		require.Equal(t, int64(2), res.Series[0].Samples[2].Value)
		require.Equal(t, int64(2), res.Series[0].Samples[3].Value)
		require.Equal(t, int64(3), res.Series[0].Samples[4].Value)
		require.Equal(t, int64(3), res.Series[0].Samples[5].Value)
		require.Equal(t, int64(4), res.Series[0].Samples[6].Value)
		require.Equal(t, int64(4), res.Series[0].Samples[expectedDataPoints-1].Value)
	})

	t.Run("test bytes_over_time samples", func(t *testing.T) {
		inst := setup()
		err := inst.Push(context.Background(), &push.PushRequest{
			Streams: []push.Stream{
				{
					Labels: lbs.String(),
					Entries: []push.Entry{
						{
							Timestamp: time.Unix(0, 0),
							Line:      "foo bar foo bars",
						},
					},
				},
			},
		})
		for i := 1; i <= 30; i++ {
			err = inst.Push(context.Background(), &push.PushRequest{
				Streams: []push.Stream{
					{
						Labels: lbs.String(),
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
		}
		require.NoError(t, err)

		expr, err := syntax.ParseSampleExpr(`bytes_over_time({test="test"}[20s])`)
		require.NoError(t, err)

		it, err := inst.QuerySample(context.Background(), expr, &logproto.QueryPatternsRequest{
			Query: expr.String(),
			Start: time.Unix(0, 0),
			End:   time.Unix(int64(20*30), 0),
			Step:  10000,
		})
		require.NoError(t, err)
		res, err := iter.ReadAllWithLabels(it)
		require.NoError(t, err)
		require.Equal(t, 1, len(res.Series))

		require.Equal(t, lbs.String(), res.Series[0].GetLabels())

		// end - start / step -- (start is 0, step is 10s)
		// plus one because end is actually inclusive for metric queries
		expectedDataPoints := ((20 * 30) / 10) + 1
		require.Equal(t, expectedDataPoints, len(res.Series[0].Samples))
		require.Equal(t, int64(16), res.Series[0].Samples[0].Value)

		expr, err = syntax.ParseSampleExpr(`bytes_over_time({test="test"}[80s])`)
		require.NoError(t, err)

		it, err = inst.QuerySample(context.Background(), expr, &logproto.QueryPatternsRequest{
			Query: expr.String(),
			Start: time.Unix(0, 0),
			End:   time.Unix(int64(20*30), 0),
			Step:  10000,
		})
		require.NoError(t, err)
		res, err = iter.ReadAllWithLabels(it)
		require.NoError(t, err)
		require.Equal(t, 1, len(res.Series))

		require.Equal(t, lbs.String(), res.Series[0].GetLabels())

		// end - start / step -- (start is 0, step is 10s)
		// plus one because end is actually inclusive for metric queries
		expectedDataPoints = ((20 * 30) / 10) + 1
		require.Equal(t, expectedDataPoints, len(res.Series[0].Samples))

		// with a larger selection range of 80s, we expect to eventually get up to 64 bytes
		// as each pushe is 16 bytes and are spaced 20s apart. We query with 10s step,
		// so we ecpect to see the value increase by 16 bytes every 2 samples,
		// maxing out and staying at 64 after 6 samples (since it starts a 1, not 0)
		require.Equal(t, int64(16), res.Series[0].Samples[0].Value)
		require.Equal(t, int64(16), res.Series[0].Samples[1].Value)
		require.Equal(t, int64(32), res.Series[0].Samples[2].Value)
		require.Equal(t, int64(32), res.Series[0].Samples[3].Value)
		require.Equal(t, int64(48), res.Series[0].Samples[4].Value)
		require.Equal(t, int64(48), res.Series[0].Samples[5].Value)
		require.Equal(t, int64(64), res.Series[0].Samples[6].Value)
		require.Equal(t, int64(64), res.Series[0].Samples[expectedDataPoints-1].Value)
	})
}
