package pattern

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/loki/pkg/push"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/pattern/metric"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstance_QuerySample(t *testing.T) {
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

	instance, err := newInstance("test", log.NewNopLogger(), newIngesterMetrics(nil, "test"), metric.AggregationConfig{
		Enabled: true,
	})
	require.NoError(t, err)

	labels := model.LabelSet{
		model.LabelName("foo"): model.LabelValue("bar"),
	}

	lastTsMilli := (then + oneMin + thirtySeconds) // 0 + 60000 + 30000 = 90000

	// TODO(twhitney): Add a few more pushes to this or another test
	instance.Push(ctx, &logproto.PushRequest{
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
	// would expect it in the 3rd bucket
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

		sample := iter.Sample()
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

		sample := iter.Sample()
		require.Equal(t, float64(80), sample.Value)
		require.Equal(t, model.Time(thirdPoint).UnixNano(), sample.Timestamp)

		next = iter.Next()
		require.False(t, next)
	})
}
