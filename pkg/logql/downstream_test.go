package logql

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/querier/astmapper"
)

var nilShardMetrics = NewShardMapperMetrics(nil)
var nilRangeMetrics = NewRangeMapperMetrics(nil)

func TestMappingEquivalence(t *testing.T) {
	var (
		shards   = 3
		nStreams = 60
		rounds   = 20
		streams  = randomStreams(nStreams, rounds+1, shards, []string{"a", "b", "c", "d"}, true)
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
		{`rate({a=~".+"}[1s])`, false},
		{`sum by (a) (rate({a=~".+"}[1s]))`, false},
		{`sum(rate({a=~".+"}[1s]))`, false},
		{`max without (a) (rate({a=~".+"}[1s]))`, false},
		{`count(rate({a=~".+"}[1s]))`, false},
		{`avg(rate({a=~".+"}[1s]))`, true},
		{`avg(rate({a=~".+"}[1s])) by (a)`, true},
		{`1 + sum by (cluster) (rate({a=~".+"}[1s]))`, false},
		{`sum(max(rate({a=~".+"}[1s])))`, false},
		{`max(count(rate({a=~".+"}[1s])))`, false},
		{`max(sum by (cluster) (rate({a=~".+"}[1s]))) / count(rate({a=~".+"}[1s]))`, false},
		{`sum(rate({a=~".+"} |= "foo" != "foo"[1s]) or vector(1))`, false},
		{`avg_over_time({a=~".+"} | logfmt | unwrap value [1s])`, false},
		{`avg_over_time({a=~".+"} | logfmt | unwrap value [1s]) by (a)`, true},
		{`quantile_over_time(0.99, {a=~".+"} | logfmt | unwrap value [1s])`, true},
		{
			`
			  (quantile_over_time(0.99, {a=~".+"} | logfmt | unwrap value [1s]) by (a) > 1)
			and
			  avg by (a) (rate({a=~".+"}[1s]))
			`,
			false,
		},
		// topk prefers already-seen values in tiebreakers. Since the test data generates
		// the same log lines for each series & the resulting promql.Vectors aren't deterministically
		// sorted by labels, we don't expect this to pass.
		// We could sort them as stated, but it doesn't seem worth the performance hit.
		// {`topk(3, rate({a=~".+"}[1s]))`, false},
	} {
		q := NewMockQuerier(
			shards,
			streams,
		)

		opts := EngineOpts{}
		regular := NewEngine(opts, q, NoLimits, log.NewNopLogger())
		sharded := NewDownstreamEngine(opts, MockDownstreamer{regular}, NoLimits, log.NewNopLogger())

		t.Run(tc.query, func(t *testing.T) {
			params, err := NewLiteralParams(
				tc.query,
				start,
				end,
				step,
				interval,
				logproto.FORWARD,
				uint32(limit),
				nil,
			)
			require.NoError(t, err)

			qry := regular.Query(params)
			ctx := user.InjectOrgID(context.Background(), "fake")

			mapper := NewShardMapper(ConstantShards(shards), nilShardMetrics, []string{})
			_, _, mapped, err := mapper.Parse(params.GetExpression())
			require.NoError(t, err)

			shardedQry := sharded.Query(ctx, ParamsWithExpressionOverride{Params: params, ExpressionOverride: mapped})

			res, err := qry.Exec(ctx)
			require.NoError(t, err)

			shardedRes, err := shardedQry.Exec(ctx)
			require.NoError(t, err)

			if tc.approximate {
				approximatelyEquals(t, res.Data.(promql.Matrix), shardedRes.Data.(promql.Matrix))
			} else {
				require.Equal(t, res.Data, shardedRes.Data)
			}
		})
	}
}

