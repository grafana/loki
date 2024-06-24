package logql

import (
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

func TestShardedStringer(t *testing.T) {
	for _, tc := range []struct {
		in  syntax.Expr
		out string
	}{
		{
			in: &ConcatLogSelectorExpr{
				DownstreamLogSelectorExpr: DownstreamLogSelectorExpr{
					shard: NewPowerOfTwoShard(index.ShardAnnotation{
						Shard: 0,
						Of:    2,
					}).Bind(nil),
					LogSelectorExpr: &syntax.MatchersExpr{
						Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
					},
				},
				next: &ConcatLogSelectorExpr{
					DownstreamLogSelectorExpr: DownstreamLogSelectorExpr{
						shard: NewPowerOfTwoShard(index.ShardAnnotation{
							Shard: 1,
							Of:    2,
						}).Bind(nil),
						LogSelectorExpr: &syntax.MatchersExpr{
							Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
						},
					},
					next: nil,
				},
			},
			out: `downstream<{foo="bar"}, shard=0_of_2> ++ downstream<{foo="bar"}, shard=1_of_2>`,
		},
	} {
		t.Run(tc.out, func(t *testing.T) {
			require.Equal(t, tc.out, tc.in.String())
		})
	}
}

func TestMapSampleExpr(t *testing.T) {

	strategy := NewPowerOfTwoStrategy(ConstantShards(2))
	m := NewShardMapper(strategy, nilShardMetrics, []string{ShardQuantileOverTime})

	for _, tc := range []struct {
		in  syntax.SampleExpr
		out syntax.SampleExpr
	}{
		{
			in: &syntax.RangeAggregationExpr{
				Operation: syntax.OpRangeTypeRate,
				Left: &syntax.LogRange{
					Left: &syntax.MatchersExpr{
						Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
					},
					Interval: time.Minute,
				},
			},
			out: &ConcatSampleExpr{
				DownstreamSampleExpr: DownstreamSampleExpr{
					shard: NewPowerOfTwoShard(index.ShardAnnotation{
						Shard: 0,
						Of:    2,
					}).Bind(nil),
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
						shard: NewPowerOfTwoShard(index.ShardAnnotation{
							Shard: 1,
							Of:    2,
						}).Bind(nil),
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
	} {
		t.Run(tc.in.String(), func(t *testing.T) {
			mapped, _, err := m.mapSampleExpr(tc.in, nilShardMetrics.downstreamRecorder())
			require.Nil(t, err)
			require.Equal(t, tc.out, mapped)
		})
	}
}

func TestMappingStrings(t *testing.T) {
	strategy := NewPowerOfTwoStrategy(ConstantShards(2))
	m := NewShardMapper(strategy, nilShardMetrics, []string{ShardQuantileOverTime})
	for _, tc := range []struct {
		in  string
		out string
	}{
		// NOTE (callum):These two queries containing quantile_over_time should result in
		// the same unsharded of the max and not the inner quantile regardless of whether
		// quantile sharding is turned on. This should be the case even if the inner quantile
		// does not contain a grouping until we decide whether to do the further optimization
		// of sharding the inner quantile.
		{
			in:  `max by (status)(quantile_over_time(0.70, {a=~".+"} | logfmt | unwrap value [1s]))`,
			out: `maxby(status)(quantile_over_time(0.7,{a=~".+"}|logfmt|unwrapvalue[1s]))`,
		},
		{
			in:  `max by (status)(quantile_over_time(0.70, {a=~".+"} | logfmt | unwrap value [1s]) by (baz))`,
			out: `maxby(status)(quantile_over_time(0.7,{a=~".+"}|logfmt|unwrapvalue[1s])by(baz))`,
		},
		{
			in: `{foo="bar"}`,
			out: `downstream<{foo="bar"}, shard=0_of_2>
					++ downstream<{foo="bar"}, shard=1_of_2>`,
		},
		{
			in: `{foo="bar"} |= "foo" |~ "bar" | json | latency >= 10s or foo<5 and bar="t" | line_format "b{{.blip}}"`,
			out: `downstream<{foo="bar"} |="foo" |~"bar" | json | (latency>=10s or (foo<5,bar="t")) | line_format "b{{.blip}}", shard=0_of_2>
					++downstream<{foo="bar"} |="foo" |~"bar" | json | (latency>=10s or (foo<5, bar="t")) | line_format "b{{.blip}}", shard=1_of_2>`,
		},
		{
			in: `sum(rate({foo="bar"}[1m]))`,
			out: `sum(
				downstream<sum(rate({foo="bar"}[1m])), shard=0_of_2>
				++ downstream<sum(rate({foo="bar"}[1m])), shard=1_of_2>
			)`,
		},
		{
			in: `max(count(rate({foo="bar"}[5m]))) / 2`,
			out: `(max(
				sum(
					downstream<count(rate({foo="bar"}[5m])), shard=0_of_2>
					++ downstream<count(rate({foo="bar"}[5m])), shard=1_of_2>)
				) / 2
			)`,
		},
		{
			in: `topk(3, rate({foo="bar"}[5m]))`,
			out: `topk(3,
				downstream<rate({foo="bar"}[5m]), shard=0_of_2>
				++ downstream<rate({foo="bar"}[5m]), shard=1_of_2>
			)`,
		},
		{
			in: `sum(max(rate({foo="bar"}[5m])))`,
			out: `sum(max(
				downstream<max(rate({foo="bar"}[5m])), shard=0_of_2>
				++ downstream<max(rate({foo="bar"}[5m])), shard=1_of_2>
			))`,
		},
		{
			in: `max without (env) (rate({foo="bar"}[5m]))`,
			out: `max without (env) (
				downstream<max without (env)(rate({foo="bar"}[5m])), shard=0_of_2> ++ downstream<max without (env)(rate({foo="bar"}[5m])), shard=1_of_2>
			)`,
		},
		{
			in: `sum(max(rate({foo="bar"} | json | label_format foo=bar [5m])))`,
			out: `sum(
				max(
					sum without() (
						downstream<rate({foo="bar"}|json|label_formatfoo=bar[5m]),shard=0_of_2>
						++
						downstream<rate({foo="bar"}|json|label_formatfoo=bar[5m]),shard=1_of_2>
					)
				)
			)`,
		},
		{
			in: `max(sum by (abc) (rate({foo="bar"} | json | label_format bazz=buzz [5m])))`,
			out: `max(
				sum by (abc) (
					downstream<sumby(abc)(rate({foo="bar"}|json|label_formatbazz=buzz[5m])),shard=0_of_2>
					++
					downstream<sumby(abc)(rate({foo="bar"}|json|label_formatbazz=buzz[5m])),shard=1_of_2>
				)
			)`,
		},
		{
			in: `rate({foo="bar"} | json | label_format foo=bar [5m])`,
			out: `sum without()(
				downstream<rate({foo="bar"}|json|label_formatfoo=bar[5m]),shard=0_of_2>
				++
				downstream<rate({foo="bar"}|json|label_formatfoo=bar[5m]),shard=1_of_2>
			)`,
		},
		{
			in: `count(rate({foo="bar"} | json [5m]))`,
			out: `sum(
				downstream<count(rate({foo="bar"}|json[5m])),shard=0_of_2>
				++
				downstream<count(rate({foo="bar"}|json[5m])),shard=1_of_2>
			)`,
		},
		{
			in: `avg(rate({foo="bar"} | json [5m]))`,
			out: `(
				sum(
					downstream<sum(rate({foo="bar"}|json[5m])),shard=0_of_2>++downstream<sum(rate({foo="bar"}|json[5m])),shard=1_of_2>
				)
				/
				sum(
					downstream<count(rate({foo="bar"}|json[5m])),shard=0_of_2>++downstream<count(rate({foo="bar"}|json[5m])),shard=1_of_2>
				)
			)`,
		},
		{
			in: `avg by(foo_extracted) (quantile_over_time(0.95, {foo="baz"} | logfmt | unwrap foo_extracted | __error__="" [5m]))`,
			out: `(
               sum by (foo_extracted) (
                   downstream<sumby(foo_extracted)(quantile_over_time(0.95,{foo="baz"}|logfmt|unwrapfoo_extracted|__error__=""[5m])),shard=0_of_2>++downstream<sumby(foo_extracted)(quantile_over_time(0.95,{foo="baz"}|logfmt|unwrapfoo_extracted|__error__=""[5m])),shard=1_of_2>
               )
               / 
               sum by (foo_extracted) (
                   downstream<countby(foo_extracted)(quantile_over_time(0.95,{foo="baz"}|logfmt|unwrapfoo_extracted|__error__=""[5m])),shard=0_of_2>++downstream<countby(foo_extracted)(quantile_over_time(0.95,{foo="baz"}|logfmt|unwrapfoo_extracted|__error__=""[5m])),shard=1_of_2>
               )
            )`,
		},
		{
			in: `count(rate({foo="bar"} | json | keep foo [5m]))`,
			out: `count(
				sum without()(
					downstream<rate({foo="bar"}|json|keepfoo[5m]),shard=0_of_2>
					++
					downstream<rate({foo="bar"}|json|keepfoo[5m]),shard=1_of_2>
				)
			)`,
		},
		{
			// renaming reduces the labelset and must be reaggregated before counting
			in: `count(rate({foo="bar"} | json | label_format foo=bar [5m]))`,
			out: `count(
				sum without() (
					downstream<rate({foo="bar"}|json|label_formatfoo=bar[5m]),shard=0_of_2>
					++
					downstream<rate({foo="bar"}|json|label_formatfoo=bar[5m]),shard=1_of_2>
				)
			)`,
		},
		{
			in: `sum without () (rate({job="foo"}[5m]))`,
			out: `sumwithout()(
				downstream<sumwithout()(rate({job="foo"}[5m])),shard=0_of_2>++downstream<sumwithout()(rate({job="foo"}[5m])),shard=1_of_2>
			)`,
		},
		{
			in: `{foo="bar"} |= "id=123"`,
			out: `downstream<{foo="bar"}|="id=123", shard=0_of_2>
					++ downstream<{foo="bar"}|="id=123", shard=1_of_2>`,
		},
		{
			in: `sum by (cluster) (rate({foo="bar"} |= "id=123" [5m]))`,
			out: `sum by (cluster) (
				downstream<sum by(cluster)(rate({foo="bar"}|="id=123"[5m])), shard=0_of_2>
				++ downstream<sum by(cluster)(rate({foo="bar"}|="id=123"[5m])), shard=1_of_2>
			)`,
		},
		{
			in: `sum by (cluster) (sum_over_time({foo="bar"} |= "id=123" | logfmt | unwrap latency [5m]))`,
			out: `sum by (cluster) (
				downstream<sum by(cluster)(sum_over_time({foo="bar"}|="id=123"| logfmt | unwrap latency[5m])), shard=0_of_2>
				++ downstream<sum by(cluster)(sum_over_time({foo="bar"}|="id=123"| logfmt | unwrap latency[5m])), shard=1_of_2>
			)`,
		},
		{
			in:  `sum by (cluster) (stddev_over_time({foo="bar"} |= "id=123" | logfmt | unwrap latency [5m]))`,
			out: `sum by (cluster) (stddev_over_time({foo="bar"} |= "id=123" | logfmt | unwrap latency [5m]))`,
		},
		{
			in: `sum without (a) (
		 			label_replace(
		   			sum without (b) (
		     				rate({foo="bar"}[5m])
		   			),
		   			"baz", "buz", "foo", "(.*)"
		 			)
				)`,
			out: `sum without(a) (
					label_replace(
						sum without (b) (
							downstream<sum without (b)(rate({foo="bar"}[5m])), shard=0_of_2>
							++downstream<sum without(b)(rate({foo="bar"}[5m])), shard=1_of_2>
						),
						"baz", "buz", "foo", "(.*)"
					)
				)`,
		},
		{
			in: `sum(count_over_time({foo="bar"} | logfmt | label_format bar=baz | bar="buz" [5m])) by (bar)`,
			out: `sum by (bar) (
				downstream<sum by (bar) (count_over_time({foo="bar"}|logfmt|label_formatbar=baz|bar="buz"[5m])),shard=0_of_2>
				++
				downstream<sum by (bar) (count_over_time({foo="bar"}|logfmt|label_formatbar=baz|bar="buz"[5m])),shard=1_of_2>
			)`,
		},
		{
			in: `sum by (cluster) (rate({foo="bar"} [5m])) + ignoring(machine) sum by (cluster,machine) (rate({foo="bar"} [5m]))`,
			out: `(
				sum by (cluster) (
					downstream<sum by (cluster) (rate({foo="bar"}[5m])), shard=0_of_2>
					++ downstream<sum by (cluster) (rate({foo="bar"}[5m])), shard=1_of_2>
				)
				+ ignoring(machine) sum by (cluster, machine) (
					downstream<sum by (cluster, machine) (rate({foo="bar"}[5m])), shard=0_of_2>
					++ downstream<sum by (cluster, machine) (rate({foo="bar"}[5m])), shard=1_of_2>
				)
			)`,
		},
		{
			in: `sum by (cluster) (sum by (cluster) (rate({foo="bar"} [5m])) + ignoring(machine) sum by (cluster,machine) (rate({foo="bar"} [5m])))`,
			out: `sum by (cluster) (
				(
					sum by (cluster) (
						downstream<sum by (cluster) (rate({foo="bar"}[5m])), shard=0_of_2>
						++ downstream<sum by (cluster) (rate({foo="bar"}[5m])), shard=1_of_2>
					)
					+ ignoring(machine) sum by (cluster, machine)(
						downstream<sum by (cluster, machine) (rate({foo="bar"}[5m])), shard=0_of_2>
						++ downstream<sum by (cluster, machine) (rate({foo="bar"}[5m])), shard=1_of_2>
					)
				)
			)`,
		},
		{
			in: `max_over_time({foo="ugh"} | unwrap baz [1m]) by ()`,
			out: `max(
				downstream<max_over_time({foo="ugh"}|unwrapbaz[1m])by(),shard=0_of_2>
				++
				downstream<max_over_time({foo="ugh"}|unwrapbaz[1m])by(),shard=1_of_2>
			)`,
		},
		{
			in: `avg(avg_over_time({job=~"myapps.*"} |= "stats" | json busy="utilization" | unwrap busy [5m]))`,
			out: `(
				sum(
					downstream<sum(avg_over_time({job=~"myapps.*"}|="stats" | json busy="utilization" | unwrap busy [5m])),shard=0_of_2>
					++
					downstream<sum(avg_over_time({job=~"myapps.*"}|="stats" | json busy="utilization" | unwrap busy [5m])),shard=1_of_2>)
				/
				sum(
					downstream<count(avg_over_time({job=~"myapps.*"}|="stats" | json busy="utilization" | unwrap busy [5m])),shard=0_of_2>
					++
					downstream<count(avg_over_time({job=~"myapps.*"}|="stats" | json busy="utilization" |unwrap busy [5m])),shard=1_of_2>
				)
			)`,
		},
		{
			in: `avg_over_time({job=~"myapps.*"} |= "stats" | json busy="utilization" | unwrap busy [5m])`,
			out: `downstream<avg_over_time({job=~"myapps.*"}|= "stats" | json busy="utilization" | unwrap busy [5m]),shard=0_of_2>
					++ downstream<avg_over_time({job=~"myapps.*"}|="stats" | json busy="utilization" | unwrap busy [5m]),shard=1_of_2>`,
		},
		{
			in: `avg_over_time({job=~"myapps.*"} |= "stats" | json busy="utilization" | unwrap busy [5m]) by (cluster)`,
			out: `(
				sum by (cluster) (
					downstream<sum by (cluster) (sum_over_time({job=~"myapps.*"}|="stats" | json busy="utilization" | unwrap busy [5m])),shard=0_of_2>
					++
					downstream<sum by (cluster) (sum_over_time({job=~"myapps.*"}|="stats" | json busy="utilization" | unwrap busy [5m])),shard=1_of_2>
				)
				/
				sum by (cluster) (
					downstream<sum by (cluster) (count_over_time({job=~"myapps.*"}|="stats" | json busy="utilization" [5m])),shard=0_of_2>
					++
					downstream<sum by (cluster) (count_over_time({job=~"myapps.*"}|="stats" | json busy="utilization" [5m])),shard=1_of_2>
				)
			)`,
		},
		{
			in: `avg_over_time({job=~"myapps.*"} |= "stats" | json | keep busy | unwrap busy [5m])`,
			out: `(
				sum without() (
					downstream<sum without() (sum_over_time({job=~"myapps.*"} |="stats" | json | keep busy | unwrap busy [5m])),shard=0_of_2>
					++
					downstream<sum without() (sum_over_time({job=~"myapps.*"} |="stats" | json | keep busy | unwrap busy [5m])),shard=1_of_2>
				)
				/
				sum without(busy) (
					downstream<sum without(busy) (count_over_time({job=~"myapps.*"} |="stats" | json | keep busy [5m])),shard=0_of_2>
					++
					downstream<sum without(busy) (count_over_time({job=~"myapps.*"} |="stats" | json | keep busy [5m])),shard=1_of_2>
				)
			)`,
		},
		{
			in: `avg_over_time({job=~"myapps.*"} |= "stats" | json | keep busy | unwrap busy [5m]) without (foo)`,
			out: `(
				sum without(foo) (
					downstream<sum without(foo) (sum_over_time({job=~"myapps.*"} |="stats" | json | keep busy | unwrap busy [5m])),shard=0_of_2>
					++
					downstream<sum without(foo) (sum_over_time({job=~"myapps.*"} |="stats" | json | keep busy | unwrap busy [5m])),shard=1_of_2>
				)
				/
				sum without(foo,busy) (
					downstream<sum without(foo,busy) (count_over_time({job=~"myapps.*"} |="stats" | json | keep busy [5m])),shard=0_of_2>
					++
					downstream<sum without(foo,busy) (count_over_time({job=~"myapps.*"} |="stats" | json | keep busy [5m])),shard=1_of_2>
				)
			)`,
		},
		// should be noop if VectorExpr
		{
			in:  `vector(0)`,
			out: `vector(0.000000)`,
		},
		{
			// or exprs aren't shardable
			in:  `count_over_time({a=~".+"}[1s]) or count_over_time({a=~".+"}[1s])`,
			out: `(downstream<count_over_time({a=~".+"}[1s]),shard=0_of_2>++downstream<count_over_time({a=~".+"}[1s]),shard=1_of_2>ordownstream<count_over_time({a=~".+"}[1s]),shard=0_of_2>++downstream<count_over_time({a=~".+"}[1s]),shard=1_of_2>)`,
		},
		{
			// vector() exprs aren't shardable
			in:  `sum(count_over_time({a=~".+"}[1s]) + vector(1))`,
			out: `sum((downstream<count_over_time({a=~".+"}[1s]),shard=0_of_2>++downstream<count_over_time({a=~".+"}[1s]),shard=1_of_2>+vector(1.000000)))`,
		},
		{
			// on() is never shardable as it can mutate labels
			in:  `sum(count_over_time({a=~".+"}[1s]) * on () count_over_time({a=~".+"}[1s]))`,
			out: `sum((downstream<count_over_time({a=~".+"}[1s]),shard=0_of_2>++downstream<count_over_time({a=~".+"}[1s]),shard=1_of_2>*on()downstream<count_over_time({a=~".+"}[1s]),shard=0_of_2>++downstream<count_over_time({a=~".+"}[1s]),shard=1_of_2>))`,
		},
		{
			// ignoring(<non-empty-labels>) is never shardable as it can mutate labels
			in:  `sum(count_over_time({a=~".+"}[1s]) * ignoring (foo) count_over_time({a=~".+"}[1s]))`,
			out: `sum((downstream<count_over_time({a=~".+"}[1s]),shard=0_of_2>++downstream<count_over_time({a=~".+"}[1s]),shard=1_of_2>*ignoring(foo)downstream<count_over_time({a=~".+"}[1s]),shard=0_of_2>++downstream<count_over_time({a=~".+"}[1s]),shard=1_of_2>))`,
		},
		{
			// ignoring () doesn't mutate labels and therefore can be shardable
			// as long as the operation is shardable
			in:  `sum(count_over_time({a=~".+"}[1s]) * ignoring () count_over_time({a=~".+"}[1s]))`,
			out: `sum(downstream<sum((count_over_time({a=~".+"}[1s])*count_over_time({a=~".+"}[1s]))),shard=0_of_2>++downstream<sum((count_over_time({a=~".+"}[1s])*count_over_time({a=~".+"}[1s]))),shard=1_of_2>)`,
		},
		{
			// shard the count since there is no label reduction in children
			in:  `count by (foo) (rate({job="bar"}[1m]))`,
			out: `sumby(foo)(downstream<countby(foo)(rate({job="bar"}[1m])),shard=0_of_2>++downstream<countby(foo)(rate({job="bar"}[1m])),shard=1_of_2>)`,
		},
		{
			// don't shard the count since there is label reduction in children
			in:  `count by (foo) (sum by (foo, bar) (rate({job="bar"}[1m])))`,
			out: `countby(foo)(sumby(foo,bar)(downstream<sumby(foo,bar)(rate({job="bar"}[1m])),shard=0_of_2>++downstream<sumby(foo,bar)(rate({job="bar"}[1m])),shard=1_of_2>))`,
		},
	} {
		t.Run(tc.in, func(t *testing.T) {
			ast, err := syntax.ParseExpr(tc.in)
			require.Nil(t, err)

			mapped, _, err := m.Map(ast, nilShardMetrics.downstreamRecorder(), true)
			require.Nil(t, err)

			require.Equal(t, removeWhiteSpace(tc.out), removeWhiteSpace(mapped.String()))
		})
	}
}

// Test that mapping of queries for operation types that have probabilistic
// sharding options, but whose sharding is turned off, are not sharded on those operations.
func TestMappingStrings_NoProbabilisticSharding(t *testing.T) {
	for _, tc := range []struct {
		in  string
		out string
	}{
		// NOTE (callum):These two queries containing quantile_over_time should result in
		// the same unsharded of the max and not the inner quantile regardless of whether
		// quantile sharding is turned on. This should be the case even if the inner quantile
		// does not contain a grouping until we decide whether to do the further optimization
		// of sharding the inner quantile.
		{
			in:  `max by (status)(quantile_over_time(0.70, {a=~".+"} | logfmt | unwrap value [1s]) by (baz))`,
			out: `maxby(status)(quantile_over_time(0.7,{a=~".+"}|logfmt|unwrapvalue[1s])by(baz))`,
		},
		{
			in:  `max by (status)(quantile_over_time(0.70, {a=~".+"} | logfmt | unwrap value [1s]))`,
			out: `maxby(status)(quantile_over_time(0.7,{a=~".+"}|logfmt|unwrapvalue[1s]))`,
		},
	} {
		t.Run(tc.in, func(t *testing.T) {

			shardedMapper := NewShardMapper(NewPowerOfTwoStrategy(ConstantShards(2)), nilShardMetrics, []string{ShardQuantileOverTime})

			ast, err := syntax.ParseExpr(tc.in)
			require.Nil(t, err)

			sharded, _, err := shardedMapper.Map(ast, nilShardMetrics.downstreamRecorder(), true)
			require.Nil(t, err)

			require.Equal(t, removeWhiteSpace(tc.out), removeWhiteSpace(sharded.String()))

			unshardedMapper := NewShardMapper(NewPowerOfTwoStrategy(ConstantShards(2)), nilShardMetrics, []string{})

			ast, err = syntax.ParseExpr(tc.in)
			require.Nil(t, err)

			unsharded, _, err := unshardedMapper.Map(ast, nilShardMetrics.downstreamRecorder(), true)
			require.Nil(t, err)

			require.Equal(t, removeWhiteSpace(tc.out), removeWhiteSpace(unsharded.String()))
		})
	}
}

func TestMapping(t *testing.T) {
	strategy := NewPowerOfTwoStrategy(ConstantShards(2))
	m := NewShardMapper(strategy, nilShardMetrics, []string{})

	for _, tc := range []struct {
		in   string
		expr syntax.Expr
		err  error
	}{
		{
			in: `{foo="bar"}`,
			expr: &ConcatLogSelectorExpr{
				DownstreamLogSelectorExpr: DownstreamLogSelectorExpr{
					shard: NewPowerOfTwoShard(index.ShardAnnotation{
						Shard: 0,
						Of:    2,
					}).Bind(nil),
					LogSelectorExpr: &syntax.MatchersExpr{
						Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
					},
				},
				next: &ConcatLogSelectorExpr{
					DownstreamLogSelectorExpr: DownstreamLogSelectorExpr{
						shard: NewPowerOfTwoShard(index.ShardAnnotation{
							Shard: 1,
							Of:    2,
						}).Bind(nil),
						LogSelectorExpr: &syntax.MatchersExpr{
							Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
						},
					},
					next: nil,
				},
			},
		},
		{
			in: `{foo="bar"} |= "error"`,
			expr: &ConcatLogSelectorExpr{
				DownstreamLogSelectorExpr: DownstreamLogSelectorExpr{
					shard: NewPowerOfTwoShard(index.ShardAnnotation{
						Shard: 0,
						Of:    2,
					}).Bind(nil),
					LogSelectorExpr: &syntax.PipelineExpr{
						Left: &syntax.MatchersExpr{
							Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
						},
						MultiStages: syntax.MultiStageExpr{
							&syntax.LineFilterExpr{
								LineFilter: syntax.LineFilter{
									Ty:    log.LineMatchEqual,
									Match: "error",
									Op:    "",
								},
							},
						},
					},
				},
				next: &ConcatLogSelectorExpr{
					DownstreamLogSelectorExpr: DownstreamLogSelectorExpr{
						shard: NewPowerOfTwoShard(index.ShardAnnotation{
							Shard: 1,
							Of:    2,
						}).Bind(nil),
						LogSelectorExpr: &syntax.PipelineExpr{
							Left: &syntax.MatchersExpr{
								Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
							},
							MultiStages: syntax.MultiStageExpr{
								&syntax.LineFilterExpr{
									LineFilter: syntax.LineFilter{
										Ty:    log.LineMatchEqual,
										Match: "error",
										Op:    "",
									},
								},
							},
						},
					},
					next: nil,
				},
			},
		},
		{
			in: `rate({foo="bar"}[5m])`,
			expr: &ConcatSampleExpr{
				DownstreamSampleExpr: DownstreamSampleExpr{
					shard: NewPowerOfTwoShard(index.ShardAnnotation{
						Shard: 0,
						Of:    2,
					}).Bind(nil),
					SampleExpr: &syntax.RangeAggregationExpr{
						Operation: syntax.OpRangeTypeRate,
						Left: &syntax.LogRange{
							Left: &syntax.MatchersExpr{
								Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
							},
							Interval: 5 * time.Minute,
						},
					},
				},
				next: &ConcatSampleExpr{
					DownstreamSampleExpr: DownstreamSampleExpr{
						shard: NewPowerOfTwoShard(index.ShardAnnotation{
							Shard: 1,
							Of:    2,
						}).Bind(nil),
						SampleExpr: &syntax.RangeAggregationExpr{
							Operation: syntax.OpRangeTypeRate,
							Left: &syntax.LogRange{
								Left: &syntax.MatchersExpr{
									Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
								},
								Interval: 5 * time.Minute,
							},
						},
					},
					next: nil,
				},
			},
		},
		{
			in: `count_over_time({foo="bar"}[5m])`,
			expr: &ConcatSampleExpr{
				DownstreamSampleExpr: DownstreamSampleExpr{
					shard: NewPowerOfTwoShard(index.ShardAnnotation{
						Shard: 0,
						Of:    2,
					}).Bind(nil),
					SampleExpr: &syntax.RangeAggregationExpr{
						Operation: syntax.OpRangeTypeCount,
						Left: &syntax.LogRange{
							Left: &syntax.MatchersExpr{
								Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
							},
							Interval: 5 * time.Minute,
						},
					},
				},
				next: &ConcatSampleExpr{
					DownstreamSampleExpr: DownstreamSampleExpr{
						shard: NewPowerOfTwoShard(index.ShardAnnotation{
							Shard: 1,
							Of:    2,
						}).Bind(nil),
						SampleExpr: &syntax.RangeAggregationExpr{
							Operation: syntax.OpRangeTypeCount,
							Left: &syntax.LogRange{
								Left: &syntax.MatchersExpr{
									Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
								},
								Interval: 5 * time.Minute,
							},
						},
					},
					next: nil,
				},
			},
		},
		{
			in: `sum(rate({foo="bar"}[5m]))`,
			expr: &syntax.VectorAggregationExpr{
				Grouping:  &syntax.Grouping{},
				Operation: syntax.OpTypeSum,
				Left: &ConcatSampleExpr{
					DownstreamSampleExpr: DownstreamSampleExpr{
						shard: NewPowerOfTwoShard(index.ShardAnnotation{
							Shard: 0,
							Of:    2,
						}).Bind(nil),
						SampleExpr: &syntax.VectorAggregationExpr{
							Grouping:  &syntax.Grouping{},
							Operation: syntax.OpTypeSum,
							Left: &syntax.RangeAggregationExpr{
								Operation: syntax.OpRangeTypeRate,
								Left: &syntax.LogRange{
									Left: &syntax.MatchersExpr{
										Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
									},
									Interval: 5 * time.Minute,
								},
							},
						},
					},
					next: &ConcatSampleExpr{
						DownstreamSampleExpr: DownstreamSampleExpr{
							shard: NewPowerOfTwoShard(index.ShardAnnotation{
								Shard: 1,
								Of:    2,
							}).Bind(nil),
							SampleExpr: &syntax.VectorAggregationExpr{
								Grouping:  &syntax.Grouping{},
								Operation: syntax.OpTypeSum,
								Left: &syntax.RangeAggregationExpr{
									Operation: syntax.OpRangeTypeRate,
									Left: &syntax.LogRange{
										Left: &syntax.MatchersExpr{
											Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
										},
										Interval: 5 * time.Minute,
									},
								},
							},
						},
						next: nil,
					},
				},
			},
		},
		{
			in: `topk(3, rate({foo="bar"}[5m]))`,
			expr: &syntax.VectorAggregationExpr{
				Grouping:  &syntax.Grouping{},
				Params:    3,
				Operation: syntax.OpTypeTopK,
				Left: &ConcatSampleExpr{
					DownstreamSampleExpr: DownstreamSampleExpr{
						shard: NewPowerOfTwoShard(index.ShardAnnotation{
							Shard: 0,
							Of:    2,
						}).Bind(nil),
						SampleExpr: &syntax.RangeAggregationExpr{
							Operation: syntax.OpRangeTypeRate,
							Left: &syntax.LogRange{
								Left: &syntax.MatchersExpr{
									Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
								},
								Interval: 5 * time.Minute,
							},
						},
					},
					next: &ConcatSampleExpr{
						DownstreamSampleExpr: DownstreamSampleExpr{
							shard: NewPowerOfTwoShard(index.ShardAnnotation{
								Shard: 1,
								Of:    2,
							}).Bind(nil),
							SampleExpr: &syntax.RangeAggregationExpr{
								Operation: syntax.OpRangeTypeRate,
								Left: &syntax.LogRange{
									Left: &syntax.MatchersExpr{
										Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
									},
									Interval: 5 * time.Minute,
								},
							},
						},
						next: nil,
					},
				},
			},
		},
		{
			in: `count(rate({foo="bar"}[5m]))`,
			expr: &syntax.VectorAggregationExpr{
				Operation: syntax.OpTypeSum,
				Grouping:  &syntax.Grouping{},
				Left: &ConcatSampleExpr{
					DownstreamSampleExpr: DownstreamSampleExpr{
						shard: NewPowerOfTwoShard(index.ShardAnnotation{
							Shard: 0,
							Of:    2,
						}).Bind(nil),
						SampleExpr: &syntax.VectorAggregationExpr{
							Grouping:  &syntax.Grouping{},
							Operation: syntax.OpTypeCount,
							Left: &syntax.RangeAggregationExpr{
								Operation: syntax.OpRangeTypeRate,
								Left: &syntax.LogRange{
									Left: &syntax.MatchersExpr{
										Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
									},
									Interval: 5 * time.Minute,
								},
							},
						},
					},
					next: &ConcatSampleExpr{
						DownstreamSampleExpr: DownstreamSampleExpr{
							shard: NewPowerOfTwoShard(index.ShardAnnotation{
								Shard: 1,
								Of:    2,
							}).Bind(nil),
							SampleExpr: &syntax.VectorAggregationExpr{
								Grouping:  &syntax.Grouping{},
								Operation: syntax.OpTypeCount,
								Left: &syntax.RangeAggregationExpr{
									Operation: syntax.OpRangeTypeRate,
									Left: &syntax.LogRange{
										Left: &syntax.MatchersExpr{
											Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
										},
										Interval: 5 * time.Minute,
									},
								},
							},
						},
						next: nil,
					},
				},
			},
		},
		{
			in: `avg(rate({foo="bar"}[5m]))`,
			expr: &syntax.BinOpExpr{
				Op: syntax.OpTypeDiv,
				SampleExpr: &syntax.VectorAggregationExpr{
					Grouping:  &syntax.Grouping{},
					Operation: syntax.OpTypeSum,
					Left: &ConcatSampleExpr{
						DownstreamSampleExpr: DownstreamSampleExpr{
							shard: NewPowerOfTwoShard(index.ShardAnnotation{
								Shard: 0,
								Of:    2,
							}).Bind(nil),
							SampleExpr: &syntax.VectorAggregationExpr{
								Grouping:  &syntax.Grouping{},
								Operation: syntax.OpTypeSum,
								Left: &syntax.RangeAggregationExpr{
									Operation: syntax.OpRangeTypeRate,
									Left: &syntax.LogRange{
										Left: &syntax.MatchersExpr{
											Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
										},
										Interval: 5 * time.Minute,
									},
								},
							},
						},
						next: &ConcatSampleExpr{
							DownstreamSampleExpr: DownstreamSampleExpr{
								shard: NewPowerOfTwoShard(index.ShardAnnotation{
									Shard: 1,
									Of:    2,
								}).Bind(nil),
								SampleExpr: &syntax.VectorAggregationExpr{
									Grouping:  &syntax.Grouping{},
									Operation: syntax.OpTypeSum,
									Left: &syntax.RangeAggregationExpr{
										Operation: syntax.OpRangeTypeRate,
										Left: &syntax.LogRange{
											Left: &syntax.MatchersExpr{
												Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
											},
											Interval: 5 * time.Minute,
										},
									},
								},
							},
							next: nil,
						},
					},
				},
				RHS: &syntax.VectorAggregationExpr{
					Operation: syntax.OpTypeSum,
					Grouping:  &syntax.Grouping{},
					Left: &ConcatSampleExpr{
						DownstreamSampleExpr: DownstreamSampleExpr{
							shard: NewPowerOfTwoShard(index.ShardAnnotation{
								Shard: 0,
								Of:    2,
							}).Bind(nil),
							SampleExpr: &syntax.VectorAggregationExpr{
								Grouping:  &syntax.Grouping{},
								Operation: syntax.OpTypeCount,
								Left: &syntax.RangeAggregationExpr{
									Operation: syntax.OpRangeTypeRate,
									Left: &syntax.LogRange{
										Left: &syntax.MatchersExpr{
											Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
										},
										Interval: 5 * time.Minute,
									},
								},
							},
						},
						next: &ConcatSampleExpr{
							DownstreamSampleExpr: DownstreamSampleExpr{
								shard: NewPowerOfTwoShard(index.ShardAnnotation{
									Shard: 1,
									Of:    2,
								}).Bind(nil),
								SampleExpr: &syntax.VectorAggregationExpr{
									Grouping:  &syntax.Grouping{},
									Operation: syntax.OpTypeCount,
									Left: &syntax.RangeAggregationExpr{
										Operation: syntax.OpRangeTypeRate,
										Left: &syntax.LogRange{
											Left: &syntax.MatchersExpr{
												Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
											},
											Interval: 5 * time.Minute,
										},
									},
								},
							},
							next: nil,
						},
					},
				},
			},
		},
		{
			in: `1 + sum by (cluster) (rate({foo="bar"}[5m]))`,
			expr: &syntax.BinOpExpr{
				Op: syntax.OpTypeAdd,
				Opts: &syntax.BinOpOptions{
					ReturnBool:     false,
					VectorMatching: &syntax.VectorMatching{Card: syntax.CardOneToOne},
				},
				SampleExpr: &syntax.LiteralExpr{Val: 1},
				RHS: &syntax.VectorAggregationExpr{
					Grouping: &syntax.Grouping{
						Groups: []string{"cluster"},
					},
					Operation: syntax.OpTypeSum,
					Left: &ConcatSampleExpr{
						DownstreamSampleExpr: DownstreamSampleExpr{
							shard: NewPowerOfTwoShard(index.ShardAnnotation{
								Shard: 0,
								Of:    2,
							}).Bind(nil),
							SampleExpr: &syntax.VectorAggregationExpr{
								Grouping: &syntax.Grouping{
									Groups: []string{"cluster"},
								},
								Operation: syntax.OpTypeSum,
								Left: &syntax.RangeAggregationExpr{
									Operation: syntax.OpRangeTypeRate,
									Left: &syntax.LogRange{
										Left: &syntax.MatchersExpr{
											Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
										},
										Interval: 5 * time.Minute,
									},
								},
							},
						},
						next: &ConcatSampleExpr{
							DownstreamSampleExpr: DownstreamSampleExpr{
								shard: NewPowerOfTwoShard(index.ShardAnnotation{
									Shard: 1,
									Of:    2,
								}).Bind(nil),
								SampleExpr: &syntax.VectorAggregationExpr{
									Grouping: &syntax.Grouping{
										Groups: []string{"cluster"},
									},
									Operation: syntax.OpTypeSum,
									Left: &syntax.RangeAggregationExpr{
										Operation: syntax.OpRangeTypeRate,
										Left: &syntax.LogRange{
											Left: &syntax.MatchersExpr{
												Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
											},
											Interval: 5 * time.Minute,
										},
									},
								},
							},
							next: nil,
						},
					},
				},
			},
		},
		{
			in: `vector(0) or sum (rate({foo="bar"}[5m]))`,
			expr: &syntax.BinOpExpr{
				Op: syntax.OpTypeOr,
				Opts: &syntax.BinOpOptions{
					ReturnBool:     false,
					VectorMatching: &syntax.VectorMatching{Card: syntax.CardOneToOne},
				},
				SampleExpr: &syntax.VectorExpr{Val: 0},
				RHS: &syntax.VectorAggregationExpr{
					Operation: syntax.OpTypeSum,
					Grouping:  &syntax.Grouping{},
					Left: &ConcatSampleExpr{
						DownstreamSampleExpr: DownstreamSampleExpr{
							shard: NewPowerOfTwoShard(index.ShardAnnotation{
								Shard: 0,
								Of:    2,
							}).Bind(nil),
							SampleExpr: &syntax.VectorAggregationExpr{
								Grouping:  &syntax.Grouping{},
								Operation: syntax.OpTypeSum,
								Left: &syntax.RangeAggregationExpr{
									Operation: syntax.OpRangeTypeRate,
									Left: &syntax.LogRange{
										Left: &syntax.MatchersExpr{
											Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
										},
										Interval: 5 * time.Minute,
									},
								},
							},
						},
						next: &ConcatSampleExpr{
							DownstreamSampleExpr: DownstreamSampleExpr{
								shard: NewPowerOfTwoShard(index.ShardAnnotation{
									Shard: 1,
									Of:    2,
								}).Bind(nil),
								SampleExpr: &syntax.VectorAggregationExpr{
									Grouping:  &syntax.Grouping{},
									Operation: syntax.OpTypeSum,
									Left: &syntax.RangeAggregationExpr{
										Operation: syntax.OpRangeTypeRate,
										Left: &syntax.LogRange{
											Left: &syntax.MatchersExpr{
												Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
											},
											Interval: 5 * time.Minute,
										},
									},
								},
							},
							next: nil,
						},
					},
				},
			},
		},
		// max(count) should shard the count, but not the max
		{
			in: `max(count(rate({foo="bar"}[5m])))`,
			expr: &syntax.VectorAggregationExpr{
				Grouping:  &syntax.Grouping{},
				Operation: syntax.OpTypeMax,
				Left: &syntax.VectorAggregationExpr{
					Operation: syntax.OpTypeSum,
					Grouping:  &syntax.Grouping{},
					Left: &ConcatSampleExpr{
						DownstreamSampleExpr: DownstreamSampleExpr{
							shard: NewPowerOfTwoShard(index.ShardAnnotation{
								Shard: 0,
								Of:    2,
							}).Bind(nil),
							SampleExpr: &syntax.VectorAggregationExpr{
								Grouping:  &syntax.Grouping{},
								Operation: syntax.OpTypeCount,
								Left: &syntax.RangeAggregationExpr{
									Operation: syntax.OpRangeTypeRate,
									Left: &syntax.LogRange{
										Left: &syntax.MatchersExpr{
											Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
										},
										Interval: 5 * time.Minute,
									},
								},
							},
						},
						next: &ConcatSampleExpr{
							DownstreamSampleExpr: DownstreamSampleExpr{
								shard: NewPowerOfTwoShard(index.ShardAnnotation{
									Shard: 1,
									Of:    2,
								}).Bind(nil),
								SampleExpr: &syntax.VectorAggregationExpr{
									Grouping:  &syntax.Grouping{},
									Operation: syntax.OpTypeCount,
									Left: &syntax.RangeAggregationExpr{
										Operation: syntax.OpRangeTypeRate,
										Left: &syntax.LogRange{
											Left: &syntax.MatchersExpr{
												Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
											},
											Interval: 5 * time.Minute,
										},
									},
								},
							},
							next: nil,
						},
					},
				},
			},
		},
		{
			in: `max(sum by (cluster) (rate({foo="bar"}[5m]))) / count(rate({foo="bar"}[5m]))`,
			expr: &syntax.BinOpExpr{
				Op: syntax.OpTypeDiv,
				Opts: &syntax.BinOpOptions{
					ReturnBool:     false,
					VectorMatching: &syntax.VectorMatching{Card: syntax.CardOneToOne},
				},
				SampleExpr: &syntax.VectorAggregationExpr{
					Operation: syntax.OpTypeMax,
					Grouping:  &syntax.Grouping{},
					Left: &syntax.VectorAggregationExpr{
						Grouping: &syntax.Grouping{
							Groups: []string{"cluster"},
						},
						Operation: syntax.OpTypeSum,
						Left: &ConcatSampleExpr{
							DownstreamSampleExpr: DownstreamSampleExpr{
								shard: NewPowerOfTwoShard(index.ShardAnnotation{
									Shard: 0,
									Of:    2,
								}).Bind(nil),
								SampleExpr: &syntax.VectorAggregationExpr{
									Grouping: &syntax.Grouping{
										Groups: []string{"cluster"},
									},
									Operation: syntax.OpTypeSum,
									Left: &syntax.RangeAggregationExpr{
										Operation: syntax.OpRangeTypeRate,
										Left: &syntax.LogRange{
											Left: &syntax.MatchersExpr{
												Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
											},
											Interval: 5 * time.Minute,
										},
									},
								},
							},
							next: &ConcatSampleExpr{
								DownstreamSampleExpr: DownstreamSampleExpr{
									shard: NewPowerOfTwoShard(index.ShardAnnotation{
										Shard: 1,
										Of:    2,
									}).Bind(nil),
									SampleExpr: &syntax.VectorAggregationExpr{
										Grouping: &syntax.Grouping{
											Groups: []string{"cluster"},
										},
										Operation: syntax.OpTypeSum,
										Left: &syntax.RangeAggregationExpr{
											Operation: syntax.OpRangeTypeRate,
											Left: &syntax.LogRange{
												Left: &syntax.MatchersExpr{
													Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
												},
												Interval: 5 * time.Minute,
											},
										},
									},
								},
								next: nil,
							},
						},
					},
				},
				RHS: &syntax.VectorAggregationExpr{
					Operation: syntax.OpTypeSum,
					Grouping:  &syntax.Grouping{},
					Left: &ConcatSampleExpr{
						DownstreamSampleExpr: DownstreamSampleExpr{
							shard: NewPowerOfTwoShard(index.ShardAnnotation{
								Shard: 0,
								Of:    2,
							}).Bind(nil),
							SampleExpr: &syntax.VectorAggregationExpr{
								Grouping:  &syntax.Grouping{},
								Operation: syntax.OpTypeCount,
								Left: &syntax.RangeAggregationExpr{
									Operation: syntax.OpRangeTypeRate,
									Left: &syntax.LogRange{
										Left: &syntax.MatchersExpr{
											Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
										},
										Interval: 5 * time.Minute,
									},
								},
							},
						},
						next: &ConcatSampleExpr{
							DownstreamSampleExpr: DownstreamSampleExpr{
								shard: NewPowerOfTwoShard(index.ShardAnnotation{
									Shard: 1,
									Of:    2,
								}).Bind(nil),
								SampleExpr: &syntax.VectorAggregationExpr{
									Grouping:  &syntax.Grouping{},
									Operation: syntax.OpTypeCount,
									Left: &syntax.RangeAggregationExpr{
										Operation: syntax.OpRangeTypeRate,
										Left: &syntax.LogRange{
											Left: &syntax.MatchersExpr{
												Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
											},
											Interval: 5 * time.Minute,
										},
									},
								},
							},
							next: nil,
						},
					},
				},
			},
		},
		{
			in: `sum by (cluster) (rate({foo="bar"}[5m])) / ignoring(cluster) count(rate({foo="bar"}[5m]))`,
			expr: &syntax.BinOpExpr{
				Op: syntax.OpTypeDiv,
				Opts: &syntax.BinOpOptions{
					ReturnBool: false,
					VectorMatching: &syntax.VectorMatching{
						On:             false,
						MatchingLabels: []string{"cluster"},
					},
				},
				SampleExpr: &syntax.VectorAggregationExpr{
					Grouping: &syntax.Grouping{
						Groups: []string{"cluster"},
					},
					Operation: syntax.OpTypeSum,
					Left: &ConcatSampleExpr{
						DownstreamSampleExpr: DownstreamSampleExpr{
							shard: NewPowerOfTwoShard(index.ShardAnnotation{
								Shard: 0,
								Of:    2,
							}).Bind(nil),
							SampleExpr: &syntax.VectorAggregationExpr{
								Grouping: &syntax.Grouping{
									Groups: []string{"cluster"},
								},
								Operation: syntax.OpTypeSum,
								Left: &syntax.RangeAggregationExpr{
									Operation: syntax.OpRangeTypeRate,
									Left: &syntax.LogRange{
										Left: &syntax.MatchersExpr{
											Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
										},
										Interval: 5 * time.Minute,
									},
								},
							},
						},
						next: &ConcatSampleExpr{
							DownstreamSampleExpr: DownstreamSampleExpr{
								shard: NewPowerOfTwoShard(index.ShardAnnotation{
									Shard: 1,
									Of:    2,
								}).Bind(nil),
								SampleExpr: &syntax.VectorAggregationExpr{
									Grouping: &syntax.Grouping{
										Groups: []string{"cluster"},
									},
									Operation: syntax.OpTypeSum,
									Left: &syntax.RangeAggregationExpr{
										Operation: syntax.OpRangeTypeRate,
										Left: &syntax.LogRange{
											Left: &syntax.MatchersExpr{
												Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
											},
											Interval: 5 * time.Minute,
										},
									},
								},
							},
							next: nil,
						},
					},
				},
				RHS: &syntax.VectorAggregationExpr{
					Operation: syntax.OpTypeSum,
					Grouping:  &syntax.Grouping{},
					Left: &ConcatSampleExpr{
						DownstreamSampleExpr: DownstreamSampleExpr{
							shard: NewPowerOfTwoShard(index.ShardAnnotation{
								Shard: 0,
								Of:    2,
							}).Bind(nil),
							SampleExpr: &syntax.VectorAggregationExpr{
								Grouping:  &syntax.Grouping{},
								Operation: syntax.OpTypeCount,
								Left: &syntax.RangeAggregationExpr{
									Operation: syntax.OpRangeTypeRate,
									Left: &syntax.LogRange{
										Left: &syntax.MatchersExpr{
											Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
										},
										Interval: 5 * time.Minute,
									},
								},
							},
						},
						next: &ConcatSampleExpr{
							DownstreamSampleExpr: DownstreamSampleExpr{
								shard: NewPowerOfTwoShard(index.ShardAnnotation{
									Shard: 1,
									Of:    2,
								}).Bind(nil),
								SampleExpr: &syntax.VectorAggregationExpr{
									Grouping:  &syntax.Grouping{},
									Operation: syntax.OpTypeCount,
									Left: &syntax.RangeAggregationExpr{
										Operation: syntax.OpRangeTypeRate,
										Left: &syntax.LogRange{
											Left: &syntax.MatchersExpr{
												Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
											},
											Interval: 5 * time.Minute,
										},
									},
								},
							},
							next: nil,
						},
					},
				},
			},
		},
		{
			in: `avg_over_time({foo="bar"} | unwrap bytes [5m]) by (cluster)`,
			expr: &syntax.BinOpExpr{
				Op: syntax.OpTypeDiv,
				SampleExpr: &syntax.VectorAggregationExpr{
					Grouping: &syntax.Grouping{
						Groups: []string{"cluster"},
					},
					Operation: syntax.OpTypeSum,
					Left: &ConcatSampleExpr{
						DownstreamSampleExpr: DownstreamSampleExpr{
							shard: NewPowerOfTwoShard(index.ShardAnnotation{
								Shard: 0,
								Of:    2,
							}).Bind(nil),
							SampleExpr: &syntax.VectorAggregationExpr{
								Grouping: &syntax.Grouping{
									Groups: []string{"cluster"},
								},
								Operation: syntax.OpTypeSum,
								Left: &syntax.RangeAggregationExpr{
									Operation: syntax.OpRangeTypeSum,
									Left: &syntax.LogRange{
										Left: &syntax.MatchersExpr{
											Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
										},
										Interval: 5 * time.Minute,
										Unwrap: &syntax.UnwrapExpr{
											Identifier: "bytes",
										},
									},
								},
							},
						},
						next: &ConcatSampleExpr{
							DownstreamSampleExpr: DownstreamSampleExpr{
								shard: NewPowerOfTwoShard(index.ShardAnnotation{
									Shard: 1,
									Of:    2,
								}).Bind(nil),
								SampleExpr: &syntax.VectorAggregationExpr{
									Grouping: &syntax.Grouping{
										Groups: []string{"cluster"},
									},
									Operation: syntax.OpTypeSum,
									Left: &syntax.RangeAggregationExpr{
										Operation: syntax.OpRangeTypeSum,
										Left: &syntax.LogRange{
											Left: &syntax.MatchersExpr{
												Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
											},
											Interval: 5 * time.Minute,
											Unwrap: &syntax.UnwrapExpr{
												Identifier: "bytes",
											},
										},
									},
								},
							},
							next: nil,
						},
					},
				},
				RHS: &syntax.VectorAggregationExpr{
					Operation: syntax.OpTypeSum,
					Grouping: &syntax.Grouping{
						Groups: []string{"cluster"},
					},
					Left: &ConcatSampleExpr{
						DownstreamSampleExpr: DownstreamSampleExpr{
							shard: NewPowerOfTwoShard(index.ShardAnnotation{
								Shard: 0,
								Of:    2,
							}).Bind(nil),
							SampleExpr: &syntax.VectorAggregationExpr{
								Grouping: &syntax.Grouping{
									Groups: []string{"cluster"},
								},
								Operation: syntax.OpTypeSum,
								Left: &syntax.RangeAggregationExpr{
									Operation: syntax.OpRangeTypeCount,
									Left: &syntax.LogRange{
										Left: &syntax.MatchersExpr{
											Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
										},
										Interval: 5 * time.Minute,
									},
								},
							},
						},
						next: &ConcatSampleExpr{
							DownstreamSampleExpr: DownstreamSampleExpr{
								shard: NewPowerOfTwoShard(index.ShardAnnotation{
									Shard: 1,
									Of:    2,
								}).Bind(nil),
								SampleExpr: &syntax.VectorAggregationExpr{
									Grouping: &syntax.Grouping{
										Groups: []string{"cluster"},
									},
									Operation: syntax.OpTypeSum,
									Left: &syntax.RangeAggregationExpr{
										Operation: syntax.OpRangeTypeCount,
										Left: &syntax.LogRange{
											Left: &syntax.MatchersExpr{
												Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
											},
											Interval: 5 * time.Minute,
										},
									},
								},
							},
							next: nil,
						},
					},
				},
			},
		},
		{
			in: `quantile_over_time(0.8, {foo="bar"} | unwrap bytes [5m])`,
			expr: &syntax.RangeAggregationExpr{
				Operation: syntax.OpRangeTypeQuantile,
				Params:    float64p(0.8),
				Left: &syntax.LogRange{
					Left: &syntax.MatchersExpr{
						Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
					},
					Unwrap: &syntax.UnwrapExpr{
						Identifier: "bytes",
					},
					Interval: 5 * time.Minute,
				},
			},
		},
		{
			in: `quantile_over_time(0.8, {foo="bar"} | unwrap bytes [5m]) by (cluster)`,
			expr: &syntax.RangeAggregationExpr{
				Operation: syntax.OpRangeTypeQuantile,
				Params:    float64p(0.8),
				Left: &syntax.LogRange{
					Left: &syntax.MatchersExpr{
						Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
					},
					Unwrap: &syntax.UnwrapExpr{
						Identifier: "bytes",
					},
					Interval: 5 * time.Minute,
				},
				Grouping: &syntax.Grouping{
					Groups: []string{"cluster"},
				},
			},
		},
		{
			in: `
			  quantile_over_time(0.99, {a="foo"} | unwrap bytes [1s]) by (b)
			and
			  sum by (b) (rate({a="bar"}[1s]))
			`,
			expr: &syntax.BinOpExpr{
				SampleExpr: DownstreamSampleExpr{
					SampleExpr: &syntax.RangeAggregationExpr{
						Operation: syntax.OpRangeTypeQuantile,
						Params:    float64p(0.99),
						Left: &syntax.LogRange{
							Left: &syntax.MatchersExpr{
								Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "a", "foo")},
							},
							Unwrap: &syntax.UnwrapExpr{
								Identifier: "bytes",
							},
							Interval: 1 * time.Second,
						},
						Grouping: &syntax.Grouping{
							Groups: []string{"b"},
						},
					},
				},
				RHS: &syntax.VectorAggregationExpr{
					Left: &ConcatSampleExpr{
						DownstreamSampleExpr: DownstreamSampleExpr{
							shard: NewPowerOfTwoShard(index.ShardAnnotation{
								Shard: 0,
								Of:    2,
							}).Bind(nil),
							SampleExpr: &syntax.VectorAggregationExpr{
								Left: &syntax.RangeAggregationExpr{
									Left: &syntax.LogRange{
										Left: &syntax.MatchersExpr{
											Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "a", "bar")},
										},
										Interval: 1 * time.Second,
									},
									Operation: syntax.OpRangeTypeRate,
								},
								Grouping: &syntax.Grouping{
									Groups: []string{"b"},
								},
								Params:    0,
								Operation: syntax.OpTypeSum,
							},
						},
						next: &ConcatSampleExpr{
							DownstreamSampleExpr: DownstreamSampleExpr{
								shard: NewPowerOfTwoShard(index.ShardAnnotation{
									Shard: 1,
									Of:    2,
								}).Bind(nil),
								SampleExpr: &syntax.VectorAggregationExpr{
									Left: &syntax.RangeAggregationExpr{
										Left: &syntax.LogRange{
											Left: &syntax.MatchersExpr{
												Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "a", "bar")},
											},
											Interval: 1 * time.Second,
										},
										Operation: syntax.OpRangeTypeRate,
									},
									Grouping: &syntax.Grouping{
										Groups: []string{"b"},
									},
									Params:    0,
									Operation: syntax.OpTypeSum,
								},
							},
							next: nil,
						},
					},
					Grouping: &syntax.Grouping{
						Groups: []string{"b"},
					},
					Operation: syntax.OpTypeSum,
				},
				Op: syntax.OpTypeAnd,
				Opts: &syntax.BinOpOptions{
					ReturnBool:     false,
					VectorMatching: &syntax.VectorMatching{},
				},
			},
		},
		{
			in: `quantile_over_time(0.99, {a="foo"} | unwrap bytes [1s]) by (a, b) > 1`,
			expr: &syntax.BinOpExpr{
				SampleExpr: DownstreamSampleExpr{
					SampleExpr: &syntax.RangeAggregationExpr{
						Operation: syntax.OpRangeTypeQuantile,
						Params:    float64p(0.99),
						Left: &syntax.LogRange{
							Left: &syntax.MatchersExpr{
								Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "a", "foo")},
							},
							Unwrap: &syntax.UnwrapExpr{
								Identifier: "bytes",
							},
							Interval: 1 * time.Second,
						},
						Grouping: &syntax.Grouping{
							Groups: []string{"a", "b"},
						},
					},
				},
				RHS: &syntax.LiteralExpr{
					Val: 1,
				},
				Op: syntax.OpTypeGT,
				Opts: &syntax.BinOpOptions{
					ReturnBool:     false,
					VectorMatching: &syntax.VectorMatching{},
				},
			},
		},
		{
			in: `1 < quantile_over_time(0.99, {a="foo"} | unwrap bytes [1s]) by (a, b)`,
			expr: &syntax.BinOpExpr{
				SampleExpr: &syntax.LiteralExpr{
					Val: 1,
				},
				RHS: DownstreamSampleExpr{
					SampleExpr: &syntax.RangeAggregationExpr{
						Operation: syntax.OpRangeTypeQuantile,
						Params:    float64p(0.99),
						Left: &syntax.LogRange{
							Left: &syntax.MatchersExpr{
								Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "a", "foo")},
							},
							Unwrap: &syntax.UnwrapExpr{
								Identifier: "bytes",
							},
							Interval: 1 * time.Second,
						},
						Grouping: &syntax.Grouping{
							Groups: []string{"a", "b"},
						},
					},
				},
				Op: syntax.OpTypeLT,
				Opts: &syntax.BinOpOptions{
					ReturnBool:     false,
					VectorMatching: &syntax.VectorMatching{},
				},
			},
		},
	} {
		t.Run(tc.in, func(t *testing.T) {
			ast, err := syntax.ParseExpr(tc.in)
			require.Equal(t, tc.err, err)

			mapped, _, err := m.Map(ast, nilShardMetrics.downstreamRecorder(), true)
			switch e := mapped.(type) {
			case syntax.SampleExpr:
				optimized, err := optimizeSampleExpr(e)
				require.NoError(t, err)
				require.Equal(t, mapped.String(), optimized.String())
			}

			require.Equal(t, tc.err, err)
			require.Equal(t, tc.expr.String(), mapped.String())
			require.Equal(t, tc.expr, mapped)
		})
	}
}

