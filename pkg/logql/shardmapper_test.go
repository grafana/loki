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
					LogSelectorExpr: &MatchersExpr{
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
						LogSelectorExpr: &MatchersExpr{
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
			in: &RangeAggregationExpr{
				Operation: OpRangeTypeRate,
				Left: &LogRange{
					Left: &MatchersExpr{
						matchers: []*labels.Matcher{
							mustNewMatcher(labels.MatchEqual, "foo", "bar"),
						},
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
					SampleExpr: &RangeAggregationExpr{
						Operation: OpRangeTypeRate,
						Left: &LogRange{
							Left: &MatchersExpr{
								matchers: []*labels.Matcher{
									mustNewMatcher(labels.MatchEqual, "foo", "bar"),
								},
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
						SampleExpr: &RangeAggregationExpr{
							Operation: OpRangeTypeRate,
							Left: &LogRange{
								Left: &MatchersExpr{
									matchers: []*labels.Matcher{
										mustNewMatcher(labels.MatchEqual, "foo", "bar"),
									},
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
			out: `(max(sum(downstream<count(rate({foo="bar"}[5m])), shard=0_of_2> ++ downstream<count(rate({foo="bar"}[5m])), shard=1_of_2>)) / 2)`,
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
		{
			in:  `sum by (cluster) (rate({foo="bar"} [5m])) + ignoring(machine) sum by (cluster,machine) (rate({foo="bar"} [5m]))`,
			out: `(sumby(cluster)(downstream<sumby(cluster)(rate({foo="bar"}[5m])),shard=0_of_2>++downstream<sumby(cluster)(rate({foo="bar"}[5m])),shard=1_of_2>)+ignoring(machine)sumby(cluster,machine)(downstream<sumby(cluster,machine)(rate({foo="bar"}[5m])),shard=0_of_2>++downstream<sumby(cluster,machine)(rate({foo="bar"}[5m])),shard=1_of_2>))`,
		},
		{
			in:  `sum by (cluster) (sum by (cluster) (rate({foo="bar"} [5m])) + ignoring(machine) sum by (cluster,machine) (rate({foo="bar"} [5m])))`,
			out: `sumby(cluster)((sumby(cluster)(downstream<sumby(cluster)(rate({foo="bar"}[5m])),shard=0_of_2>++downstream<sumby(cluster)(rate({foo="bar"}[5m])),shard=1_of_2>)+ignoring(machine)sumby(cluster,machine)(downstream<sumby(cluster,machine)(rate({foo="bar"}[5m])),shard=0_of_2>++downstream<sumby(cluster,machine)(rate({foo="bar"}[5m])),shard=1_of_2>)))`,
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
					LogSelectorExpr: &MatchersExpr{
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
						LogSelectorExpr: &MatchersExpr{
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
							newLineFilterExpr(labels.MatchEqual, "", "error"),
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
								newLineFilterExpr(labels.MatchEqual, "", "error"),
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
					SampleExpr: &RangeAggregationExpr{
						Operation: OpRangeTypeRate,
						Left: &LogRange{
							Left: &MatchersExpr{
								matchers: []*labels.Matcher{
									mustNewMatcher(labels.MatchEqual, "foo", "bar"),
								},
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
						SampleExpr: &RangeAggregationExpr{
							Operation: OpRangeTypeRate,
							Left: &LogRange{
								Left: &MatchersExpr{
									matchers: []*labels.Matcher{
										mustNewMatcher(labels.MatchEqual, "foo", "bar"),
									},
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
					SampleExpr: &RangeAggregationExpr{
						Operation: OpRangeTypeCount,
						Left: &LogRange{
							Left: &MatchersExpr{
								matchers: []*labels.Matcher{
									mustNewMatcher(labels.MatchEqual, "foo", "bar"),
								},
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
						SampleExpr: &RangeAggregationExpr{
							Operation: OpRangeTypeCount,
							Left: &LogRange{
								Left: &MatchersExpr{
									matchers: []*labels.Matcher{
										mustNewMatcher(labels.MatchEqual, "foo", "bar"),
									},
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
			expr: &VectorAggregationExpr{
				Grouping:  &Grouping{},
				Operation: OpTypeSum,
				Left: &ConcatSampleExpr{
					DownstreamSampleExpr: DownstreamSampleExpr{
						shard: &astmapper.ShardAnnotation{
							Shard: 0,
							Of:    2,
						},
						SampleExpr: &VectorAggregationExpr{
							Grouping:  &Grouping{},
							Operation: OpTypeSum,
							Left: &RangeAggregationExpr{
								Operation: OpRangeTypeRate,
								Left: &LogRange{
									Left: &MatchersExpr{
										matchers: []*labels.Matcher{
											mustNewMatcher(labels.MatchEqual, "foo", "bar"),
										},
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
							SampleExpr: &VectorAggregationExpr{
								Grouping:  &Grouping{},
								Operation: OpTypeSum,
								Left: &RangeAggregationExpr{
									Operation: OpRangeTypeRate,
									Left: &LogRange{
										Left: &MatchersExpr{
											matchers: []*labels.Matcher{
												mustNewMatcher(labels.MatchEqual, "foo", "bar"),
											},
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
			expr: &VectorAggregationExpr{
				Grouping:  &Grouping{},
				Params:    3,
				Operation: OpTypeTopK,
				Left: &ConcatSampleExpr{
					DownstreamSampleExpr: DownstreamSampleExpr{
						shard: &astmapper.ShardAnnotation{
							Shard: 0,
							Of:    2,
						},
						SampleExpr: &RangeAggregationExpr{
							Operation: OpRangeTypeRate,
							Left: &LogRange{
								Left: &MatchersExpr{
									matchers: []*labels.Matcher{
										mustNewMatcher(labels.MatchEqual, "foo", "bar"),
									},
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
							SampleExpr: &RangeAggregationExpr{
								Operation: OpRangeTypeRate,
								Left: &LogRange{
									Left: &MatchersExpr{
										matchers: []*labels.Matcher{
											mustNewMatcher(labels.MatchEqual, "foo", "bar"),
										},
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
			expr: &VectorAggregationExpr{
				Grouping: &Grouping{
					Without: true,
					Groups:  []string{"env"},
				},
				Operation: OpTypeMax,
				Left: &ConcatSampleExpr{
					DownstreamSampleExpr: DownstreamSampleExpr{
						shard: &astmapper.ShardAnnotation{
							Shard: 0,
							Of:    2,
						},
						SampleExpr: &RangeAggregationExpr{
							Operation: OpRangeTypeRate,
							Left: &LogRange{
								Left: &MatchersExpr{
									matchers: []*labels.Matcher{
										mustNewMatcher(labels.MatchEqual, "foo", "bar"),
									},
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
							SampleExpr: &RangeAggregationExpr{
								Operation: OpRangeTypeRate,
								Left: &LogRange{
									Left: &MatchersExpr{
										matchers: []*labels.Matcher{
											mustNewMatcher(labels.MatchEqual, "foo", "bar"),
										},
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
			expr: &VectorAggregationExpr{
				Operation: OpTypeSum,
				Grouping:  &Grouping{},
				Left: &ConcatSampleExpr{
					DownstreamSampleExpr: DownstreamSampleExpr{
						shard: &astmapper.ShardAnnotation{
							Shard: 0,
							Of:    2,
						},
						SampleExpr: &VectorAggregationExpr{
							Grouping:  &Grouping{},
							Operation: OpTypeCount,
							Left: &RangeAggregationExpr{
								Operation: OpRangeTypeRate,
								Left: &LogRange{
									Left: &MatchersExpr{
										matchers: []*labels.Matcher{
											mustNewMatcher(labels.MatchEqual, "foo", "bar"),
										},
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
							SampleExpr: &VectorAggregationExpr{
								Grouping:  &Grouping{},
								Operation: OpTypeCount,
								Left: &RangeAggregationExpr{
									Operation: OpRangeTypeRate,
									Left: &LogRange{
										Left: &MatchersExpr{
											matchers: []*labels.Matcher{
												mustNewMatcher(labels.MatchEqual, "foo", "bar"),
											},
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
			expr: &BinOpExpr{
				Op: OpTypeDiv,
				SampleExpr: &VectorAggregationExpr{
					Grouping:  &Grouping{},
					Operation: OpTypeSum,
					Left: &ConcatSampleExpr{
						DownstreamSampleExpr: DownstreamSampleExpr{
							shard: &astmapper.ShardAnnotation{
								Shard: 0,
								Of:    2,
							},
							SampleExpr: &VectorAggregationExpr{
								Grouping:  &Grouping{},
								Operation: OpTypeSum,
								Left: &RangeAggregationExpr{
									Operation: OpRangeTypeRate,
									Left: &LogRange{
										Left: &MatchersExpr{
											matchers: []*labels.Matcher{
												mustNewMatcher(labels.MatchEqual, "foo", "bar"),
											},
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
								SampleExpr: &VectorAggregationExpr{
									Grouping:  &Grouping{},
									Operation: OpTypeSum,
									Left: &RangeAggregationExpr{
										Operation: OpRangeTypeRate,
										Left: &LogRange{
											Left: &MatchersExpr{
												matchers: []*labels.Matcher{
													mustNewMatcher(labels.MatchEqual, "foo", "bar"),
												},
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
				RHS: &VectorAggregationExpr{
					Operation: OpTypeSum,
					Grouping:  &Grouping{},
					Left: &ConcatSampleExpr{
						DownstreamSampleExpr: DownstreamSampleExpr{
							shard: &astmapper.ShardAnnotation{
								Shard: 0,
								Of:    2,
							},
							SampleExpr: &VectorAggregationExpr{
								Grouping:  &Grouping{},
								Operation: OpTypeCount,
								Left: &RangeAggregationExpr{
									Operation: OpRangeTypeRate,
									Left: &LogRange{
										Left: &MatchersExpr{
											matchers: []*labels.Matcher{
												mustNewMatcher(labels.MatchEqual, "foo", "bar"),
											},
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
								SampleExpr: &VectorAggregationExpr{
									Grouping:  &Grouping{},
									Operation: OpTypeCount,
									Left: &RangeAggregationExpr{
										Operation: OpRangeTypeRate,
										Left: &LogRange{
											Left: &MatchersExpr{
												matchers: []*labels.Matcher{
													mustNewMatcher(labels.MatchEqual, "foo", "bar"),
												},
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
			expr: &BinOpExpr{
				Op: OpTypeAdd,
				Opts: &BinOpOptions{
					ReturnBool:     false,
					VectorMatching: &VectorMatching{Card: CardOneToOne},
				},
				SampleExpr: &LiteralExpr{value: 1},
				RHS: &VectorAggregationExpr{
					Grouping: &Grouping{
						Groups: []string{"cluster"},
					},
					Operation: OpTypeSum,
					Left: &ConcatSampleExpr{
						DownstreamSampleExpr: DownstreamSampleExpr{
							shard: &astmapper.ShardAnnotation{
								Shard: 0,
								Of:    2,
							},
							SampleExpr: &VectorAggregationExpr{
								Grouping: &Grouping{
									Groups: []string{"cluster"},
								},
								Operation: OpTypeSum,
								Left: &RangeAggregationExpr{
									Operation: OpRangeTypeRate,
									Left: &LogRange{
										Left: &MatchersExpr{
											matchers: []*labels.Matcher{
												mustNewMatcher(labels.MatchEqual, "foo", "bar"),
											},
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
								SampleExpr: &VectorAggregationExpr{
									Grouping: &Grouping{
										Groups: []string{"cluster"},
									},
									Operation: OpTypeSum,
									Left: &RangeAggregationExpr{
										Operation: OpRangeTypeRate,
										Left: &LogRange{
											Left: &MatchersExpr{
												matchers: []*labels.Matcher{
													mustNewMatcher(labels.MatchEqual, "foo", "bar"),
												},
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
			expr: &VectorAggregationExpr{
				Grouping:  &Grouping{},
				Operation: OpTypeSum,
				Left: &VectorAggregationExpr{
					Grouping:  &Grouping{},
					Operation: OpTypeMax,
					Left: &ConcatSampleExpr{
						DownstreamSampleExpr: DownstreamSampleExpr{
							shard: &astmapper.ShardAnnotation{
								Shard: 0,
								Of:    2,
							},
							SampleExpr: &RangeAggregationExpr{
								Operation: OpRangeTypeRate,
								Left: &LogRange{
									Left: &MatchersExpr{
										matchers: []*labels.Matcher{
											mustNewMatcher(labels.MatchEqual, "foo", "bar"),
										},
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
								SampleExpr: &RangeAggregationExpr{
									Operation: OpRangeTypeRate,
									Left: &LogRange{
										Left: &MatchersExpr{
											matchers: []*labels.Matcher{
												mustNewMatcher(labels.MatchEqual, "foo", "bar"),
											},
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
			expr: &VectorAggregationExpr{
				Grouping:  &Grouping{},
				Operation: OpTypeMax,
				Left: &VectorAggregationExpr{
					Operation: OpTypeSum,
					Grouping:  &Grouping{},
					Left: &ConcatSampleExpr{
						DownstreamSampleExpr: DownstreamSampleExpr{
							shard: &astmapper.ShardAnnotation{
								Shard: 0,
								Of:    2,
							},
							SampleExpr: &VectorAggregationExpr{
								Grouping:  &Grouping{},
								Operation: OpTypeCount,
								Left: &RangeAggregationExpr{
									Operation: OpRangeTypeRate,
									Left: &LogRange{
										Left: &MatchersExpr{
											matchers: []*labels.Matcher{
												mustNewMatcher(labels.MatchEqual, "foo", "bar"),
											},
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
								SampleExpr: &VectorAggregationExpr{
									Grouping:  &Grouping{},
									Operation: OpTypeCount,
									Left: &RangeAggregationExpr{
										Operation: OpRangeTypeRate,
										Left: &LogRange{
											Left: &MatchersExpr{
												matchers: []*labels.Matcher{
													mustNewMatcher(labels.MatchEqual, "foo", "bar"),
												},
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
			expr: &BinOpExpr{
				Op: OpTypeDiv,
				Opts: &BinOpOptions{
					ReturnBool:     false,
					VectorMatching: &VectorMatching{Card: CardOneToOne},
				},
				SampleExpr: &VectorAggregationExpr{
					Operation: OpTypeMax,
					Grouping:  &Grouping{},
					Left: &VectorAggregationExpr{
						Grouping: &Grouping{
							Groups: []string{"cluster"},
						},
						Operation: OpTypeSum,
						Left: &ConcatSampleExpr{
							DownstreamSampleExpr: DownstreamSampleExpr{
								shard: &astmapper.ShardAnnotation{
									Shard: 0,
									Of:    2,
								},
								SampleExpr: &VectorAggregationExpr{
									Grouping: &Grouping{
										Groups: []string{"cluster"},
									},
									Operation: OpTypeSum,
									Left: &RangeAggregationExpr{
										Operation: OpRangeTypeRate,
										Left: &LogRange{
											Left: &MatchersExpr{
												matchers: []*labels.Matcher{
													mustNewMatcher(labels.MatchEqual, "foo", "bar"),
												},
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
									SampleExpr: &VectorAggregationExpr{
										Grouping: &Grouping{
											Groups: []string{"cluster"},
										},
										Operation: OpTypeSum,
										Left: &RangeAggregationExpr{
											Operation: OpRangeTypeRate,
											Left: &LogRange{
												Left: &MatchersExpr{
													matchers: []*labels.Matcher{
														mustNewMatcher(labels.MatchEqual, "foo", "bar"),
													},
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
				RHS: &VectorAggregationExpr{
					Operation: OpTypeSum,
					Grouping:  &Grouping{},
					Left: &ConcatSampleExpr{
						DownstreamSampleExpr: DownstreamSampleExpr{
							shard: &astmapper.ShardAnnotation{
								Shard: 0,
								Of:    2,
							},
							SampleExpr: &VectorAggregationExpr{
								Grouping:  &Grouping{},
								Operation: OpTypeCount,
								Left: &RangeAggregationExpr{
									Operation: OpRangeTypeRate,
									Left: &LogRange{
										Left: &MatchersExpr{
											matchers: []*labels.Matcher{
												mustNewMatcher(labels.MatchEqual, "foo", "bar"),
											},
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
								SampleExpr: &VectorAggregationExpr{
									Grouping:  &Grouping{},
									Operation: OpTypeCount,
									Left: &RangeAggregationExpr{
										Operation: OpRangeTypeRate,
										Left: &LogRange{
											Left: &MatchersExpr{
												matchers: []*labels.Matcher{
													mustNewMatcher(labels.MatchEqual, "foo", "bar"),
												},
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
			expr: &BinOpExpr{
				Op: OpTypeDiv,
				Opts: &BinOpOptions{
					ReturnBool: false,
					VectorMatching: &VectorMatching{
						On:             false,
						MatchingLabels: []string{"cluster"},
					},
				},
				SampleExpr: &VectorAggregationExpr{
					Grouping: &Grouping{
						Groups: []string{"cluster"},
					},
					Operation: OpTypeSum,
					Left: &ConcatSampleExpr{
						DownstreamSampleExpr: DownstreamSampleExpr{
							shard: &astmapper.ShardAnnotation{
								Shard: 0,
								Of:    2,
							},
							SampleExpr: &VectorAggregationExpr{
								Grouping: &Grouping{
									Groups: []string{"cluster"},
								},
								Operation: OpTypeSum,
								Left: &RangeAggregationExpr{
									Operation: OpRangeTypeRate,
									Left: &LogRange{
										Left: &MatchersExpr{
											matchers: []*labels.Matcher{
												mustNewMatcher(labels.MatchEqual, "foo", "bar"),
											},
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
								SampleExpr: &VectorAggregationExpr{
									Grouping: &Grouping{
										Groups: []string{"cluster"},
									},
									Operation: OpTypeSum,
									Left: &RangeAggregationExpr{
										Operation: OpRangeTypeRate,
										Left: &LogRange{
											Left: &MatchersExpr{
												matchers: []*labels.Matcher{
													mustNewMatcher(labels.MatchEqual, "foo", "bar"),
												},
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
				RHS: &VectorAggregationExpr{
					Operation: OpTypeSum,
					Grouping:  &Grouping{},
					Left: &ConcatSampleExpr{
						DownstreamSampleExpr: DownstreamSampleExpr{
							shard: &astmapper.ShardAnnotation{
								Shard: 0,
								Of:    2,
							},
							SampleExpr: &VectorAggregationExpr{
								Grouping:  &Grouping{},
								Operation: OpTypeCount,
								Left: &RangeAggregationExpr{
									Operation: OpRangeTypeRate,
									Left: &LogRange{
										Left: &MatchersExpr{
											matchers: []*labels.Matcher{
												mustNewMatcher(labels.MatchEqual, "foo", "bar"),
											},
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
								SampleExpr: &VectorAggregationExpr{
									Grouping:  &Grouping{},
									Operation: OpTypeCount,
									Left: &RangeAggregationExpr{
										Operation: OpRangeTypeRate,
										Left: &LogRange{
											Left: &MatchersExpr{
												matchers: []*labels.Matcher{
													mustNewMatcher(labels.MatchEqual, "foo", "bar"),
												},
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
			ast, err := ParseExpr(tc.in)
			require.Equal(t, tc.err, err)

			mapped, err := m.Map(ast, nilMetrics.shardRecorder())

			require.Equal(t, tc.err, err)
			require.Equal(t, tc.expr.String(), mapped.String())
			require.Equal(t, tc.expr, mapped)
		})
	}
}