func TestMappingEquivalenceSketches(t *testing.T) {
	var (
		shards   = 3
		nStreams = 10_000
		rounds   = 20
		streams  = randomStreams(nStreams, rounds+1, shards, []string{"a", "b", "c", "d"}, true)
		start    = time.Unix(0, 0)
		end      = time.Unix(0, int64(time.Second*time.Duration(rounds)))
		step     = time.Second
		interval = time.Duration(0)
		limit    = 100
	)

	for _, tc := range []struct {
		query         string
		realtiveError float64
	}{
		{`quantile_over_time(0.70, {a=~".+"} | logfmt | unwrap value [1s]) by (a)`, 0.03},
		{`quantile_over_time(0.99, {a=~".+"} | logfmt | unwrap value [1s]) by (a)`, 0.02},
	} {
		q := NewMockQuerier(
			shards,
			streams,
		)

		opts := EngineOpts{}
		regular := NewEngine(opts, q, NoLimits, log.NewNopLogger())
		sharded := NewDownstreamEngine(opts, MockDownstreamer{regular}, NoLimits, log.NewNopLogger())

		t.Run(tc.query+"_range", func(t *testing.T) {
			params, err := NewLiteralParams(
				tc.query,
				start,
				end,
				step,
				interval,
				logproto.FORWARD,
				uint32(limit),
				nil,
			)
			require.NoError(t, err)
			qry := regular.Query(params)
			ctx := user.InjectOrgID(context.Background(), "fake")

			mapper := NewShardMapper(ConstantShards(shards), nilShardMetrics, []string{ShardQuantileOverTime})
			_, _, mapped, err := mapper.Parse(params.GetExpression())
			require.NoError(t, err)

			shardedQry := sharded.Query(ctx, ParamsWithExpressionOverride{
				Params:             params,
				ExpressionOverride: mapped,
			})

			res, err := qry.Exec(ctx)
			require.NoError(t, err)

			shardedRes, err := shardedQry.Exec(ctx)
			require.NoError(t, err)

			relativeError(t, res.Data.(promql.Matrix), shardedRes.Data.(promql.Matrix), tc.realtiveError)
		})
		t.Run(tc.query+"_instant", func(t *testing.T) {
			// for an instant query we set the start and end to the same timestamp
			// plus set step and interval to 0
			params, err := NewLiteralParams(
				tc.query,
				time.Unix(0, int64(rounds+1)),
				time.Unix(0, int64(rounds+1)),
				0,
				0,
				logproto.FORWARD,
				uint32(limit),
				nil,
			)
			require.NoError(t, err)
			qry := regular.Query(params)
			ctx := user.InjectOrgID(context.Background(), "fake")

			mapper := NewShardMapper(ConstantShards(shards), nilShardMetrics, []string{ShardQuantileOverTime})
			_, _, mapped, err := mapper.Parse(params.GetExpression())
			require.NoError(t, err)

			shardedQry := sharded.Query(ctx, ParamsWithExpressionOverride{
				Params:             params,
				ExpressionOverride: mapped,
			})

			res, err := qry.Exec(ctx)
			require.NoError(t, err)

			shardedRes, err := shardedQry.Exec(ctx)
			require.NoError(t, err)

			relativeErrorVector(t, res.Data.(promql.Vector), shardedRes.Data.(promql.Vector), tc.realtiveError)
		})
	}
}

func TestShardCounter(t *testing.T) {
	var (
		shards   = 3
		nStreams = 60
		rounds   = 20
		streams  = randomStreams(nStreams, rounds+1, shards, []string{"a", "b", "c", "d"}, false)
		start    = time.Unix(0, 0)
		end      = time.Unix(0, int64(time.Second*time.Duration(rounds)))
		step     = time.Second
		interval = time.Duration(0)
		limit    = 100
	)

	for _, tc := range []struct {
		query string
	}{
		// Test a few queries which will not shard and shard
		// Avoid testing queries where the shard mapping produces a different query such as avg()
		{`1`},
		{`rate({a=~".+"}[1s])`},
		{`sum by (a) (rate({a=~".+"}[1s]))`},
	} {
		q := NewMockQuerier(
			shards,
			streams,
		)

		opts := EngineOpts{}
		regular := NewEngine(opts, q, NoLimits, log.NewNopLogger())
		sharded := NewDownstreamEngine(opts, MockDownstreamer{regular}, NoLimits, log.NewNopLogger())

		t.Run(tc.query, func(t *testing.T) {
			params, err := NewLiteralParams(
				tc.query,
				start,
				end,
				step,
				interval,
				logproto.FORWARD,
				uint32(limit),
				nil,
			)
			require.NoError(t, err)
			ctx := user.InjectOrgID(context.Background(), "fake")

			mapper := NewShardMapper(ConstantShards(shards), nilShardMetrics, []string{ShardQuantileOverTime})
			noop, _, mapped, err := mapper.Parse(params.GetExpression())
			require.NoError(t, err)

			shardedQry := sharded.Query(ctx, ParamsWithExpressionOverride{Params: params, ExpressionOverride: mapped})

			shardedRes, err := shardedQry.Exec(ctx)
			require.Nil(t, err)

			if noop {
				assert.Equal(t, int64(0), shardedRes.Statistics.Summary.Shards)
			} else {
				assert.Equal(t, int64(shards), shardedRes.Statistics.Summary.Shards)
			}
		})
	}
}

