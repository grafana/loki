package logql

import (
	"strings"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/querier/astmapper"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/require"
)

func TestShardedStringer(t *testing.T) {
	for _, tc := range []struct {
		in  Expr
		out string
	}{
		{
			in: &ConcatLogSelectorExpr{
				DownstreamLogSelectorExpr: DownstreamLogSelectorExpr{
					shard: &astmapper.ShardAnnotation{
						Shard: 0,
						Of:    2,
					},
					LogSelectorExpr: &matchersExpr{
						matchers: []*labels.Matcher{
							mustNewMatcher(labels.MatchEqual, "foo", "bar"),
						},
					},
				},
				next: &ConcatLogSelectorExpr{
					DownstreamLogSelectorExpr: DownstreamLogSelectorExpr{
						shard: &astmapper.ShardAnnotation{
							Shard: 1,
							Of:    2,
						},
						LogSelectorExpr: &matchersExpr{
							matchers: []*labels.Matcher{
								mustNewMatcher(labels.MatchEqual, "foo", "bar"),
							},
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
	m, err := NewShardMapper(2, nilMetrics)
	require.Nil(t, err)

	for _, tc := range []struct {
		in  SampleExpr
		out SampleExpr
	}{
		{
			in: &rangeAggregationExpr{
				operation: OpRangeTypeRate,
				left: &logRange{
					left: &matchersExpr{
						matchers: []*labels.Matcher{
							mustNewMatcher(labels.MatchEqual, "foo", "bar"),
						},
					},
					interval: time.Minute,
				},
			},
			out: &ConcatSampleExpr{
				DownstreamSampleExpr: DownstreamSampleExpr{
					shard: &astmapper.ShardAnnotation{
						Shard: 0,
						Of:    2,
					},
					SampleExpr: &rangeAggregationExpr{
						operation: OpRangeTypeRate,
						left: &logRange{
							left: &matchersExpr{
								matchers: []*labels.Matcher{
									mustNewMatcher(labels.MatchEqual, "foo", "bar"),
								},
							},
							interval: time.Minute,
						},
					},
				},
				next: &ConcatSampleExpr{
					DownstreamSampleExpr: DownstreamSampleExpr{
						shard: &astmapper.ShardAnnotation{
							Shard: 1,
							Of:    2,
						},
						SampleExpr: &rangeAggregationExpr{
							operation: OpRangeTypeRate,
							left: &logRange{
								left: &matchersExpr{
									matchers: []*labels.Matcher{
										mustNewMatcher(labels.MatchEqual, "foo", "bar"),
									},
								},
								interval: time.Minute,
							},
						},
					},
					next: nil,
				},
			},
		},
	} {
		t.Run(tc.in.String(), func(t *testing.T) {
			require.Equal(t, tc.out, m.mapSampleExpr(tc.in, nilMetrics.shardRecorder()))
		})

	}
}

func TestMappingStrings(t *testing.T) {
	m, err := NewShardMapper(2, nilMetrics)
	require.Nil(t, err)
	for _, tc := range []struct {
		in  string
		out string
	}{
		{
			in:  `{foo="bar"}`,
			out: `downstream<{foo="bar"}, shard=0_of_2> ++ downstream<{foo="bar"}, shard=1_of_2>`,
		},
		{
			in:  `{foo="bar"} |= "foo" |~ "bar" | json | latency >= 10s or foo<5 and bar="t" | line_format "b{{.blip}}"`,
			out: `downstream<{foo="bar"} |="foo" |~"bar" | json | (latency>=10s or (foo<5,bar="t"))| line_format "b{{.blip}}",shard=0_of_2>++downstream<{foo="bar"} |="foo" |~"bar" | json | (latency>=10s or (foo<5, bar="t")) | line_format "b{{.blip}}",shard=1_of_2>`,
		},
		{
			in:  `sum(rate({foo="bar"}[1m]))`,
			out: `sum(downstream<sum(rate({foo="bar"}[1m])), shard=0_of_2> ++ downstream<sum(rate({foo="bar"}[1m])), shard=1_of_2>)`,
		},
		{
			in:  `max(count(rate({foo="bar"}[5m]))) / 2`,
			out: `max(sum(downstream<count(rate({foo="bar"}[5m])), shard=0_of_2> ++ downstream<count(rate({foo="bar"}[5m])), shard=1_of_2>)) / 2`,
		},
		{
			in:  `topk(3, rate({foo="bar"}[5m]))`,
			out: `topk(3,downstream<rate({foo="bar"}[5m]), shard=0_of_2> ++ downstream<rate({foo="bar"}[5m]), shard=1_of_2>)`,
		},
		{
			in:  `sum(max(rate({foo="bar"}[5m])))`,
			out: `sum(max(downstream<rate({foo="bar"}[5m]), shard=0_of_2> ++ downstream<rate({foo="bar"}[5m]), shard=1_of_2>))`,
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
			in:  `{foo="bar"} |= "id=123"`,
			out: `downstream<{foo="bar"}|="id=123", shard=0_of_2> ++ downstream<{foo="bar"}|="id=123", shard=1_of_2>`,
		},
		{
			in:  `sum by (cluster) (rate({foo="bar"} |= "id=123" [5m]))`,
			out: `sum by(cluster)(downstream<sum by(cluster)(rate({foo="bar"}|="id=123"[5m])), shard=0_of_2> ++ downstream<sum by(cluster)(rate({foo="bar"}|="id=123"[5m])), shard=1_of_2>)`,
		},
		{
			in:  `sum by (cluster) (sum_over_time({foo="bar"} |= "id=123" | logfmt | unwrap latency [5m]))`,
			out: `sum by(cluster)(downstream<sum by(cluster)(sum_over_time({foo="bar"}|="id=123"| logfmt | unwrap latency[5m])), shard=0_of_2> ++ downstream<sum by(cluster)(sum_over_time({foo="bar"}|="id=123"| logfmt | unwrap latency[5m])), shard=1_of_2>)`,
		},
		{
			in:  `sum by (cluster) (stddev_over_time({foo="bar"} |= "id=123" | logfmt | unwrap latency [5m]))`,
			out: `sum by (cluster) (stddev_over_time({foo="bar"} |= "id=123" | logfmt | unwrap latency [5m]))`,
		},
		{
			in: `
		sum without (a) (
		  label_replace(
		    sum without (b) (
		      rate({foo="bar"}[5m])
		    ),
		    "baz", "buz", "foo", "(.*)"
		  )
		)
		`,
			out: `sum without(a)(label_replace(sum without(b)(downstream<sum without(b)(rate({foo="bar"}[5m])),shard=0_of_2>++downstream<sum without(b)(rate({foo="bar"}[5m])),shard=1_of_2>),"baz","buz","foo","(.*)"))`,
		},
		{
			// Ensure we don't try to shard expressions that include label reformatting.
			in:  `sum(count_over_time({foo="bar"} | logfmt | label_format bar=baz | bar="buz" [5m]))`,
			out: `sum(count_over_time({foo="bar"} | logfmt | label_format bar=baz | bar="buz" [5m]))`,
		},
	} {
		t.Run(tc.in, func(t *testing.T) {
			ast, err := ParseExpr(tc.in)
			require.Nil(t, err)

			mapped, err := m.Map(ast, nilMetrics.shardRecorder())
			require.Nil(t, err)

			require.Equal(t, strings.ReplaceAll(tc.out, " ", ""), strings.ReplaceAll(mapped.String(), " ", ""))

		})
	}
}

func TestMapping(t *testing.T) {
	m, err := NewShardMapper(2, nilMetrics)
	require.Nil(t, err)

	for _, tc := range []struct {
		in   string
		expr Expr
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
					LogSelectorExpr: &matchersExpr{
						matchers: []*labels.Matcher{
							mustNewMatcher(labels.MatchEqual, "foo", "bar"),
						},
					},
				},
				next: &ConcatLogSelectorExpr{
					DownstreamLogSelectorExpr: DownstreamLogSelectorExpr{
						shard: &astmapper.ShardAnnotation{
							Shard: 1,
							Of:    2,
						},
						LogSelectorExpr: &matchersExpr{
							matchers: []*labels.Matcher{
								mustNewMatcher(labels.MatchEqual, "foo", "bar"),
							},
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
					LogSelectorExpr: newPipelineExpr(
						newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
						MultiStageExpr{
							newLineFilterExpr(nil, labels.MatchEqual, "error"),
						},
					),
				},
				next: &ConcatLogSelectorExpr{
					DownstreamLogSelectorExpr: DownstreamLogSelectorExpr{
						shard: &astmapper.ShardAnnotation{
							Shard: 1,
							Of:    2,
						},
						LogSelectorExpr: newPipelineExpr(
							newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
							MultiStageExpr{
								newLineFilterExpr(nil, labels.MatchEqual, "error"),
							},
						),
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
					SampleExpr: &rangeAggregationExpr{
						operation: OpRangeTypeRate,
						left: &logRange{
							left: &matchersExpr{
								matchers: []*labels.Matcher{
									mustNewMatcher(labels.MatchEqual, "foo", "bar"),
								},
							},
							interval: 5 * time.Minute,
						},
					},
				},
				next: &ConcatSampleExpr{
					DownstreamSampleExpr: DownstreamSampleExpr{
						shard: &astmapper.ShardAnnotation{
							Shard: 1,
							Of:    2,
						},
						SampleExpr: &rangeAggregationExpr{
							operation: OpRangeTypeRate,
							left: &logRange{
								left: &matchersExpr{
									matchers: []*labels.Matcher{
										mustNewMatcher(labels.MatchEqual, "foo", "bar"),
									},
								},
								interval: 5 * time.Minute,
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
					SampleExpr: &rangeAggregationExpr{
						operation: OpRangeTypeCount,
						left: &logRange{
							left: &matchersExpr{
								matchers: []*labels.Matcher{
									mustNewMatcher(labels.MatchEqual, "foo", "bar"),
								},
							},
							interval: 5 * time.Minute,
						},
					},
				},
				next: &ConcatSampleExpr{
					DownstreamSampleExpr: DownstreamSampleExpr{
						shard: &astmapper.ShardAnnotation{
							Shard: 1,
							Of:    2,
						},
						SampleExpr: &rangeAggregationExpr{
							operation: OpRangeTypeCount,
							left: &logRange{
								left: &matchersExpr{
									matchers: []*labels.Matcher{
										mustNewMatcher(labels.MatchEqual, "foo", "bar"),
									},
								},
								interval: 5 * time.Minute,
							},
						},
					},
					next: nil,
				},
			},
		},
		{
			in: `sum(rate({foo="bar"}[5m]))`,
			expr: &vectorAggregationExpr{
				grouping:  &grouping{},
				operation: OpTypeSum,
				left: &ConcatSampleExpr{
					DownstreamSampleExpr: DownstreamSampleExpr{
						shard: &astmapper.ShardAnnotation{
							Shard: 0,
							Of:    2,
						},
						SampleExpr: &vectorAggregationExpr{
							grouping:  &grouping{},
							operation: OpTypeSum,
							left: &rangeAggregationExpr{
								operation: OpRangeTypeRate,
								left: &logRange{
									left: &matchersExpr{
										matchers: []*labels.Matcher{
											mustNewMatcher(labels.MatchEqual, "foo", "bar"),
										},
									},
									interval: 5 * time.Minute,
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
							SampleExpr: &vectorAggregationExpr{
								grouping:  &grouping{},
								operation: OpTypeSum,
								left: &rangeAggregationExpr{
									operation: OpRangeTypeRate,
									left: &logRange{
										left: &matchersExpr{
											matchers: []*labels.Matcher{
												mustNewMatcher(labels.MatchEqual, "foo", "bar"),
											},
										},
										interval: 5 * time.Minute,
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
			expr: &vectorAggregationExpr{
				grouping:  &grouping{},
				params:    3,
				operation: OpTypeTopK,
				left: &ConcatSampleExpr{
					DownstreamSampleExpr: DownstreamSampleExpr{
						shard: &astmapper.ShardAnnotation{
							Shard: 0,
							Of:    2,
						},
						SampleExpr: &rangeAggregationExpr{
							operation: OpRangeTypeRate,
							left: &logRange{
								left: &matchersExpr{
									matchers: []*labels.Matcher{
										mustNewMatcher(labels.MatchEqual, "foo", "bar"),
									},
								},
								interval: 5 * time.Minute,
							},
						},
					},
					next: &ConcatSampleExpr{
						DownstreamSampleExpr: DownstreamSampleExpr{
							shard: &astmapper.ShardAnnotation{
								Shard: 1,
								Of:    2,
							},
							SampleExpr: &rangeAggregationExpr{
								operation: OpRangeTypeRate,
								left: &logRange{
									left: &matchersExpr{
										matchers: []*labels.Matcher{
											mustNewMatcher(labels.MatchEqual, "foo", "bar"),
										},
									},
									interval: 5 * time.Minute,
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
			expr: &vectorAggregationExpr{
				grouping: &grouping{
					without: true,
					groups:  []string{"env"},
				},
				operation: OpTypeMax,
				left: &ConcatSampleExpr{
					DownstreamSampleExpr: DownstreamSampleExpr{
						shard: &astmapper.ShardAnnotation{
							Shard: 0,
							Of:    2,
						},
						SampleExpr: &rangeAggregationExpr{
							operation: OpRangeTypeRate,
							left: &logRange{
								left: &matchersExpr{
									matchers: []*labels.Matcher{
										mustNewMatcher(labels.MatchEqual, "foo", "bar"),
									},
								},
								interval: 5 * time.Minute,
							},
						},
					},
					next: &ConcatSampleExpr{
						DownstreamSampleExpr: DownstreamSampleExpr{
							shard: &astmapper.ShardAnnotation{
								Shard: 1,
								Of:    2,
							},
							SampleExpr: &rangeAggregationExpr{
								operation: OpRangeTypeRate,
								left: &logRange{
									left: &matchersExpr{
										matchers: []*labels.Matcher{
											mustNewMatcher(labels.MatchEqual, "foo", "bar"),
										},
									},
									interval: 5 * time.Minute,
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
			expr: &vectorAggregationExpr{
				operation: OpTypeSum,
				grouping:  &grouping{},
				left: &ConcatSampleExpr{
					DownstreamSampleExpr: DownstreamSampleExpr{
						shard: &astmapper.ShardAnnotation{
							Shard: 0,
							Of:    2,
						},
						SampleExpr: &vectorAggregationExpr{
							grouping:  &grouping{},
							operation: OpTypeCount,
							left: &rangeAggregationExpr{
								operation: OpRangeTypeRate,
								left: &logRange{
									left: &matchersExpr{
										matchers: []*labels.Matcher{
											mustNewMatcher(labels.MatchEqual, "foo", "bar"),
										},
									},
									interval: 5 * time.Minute,
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
							SampleExpr: &vectorAggregationExpr{
								grouping:  &grouping{},
								operation: OpTypeCount,
								left: &rangeAggregationExpr{
									operation: OpRangeTypeRate,
									left: &logRange{
										left: &matchersExpr{
											matchers: []*labels.Matcher{
												mustNewMatcher(labels.MatchEqual, "foo", "bar"),
											},
										},
										interval: 5 * time.Minute,
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
			expr: &binOpExpr{
				op: OpTypeDiv,
				SampleExpr: &vectorAggregationExpr{
					grouping:  &grouping{},
					operation: OpTypeSum,
					left: &ConcatSampleExpr{
						DownstreamSampleExpr: DownstreamSampleExpr{
							shard: &astmapper.ShardAnnotation{
								Shard: 0,
								Of:    2,
							},
							SampleExpr: &vectorAggregationExpr{
								grouping:  &grouping{},
								operation: OpTypeSum,
								left: &rangeAggregationExpr{
									operation: OpRangeTypeRate,
									left: &logRange{
										left: &matchersExpr{
											matchers: []*labels.Matcher{
												mustNewMatcher(labels.MatchEqual, "foo", "bar"),
											},
										},
										interval: 5 * time.Minute,
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
								SampleExpr: &vectorAggregationExpr{
									grouping:  &grouping{},
									operation: OpTypeSum,
									left: &rangeAggregationExpr{
										operation: OpRangeTypeRate,
										left: &logRange{
											left: &matchersExpr{
												matchers: []*labels.Matcher{
													mustNewMatcher(labels.MatchEqual, "foo", "bar"),
												},
											},
											interval: 5 * time.Minute,
										},
									},
								},
							},
							next: nil,
						},
					},
				},
				RHS: &vectorAggregationExpr{
					operation: OpTypeSum,
					grouping:  &grouping{},
					left: &ConcatSampleExpr{
						DownstreamSampleExpr: DownstreamSampleExpr{
							shard: &astmapper.ShardAnnotation{
								Shard: 0,
								Of:    2,
							},
							SampleExpr: &vectorAggregationExpr{
								grouping:  &grouping{},
								operation: OpTypeCount,
								left: &rangeAggregationExpr{
									operation: OpRangeTypeRate,
									left: &logRange{
										left: &matchersExpr{
											matchers: []*labels.Matcher{
												mustNewMatcher(labels.MatchEqual, "foo", "bar"),
											},
										},
										interval: 5 * time.Minute,
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
								SampleExpr: &vectorAggregationExpr{
									grouping:  &grouping{},
									operation: OpTypeCount,
									left: &rangeAggregationExpr{
										operation: OpRangeTypeRate,
										left: &logRange{
											left: &matchersExpr{
												matchers: []*labels.Matcher{
													mustNewMatcher(labels.MatchEqual, "foo", "bar"),
												},
											},
											interval: 5 * time.Minute,
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
			expr: &binOpExpr{
				op:         OpTypeAdd,
				SampleExpr: &literalExpr{value: 1},
				RHS: &vectorAggregationExpr{
					grouping: &grouping{
						groups: []string{"cluster"},
					},
					operation: OpTypeSum,
					left: &ConcatSampleExpr{
						DownstreamSampleExpr: DownstreamSampleExpr{
							shard: &astmapper.ShardAnnotation{
								Shard: 0,
								Of:    2,
							},
							SampleExpr: &vectorAggregationExpr{
								grouping: &grouping{
									groups: []string{"cluster"},
								},
								operation: OpTypeSum,
								left: &rangeAggregationExpr{
									operation: OpRangeTypeRate,
									left: &logRange{
										left: &matchersExpr{
											matchers: []*labels.Matcher{
												mustNewMatcher(labels.MatchEqual, "foo", "bar"),
											},
										},
										interval: 5 * time.Minute,
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
								SampleExpr: &vectorAggregationExpr{
									grouping: &grouping{
										groups: []string{"cluster"},
									},
									operation: OpTypeSum,
									left: &rangeAggregationExpr{
										operation: OpRangeTypeRate,
										left: &logRange{
											left: &matchersExpr{
												matchers: []*labels.Matcher{
													mustNewMatcher(labels.MatchEqual, "foo", "bar"),
												},
											},
											interval: 5 * time.Minute,
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
			expr: &vectorAggregationExpr{
				grouping:  &grouping{},
				operation: OpTypeSum,
				left: &vectorAggregationExpr{
					grouping:  &grouping{},
					operation: OpTypeMax,
					left: &ConcatSampleExpr{
						DownstreamSampleExpr: DownstreamSampleExpr{
							shard: &astmapper.ShardAnnotation{
								Shard: 0,
								Of:    2,
							},
							SampleExpr: &rangeAggregationExpr{
								operation: OpRangeTypeRate,
								left: &logRange{
									left: &matchersExpr{
										matchers: []*labels.Matcher{
											mustNewMatcher(labels.MatchEqual, "foo", "bar"),
										},
									},
									interval: 5 * time.Minute,
								},
							},
						},
						next: &ConcatSampleExpr{
							DownstreamSampleExpr: DownstreamSampleExpr{
								shard: &astmapper.ShardAnnotation{
									Shard: 1,
									Of:    2,
								},
								SampleExpr: &rangeAggregationExpr{
									operation: OpRangeTypeRate,
									left: &logRange{
										left: &matchersExpr{
											matchers: []*labels.Matcher{
												mustNewMatcher(labels.MatchEqual, "foo", "bar"),
											},
										},
										interval: 5 * time.Minute,
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
			expr: &vectorAggregationExpr{
				grouping:  &grouping{},
				operation: OpTypeMax,
				left: &vectorAggregationExpr{
					operation: OpTypeSum,
					grouping:  &grouping{},
					left: &ConcatSampleExpr{
						DownstreamSampleExpr: DownstreamSampleExpr{
							shard: &astmapper.ShardAnnotation{
								Shard: 0,
								Of:    2,
							},
							SampleExpr: &vectorAggregationExpr{
								grouping:  &grouping{},
								operation: OpTypeCount,
								left: &rangeAggregationExpr{
									operation: OpRangeTypeRate,
									left: &logRange{
										left: &matchersExpr{
											matchers: []*labels.Matcher{
												mustNewMatcher(labels.MatchEqual, "foo", "bar"),
											},
										},
										interval: 5 * time.Minute,
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
								SampleExpr: &vectorAggregationExpr{
									grouping:  &grouping{},
									operation: OpTypeCount,
									left: &rangeAggregationExpr{
										operation: OpRangeTypeRate,
										left: &logRange{
											left: &matchersExpr{
												matchers: []*labels.Matcher{
													mustNewMatcher(labels.MatchEqual, "foo", "bar"),
												},
											},
											interval: 5 * time.Minute,
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
			expr: &binOpExpr{
				op: OpTypeDiv,
				SampleExpr: &vectorAggregationExpr{
					operation: OpTypeMax,
					grouping:  &grouping{},
					left: &vectorAggregationExpr{
						grouping: &grouping{
							groups: []string{"cluster"},
						},
						operation: OpTypeSum,
						left: &ConcatSampleExpr{
							DownstreamSampleExpr: DownstreamSampleExpr{
								shard: &astmapper.ShardAnnotation{
									Shard: 0,
									Of:    2,
								},
								SampleExpr: &vectorAggregationExpr{
									grouping: &grouping{
										groups: []string{"cluster"},
									},
									operation: OpTypeSum,
									left: &rangeAggregationExpr{
										operation: OpRangeTypeRate,
										left: &logRange{
											left: &matchersExpr{
												matchers: []*labels.Matcher{
													mustNewMatcher(labels.MatchEqual, "foo", "bar"),
												},
											},
											interval: 5 * time.Minute,
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
									SampleExpr: &vectorAggregationExpr{
										grouping: &grouping{
											groups: []string{"cluster"},
										},
										operation: OpTypeSum,
										left: &rangeAggregationExpr{
											operation: OpRangeTypeRate,
											left: &logRange{
												left: &matchersExpr{
													matchers: []*labels.Matcher{
														mustNewMatcher(labels.MatchEqual, "foo", "bar"),
													},
												},
												interval: 5 * time.Minute,
											},
										},
									},
								},
								next: nil,
							},
						},
					},
				},
				RHS: &vectorAggregationExpr{
					operation: OpTypeSum,
					grouping:  &grouping{},
					left: &ConcatSampleExpr{
						DownstreamSampleExpr: DownstreamSampleExpr{
							shard: &astmapper.ShardAnnotation{
								Shard: 0,
								Of:    2,
							},
							SampleExpr: &vectorAggregationExpr{
								grouping:  &grouping{},
								operation: OpTypeCount,
								left: &rangeAggregationExpr{
									operation: OpRangeTypeRate,
									left: &logRange{
										left: &matchersExpr{
											matchers: []*labels.Matcher{
												mustNewMatcher(labels.MatchEqual, "foo", "bar"),
											},
										},
										interval: 5 * time.Minute,
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
								SampleExpr: &vectorAggregationExpr{
									grouping:  &grouping{},
									operation: OpTypeCount,
									left: &rangeAggregationExpr{
										operation: OpRangeTypeRate,
										left: &logRange{
											left: &matchersExpr{
												matchers: []*labels.Matcher{
													mustNewMatcher(labels.MatchEqual, "foo", "bar"),
												},
											},
											interval: 5 * time.Minute,
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
			ast, err := ParseExpr(tc.in)
			require.Equal(t, tc.err, err)

			mapped, err := m.Map(ast, nilMetrics.shardRecorder())

			require.Equal(t, tc.err, err)
			require.Equal(t, tc.expr.String(), mapped.String())
			require.Equal(t, tc.expr, mapped)
		})
	}
}
