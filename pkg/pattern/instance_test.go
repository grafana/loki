package pattern

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/pattern/metric"

	"github.com/grafana/loki/pkg/push"
)

func TestInstance_QuerySample(t *testing.T) {
	setupInstance := func() *instance {
		instance, err := newInstance(
			"test",
			log.NewNopLogger(),
			newIngesterMetrics(nil, "test"),
			metric.NewChunkMetrics(nil, "test"),
			metric.AggregationConfig{
				Enabled: true,
			},
		)

		require.NoError(t, err)

		return instance
	}

	ctx := context.Background()

	thirtySeconds := int64(30000)
	oneMin := int64(60000)
	fiveMin := oneMin * 5
	now := fiveMin
	then := int64(0)

	mockReq := &logproto.QuerySamplesRequest{
		Start: time.UnixMilli(then),
		End:   time.UnixMilli(now + 1e4),
		Step:  oneMin,
	}

	labels := model.LabelSet{
		model.LabelName("foo"): model.LabelValue("bar"),
	}

	lastTsMilli := (then + oneMin + thirtySeconds) // 0 + 60000 + 30000 = 90000

	t.Run("single push", func(t *testing.T) {
		instance := setupInstance()
		err := instance.Push(ctx, &logproto.PushRequest{
			Streams: []push.Stream{
				{
					Labels: labels.String(),
					Entries: []push.Entry{
						{
							Timestamp: time.UnixMilli(then),
							Line:      "this=that color=blue",
						},
						{
							Timestamp: time.UnixMilli(then + thirtySeconds),
							Line:      "this=that color=blue",
						},
						{
							Timestamp: time.UnixMilli(then + oneMin),
							Line:      "this=that color=blue",
						},
						{
							Timestamp: time.UnixMilli(lastTsMilli),
							Line:      "this=that color=blue",
						},
					},
					Hash: uint64(labels.Fingerprint()),
				},
			},
		})
		require.NoError(t, err)

		// 5 min query range
		// 1 min step
		// 1 min selection range

		// first: -60000  to 0
		// second: 0      to 60000
		// third:  60000  to 120000
		// fourth: 120000 to 180000
		// fifth:  180000 to 240000
		// sixth:  240000 to 300000

		// lastTsMilli is 90000
		// would expect it in the 3rd bucket, but since there's only push, it return
		// on the first iteration
		start := then
		secondPoint := start + oneMin
		thirdPoint := secondPoint + oneMin

		t.Run("successful count over time query", func(t *testing.T) {
			expr, err := syntax.ParseSampleExpr(`count_over_time({foo="bar"}[60s])`)
			require.NoError(t, err)

			iter, err := instance.QuerySample(ctx, expr, mockReq)
			assert.NoError(t, err)
			assert.NotNil(t, iter)

			next := iter.Next()
			require.True(t, next)

			sample := iter.At()
			require.Equal(t, float64(4), sample.Value)
			require.Equal(t, model.Time(thirdPoint).UnixNano(), sample.Timestamp)

			next = iter.Next()
			require.False(t, next)
		})

		t.Run("successful bytes over time query", func(t *testing.T) {
			expr, err := syntax.ParseSampleExpr(`bytes_over_time({foo="bar"}[60s])`)
			require.NoError(t, err)

			iter, err := instance.QuerySample(ctx, expr, mockReq)
			assert.NoError(t, err)
			assert.NotNil(t, iter)

			next := iter.Next()
			require.True(t, next)

			sample := iter.At()
			require.Equal(t, float64(80), sample.Value)
			require.Equal(t, model.Time(thirdPoint).UnixNano(), sample.Timestamp)

			next = iter.Next()
			require.False(t, next)
		})
	})

	t.Run("multiple streams, multiple pushes", func(t *testing.T) {
		instance := setupInstance()
		stream1 := model.LabelSet{
			model.LabelName("foo"):  model.LabelValue("bar"),
			model.LabelName("fizz"): model.LabelValue("buzz"),
		}
		stream2 := model.LabelSet{
			model.LabelName("foo"):  model.LabelValue("bar"),
			model.LabelName("fizz"): model.LabelValue("bang"),
		}

		err := instance.Push(ctx, &logproto.PushRequest{
			Streams: []push.Stream{
				{
					Labels: stream1.String(),
					Entries: []push.Entry{
						{
							Timestamp: time.UnixMilli(then),
							Line:      "this=that color=blue",
						},
						{
							Timestamp: time.UnixMilli(then + thirtySeconds),
							Line:      "this=that color=blue",
						},
						{
							Timestamp: time.UnixMilli(then + oneMin),
							Line:      "this=that color=blue",
						},
						{
							Timestamp: time.UnixMilli(then + oneMin + thirtySeconds), // 60000 + 30000 = 90000
							Line:      "this=that color=blue",
						},
					},
					Hash: uint64(stream1.Fingerprint()),
				},
				{
					Labels: stream2.String(),
					Entries: []push.Entry{
						{
							Timestamp: time.UnixMilli(then),
							Line:      "this=that color=blue",
						},
						{
							Timestamp: time.UnixMilli(then + thirtySeconds),
							Line:      "this=that color=blue",
						},
						{
							Timestamp: time.UnixMilli(then + oneMin),
							Line:      "this=that color=blue",
						},
						{
							Timestamp: time.UnixMilli(then + oneMin + thirtySeconds),
							Line:      "this=that color=blue",
						},
					},
					Hash: uint64(stream2.Fingerprint()),
				},
			},
		})
		require.NoError(t, err)

		err = instance.Push(ctx, &logproto.PushRequest{
			Streams: []push.Stream{
				{
					Labels: stream1.String(),
					Entries: []push.Entry{
						{
							Timestamp: time.UnixMilli(then + oneMin + oneMin + thirtySeconds),
							Line:      "this=that color=blue",
						},
						{
							Timestamp: time.UnixMilli(then + oneMin + oneMin + oneMin),
							Line:      "this=that color=blue",
						},
						{
							Timestamp: time.UnixMilli(then + oneMin + oneMin + oneMin + thirtySeconds), // 180000 + 30000 = 210000
							Line:      "this=that color=blue",
						},
					},
					Hash: uint64(stream1.Fingerprint()),
				},
				{
					Labels: stream2.String(),
					Entries: []push.Entry{
						{
							Timestamp: time.UnixMilli(then + oneMin + oneMin + thirtySeconds),
							Line:      "this=that color=blue",
						},
						{
							Timestamp: time.UnixMilli(then + oneMin + oneMin + oneMin),
							Line:      "this=that color=blue",
						},
						{
							Timestamp: time.UnixMilli(then + oneMin + oneMin + oneMin + thirtySeconds),
							Line:      "this=that color=blue",
						},
					},
					Hash: uint64(stream2.Fingerprint()),
				},
			},
		})
		require.NoError(t, err)

		// steps
		start := then
		secondStep := start + oneMin     // 60000
		thirdStep := secondStep + oneMin // 120000
		fourthStep := thirdStep + oneMin // 180000
		fifthStep := fourthStep + oneMin // 240000

		// our first push had a timestamp of 90000 (equal to the timestamp of it's last entry)
		// therefore our first datapoint will be at 120000, since we have nothing for
		// the first step

		t.Run("successful count over time query with grouping and 1 sample per step and selection range", func(t *testing.T) {
			expr, err := syntax.ParseSampleExpr(`sum by(foo) (count_over_time({foo="bar"}[60s]))`)
			require.NoError(t, err)

			iter, err := instance.QuerySample(ctx, expr, mockReq)
			assert.NoError(t, err)
			assert.NotNil(t, iter)

			// test that the grouping is correct
			expectedLabels := model.LabelSet{
				model.LabelName("foo"): model.LabelValue("bar"),
			}

			// first sample is at 120000, and should be a sum of the first push to both streams,
			// due to the grouping
			next := iter.Next()
			require.True(t, next)

			sample := iter.At()
			require.Equal(t, model.Time(thirdStep).UnixNano(), sample.Timestamp)
			require.Equal(t, float64(8), sample.Value)
			require.Equal(t, expectedLabels.String(), iter.Labels())

			// next should be at 180000 (fourth step), but because of our selection range, we'll have no datapoint here
			// so actual next will be 240000 (fifth step), which is late enough to see the second push at 210000
			// this point will be the sum of the second push to both streams
			next = iter.Next()
			require.True(t, next)

			sample = iter.At()
			require.Equal(t, model.Time(fifthStep).UnixNano(), sample.Timestamp)
			require.Equal(t, float64(6), sample.Value)
			require.Equal(t, expectedLabels.String(), iter.Labels())

			next = iter.Next()
			require.False(t, next)
		})

		t.Run(
			"successful count over time query with grouping and multiple samples per step and selection range",
			func(t *testing.T) {
				// with a 5m slection range we should get samples from both pushes
				expr, err := syntax.ParseSampleExpr(
					`sum by(foo) (count_over_time({foo="bar"}[5m]))`,
				)
				require.NoError(t, err)

				iter, err := instance.QuerySample(ctx, expr, mockReq)
				assert.NoError(t, err)
				assert.NotNil(t, iter)

				// test that the grouping is correct
				expectedLabels := model.LabelSet{
					model.LabelName("foo"): model.LabelValue("bar"),
				}

				// the first datapoint is again at 1200000, but from the second stream
				next := iter.Next()
				require.True(t, next)

				sample := iter.At()
				require.Equal(t, model.Time(thirdStep).UnixNano(), sample.Timestamp)
				require.Equal(t, float64(8), sample.Value)
				require.Equal(t, expectedLabels.String(), iter.Labels())

				// next will be the second step, which still only has the first push in it's selection range
				next = iter.Next()
				require.True(t, next)

				sample = iter.At()
				require.Equal(t, model.Time(fourthStep).UnixNano(), sample.Timestamp)
				require.Equal(t, float64(8), sample.Value)
				require.Equal(t, expectedLabels.String(), iter.Labels())

				// next will be the second push, which has both pushes in it's selection range
				next = iter.Next()
				require.True(t, next)

				sample = iter.At()
				require.Equal(t, model.Time(fifthStep).UnixNano(), sample.Timestamp)
				require.Equal(t, float64(14), sample.Value)
				require.Equal(t, expectedLabels.String(), iter.Labels())

				// our time range through goes to 310000, but will be truncated, so this is the end
				next = iter.Next()
				require.False(t, next)
			},
		)
	})
}