func TestRangeMappingEquivalence(t *testing.T) {
	var (
		shards   = 3
		nStreams = 60
		rounds   = 20
		streams  = randomStreams(nStreams, rounds+1, shards, []string{"a", "b", "c", "d"}, false)
		start    = time.Unix(0, 0)
		end      = time.Unix(0, int64(time.Second*time.Duration(rounds)))
		step     = time.Second
		interval = time.Duration(0)
		limit    = 100
	)

	for _, tc := range []struct {
		query           string
		splitByInterval time.Duration
	}{
		// Range vector aggregators
		{`bytes_over_time({a=~".+"}[2s])`, time.Second},
		{`count_over_time({a=~".+"}[2s])`, time.Second},
		{`sum_over_time({a=~".+"} | unwrap b [2s])`, time.Second},
		{`max_over_time({a=~".+"} | unwrap b [2s])`, time.Second},
		{`max_over_time({a=~".+"} | unwrap b [2s]) by (a)`, time.Second},
		{`min_over_time({a=~".+"} | unwrap b [2s])`, time.Second},
		{`min_over_time({a=~".+"} | unwrap b [2s]) by (a)`, time.Second},
		{`rate({a=~".+"}[2s])`, time.Second},
		{`rate({a=~".+"} | unwrap b [2s])`, time.Second},
		{`bytes_rate({a=~".+"}[2s])`, time.Second},

		// sum
		{`sum(bytes_over_time({a=~".+"}[2s]))`, time.Second},
		{`sum(count_over_time({a=~".+"}[2s]))`, time.Second},
		{`sum(sum_over_time({a=~".+"} | unwrap b [2s]))`, time.Second},
		{`sum(max_over_time({a=~".+"} | unwrap b [2s]))`, time.Second},
		{`sum(max_over_time({a=~".+"} | unwrap b [2s]) by (a))`, time.Second},
		{`sum(min_over_time({a=~".+"} | unwrap b [2s]))`, time.Second},
		{`sum(min_over_time({a=~".+"} | unwrap b [2s]) by (a))`, time.Second},
		{`sum(rate({a=~".+"}[2s]))`, time.Second},
		{`sum(rate({a=~".+"} | unwrap b [2s]))`, time.Second},
		{`sum(bytes_rate({a=~".+"}[2s]))`, time.Second},

		// sum by
		{`sum by (a) (bytes_over_time({a=~".+"}[2s]))`, time.Second},
		{`sum by (a) (count_over_time({a=~".+"}[2s]))`, time.Second},
		{`sum by (a) (sum_over_time({a=~".+"} | unwrap b [2s]))`, time.Second},
		{`sum by (a) (max_over_time({a=~".+"} | unwrap b [2s]))`, time.Second},
		{`sum by (a) (max_over_time({a=~".+"} | unwrap b [2s]) by (a))`, time.Second},
		{`sum by (a) (min_over_time({a=~".+"} | unwrap b [2s]))`, time.Second},
		{`sum by (a) (min_over_time({a=~".+"} | unwrap b [2s]) by (a))`, time.Second},
		{`sum by (a) (rate({a=~".+"}[2s]))`, time.Second},
		{`sum by (a) (rate({a=~".+"} | unwrap b [2s]))`, time.Second},
		{`sum by (a) (bytes_rate({a=~".+"}[2s]))`, time.Second},

		// count
		{`count(bytes_over_time({a=~".+"}[2s]))`, time.Second},
		{`count(count_over_time({a=~".+"}[2s]))`, time.Second},
		{`count(sum_over_time({a=~".+"} | unwrap b [2s]))`, time.Second},
		{`count(max_over_time({a=~".+"} | unwrap b [2s]))`, time.Second},
		{`count(max_over_time({a=~".+"} | unwrap b [2s]) by (a))`, time.Second},
		{`count(min_over_time({a=~".+"} | unwrap b [2s]))`, time.Second},
		{`count(min_over_time({a=~".+"} | unwrap b [2s]) by (a))`, time.Second},
		{`count(rate({a=~".+"}[2s]))`, time.Second},
		{`count(rate({a=~".+"} | unwrap b [2s]))`, time.Second},
		{`count(bytes_rate({a=~".+"}[2s]))`, time.Second},

		// count by
		{`count by (a) (bytes_over_time({a=~".+"}[2s]))`, time.Second},
		{`count by (a) (count_over_time({a=~".+"}[2s]))`, time.Second},
		{`count by (a) (sum_over_time({a=~".+"} | unwrap b [2s]))`, time.Second},
		{`count by (a) (max_over_time({a=~".+"} | unwrap b [2s]))`, time.Second},
		{`count by (a) (max_over_time({a=~".+"} | unwrap b [2s]) by (a))`, time.Second},
		{`count by (a) (min_over_time({a=~".+"} | unwrap b [2s]))`, time.Second},
		{`count by (a) (min_over_time({a=~".+"} | unwrap b [2s]) by (a))`, time.Second},
		{`count by (a) (rate({a=~".+"}[2s]))`, time.Second},
		{`count by (a) (rate({a=~".+"} | unwrap b [2s]))`, time.Second},
		{`count by (a) (bytes_rate({a=~".+"}[2s]))`, time.Second},

		// max
		{`max(bytes_over_time({a=~".+"}[2s]))`, time.Second},
		{`max(count_over_time({a=~".+"}[2s]))`, time.Second},
		{`max(sum_over_time({a=~".+"} | unwrap b [2s]))`, time.Second},
		{`max(max_over_time({a=~".+"} | unwrap b [2s]))`, time.Second},
		{`max(max_over_time({a=~".+"} | unwrap b [2s]) by (a))`, time.Second},
		{`max(min_over_time({a=~".+"} | unwrap b [2s]))`, time.Second},
		{`max(min_over_time({a=~".+"} | unwrap b [2s]) by (a))`, time.Second},
		{`max(rate({a=~".+"}[2s]))`, time.Second},
		{`max(rate({a=~".+"} | unwrap b [2s]))`, time.Second},
		{`max(bytes_rate({a=~".+"}[2s]))`, time.Second},

		// max by
		{`max by (a) (bytes_over_time({a=~".+"}[2s]))`, time.Second},
		{`max by (a) (count_over_time({a=~".+"}[2s]))`, time.Second},
		{`max by (a) (sum_over_time({a=~".+"} | unwrap b [2s]))`, time.Second},
		{`max by (a) (max_over_time({a=~".+"} | unwrap b [2s]))`, time.Second},
		{`max by (a) (max_over_time({a=~".+"} | unwrap b [2s]) by (a))`, time.Second},
		{`max by (a) (min_over_time({a=~".+"} | unwrap b [2s]))`, time.Second},
		{`max by (a) (min_over_time({a=~".+"} | unwrap b [2s]) by (a))`, time.Second},
		{`max by (a) (rate({a=~".+"}[2s]))`, time.Second},
		{`max by (a) (rate({a=~".+"} | unwrap b [2s]))`, time.Second},
		{`max by (a) (bytes_rate({a=~".+"}[2s]))`, time.Second},

		// min
		{`min(bytes_over_time({a=~".+"}[2s]))`, time.Second},
		{`min(count_over_time({a=~".+"}[2s]))`, time.Second},
		{`min(sum_over_time({a=~".+"} | unwrap b [2s]))`, time.Second},
		{`min(max_over_time({a=~".+"} | unwrap b [2s]))`, time.Second},
		{`min(max_over_time({a=~".+"} | unwrap b [2s]) by (a))`, time.Second},
		{`min(min_over_time({a=~".+"} | unwrap b [2s]))`, time.Second},
		{`min(min_over_time({a=~".+"} | unwrap b [2s]) by (a))`, time.Second},
		{`min(rate({a=~".+"}[2s]))`, time.Second},
		{`min(rate({a=~".+"} | unwrap b [2s]))`, time.Second},
		{`min(bytes_rate({a=~".+"}[2s]))`, time.Second},

		// min by
		{`min by (a) (bytes_over_time({a=~".+"}[2s]))`, time.Second},
		{`min by (a) (count_over_time({a=~".+"}[2s]))`, time.Second},
		{`min by (a) (sum_over_time({a=~".+"} | unwrap b [2s]))`, time.Second},
		{`min by (a) (max_over_time({a=~".+"} | unwrap b [2s]))`, time.Second},
		{`min by (a) (max_over_time({a=~".+"} | unwrap b [2s]) by (a))`, time.Second},
		{`min by (a) (min_over_time({a=~".+"} | unwrap b [2s]))`, time.Second},
		{`min by (a) (min_over_time({a=~".+"} | unwrap b [2s]) by (a))`, time.Second},
		{`min by (a) (rate({a=~".+"}[2s]))`, time.Second},
		{`min by (a) (rate({a=~".+"} | unwrap b [2s]))`, time.Second},
		{`min by (a) (bytes_rate({a=~".+"}[2s]))`, time.Second},

		// Label extraction stage
		{`max_over_time({a=~".+"} | logfmt | unwrap line [2s]) by (a)`, time.Second},
		{`min_over_time({a=~".+"} | logfmt | unwrap line [2s]) by (a)`, time.Second},

		{`sum(bytes_over_time({a=~".+"} | logfmt | line > 5 [2s]))`, time.Second},
		{`sum(count_over_time({a=~".+"} | logfmt [2s]))`, time.Second},
		{`sum(sum_over_time({a=~".+"} | logfmt | unwrap line [2s]))`, time.Second},
		{`sum(max_over_time({a=~".+"} | logfmt | unwrap line [2s]))`, time.Second},
		{`sum(max_over_time({a=~".+"} | logfmt | unwrap line [2s]) by (a))`, time.Second},
		{`sum(min_over_time({a=~".+"} | logfmt | unwrap line [2s]))`, time.Second},
		{`sum(min_over_time({a=~".+"} | logfmt | unwrap line [2s]) by (a))`, time.Second},
		{`sum(rate({a=~".+"} | logfmt[2s]))`, time.Second},
		{`sum(rate({a=~".+"} | logfmt | unwrap line [2s]))`, time.Second},
		{`sum(bytes_rate({a=~".+"} | logfmt[2s]))`, time.Second},
		{`sum by (a) (bytes_over_time({a=~".+"} | logfmt [2s]))`, time.Second},
		{`sum by (a) (count_over_time({a=~".+"} | logfmt [2s]))`, time.Second},
		{`sum by (a) (sum_over_time({a=~".+"} | logfmt | unwrap line [2s]))`, time.Second},
		{`sum by (a) (max_over_time({a=~".+"} | logfmt | unwrap line [2s]))`, time.Second},
		{`sum by (a) (max_over_time({a=~".+"} | logfmt | unwrap line [2s]) by (a))`, time.Second},
		{`sum by (a) (min_over_time({a=~".+"} | logfmt | unwrap line [2s]))`, time.Second},
		{`sum by (a) (min_over_time({a=~".+"} | logfmt | unwrap line [2s]) by (a))`, time.Second},
		{`sum by (a) (rate({a=~".+"} | logfmt[2s]))`, time.Second},
		{`sum by (a) (rate({a=~".+"} | logfmt | unwrap line [2s]))`, time.Second},
		{`sum by (a) (bytes_rate({a=~".+"} | logfmt[2s]))`, time.Second},

		{`count(max_over_time({a=~".+"} | logfmt | unwrap line [2s]) by (a))`, time.Second},
		{`count(min_over_time({a=~".+"} | logfmt | unwrap line [2s]) by (a))`, time.Second},
		{`count by (a) (max_over_time({a=~".+"} | logfmt | unwrap line [2s]) by (a))`, time.Second},
		{`count by (a) (min_over_time({a=~".+"} | logfmt | unwrap line [2s]) by (a))`, time.Second},

		{`max(bytes_over_time({a=~".+"} | logfmt [2s]))`, time.Second},
		{`max(count_over_time({a=~".+"} | logfmt [2s]))`, time.Second},
		{`max(sum_over_time({a=~".+"} | logfmt | unwrap line [2s]))`, time.Second},
		{`max(max_over_time({a=~".+"} | logfmt | unwrap line [2s]))`, time.Second},
		{`max(max_over_time({a=~".+"} | logfmt | unwrap line [2s]) by (a))`, time.Second},
		{`max(min_over_time({a=~".+"} | logfmt | unwrap line [2s]))`, time.Second},
		{`max(min_over_time({a=~".+"} | logfmt | unwrap line [2s]) by (a))`, time.Second},
		{`max by (a) (bytes_over_time({a=~".+"} | logfmt [2s]))`, time.Second},
		{`max by (a) (count_over_time({a=~".+"} | logfmt [2s]))`, time.Second},
		{`max by (a) (sum_over_time({a=~".+"} | logfmt | unwrap line [2s]))`, time.Second},
		{`max by (a) (max_over_time({a=~".+"} | logfmt | unwrap line [2s]))`, time.Second},
		{`max by (a) (max_over_time({a=~".+"} | logfmt | unwrap line [2s]) by (a))`, time.Second},
		{`max by (a) (min_over_time({a=~".+"} | logfmt | unwrap line [2s]))`, time.Second},
		{`max by (a) (min_over_time({a=~".+"} | logfmt | unwrap line [2s]) by (a))`, time.Second},

		{`min(bytes_over_time({a=~".+"} | logfmt [2s]))`, time.Second},
		{`min(count_over_time({a=~".+"} | logfmt [2s]))`, time.Second},
		{`min(sum_over_time({a=~".+"} | logfmt | unwrap line [2s]))`, time.Second},
		{`min(max_over_time({a=~".+"} | logfmt | unwrap line [2s]))`, time.Second},
		{`min(max_over_time({a=~".+"} | logfmt | unwrap line [2s]) by (a))`, time.Second},
		{`min(min_over_time({a=~".+"} | logfmt | unwrap line [2s]))`, time.Second},
		{`min(min_over_time({a=~".+"} | logfmt | unwrap line [2s]) by (a))`, time.Second},
		{`min by (a) (bytes_over_time({a=~".+"} | logfmt [2s]))`, time.Second},
		{`min by (a) (count_over_time({a=~".+"} | logfmt [2s]))`, time.Second},
		{`min by (a) (sum_over_time({a=~".+"} | logfmt | unwrap line [2s]))`, time.Second},
		{`min by (a) (max_over_time({a=~".+"} | logfmt | unwrap line [2s]))`, time.Second},
		{`min by (a) (max_over_time({a=~".+"} | logfmt | unwrap line [2s]) by (a))`, time.Second},
		{`min by (a) (min_over_time({a=~".+"} | logfmt | unwrap line [2s]))`, time.Second},
		{`min by (a) (min_over_time({a=~".+"} | logfmt | unwrap line [2s]) by (a))`, time.Second},

		// Binary operations
		{`2 * bytes_over_time({a=~".+"}[3s])`, time.Second},
		{`count_over_time({a=~".+"}[3s]) * 2`, time.Second},
		{`bytes_over_time({a=~".+"}[3s]) + count_over_time({a=~".+"}[5s])`, time.Second},
		{`sum(count_over_time({a=~".+"}[3s]) * count(sum_over_time({a=~".+"} | unwrap b [5s])))`, time.Second},
		{`sum by (a) (count_over_time({a=~".+"} | logfmt | line > 5 [3s])) / sum by (a) (count_over_time({a=~".+"} [3s]))`, time.Second},

		// Multi vector aggregator layer queries
		{`sum(max(bytes_over_time({a=~".+"}[3s])))`, time.Second},
		{`sum(min by (a)(max(sum by (b) (count_over_time({a=~".+"} [2s])))))`, time.Second},

		// Non-splittable vector aggregators
		// TODO: Fix topk
		//{`topk(2, count_over_time({a=~".+"}[2s]))`, time.Second},
		{`avg(count_over_time({a=~".+"}[2s]))`, time.Second},

		// Uneven split times
		{`bytes_over_time({a=~".+"}[3s])`, 2 * time.Second},
		{`count_over_time({a=~".+"}[5s])`, 2 * time.Second},

		// range with offset
		{`rate({a=~".+"}[2s] offset 2s)`, time.Second},
		{`rate({a=~".+"}[4s] offset 1s)`, 2 * time.Second},
		{`rate({a=~".+"}[3s] offset 1s)`, 2 * time.Second},
		{`rate({a=~".+"}[5s] offset 0s)`, 2 * time.Second},
		{`rate({a=~".+"}[3s] offset -1s)`, 2 * time.Second},

		// label_replace
		{`label_replace(sum by (a) (count_over_time({a=~".+"}[3s])), "", "", "", "")`, time.Second},
		{`label_replace(sum by (a) (count_over_time({a=~".+"}[3s])), "foo", "$1", "a", "(.*)")`, time.Second},
	} {
		q := NewMockQuerier(
			shards,
			streams,
		)

		opts := EngineOpts{}
		regularEngine := NewEngine(opts, q, NoLimits, log.NewNopLogger())
		downstreamEngine := NewDownstreamEngine(opts, MockDownstreamer{regularEngine}, NoLimits, log.NewNopLogger())

		t.Run(tc.query, func(t *testing.T) {
			ctx := user.InjectOrgID(context.Background(), "fake")

			params, err := NewLiteralParams(
				tc.query,
				start,
				end,
				step,
				interval,
				logproto.FORWARD,
				uint32(limit),
				nil,
			)
			require.NoError(t, err)

			// Regular engine
			qry := regularEngine.Query(params)
			res, err := qry.Exec(ctx)
			require.NoError(t, err)

			// Downstream engine - split by range
			rangeMapper, err := NewRangeMapper(tc.splitByInterval, nilRangeMetrics, NewMapperStats())
			require.NoError(t, err)
			noop, rangeExpr, err := rangeMapper.Parse(syntax.MustParseExpr(tc.query))
			require.NoError(t, err)

			require.False(t, noop, "downstream engine cannot execute noop")

			rangeQry := downstreamEngine.Query(ctx, ParamsWithExpressionOverride{Params: params, ExpressionOverride: rangeExpr})
			rangeRes, err := rangeQry.Exec(ctx)
			require.Nil(t, err)

			require.Equal(t, res.Data, rangeRes.Data)
		})
	}
}

