package logql

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/logproto"
)

var nilMetrics = NewShardingMetrics(nil)

func TestMappingEquivalence(t *testing.T) {
	var (
		shards   = 3
		nStreams = 60
		rounds   = 20
		streams  = randomStreams(nStreams, rounds, shards, []string{"a", "b", "c", "d"})
		start    = time.Unix(0, 0)
		end      = time.Unix(0, int64(time.Second*time.Duration(rounds)))
		step     = time.Second
		interval = time.Duration(0)
		limit    = 100
	)

	for _, tc := range []struct {
		query       string
		approximate bool
	}{
		{`1`, false},
		{`1 + 1`, false},
		{`{a="1"}`, false},
		{`{a="1"} |= "number: 10"`, false},
		{`rate({a=~".*"}[1s])`, false},
		{`sum by (a) (rate({a=~".*"}[1s]))`, false},
		{`sum(rate({a=~".*"}[1s]))`, false},
		{`max without (a) (rate({a=~".*"}[1s]))`, false},
		{`count(rate({a=~".*"}[1s]))`, false},
		{`avg(rate({a=~".*"}[1s]))`, true},
		{`avg(rate({a=~".*"}[1s])) by (a)`, true},
		{`1 + sum by (cluster) (rate({a=~".*"}[1s]))`, false},
		{`sum(max(rate({a=~".*"}[1s])))`, false},
		{`max(count(rate({a=~".*"}[1s])))`, false},
		{`max(sum by (cluster) (rate({a=~".*"}[1s]))) / count(rate({a=~".*"}[1s]))`, false},
		// topk prefers already-seen values in tiebreakers. Since the test data generates
		// the same log lines for each series & the resulting promql.Vectors aren't deterministically
		// sorted by labels, we don't expect this to pass.
		// We could sort them as stated, but it doesn't seem worth the performance hit.
		// {`topk(3, rate({a=~".*"}[1s]))`, false},
	} {
		q := NewMockQuerier(
			shards,
			streams,
		)

		opts := EngineOpts{}
		regular := NewEngine(opts, q, NoLimits)
		sharded := NewShardedEngine(opts, MockDownstreamer{regular}, nilMetrics, NoLimits)

		t.Run(tc.query, func(t *testing.T) {
			params := NewLiteralParams(
				tc.query,
				start,
				end,
				step,
				interval,
				logproto.FORWARD,
				uint32(limit),
				nil,
			)
			qry := regular.Query(params)
			ctx := user.InjectOrgID(context.Background(), "fake")

			mapper, err := NewShardMapper(shards, nilMetrics)
			require.Nil(t, err)
			_, mapped, err := mapper.Parse(tc.query)
			require.Nil(t, err)

			shardedQry := sharded.Query(params, mapped)

			res, err := qry.Exec(ctx)
			require.Nil(t, err)

			shardedRes, err := shardedQry.Exec(ctx)
			require.Nil(t, err)

			if tc.approximate {
				approximatelyEquals(t, res.Data.(promql.Matrix), shardedRes.Data.(promql.Matrix))
			} else {
				require.Equal(t, res.Data, shardedRes.Data)
			}
		})
	}
}

// approximatelyEquals ensures two responses are approximately equal, up to 6 decimals precision per sample
func approximatelyEquals(t *testing.T, as, bs promql.Matrix) {
	require.Equal(t, len(as), len(bs))

	for i := 0; i < len(as); i++ {
		a := as[i]
		b := bs[i]
		require.Equal(t, a.Metric, b.Metric)
		require.Equal(t, len(a.Points), len(b.Points))

		for j := 0; j < len(a.Points); j++ {
			aSample := &a.Points[j]
			aSample.V = math.Round(aSample.V*1e6) / 1e6
			bSample := &b.Points[j]
			bSample.V = math.Round(bSample.V*1e6) / 1e6
		}
		require.Equal(t, a, b)
	}
}