// nolint unparam
func mustNewMatcher(t labels.MatchType, n, v string) *labels.Matcher {
	m, err := labels.NewMatcher(t, n, v)
	if err != nil {
		panic(logqlmodel.NewParseError(err.Error(), 0, 0))
	}
	return m
}

func TestStringTrimming(t *testing.T) {
	for _, tc := range []struct {
		expr     syntax.Expr
		expected string
		shards   int
	}{
		{
			// sample expr in entirety for low shard count
			shards: 2,
			expr:   syntax.MustParseExpr(`count_over_time({app="foo"}[1m])`),
			expected: `
				downstream<count_over_time({app="foo"}[1m]),shard=0_of_2> ++
				downstream<count_over_time({app="foo"}[1m]),shard=1_of_2>
			`,
		},
		{
			// sample expr doesnt display infinite shards
			shards: 5,
			expr:   syntax.MustParseExpr(`count_over_time({app="foo"}[1m])`),
			expected: `
				downstream<count_over_time({app="foo"}[1m]),shard=0_of_5> ++
				downstream<count_over_time({app="foo"}[1m]),shard=1_of_5> ++
				downstream<count_over_time({app="foo"}[1m]),shard=2_of_5> ++
				downstream<count_over_time({app="foo"}[1m]),shard=3_of_5> ++
				...
			`,
		},
		{
			// log selector expr in entirety for low shard count
			shards: 2,
			expr:   syntax.MustParseExpr(`{app="foo"}`),
			expected: `
				downstream<{app="foo"},shard=0_of_2> ++
				downstream<{app="foo"},shard=1_of_2>
			`,
		},
		{
			// log selector expr doesnt display infinite shards
			shards: 5,
			expr:   syntax.MustParseExpr(`{app="foo"}`),
			expected: `
				downstream<{app="foo"},shard=0_of_5> ++
				downstream<{app="foo"},shard=1_of_5> ++
				downstream<{app="foo"},shard=2_of_5> ++
				downstream<{app="foo"},shard=3_of_5> ++
				...
			`,
		},
	} {
		t.Run(tc.expr.String(), func(t *testing.T) {
			strategy := NewPowerOfTwoStrategy(ConstantShards(tc.shards))
			m := NewShardMapper(strategy, nilShardMetrics, []string{ShardQuantileOverTime})
			_, _, mappedExpr, err := m.Parse(tc.expr)
			require.Nil(t, err)
			require.Equal(t, removeWhiteSpace(tc.expected), removeWhiteSpace(mappedExpr.String()))
		})
	}
}