// approximatelyEquals ensures two responses are approximately equal,
// up to 6 decimals precision per sample
func approximatelyEquals(t *testing.T, as, bs promql.Matrix) {
	require.Len(t, bs, len(as))

	for i := 0; i < len(as); i++ {
		a := as[i]
		b := bs[i]
		require.Equal(t, a.Metric, b.Metric)
		require.Lenf(t, b.Floats, len(a.Floats), "at step %d", i)

		for j := 0; j < len(a.Floats); j++ {
			aSample := &a.Floats[j]
			aSample.F = math.Round(aSample.F*1e6) / 1e6
			bSample := &b.Floats[j]
			bSample.F = math.Round(bSample.F*1e6) / 1e6
		}
		require.Equalf(t, a, b, "metric %s differs from %s at %d", a.Metric, b.Metric, i)
	}
}

func relativeError(t *testing.T, expected, actual promql.Matrix, alpha float64) {
	require.Len(t, actual, len(expected))

	for i := 0; i < len(expected); i++ {
		expectedSeries := expected[i]
		actualSeries := actual[i]
		require.Equal(t, expectedSeries.Metric, actualSeries.Metric)
		require.Lenf(t, actualSeries.Floats, len(expectedSeries.Floats), "for series %s", expectedSeries.Metric)

		e := make([]float64, len(expectedSeries.Floats))
		a := make([]float64, len(expectedSeries.Floats))
		for j := 0; j < len(expectedSeries.Floats); j++ {
			e[j] = expectedSeries.Floats[j].F
			a[j] = actualSeries.Floats[j].F
		}
		require.InEpsilonSlice(t, e, a, alpha)
	}
}

