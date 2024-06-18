package pattern

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/pattern/iter"
	"github.com/grafana/loki/v3/pkg/pattern/metric"

	"github.com/grafana/loki/pkg/push"
)

func TestAddStream(t *testing.T) {
	lbs := labels.New(labels.Label{Name: "test", Value: "test"})
	stream, err := newStream(
		model.Fingerprint(lbs.Hash()),
		lbs,
		newIngesterMetrics(nil, "test"),
		metric.NewChunkMetrics(nil, "test"),
		metric.AggregationConfig{
			Enabled: false,
		},
		log.NewNopLogger(),
	)
	require.NoError(t, err)

	err = stream.Push(context.Background(), []push.Entry{
		{
			Timestamp: time.Unix(20, 0),
			Line:      "ts=1 msg=hello",
		},
		{
			Timestamp: time.Unix(20, 0),
			Line:      "ts=2 msg=hello",
		},
		{
			Timestamp: time.Unix(10, 0),
			Line:      "ts=3 msg=hello", // this should be ignored because it's older than the last entry
		},
	})
	require.NoError(t, err)
	it, err := stream.Iterator(context.Background(), model.Earliest, model.Latest, model.Time(time.Second))
	require.NoError(t, err)
	res, err := iter.ReadAll(it)
	require.NoError(t, err)
	require.Equal(t, 1, len(res.Series))
	require.Equal(t, int64(2), res.Series[0].Samples[0].Value)
}

func TestPruneStream(t *testing.T) {
	lbs := labels.New(labels.Label{Name: "test", Value: "test"})
	stream, err := newStream(
		model.Fingerprint(lbs.Hash()),
		lbs,
		newIngesterMetrics(nil, "test"),
		metric.NewChunkMetrics(nil, "test"),
		metric.AggregationConfig{
			Enabled: false,
		},
		log.NewNopLogger(),
	)
	require.NoError(t, err)

	err = stream.Push(context.Background(), []push.Entry{
		{
			Timestamp: time.Unix(20, 0),
			Line:      "ts=1 msg=hello",
		},
		{
			Timestamp: time.Unix(20, 0),
			Line:      "ts=2 msg=hello",
		},
	})
	require.NoError(t, err)
	require.Equal(t, true, stream.prune(time.Hour))

	err = stream.Push(context.Background(), []push.Entry{
		{
			Timestamp: time.Now(),
			Line:      "ts=1 msg=hello",
		},
	})
	require.NoError(t, err)
	require.Equal(t, false, stream.prune(time.Hour))
	it, err := stream.Iterator(context.Background(), model.Earliest, model.Latest, model.Time(time.Second))
	require.NoError(t, err)
	res, err := iter.ReadAll(it)
	require.NoError(t, err)
	require.Equal(t, 1, len(res.Series))
	require.Equal(t, int64(1), res.Series[0].Samples[0].Value)
}

func TestSampleIterator(t *testing.T) {
	lbs := labels.New(
		labels.Label{Name: "foo", Value: "bar"},
		labels.Label{Name: "fizz", Value: "buzz"},
	)

	t.Run("single stream single push", func(t *testing.T) {
		stream, err := newStream(
			model.Fingerprint(lbs.Hash()),
			lbs,
			newIngesterMetrics(nil, "test"),
			metric.NewChunkMetrics(nil, "test"),
			metric.AggregationConfig{
				Enabled: true,
			},
			log.NewNopLogger(),
		)
		require.NoError(t, err)

		err = stream.Push(context.Background(), []push.Entry{
			{
				Timestamp: time.Unix(20, 0),
				Line:      "ts=1 msg=hello",
			},
			{
				Timestamp: time.Unix(20, 0),
				Line:      "ts=2 msg=hello",
			},
		})

		require.NoError(t, err)

		expr, err := syntax.ParseSampleExpr("count_over_time({foo=\"bar\"}[5s])")
		it, err := stream.SampleIterator(
			context.Background(),
			expr,
			0,
			model.Time(1*time.Minute/1e6),
			model.Time(5*time.Second/1e6),
		)
		require.NoError(t, err)

		res, err := iter.ReadAllSamples(it)
		require.Equal(t, 1, len(res.Series))
		require.Equal(t, 1, len(res.Series[0].Samples))
		require.Equal(t, float64(2), res.Series[0].Samples[0].Value)
	})

	t.Run("single stream multiple pushes", func(t *testing.T) {
		stream, err := newStream(
			model.Fingerprint(lbs.Hash()),
			lbs,
			newIngesterMetrics(nil, "test"),
			metric.NewChunkMetrics(nil, "test"),
			metric.AggregationConfig{
				Enabled: true,
			},
			log.NewNopLogger(),
		)
		require.NoError(t, err)

		err = stream.Push(context.Background(), []push.Entry{
			{
				Timestamp: time.Unix(20, 0),
				Line:      "ts=1 msg=hello",
			},
			{
				Timestamp: time.Unix(20, 0),
				Line:      "ts=2 msg=hello",
			},
		})
		require.NoError(t, err)

		err = stream.Push(context.Background(), []push.Entry{
			{
				Timestamp: time.Unix(40, 0),
				Line:      "ts=1 msg=hello",
			},
			{
				Timestamp: time.Unix(40, 0),
				Line:      "ts=2 msg=hello",
			},
		})
		require.NoError(t, err)

		t.Run("non-overlapping timestamps", func(t *testing.T) {
			expr, err := syntax.ParseSampleExpr("count_over_time({foo=\"bar\"}[5s])")
			it, err := stream.SampleIterator(
				context.Background(),
				expr,
				0,
				model.Time(1*time.Minute/1e6),
				model.Time(5*time.Second/1e6),
			)
			require.NoError(t, err)

			res, err := iter.ReadAllSamples(it)
			require.Equal(t, 1, len(res.Series))
			require.Equal(t, 2, len(res.Series[0].Samples))
			require.Equal(t, float64(2), res.Series[0].Samples[0].Value)
			require.Equal(t, float64(2), res.Series[0].Samples[1].Value)
		})

		t.Run("overlapping timestamps", func(t *testing.T) {
			expr, err := syntax.ParseSampleExpr("count_over_time({foo=\"bar\"}[1m])")
			it, err := stream.SampleIterator(
				context.Background(),
				expr,
				0,
				model.Time(1*time.Minute/1e6),
				model.Time(5*time.Second/1e6),
			)
			require.NoError(t, err)

			res, err := iter.ReadAllSamples(it)
			require.Equal(t, 1, len(res.Series))
			require.Equal(
				t,
				8,
				len(res.Series[0].Samples),
			) // bigger selection range keeps pushes in winodow longer
			require.Equal(t, float64(2), res.Series[0].Samples[0].Value)
			require.Equal(t, float64(4), res.Series[0].Samples[7].Value)
		})
	})
}
