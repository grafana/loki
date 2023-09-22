package logql

import (
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/logqlmodel"
	"github.com/grafana/loki/pkg/querier/astmapper"
)

func TestShardedStringer(t *testing.T) {
	for _, tc := range []struct {
		in  syntax.Expr
		out string
	}{
		{
			in: &ConcatLogSelectorExpr{
				DownstreamLogSelectorExpr: DownstreamLogSelectorExpr{
					shard: &astmapper.ShardAnnotation{
						Shard: 0,
						Of:    2,
					},
					LogSelectorExpr: &syntax.MatchersExpr{
						Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
					},
				},
				next: &ConcatLogSelectorExpr{
					DownstreamLogSelectorExpr: DownstreamLogSelectorExpr{
						shard: &astmapper.ShardAnnotation{
							Shard: 1,
							Of:    2,
						},
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
	m := NewShardMapper(ConstantShards(2), nilShardMetrics)

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
					shard: &astmapper.ShardAnnotation{
						Shard: 0,
						Of:    2,
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
							Of:    2,
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
	} {
		t.Run(tc.in.String(), func(t *testing.T) {
			mapped, _, err := m.mapSampleExpr(tc.in, nilShardMetrics.downstreamRecorder())
			require.Nil(t, err)
			require.Equal(t, tc.out, mapped)
		})
	}
}

func TestMappingStrings(t *testing.T) {
	m := NewShardMapper(ConstantShards(2), nilShardMetrics)
	for _, tc := range []struct {
		in  string
		out string
	}{
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
				downstream<rate({foo="bar"}[5m]), shard=0_of_2>
				++ downstream<rate({foo="bar"}[5m]), shard=1_of_2>
			))`,
		},
		{
			in:  `sum(max(rate({foo="bar"} | json | label_format foo=bar [5m])))`,
			out: `sum(max(rate({foo="bar"} | json | label_format foo=bar [5m])))`,
		},
		{
			in:  `rate({foo="bar"} | json | label_format foo=bar [5m])`,
			out: `rate({foo="bar"} | json | label_format foo=bar [5m])`,
		},
		{
			in: `count(rate({foo="bar"} | json [5m]))`,
			out: `count(
				downstream<rate({foo="bar"} | json [5m]), shard=0_of_2>
				++ downstream<rate({foo="bar"} | json [5m]), shard=1_of_2>
			)`,
		},
		{
			in: `avg(rate({foo="bar"} | json [5m]))`,
			out: `avg(
				downstream<rate({foo="bar"} | json [5m]), shard=0_of_2>
				++ downstream<rate({foo="bar"} | json [5m]), shard=1_of_2>
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
			// Ensure we don't try to shard expressions that include label reformatting.
			in:  `sum(count_over_time({foo="bar"} | logfmt | label_format bar=baz | bar="buz" [5m]))`,
			out: `sum(count_over_time({foo="bar"} | logfmt | label_format bar=baz | bar="buz" [5m]))`,
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
			in:  `avg(avg_over_time({job=~"myapps.*"} |= "stats" | json busy="utilization" | unwrap busy [5m]))`,
			out: `avg(avg_over_time({job=~"myapps.*"} |= "stats" | json busy="utilization" | unwrap busy [5m]))`,
		},
		{
			in:  `avg_over_time({job=~"myapps.*"} |= "stats" | json busy="utilization" | unwrap busy [5m])`,
			out: `avg_over_time({job=~"myapps.*"} |= "stats" | json busy="utilization" | unwrap busy [5m])`,
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

			mapped, _, err := m.Map(ast, nilShardMetrics.downstreamRecorder())
			require.Nil(t, err)

			require.Equal(t, removeWhiteSpace(tc.out), removeWhiteSpace(mapped.String()))
		})
	}
}

func TestMapping(t *testing.T) {
	m := NewShardMapper(ConstantShards(2), nilShardMetrics)

	for _, tc := range []struct {
		in   string
		expr syntax.Expr
		err  error
	}{
		{
			in: `{foo="bar"}`,
			expr: &ConcatLogSelectorExpr{
				DownstreamLogSelectorExpr: DownstreamLogSelectorExpr{
					shard: &astmapper.ShardAnnotation{
						Shard: 0,
						Of:    2,
					},
					LogSelectorExpr: &syntax.MatchersExpr{
						Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
					},
				},
				next: &ConcatLogSelectorExpr{
					DownstreamLogSelectorExpr: DownstreamLogSelectorExpr{
						shard: &astmapper.ShardAnnotation{
							Shard: 1,
							Of:    2,
						},
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
					shard: &astmapper.ShardAnnotation{
						Shard: 0,
						Of:    2,
					},
					LogSelectorExpr: &syntax.PipelineExpr{
						Left: &syntax.MatchersExpr{
							Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
						},
						MultiStages: syntax.MultiStageExpr{
							&syntax.LineFilterExpr{
								Ty:    labels.MatchEqual,
								Match: "error",
								Op:    "",
							},
						},
					},
				},
				next: &ConcatLogSelectorExpr{
					DownstreamLogSelectorExpr: DownstreamLogSelectorExpr{
						shard: &astmapper.ShardAnnotation{
							Shard: 1,
							Of:    2,
						},
						LogSelectorExpr: &syntax.PipelineExpr{
							Left: &syntax.MatchersExpr{
								Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")},
							},
							MultiStages: syntax.MultiStageExpr{
								&syntax.LineFilterExpr{
									Ty:    labels.MatchEqual,
									Match: "error",
									Op:    "",
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
					shard: &astmapper.ShardAnnotation{
						Shard: 0,
						Of:    2,
					},
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
						shard: &astmapper.ShardAnnotation{
							Shard: 1,
							Of:    2,
						},
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
					shard: &astmapper.ShardAnnotation{
						Shard: 0,
						Of:    2,
					},
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
						shard: &astmapper.ShardAnnotation{
							Shard: 1,
							Of:    2,
						},
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
						shard: &astmapper.ShardAnnotation{
							Shard: 0,
							Of:    2,
						},
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
							shard: &astmapper.ShardAnnotation{
								Shard: 1,
								Of:    2,
							},
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
						shard: &astmapper.ShardAnnotation{
							Shard: 0,
							Of:    2,
						},
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
							shard: &astmapper.ShardAnnotation{
								Shard: 1,
								Of:    2,
							},
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
			in: `max without (env) (rate({foo="bar"}[5m]))`,
			expr: &syntax.VectorAggregationExpr{
				Grouping: &syntax.Grouping{
					Without: true,
					Groups:  []string{"env"},
				},
				Operation: syntax.OpTypeMax,
				Left: &ConcatSampleExpr{
					DownstreamSampleExpr: DownstreamSampleExpr{
						shard: &astmapper.ShardAnnotation{
							Shard: 0,
							Of:    2,
						},
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
							shard: &astmapper.ShardAnnotation{
								Shard: 1,
								Of:    2,
							},
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
						shard: &astmapper.ShardAnnotation{
							Shard: 0,
							Of:    2,
						},
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
							shard: &astmapper.ShardAnnotation{
								Shard: 1,
								Of:    2,
							},
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
							shard: &astmapper.ShardAnnotation{
								Shard: 0,
								Of:    2,
							},
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
								shard: &astmapper.ShardAnnotation{
									Shard: 1,
									Of:    2,
								},
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
							shard: &astmapper.ShardAnnotation{
								Shard: 0,
								Of:    2,
							},
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
								shard: &astmapper.ShardAnnotation{
									Shard: 1,
									Of:    2,
								},
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
							shard: &astmapper.ShardAnnotation{
								Shard: 0,
								Of:    2,
							},
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
								shard: &astmapper.ShardAnnotation{
									Shard: 1,
									Of:    2,
								},
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
							shard: &astmapper.ShardAnnotation{
								Shard: 0,
								Of:    2,
							},
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
								shard: &astmapper.ShardAnnotation{
									Shard: 1,
									Of:    2,
								},
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
		// sum(max) should not shard the maxes
		{
			in: `sum(max(rate({foo="bar"}[5m])))`,
			expr: &syntax.VectorAggregationExpr{
				Grouping:  &syntax.Grouping{},
				Operation: syntax.OpTypeSum,
				Left: &syntax.VectorAggregationExpr{
					Grouping:  &syntax.Grouping{},
					Operation: syntax.OpTypeMax,
					Left: &ConcatSampleExpr{
						DownstreamSampleExpr: DownstreamSampleExpr{
							shard: &astmapper.ShardAnnotation{
								Shard: 0,
								Of:    2,
							},
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
								shard: &astmapper.ShardAnnotation{
									Shard: 1,
									Of:    2,
								},
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
							shard: &astmapper.ShardAnnotation{
								Shard: 0,
								Of:    2,
							},
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
								shard: &astmapper.ShardAnnotation{
									Shard: 1,
									Of:    2,
								},
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
								shard: &astmapper.ShardAnnotation{
									Shard: 0,
									Of:    2,
								},
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
									shard: &astmapper.ShardAnnotation{
										Shard: 1,
										Of:    2,
									},
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
							shard: &astmapper.ShardAnnotation{
								Shard: 0,
								Of:    2,
							},
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
								shard: &astmapper.ShardAnnotation{
									Shard: 1,
									Of:    2,
								},
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
							shard: &astmapper.ShardAnnotation{
								Shard: 0,
								Of:    2,
							},
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
								shard: &astmapper.ShardAnnotation{
									Shard: 1,
									Of:    2,
								},
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
							shard: &astmapper.ShardAnnotation{
								Shard: 0,
								Of:    2,
							},
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
								shard: &astmapper.ShardAnnotation{
									Shard: 1,
									Of:    2,
								},
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
	} {
		t.Run(tc.in, func(t *testing.T) {
			ast, err := syntax.ParseExpr(tc.in)
			require.Equal(t, tc.err, err)

			mapped, _, err := m.Map(ast, nilShardMetrics.downstreamRecorder())

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
		expr     string
		expected string
		shards   int
	}{
		{
			// sample expr in entirety for low shard count
			shards: 2,
			expr:   `count_over_time({app="foo"}[1m])`,
			expected: `
				downstream<count_over_time({app="foo"}[1m]),shard=0_of_2> ++
				downstream<count_over_time({app="foo"}[1m]),shard=1_of_2>
			`,
		},
		{
			// sample expr doesnt display infinite shards
			shards: 5,
			expr:   `count_over_time({app="foo"}[1m])`,
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
			expr:   `{app="foo"}`,
			expected: `
				downstream<{app="foo"},shard=0_of_2> ++
				downstream<{app="foo"},shard=1_of_2>
			`,
		},
		{
			// log selector expr doesnt display infinite shards
			shards: 5,
			expr:   `{app="foo"}`,
			expected: `
				downstream<{app="foo"},shard=0_of_5> ++
				downstream<{app="foo"},shard=1_of_5> ++
				downstream<{app="foo"},shard=2_of_5> ++
				downstream<{app="foo"},shard=3_of_5> ++
				...
			`,
		},
	} {
		t.Run(tc.expr, func(t *testing.T) {
			m := NewShardMapper(ConstantShards(tc.shards), nilShardMetrics)
			_, _, mappedExpr, err := m.Parse(tc.expr)
			require.Nil(t, err)
			require.Equal(t, removeWhiteSpace(tc.expected), removeWhiteSpace(mappedExpr.String()))
		})
	}
}