func relativeErrorVector(t *testing.T, expected, actual promql.Vector, alpha float64) {
	require.Len(t, actual, len(expected))

	e := make([]float64, len(expected))
	a := make([]float64, len(expected))
	for i := 0; i < len(expected); i++ {
		require.Equal(t, expected[i].Metric, actual[i].Metric)

		e[i] = expected[i].F
		a[i] = expected[i].F
	}
	require.InEpsilonSlice(t, e, a, alpha)

}

func TestFormat_ShardedExpr(t *testing.T) {
	oldMax := syntax.MaxCharsPerLine
	syntax.MaxCharsPerLine = 20

	oldDefaultDepth := defaultMaxDepth
	defaultMaxDepth = 2
	defer func() {
		syntax.MaxCharsPerLine = oldMax
		defaultMaxDepth = oldDefaultDepth
	}()

	cases := []struct {
		name string
		in   syntax.Expr
		exp  string
	}{
		{
			name: "ConcatSampleExpr",
			in: &ConcatSampleExpr{
				DownstreamSampleExpr: DownstreamSampleExpr{
					shard: &astmapper.ShardAnnotation{
						Shard: 0,
						Of:    3,
					},
					SampleExpr: &syntax.RangeAggregationExpr{
						Operation: syntax.OpRangeTypeRate,
						Left: &syntax.LogRange{
							Left: &syntax.MatchersExpr{
								Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
							},
							Interval: time.Minute,
						},
					},
				},
				next: &ConcatSampleExpr{
					DownstreamSampleExpr: DownstreamSampleExpr{
						shard: &astmapper.ShardAnnotation{
							Shard: 1,
							Of:    3,
						},
						SampleExpr: &syntax.RangeAggregationExpr{
							Operation: syntax.OpRangeTypeRate,
							Left: &syntax.LogRange{
								Left: &syntax.MatchersExpr{
									Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
								},
								Interval: time.Minute,
							},
						},
					},
					next: &ConcatSampleExpr{
						DownstreamSampleExpr: DownstreamSampleExpr{
							shard: &astmapper.ShardAnnotation{
								Shard: 1,
								Of:    3,
							},
							SampleExpr: &syntax.RangeAggregationExpr{
								Operation: syntax.OpRangeTypeRate,
								Left: &syntax.LogRange{
									Left: &syntax.MatchersExpr{
										Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
									},
									Interval: time.Minute,
								},
							},
						},
						next: nil,
					},
				},
			},
			exp: `concat(
  downstream<
    rate(
      {foo="bar"} [1m]
    ),
    shard=0_of_3
  >
  ++
  downstream<
    rate(
      {foo="bar"} [1m]
    ),
    shard=1_of_3
  >
  ++ ...
)`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := syntax.Prettify(c.in)
			assert.Equal(t, c.exp, got)
		})
	}
}

