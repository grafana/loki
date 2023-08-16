package logql

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/promql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logproto"
)

var nilShardMetrics = NewShardMapperMetrics(nil)
var nilRangeMetrics = NewRangeMapperMetrics(nil)

func TestMappingEquivalence(t *testing.T) {
	var (
		shards   = 3
		nStreams = 60
		rounds   = 20
		streams  = randomStreams(nStreams, rounds+1, shards, []string{"a", "b", "c", "d"})
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

			// TODO: test with probabilistic true
			mapper := NewShardMapper(ConstantShards(shards), false, nilShardMetrics)
			_, _, mapped, err := mapper.Parse(tc.query)
			require.Nil(t, err)

			shardedQry := sharded.Query(ctx, params, mapped)

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

func TestShardCounter(t *testing.T) {
	var (
		shards   = 3
		nStreams = 60
		rounds   = 20
		streams  = randomStreams(nStreams, rounds+1, shards, []string{"a", "b", "c", "d"})
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
			ctx := user.InjectOrgID(context.Background(), "fake")

			// TODO: test with probabilistic true
			mapper := NewShardMapper(ConstantShards(shards), false, nilShardMetrics)
			noop, _, mapped, err := mapper.Parse(tc.query)
			require.Nil(t, err)

			shardedQry := sharded.Query(ctx, params, mapped)

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
		streams  = randomStreams(nStreams, rounds+1, shards, []string{"a", "b", "c", "d"})
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

			// Regular engine
			qry := regularEngine.Query(params)
			res, err := qry.Exec(ctx)
			require.Nil(t, err)

			// Downstream engine - split by range
			rangeMapper, err := NewRangeMapper(tc.splitByInterval, nilRangeMetrics, NewMapperStats())
			require.Nil(t, err)
			noop, rangeExpr, err := rangeMapper.Parse(tc.query)
			require.Nil(t, err)

			require.False(t, noop, "downstream engine cannot execute noop")

			rangeQry := downstreamEngine.Query(ctx, params, rangeExpr)
			rangeRes, err := rangeQry.Exec(ctx)
			require.Nil(t, err)

			require.Equal(t, res.Data, rangeRes.Data)
		})
	}
}

func TestSketchEquivalence(t *testing.T) {
	var (
		shards   = 3
		nStreams = 60
		rounds   = 20
		streams  = randomStreams(nStreams, rounds+1, shards, []string{"a", "b", "c", "d"})
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
		downstream := NewDownstreamEngine(opts, MockDownstreamer{regular}, NoLimits, log.NewNopLogger())

		mapper := NewShardMapper(ConstantShards(shards), false, nilShardMetrics)
		probabilisticMapper := NewShardMapper(ConstantShards(shards), true, nilShardMetrics)

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
			ctx := user.InjectOrgID(context.Background(), "fake")

			_, _, mapped, err := mapper.Parse(tc.query)
			require.Nil(t, err)
			shardedQry := downstream.Query(ctx, params, mapped)
			shardedRes, err := shardedQry.Exec(ctx)
			require.Nil(t, err)

			_, _, probabilisticMapped, err := probabilisticMapper.Parse(tc.query)
			require.Nil(t, err)
			probabilisticQry := downstream.Query(ctx, params, probabilisticMapped)
			probabilisticResult, err := probabilisticQry.Exec(ctx)
			require.Nil(t, err)

			require.Equal(t, probabilisticResult.Data, shardedRes.Data)
		})
	}
}

// approximatelyEquals ensures two responses are approximately equal,
// up to 6 decimals precision per sample
func approximatelyEquals(t *testing.T, as, bs promql.Matrix) {
	require.Equal(t, len(as), len(bs))

	for i := 0; i < len(as); i++ {
		a := as[i]
		b := bs[i]
		require.Equal(t, a.Metric, b.Metric)
		require.Equal(t, len(a.Floats), len(b.Floats))

		for j := 0; j < len(a.Floats); j++ {
			aSample := &a.Floats[j]
			aSample.F = math.Round(aSample.F*1e6) / 1e6
			bSample := &b.Floats[j]
			bSample.F = math.Round(bSample.F*1e6) / 1e6
		}
		require.Equal(t, a, b)
	}
}