func float64p(v float64) *float64 {
	return &v
}

func TestShardTopk(t *testing.T) {
	expr := `topk(
		10,
		sum by (ip) (
			sum_over_time({job="foo"} | json | unwrap bytes(bytes)[1m])
		)
	)`
	m := NewShardMapper(NewPowerOfTwoStrategy(ConstantShards(5)), nilShardMetrics, []string{ShardQuantileOverTime})
	_, _, mappedExpr, err := m.Parse(syntax.MustParseExpr(expr))
	require.NoError(t, err)

	expected := `topk(
  10,
  sum by (ip)(
    concat(
      downstream<sum by (ip)(sum_over_time({job="foo"} | json | unwrap bytes(bytes)[1m])), shard=0_of_5>
      ++
      downstream<sum by (ip)(sum_over_time({job="foo"} | json | unwrap bytes(bytes)[1m])), shard=1_of_5>
      ++
      downstream<sum by (ip)(sum_over_time({job="foo"} | json | unwrap bytes(bytes)[1m])), shard=2_of_5>
      ++
      downstream<sum by (ip)(sum_over_time({job="foo"} | json | unwrap bytes(bytes)[1m])), shard=3_of_5>
      ++ ...
    )
  )
)`
	require.Equal(t, expected, mappedExpr.Pretty(0))
}