func TestPrettierWithoutShards(t *testing.T) {
	q := `((quantile_over_time(0.5,{foo="bar"} | json | unwrap bytes[1d]) by (cluster) > 42) and (count by (cluster)(max_over_time({foo="baz"} |= "error" | json | unwrap bytes[1d]) by (cluster,namespace)) > 10))`
	e := syntax.MustParseExpr(q)

	mapper := NewShardMapper(ConstantShards(4), nilShardMetrics, []string{})
	_, _, mapped, err := mapper.Parse(e)
	require.NoError(t, err)
	got := syntax.Prettify(mapped)
	expected := `    downstream<quantile_over_time(0.5,{foo="bar"} | json | unwrap bytes[1d]) by (cluster), shard=<nil>>
  >
    42
and
    count by (cluster)(
      max by (cluster, namespace)(
        concat(
          downstream<
            max_over_time({foo="baz"} |= "error" | json | unwrap bytes[1d]) by (cluster,namespace),
            shard=0_of_4
          >
          ++
          downstream<
            max_over_time({foo="baz"} |= "error" | json | unwrap bytes[1d]) by (cluster,namespace),
            shard=1_of_4
          >
          ++
          downstream<
            max_over_time({foo="baz"} |= "error" | json | unwrap bytes[1d]) by (cluster,namespace),
            shard=2_of_4
          >
          ++
          downstream<
            max_over_time({foo="baz"} |= "error" | json | unwrap bytes[1d]) by (cluster,namespace),
            shard=3_of_4
          >
        )
      )
    )
  >
    10`
	assert.Equal(t, expected, got)
}

func TestParseShardCount(t *testing.T) {
	for _, st := range []struct {
		name     string
		shards   []string
		expected int
	}{
		{
			name:     "empty shards",
			shards:   []string{},
			expected: 0,
		},
		{
			name:     "single shard",
			shards:   []string{"0_of_3"},
			expected: 3,
		},
		{
			name:     "single shard with error",
			shards:   []string{"0_of_"},
			expected: 0,
		},
		{
			name:     "multiple shards",
			shards:   []string{"0_of_3", "0_of_4"},
			expected: 3,
		},
		{
			name:     "multiple shards with errors",
			shards:   []string{"_of_3", "0_of_4"},
			expected: 4,
		},
	} {
		t.Run(st.name, func(t *testing.T) {
			require.Equal(t, st.expected, ParseShardCount(st.shards))
		})

	}
}
