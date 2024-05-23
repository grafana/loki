package pattern

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/loki/pkg/push"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstance_QuerySample(t *testing.T) {
	ctx := context.Background()
	thirtySeconds := int64(30000)
	oneMin := int64(60000)
	fiveMin := oneMin * 5
	now := int64(1715964275000)
	then := now - fiveMin // 1715963975000

	mockReq := &logproto.QueryPatternsRequest{
		Start: time.Unix(then/1000, 0),
		End:   time.Now(),
		Step:  oneMin,
	}

	instance, err := newInstance("test", log.NewNopLogger(), nil)
	require.NoError(t, err)

	labels := model.LabelSet{
		model.LabelName("foo"): model.LabelValue("bar"),
	}

	lastTsMilli := (then + oneMin + oneMin) // 1715964095000

  //TODO(twhitney): Add a few more pushes to this or another test
	instance.Push(ctx, &logproto.PushRequest{
		Streams: []push.Stream{
			{
				Labels: labels.String(),
				Entries: []push.Entry{
					{
						Timestamp: time.Unix(then/1000, 0),
						Line:      "this=that color=blue",
					},
					{
						Timestamp: time.Unix((then+thirtySeconds)/1000, 0),
						Line:      "this=that color=blue",
					},
					{
						Timestamp: time.Unix((then+oneMin)/1000, 0),
						Line:      "this=that color=blue",
					},
					{
						Timestamp: time.Unix(lastTsMilli/1000, 0),
						Line:      "this=that color=blue",
					},
				},
				Hash: uint64(labels.Fingerprint()),
			},
		},
	})

	t.Run("successful count over time query", func(t *testing.T) {
		expr, err := syntax.ParseSampleExpr(`count_over_time({foo="bar"}[30s])`)
		require.NoError(t, err)

		iter, err := instance.QuerySample(ctx, expr, mockReq)
		assert.NoError(t, err)
		assert.NotNil(t, iter)

		// start is request start minus range, which is 30s here
		start := then - 30000
		require.True(t, start < lastTsMilli-30000)
		secondPoint := start + oneMin
		require.True(t, secondPoint < lastTsMilli-30000)
		// this is the first point past the lastTsMilli
		thirdPoint := secondPoint + oneMin
		require.Equal(t, lastTsMilli-30000, thirdPoint)

		next := iter.Next()
		require.True(t, next)

		sample := iter.At()
		require.Equal(t, int64(4), sample.Value)
		require.Equal(t, model.Time(thirdPoint), sample.Timestamp)

		next = iter.Next()
		require.False(t, next)
	})

	t.Run("successful bytes over time query", func(t *testing.T) {
		expr, err := syntax.ParseSampleExpr(`bytes_over_time({foo="bar"}[30s])`)
		require.NoError(t, err)

		iter, err := instance.QuerySample(ctx, expr, mockReq)
		assert.NoError(t, err)
		assert.NotNil(t, iter)

		next := iter.Next()
		require.True(t, next)

		expctedTs := (then - 30000) + oneMin + oneMin
		sample := iter.At()
		require.Equal(t, int64(80), sample.Value)
		require.Equal(t, model.Time(expctedTs), sample.Timestamp)

		next = iter.Next()
		require.False(t, next)
	})
}
