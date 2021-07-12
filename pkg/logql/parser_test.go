package logql

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logql/log"
	"github.com/grafana/loki/pkg/logqlmodel"
)

func NewStringLabelFilter(s string) *string {
	return &s
}

func TestParse(t *testing.T) {
	for _, tc := range []struct {
		in  string
		exp Expr
		err error
	}{
		{
			// raw string
			in: "count_over_time({foo=~`bar\\w+`}[12h] |~ `error\\`)",
			exp: &RangeAggregationExpr{
				operation: "count_over_time",
				left: &LogRange{
					left: &PipelineExpr{
						pipeline: MultiStageExpr{
							newLineFilterExpr(nil, labels.MatchRegexp, "error\\"),
						},
						left: &MatchersExpr{
							matchers: []*labels.Matcher{
								mustNewMatcher(labels.MatchRegexp, "foo", "bar\\w+"),
							},
						},
					},
					interval: 12 * time.Hour,
				},
			},
		},
		{
			// test [12h] before filter expr
			in: `count_over_time({foo="bar"}[12h] |= "error")`,
			exp: &RangeAggregationExpr{
				operation: "count_over_time",
				left: &LogRange{
					left: newPipelineExpr(
						newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "foo", Value: "bar"}}),
						MultiStageExpr{
							newLineFilterExpr(nil, labels.MatchEqual, "error"),
						},
					),
					interval: 12 * time.Hour,
				},
			},
		},
		{
			// test [12h] after filter expr
			in: `count_over_time({foo="bar"} |= "error" [12h])`,
			exp: &RangeAggregationExpr{
				operation: "count_over_time",
				left: &LogRange{
					left: newPipelineExpr(
						newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "foo", Value: "bar"}}),
						MultiStageExpr{newLineFilterExpr(nil, labels.MatchEqual, "error")},
					),
					interval: 12 * time.Hour,
				},
			},
		},
		{
			in:  `{foo="bar"}`,
			exp: &MatchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
		},
		{
			in:  `{ foo = "bar" }`,
			exp: &MatchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
		},
		{
			in: `{ namespace="buzz", foo != "bar" }`,
			exp: &MatchersExpr{matchers: []*labels.Matcher{
				mustNewMatcher(labels.MatchEqual, "namespace", "buzz"),
				mustNewMatcher(labels.MatchNotEqual, "foo", "bar"),
			}},
		},
		{
			in:  `{ foo =~ "bar" }`,
			exp: &MatchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchRegexp, "foo", "bar")}},
		},
		{
			in: `{ namespace="buzz", foo !~ "bar" }`,
			exp: &MatchersExpr{matchers: []*labels.Matcher{
				mustNewMatcher(labels.MatchEqual, "namespace", "buzz"),
				mustNewMatcher(labels.MatchNotRegexp, "foo", "bar"),
			}},
		},
		{
			in: `count_over_time({ foo = "bar" }[12m])`,
			exp: &RangeAggregationExpr{
				left: &LogRange{
					left:     &MatchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
					interval: 12 * time.Minute,
				},
				operation: "count_over_time",
			},
		},
		{
			in: `bytes_over_time({ foo = "bar" }[12m])`,
			exp: &RangeAggregationExpr{
				left: &LogRange{
					left:     &MatchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
					interval: 12 * time.Minute,
				},
				operation: OpRangeTypeBytes,
			},
		},
		{
			in: `bytes_rate({ foo = "bar" }[12m])`,
			exp: &RangeAggregationExpr{
				left: &LogRange{
					left:     &MatchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
					interval: 12 * time.Minute,
				},
				operation: OpRangeTypeBytesRate,
			},
		},
		{
			in: `rate({ foo = "bar" }[5h])`,
			exp: &RangeAggregationExpr{
				left: &LogRange{
					left:     &MatchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
					interval: 5 * time.Hour,
				},
				operation: "rate",
			},
		},
		{
			in: `rate({ foo = "bar" }[5d])`,
			exp: &RangeAggregationExpr{
				left: &LogRange{
					left:     &MatchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
					interval: 5 * 24 * time.Hour,
				},
				operation: "rate",
			},
		},
		{
			in: `count_over_time({ foo = "bar" }[1w])`,
			exp: &RangeAggregationExpr{
				left: &LogRange{
					left:     &MatchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
					interval: 7 * 24 * time.Hour,
				},
				operation: "count_over_time",
			},
		},
		{
			in: `absent_over_time({ foo = "bar" }[1w])`,
			exp: &RangeAggregationExpr{
				left: &LogRange{
					left:     &MatchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
					interval: 7 * 24 * time.Hour,
				},
				operation: OpRangeTypeAbsent,
			},
		},
		{
			in: `sum(rate({ foo = "bar" }[5h]))`,
			exp: mustNewVectorAggregationExpr(&RangeAggregationExpr{
				left: &LogRange{
					left:     &MatchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
					interval: 5 * time.Hour,
				},
				operation: "rate",
			}, "sum", nil, nil),
		},
		{
			in: `sum(rate({ foo ="bar" }[1y]))`,
			exp: mustNewVectorAggregationExpr(&RangeAggregationExpr{
				left: &LogRange{
					left:     &MatchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
					interval: 365 * 24 * time.Hour,
				},
				operation: "rate",
			}, "sum", nil, nil),
		},
		{
			in: `avg(count_over_time({ foo = "bar" }[5h])) by (bar,foo)`,
			exp: mustNewVectorAggregationExpr(&RangeAggregationExpr{
				left: &LogRange{
					left:     &MatchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
					interval: 5 * time.Hour,
				},
				operation: "count_over_time",
			}, "avg", &grouping{
				without: false,
				groups:  []string{"bar", "foo"},
			}, nil),
		},
		{
			in: `avg(
					label_replace(
						count_over_time({ foo = "bar" }[5h]),
						"bar",
						"$1$2",
						"foo",
						"(.*).(.*)"
					)
				) by (bar,foo)`,
			exp: mustNewVectorAggregationExpr(
				mustNewLabelReplaceExpr(
					&RangeAggregationExpr{
						left: &LogRange{
							left:     &MatchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
							interval: 5 * time.Hour,
						},
						operation: "count_over_time",
					},
					"bar", "$1$2", "foo", "(.*).(.*)",
				),
				"avg", &grouping{
					without: false,
					groups:  []string{"bar", "foo"},
				}, nil),
		},
		{
			in: `avg(count_over_time({ foo = "bar" }[5h])) by ()`,
			exp: mustNewVectorAggregationExpr(&RangeAggregationExpr{
				left: &LogRange{
					left:     &MatchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
					interval: 5 * time.Hour,
				},
				operation: "count_over_time",
			}, "avg", &grouping{
				without: false,
				groups:  nil,
			}, nil),
		},
		{
			in: `max without (bar) (count_over_time({ foo = "bar" }[5h]))`,
			exp: mustNewVectorAggregationExpr(&RangeAggregationExpr{
				left: &LogRange{
					left:     &MatchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
					interval: 5 * time.Hour,
				},
				operation: "count_over_time",
			}, "max", &grouping{
				without: true,
				groups:  []string{"bar"},
			}, nil),
		},
		{
			in: `max without () (count_over_time({ foo = "bar" }[5h]))`,
			exp: mustNewVectorAggregationExpr(&RangeAggregationExpr{
				left: &LogRange{
					left:     &MatchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
					interval: 5 * time.Hour,
				},
				operation: "count_over_time",
			}, "max", &grouping{
				without: true,
				groups:  nil,
			}, nil),
		},
		{
			in: `topk(10,count_over_time({ foo = "bar" }[5h])) without (bar)`,
			exp: mustNewVectorAggregationExpr(&RangeAggregationExpr{
				left: &LogRange{
					left:     &MatchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
					interval: 5 * time.Hour,
				},
				operation: "count_over_time",
			}, "topk", &grouping{
				without: true,
				groups:  []string{"bar"},
			}, NewStringLabelFilter("10")),
		},
		{
			in: `bottomk(30 ,sum(rate({ foo = "bar" }[5h])) by (foo))`,
			exp: mustNewVectorAggregationExpr(mustNewVectorAggregationExpr(&RangeAggregationExpr{
				left: &LogRange{
					left:     &MatchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
					interval: 5 * time.Hour,
				},
				operation: "rate",
			}, "sum", &grouping{
				groups:  []string{"foo"},
				without: false,
			}, nil), "bottomk", nil,
				NewStringLabelFilter("30")),
		},
		{
			in: `max( sum(count_over_time({ foo = "bar" }[5h])) without (foo,bar) ) by (foo)`,
			exp: mustNewVectorAggregationExpr(mustNewVectorAggregationExpr(&RangeAggregationExpr{
				left: &LogRange{
					left:     &MatchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
					interval: 5 * time.Hour,
				},
				operation: "count_over_time",
			}, "sum", &grouping{
				groups:  []string{"foo", "bar"},
				without: true,
			}, nil), "max", &grouping{
				groups:  []string{"foo"},
				without: false,
			}, nil),
		},
		{
			in:  `unk({ foo = "bar" }[5m])`,
			err: logqlmodel.NewParseError("syntax error: unexpected IDENTIFIER", 1, 1),
		},
		{
			in:  `absent_over_time({ foo = "bar" }[5h]) by (foo)`,
			err: logqlmodel.NewParseError("grouping not allowed for absent_over_time aggregation", 0, 0),
		},
		{
			in:  `rate({ foo = "bar" }[5minutes])`,
			err: logqlmodel.NewParseError(`not a valid duration string: "5minutes"`, 0, 21),
		},
		{
			in:  `label_replace(rate({ foo = "bar" }[5m]),"")`,
			err: logqlmodel.NewParseError(`syntax error: unexpected ), expecting ,`, 1, 43),
		},
		{
			in:  `label_replace(rate({ foo = "bar" }[5m]),"foo","$1","bar","^^^^x43\\q")`,
			err: logqlmodel.NewParseError("invalid regex in label_replace: error parsing regexp: invalid escape sequence: `\\q`", 0, 0),
		},
		{
			in:  `rate({ foo = "bar" }[5)`,
			err: logqlmodel.NewParseError("missing closing ']' in duration", 0, 21),
		},
		{
			in:  `min({ foo = "bar" }[5m])`,
			err: logqlmodel.NewParseError("syntax error: unexpected RANGE", 0, 20),
		},
		{
			in:  `{ foo = "bar" }|logfmt|addr!=ip("1.2.3.4")`,
			err: logqlmodel.NewParseError("syntax error: unexpected ip, expecting BYTES or STRING or NUMBER or DURATION", 1, 30),
		},
		{
			in:  `{ foo = "bar" }|logfmt|addr>=ip("1.2.3.4")`,
			err: logqlmodel.NewParseError("syntax error: unexpected ip, expecting BYTES or NUMBER or DURATION", 1, 30),
		},
		{
			in:  `{ foo = "bar" }|logfmt|addr>ip("1.2.3.4")`,
			err: logqlmodel.NewParseError("syntax error: unexpected ip, expecting BYTES or NUMBER or DURATION", 1, 29),
		},
		{
			in:  `{ foo = "bar" }|logfmt|addr<=ip("1.2.3.4")`,
			err: logqlmodel.NewParseError("syntax error: unexpected ip, expecting BYTES or NUMBER or DURATION", 1, 30),
		},
		{
			in:  `{ foo = "bar" }|logfmt|addr<ip("1.2.3.4")`,
			err: logqlmodel.NewParseError("syntax error: unexpected ip, expecting BYTES or NUMBER or DURATION", 1, 29),
		},
		{
			in: `{ foo = "bar" }|logfmt|addr=ip("1.2.3.4")`,
			exp: newPipelineExpr(
				newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
				MultiStageExpr{newLabelParserExpr(OpParserTypeLogfmt, ""), newLabelFilterExpr(log.NewIPLabelFilter("1.2.3.4", "addr"))},
			),
		},
		{
			in: `{ foo = "bar" }|logfmt|level="error"| addr=ip("1.2.3.4")`,
			exp: newPipelineExpr(
				newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
				MultiStageExpr{
					newLabelParserExpr(OpParserTypeLogfmt, ""),
					newLabelFilterExpr(log.NewStringLabelFilter(mustNewMatcher(labels.MatchEqual, "level", "error"))),
					newLabelFilterExpr(log.NewIPLabelFilter("1.2.3.4", "addr")),
				},
			),
		},
		{
			in: `{ foo = "bar" }|logfmt|remote_addr=ip("2.3.4.5")|level="error"| addr=ip("1.2.3.4")`, // chain label filters with ip matcher
			exp: newPipelineExpr(
				newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
				MultiStageExpr{
					newLabelParserExpr(OpParserTypeLogfmt, ""),
					newLabelFilterExpr(log.NewIPLabelFilter("2.3.4.5", "remote_addr")),
					newLabelFilterExpr(log.NewStringLabelFilter(mustNewMatcher(labels.MatchEqual, "level", "error"))),
					newLabelFilterExpr(log.NewIPLabelFilter("1.2.3.4", "addr")),
				},
			),
		},

		{
			in:  `sum(3 ,count_over_time({ foo = "bar" }[5h]))`,
			err: logqlmodel.NewParseError("unsupported parameter for operation sum(3,", 0, 0),
		},
		{
			in:  `topk(count_over_time({ foo = "bar" }[5h]))`,
			err: logqlmodel.NewParseError("parameter required for operation topk", 0, 0),
		},
		{
			in:  `bottomk(he,count_over_time({ foo = "bar" }[5h]))`,
			err: logqlmodel.NewParseError("syntax error: unexpected IDENTIFIER", 1, 9),
		},
		{
			in:  `bottomk(1.2,count_over_time({ foo = "bar" }[5h]))`,
			err: logqlmodel.NewParseError("invalid parameter bottomk(1.2,", 0, 0),
		},
		{
			in:  `stddev({ foo = "bar" })`,
			err: logqlmodel.NewParseError("syntax error: unexpected )", 1, 23),
		},
		{
			in: `{ foo = "bar", bar != "baz" }`,
			exp: &MatchersExpr{matchers: []*labels.Matcher{
				mustNewMatcher(labels.MatchEqual, "foo", "bar"),
				mustNewMatcher(labels.MatchNotEqual, "bar", "baz"),
			}},
		},
		{
			in: `{foo="bar"} |= "baz"`,
			exp: newPipelineExpr(
				newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
				MultiStageExpr{newLineFilterExpr(nil, labels.MatchEqual, "baz")},
			),
		},
		{
			in: `{foo="bar"} |= "baz" |~ "blip" != "flip" !~ "flap"`,
			exp: newPipelineExpr(
				newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
				MultiStageExpr{
					newLineFilterExpr(
						newLineFilterExpr(
							newLineFilterExpr(
								newLineFilterExpr(nil, labels.MatchEqual, "baz"),
								labels.MatchRegexp, "blip"),
							labels.MatchNotEqual, "flip"),
						labels.MatchNotRegexp, "flap"),
				},
			),
		},
		{
			in: `count_over_time(({foo="bar"} |= "baz" |~ "blip" != "flip" !~ "flap")[5m])`,
			exp: newRangeAggregationExpr(
				&LogRange{
					left: newPipelineExpr(
						newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
						MultiStageExpr{
							newLineFilterExpr(
								newLineFilterExpr(
									newLineFilterExpr(
										newLineFilterExpr(nil, labels.MatchEqual, "baz"),
										labels.MatchRegexp, "blip"),
									labels.MatchNotEqual, "flip"),
								labels.MatchNotRegexp, "flap"),
						},
					),
					interval: 5 * time.Minute,
				}, OpRangeTypeCount, nil, nil),
		},
		{
			in: `bytes_over_time(({foo="bar"} |= "baz" |~ "blip" != "flip" !~ "flap")[5m])`,
			exp: newRangeAggregationExpr(
				&LogRange{
					left: newPipelineExpr(
						newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
						MultiStageExpr{
							newLineFilterExpr(
								newLineFilterExpr(
									newLineFilterExpr(
										newLineFilterExpr(nil, labels.MatchEqual, "baz"),
										labels.MatchRegexp, "blip"),
									labels.MatchNotEqual, "flip"),
								labels.MatchNotRegexp, "flap"),
						},
					),
					interval: 5 * time.Minute,
				}, OpRangeTypeBytes, nil, nil),
		},
		{
			in: `bytes_over_time(({foo="bar"} |= "baz" |~ "blip" != "flip" !~ "flap" | unpack)[5m])`,
			exp: newRangeAggregationExpr(
				&LogRange{
					left: newPipelineExpr(
						newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
						MultiStageExpr{
							newLineFilterExpr(
								newLineFilterExpr(
									newLineFilterExpr(
										newLineFilterExpr(nil, labels.MatchEqual, "baz"),
										labels.MatchRegexp, "blip"),
									labels.MatchNotEqual, "flip"),
								labels.MatchNotRegexp, "flap"),
							newLabelParserExpr(OpParserTypeUnpack, ""),
						},
					),
					interval: 5 * time.Minute,
				}, OpRangeTypeBytes, nil, nil),
		},
		{
			in: `
			label_replace(
				bytes_over_time(({foo="bar"} |= "baz" |~ "blip" != "flip" !~ "flap")[5m]),
				"buzz",
				"$2",
				"bar",
				"(.*):(.*)"
			)
			`,
			exp: mustNewLabelReplaceExpr(
				newRangeAggregationExpr(
					&LogRange{
						left: newPipelineExpr(
							newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
							MultiStageExpr{
								newLineFilterExpr(
									newLineFilterExpr(
										newLineFilterExpr(
											newLineFilterExpr(nil, labels.MatchEqual, "baz"),
											labels.MatchRegexp, "blip"),
										labels.MatchNotEqual, "flip"),
									labels.MatchNotRegexp, "flap"),
							},
						),
						interval: 5 * time.Minute,
					}, OpRangeTypeBytes, nil, nil),
				"buzz",
				"$2",
				"bar",
				"(.*):(.*)",
			),
		},
		{
			in: `sum(count_over_time(({foo="bar"} |= "baz" |~ "blip" != "flip" !~ "flap")[5m])) by (foo)`,
			exp: mustNewVectorAggregationExpr(newRangeAggregationExpr(
				&LogRange{
					left: newPipelineExpr(
						newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
						MultiStageExpr{
							newLineFilterExpr(
								newLineFilterExpr(
									newLineFilterExpr(
										newLineFilterExpr(nil, labels.MatchEqual, "baz"),
										labels.MatchRegexp, "blip"),
									labels.MatchNotEqual, "flip"),
								labels.MatchNotRegexp, "flap"),
						},
					),
					interval: 5 * time.Minute,
				}, OpRangeTypeCount, nil, nil),
				"sum",
				&grouping{
					without: false,
					groups:  []string{"foo"},
				},
				nil),
		},
		{
			in: `sum(bytes_rate(({foo="bar"} |= "baz" |~ "blip" != "flip" !~ "flap")[5m])) by (foo)`,
			exp: mustNewVectorAggregationExpr(newRangeAggregationExpr(
				&LogRange{
					left: newPipelineExpr(
						newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
						MultiStageExpr{
							newLineFilterExpr(
								newLineFilterExpr(
									newLineFilterExpr(
										newLineFilterExpr(nil, labels.MatchEqual, "baz"),
										labels.MatchRegexp, "blip"),
									labels.MatchNotEqual, "flip"),
								labels.MatchNotRegexp, "flap"),
						},
					),
					interval: 5 * time.Minute,
				}, OpRangeTypeBytesRate, nil, nil),
				"sum",
				&grouping{
					without: false,
					groups:  []string{"foo"},
				},
				nil),
		},
		{
			in: `topk(5,count_over_time(({foo="bar"} |= "baz" |~ "blip" != "flip" !~ "flap")[5m])) without (foo)`,
			exp: mustNewVectorAggregationExpr(newRangeAggregationExpr(
				&LogRange{
					left: newPipelineExpr(
						newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
						MultiStageExpr{
							newLineFilterExpr(
								newLineFilterExpr(
									newLineFilterExpr(
										newLineFilterExpr(nil, labels.MatchEqual, "baz"),
										labels.MatchRegexp, "blip"),
									labels.MatchNotEqual, "flip"),
								labels.MatchNotRegexp, "flap"),
						},
					),
					interval: 5 * time.Minute,
				}, OpRangeTypeCount, nil, nil),
				"topk",
				&grouping{
					without: true,
					groups:  []string{"foo"},
				},
				NewStringLabelFilter("5")),
		},
		{
			in: `topk(5,sum(rate(({foo="bar"} |= "baz" |~ "blip" != "flip" !~ "flap")[5m])) by (app))`,
			exp: mustNewVectorAggregationExpr(
				mustNewVectorAggregationExpr(
					newRangeAggregationExpr(
						&LogRange{
							left: newPipelineExpr(
								newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
								MultiStageExpr{
									newLineFilterExpr(
										newLineFilterExpr(
											newLineFilterExpr(
												newLineFilterExpr(nil, labels.MatchEqual, "baz"),
												labels.MatchRegexp, "blip"),
											labels.MatchNotEqual, "flip"),
										labels.MatchNotRegexp, "flap"),
								},
							),
							interval: 5 * time.Minute,
						}, OpRangeTypeRate, nil, nil),
					"sum",
					&grouping{
						without: false,
						groups:  []string{"app"},
					},
					nil),
				"topk",
				nil,
				NewStringLabelFilter("5")),
		},
		{
			in: `count_over_time({foo="bar"}[5m] |= "baz" |~ "blip" != "flip" !~ "flap")`,
			exp: newRangeAggregationExpr(
				&LogRange{
					left: newPipelineExpr(
						newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
						MultiStageExpr{
							newLineFilterExpr(
								newLineFilterExpr(
									newLineFilterExpr(
										newLineFilterExpr(nil, labels.MatchEqual, "baz"),
										labels.MatchRegexp, "blip"),
									labels.MatchNotEqual, "flip"),
								labels.MatchNotRegexp, "flap"),
						},
					),
					interval: 5 * time.Minute,
				}, OpRangeTypeCount, nil, nil),
		},
		{
			in: `sum(count_over_time({foo="bar"}[5m] |= "baz" |~ "blip" != "flip" !~ "flap")) by (foo)`,
			exp: mustNewVectorAggregationExpr(newRangeAggregationExpr(
				&LogRange{
					left: newPipelineExpr(
						newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
						MultiStageExpr{
							newLineFilterExpr(
								newLineFilterExpr(
									newLineFilterExpr(
										newLineFilterExpr(nil, labels.MatchEqual, "baz"),
										labels.MatchRegexp, "blip"),
									labels.MatchNotEqual, "flip"),
								labels.MatchNotRegexp, "flap"),
						},
					),
					interval: 5 * time.Minute,
				}, OpRangeTypeCount, nil, nil),
				"sum",
				&grouping{
					without: false,
					groups:  []string{"foo"},
				},
				nil),
		},
		{
			in: `topk(5,count_over_time({foo="bar"}[5m] |= "baz" |~ "blip" != "flip" !~ "flap")) without (foo)`,
			exp: mustNewVectorAggregationExpr(newRangeAggregationExpr(
				&LogRange{
					left: newPipelineExpr(
						newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
						MultiStageExpr{
							newLineFilterExpr(
								newLineFilterExpr(
									newLineFilterExpr(
										newLineFilterExpr(nil, labels.MatchEqual, "baz"),
										labels.MatchRegexp, "blip"),
									labels.MatchNotEqual, "flip"),
								labels.MatchNotRegexp, "flap"),
						},
					),
					interval: 5 * time.Minute,
				}, OpRangeTypeCount, nil, nil),
				"topk",
				&grouping{
					without: true,
					groups:  []string{"foo"},
				},
				NewStringLabelFilter("5")),
		},
		{
			in: `topk(5,sum(rate({foo="bar"}[5m] |= "baz" |~ "blip" != "flip" !~ "flap")) by (app))`,
			exp: mustNewVectorAggregationExpr(
				mustNewVectorAggregationExpr(
					newRangeAggregationExpr(
						&LogRange{
							left: newPipelineExpr(
								newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
								MultiStageExpr{
									newLineFilterExpr(
										newLineFilterExpr(
											newLineFilterExpr(
												newLineFilterExpr(nil, labels.MatchEqual, "baz"),
												labels.MatchRegexp, "blip"),
											labels.MatchNotEqual, "flip"),
										labels.MatchNotRegexp, "flap"),
								},
							),
							interval: 5 * time.Minute,
						}, OpRangeTypeRate, nil, nil),
					"sum",
					&grouping{
						without: false,
						groups:  []string{"app"},
					},
					nil),
				"topk",
				nil,
				NewStringLabelFilter("5")),
		},
		{
			in:  `{foo="bar}`,
			err: logqlmodel.NewParseError("literal not terminated", 1, 6),
		},
		{
			in:  `{foo="bar"`,
			err: logqlmodel.NewParseError("syntax error: unexpected $end, expecting } or ,", 1, 11),
		},

		{
			in:  `{foo="bar"} |~`,
			err: logqlmodel.NewParseError("syntax error: unexpected $end, expecting STRING", 1, 15),
		},

		{
			in:  `{foo="bar"} "foo"`,
			err: logqlmodel.NewParseError("syntax error: unexpected STRING", 1, 13),
		},
		{
			in:  `{foo="bar"} foo`,
			err: logqlmodel.NewParseError("syntax error: unexpected IDENTIFIER", 1, 13),
		},
		{
			// require left associativity
			in: `
			sum(count_over_time({foo="bar"}[5m])) by (foo) /
			sum(count_over_time({foo="bar"}[5m])) by (foo) /
			sum(count_over_time({foo="bar"}[5m])) by (foo)
			`,
			exp: mustNewBinOpExpr(
				OpTypeDiv,
				BinOpOptions{},
				mustNewBinOpExpr(
					OpTypeDiv,
					BinOpOptions{},
					mustNewVectorAggregationExpr(newRangeAggregationExpr(
						&LogRange{
							left: &MatchersExpr{
								matchers: []*labels.Matcher{
									mustNewMatcher(labels.MatchEqual, "foo", "bar"),
								},
							},
							interval: 5 * time.Minute,
						}, OpRangeTypeCount, nil, nil),
						"sum",
						&grouping{
							without: false,
							groups:  []string{"foo"},
						},
						nil,
					),
					mustNewVectorAggregationExpr(newRangeAggregationExpr(
						&LogRange{
							left: &MatchersExpr{
								matchers: []*labels.Matcher{
									mustNewMatcher(labels.MatchEqual, "foo", "bar"),
								},
							},
							interval: 5 * time.Minute,
						}, OpRangeTypeCount, nil, nil),
						"sum",
						&grouping{
							without: false,
							groups:  []string{"foo"},
						},
						nil,
					),
				),
				mustNewVectorAggregationExpr(newRangeAggregationExpr(
					&LogRange{
						left: &MatchersExpr{
							matchers: []*labels.Matcher{
								mustNewMatcher(labels.MatchEqual, "foo", "bar"),
							},
						},
						interval: 5 * time.Minute,
					}, OpRangeTypeCount, nil, nil),
					"sum",
					&grouping{
						without: false,
						groups:  []string{"foo"},
					},
					nil,
				),
			),
		},
		{
			in: `
					sum(count_over_time({foo="bar"}[5m])) by (foo) ^
					sum(count_over_time({foo="bar"}[5m])) by (foo) /
					sum(count_over_time({foo="bar"}[5m])) by (foo)
					`,
			exp: mustNewBinOpExpr(
				OpTypeDiv,
				BinOpOptions{},
				mustNewBinOpExpr(
					OpTypePow,
					BinOpOptions{},
					mustNewVectorAggregationExpr(newRangeAggregationExpr(
						&LogRange{
							left: &MatchersExpr{
								matchers: []*labels.Matcher{
									mustNewMatcher(labels.MatchEqual, "foo", "bar"),
								},
							},
							interval: 5 * time.Minute,
						}, OpRangeTypeCount, nil, nil),
						"sum",
						&grouping{
							without: false,
							groups:  []string{"foo"},
						},
						nil,
					),
					mustNewVectorAggregationExpr(newRangeAggregationExpr(
						&LogRange{
							left: &MatchersExpr{
								matchers: []*labels.Matcher{
									mustNewMatcher(labels.MatchEqual, "foo", "bar"),
								},
							},
							interval: 5 * time.Minute,
						}, OpRangeTypeCount, nil, nil),
						"sum",
						&grouping{
							without: false,
							groups:  []string{"foo"},
						},
						nil,
					),
				),
				mustNewVectorAggregationExpr(newRangeAggregationExpr(
					&LogRange{
						left: &MatchersExpr{
							matchers: []*labels.Matcher{
								mustNewMatcher(labels.MatchEqual, "foo", "bar"),
							},
						},
						interval: 5 * time.Minute,
					}, OpRangeTypeCount, nil, nil),
					"sum",
					&grouping{
						without: false,
						groups:  []string{"foo"},
					},
					nil,
				),
			),
		},
		{
			// operator precedence before left associativity
			in: `
					sum(count_over_time({foo="bar"}[5m])) by (foo) +
					sum(count_over_time({foo="bar"}[5m])) by (foo) /
					sum(count_over_time({foo="bar"}[5m])) by (foo)
					`,
			exp: mustNewBinOpExpr(
				OpTypeAdd,
				BinOpOptions{},
				mustNewVectorAggregationExpr(newRangeAggregationExpr(
					&LogRange{
						left: &MatchersExpr{
							matchers: []*labels.Matcher{
								mustNewMatcher(labels.MatchEqual, "foo", "bar"),
							},
						},
						interval: 5 * time.Minute,
					}, OpRangeTypeCount, nil, nil),
					"sum",
					&grouping{
						without: false,
						groups:  []string{"foo"},
					},
					nil,
				),
				mustNewBinOpExpr(
					OpTypeDiv,
					BinOpOptions{},
					mustNewVectorAggregationExpr(newRangeAggregationExpr(
						&LogRange{
							left: &MatchersExpr{
								matchers: []*labels.Matcher{
									mustNewMatcher(labels.MatchEqual, "foo", "bar"),
								},
							},
							interval: 5 * time.Minute,
						}, OpRangeTypeCount, nil, nil),
						"sum",
						&grouping{
							without: false,
							groups:  []string{"foo"},
						},
						nil,
					),
					mustNewVectorAggregationExpr(newRangeAggregationExpr(
						&LogRange{
							left: &MatchersExpr{
								matchers: []*labels.Matcher{
									mustNewMatcher(labels.MatchEqual, "foo", "bar"),
								},
							},
							interval: 5 * time.Minute,
						}, OpRangeTypeCount, nil, nil),
						"sum",
						&grouping{
							without: false,
							groups:  []string{"foo"},
						},
						nil,
					),
				),
			),
		},
		{
			in: `sum by (job) (
							count_over_time({namespace="tns"} |= "level=error"[5m])
						/
							count_over_time({namespace="tns"}[5m])
						)`,
			exp: mustNewVectorAggregationExpr(
				mustNewBinOpExpr(OpTypeDiv,
					BinOpOptions{},
					newRangeAggregationExpr(
						&LogRange{
							left: newPipelineExpr(
								newMatcherExpr([]*labels.Matcher{
									mustNewMatcher(labels.MatchEqual, "namespace", "tns"),
								}),
								MultiStageExpr{
									newLineFilterExpr(nil, labels.MatchEqual, "level=error"),
								}),
							interval: 5 * time.Minute,
						}, OpRangeTypeCount, nil, nil),
					newRangeAggregationExpr(
						&LogRange{
							left: &MatchersExpr{
								matchers: []*labels.Matcher{
									mustNewMatcher(labels.MatchEqual, "namespace", "tns"),
								},
							},
							interval: 5 * time.Minute,
						}, OpRangeTypeCount, nil, nil)), OpTypeSum, &grouping{groups: []string{"job"}}, nil),
		},
		{
			in: `sum by (job) (
							count_over_time({namespace="tns"} |= "level=error"[5m])
						/
							count_over_time({namespace="tns"}[5m])
						) * 100`,
			exp: mustNewBinOpExpr(OpTypeMul, BinOpOptions{}, mustNewVectorAggregationExpr(
				mustNewBinOpExpr(OpTypeDiv,
					BinOpOptions{},
					newRangeAggregationExpr(
						&LogRange{
							left: newPipelineExpr(
								newMatcherExpr([]*labels.Matcher{
									mustNewMatcher(labels.MatchEqual, "namespace", "tns"),
								}),
								MultiStageExpr{
									newLineFilterExpr(nil, labels.MatchEqual, "level=error"),
								}),
							interval: 5 * time.Minute,
						}, OpRangeTypeCount, nil, nil),
					newRangeAggregationExpr(
						&LogRange{
							left: &MatchersExpr{
								matchers: []*labels.Matcher{
									mustNewMatcher(labels.MatchEqual, "namespace", "tns"),
								},
							},
							interval: 5 * time.Minute,
						}, OpRangeTypeCount, nil, nil)), OpTypeSum, &grouping{groups: []string{"job"}}, nil),
				mustNewLiteralExpr("100", false),
			),
		},
		{
			// reduces binop with two literalExprs
			in: `sum(count_over_time({foo="bar"}[5m])) by (foo) + 1 / 2`,
			exp: mustNewBinOpExpr(
				OpTypeAdd,
				BinOpOptions{},
				mustNewVectorAggregationExpr(
					newRangeAggregationExpr(
						&LogRange{
							left: &MatchersExpr{
								matchers: []*labels.Matcher{
									mustNewMatcher(labels.MatchEqual, "foo", "bar"),
								},
							},
							interval: 5 * time.Minute,
						}, OpRangeTypeCount, nil, nil),
					"sum",
					&grouping{
						without: false,
						groups:  []string{"foo"},
					},
					nil,
				),
				&LiteralExpr{value: 0.5},
			),
		},
		{
			// test signs
			in: `1 + -2 / 1`,
			exp: mustNewBinOpExpr(
				OpTypeAdd,
				BinOpOptions{},
				&LiteralExpr{value: 1},
				mustNewBinOpExpr(OpTypeDiv, BinOpOptions{}, &LiteralExpr{value: -2}, &LiteralExpr{value: 1}),
			),
		},
		{
			// test signs/ops with equal associativity
			in: `1 + 1 - -1`,
			exp: mustNewBinOpExpr(
				OpTypeSub,
				BinOpOptions{},
				mustNewBinOpExpr(OpTypeAdd, BinOpOptions{}, &LiteralExpr{value: 1}, &LiteralExpr{value: 1}),
				&LiteralExpr{value: -1},
			),
		},
		{
			in: `{app="foo"} |= "bar" | json | latency >= 250ms or ( status_code < 500 and status_code > 200)`,
			exp: &PipelineExpr{
				left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
				pipeline: MultiStageExpr{
					newLineFilterExpr(nil, labels.MatchEqual, "bar"),
					newLabelParserExpr(OpParserTypeJSON, ""),
					&LabelFilterExpr{
						LabelFilterer: log.NewOrLabelFilter(
							log.NewDurationLabelFilter(log.LabelFilterGreaterThanOrEqual, "latency", 250*time.Millisecond),
							log.NewAndLabelFilter(
								log.NewNumericLabelFilter(log.LabelFilterLesserThan, "status_code", 500.0),
								log.NewNumericLabelFilter(log.LabelFilterGreaterThan, "status_code", 200.0),
							),
						),
					},
				},
			},
		},
		{
			in: `{app="foo"} |= "bar" | unpack | json | latency >= 250ms or ( status_code < 500 and status_code > 200)`,
			exp: &PipelineExpr{
				left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
				pipeline: MultiStageExpr{
					newLineFilterExpr(nil, labels.MatchEqual, "bar"),
					newLabelParserExpr(OpParserTypeUnpack, ""),
					newLabelParserExpr(OpParserTypeJSON, ""),
					&LabelFilterExpr{
						LabelFilterer: log.NewOrLabelFilter(
							log.NewDurationLabelFilter(log.LabelFilterGreaterThanOrEqual, "latency", 250*time.Millisecond),
							log.NewAndLabelFilter(
								log.NewNumericLabelFilter(log.LabelFilterLesserThan, "status_code", 500.0),
								log.NewNumericLabelFilter(log.LabelFilterGreaterThan, "status_code", 200.0),
							),
						),
					},
				},
			},
		},
		{
			in: `{app="foo"} |= "bar" | json | (duration > 1s or status!= 200) and method!="POST"`,
			exp: &PipelineExpr{
				left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
				pipeline: MultiStageExpr{
					newLineFilterExpr(nil, labels.MatchEqual, "bar"),
					newLabelParserExpr(OpParserTypeJSON, ""),
					&LabelFilterExpr{
						LabelFilterer: log.NewAndLabelFilter(
							log.NewOrLabelFilter(
								log.NewDurationLabelFilter(log.LabelFilterGreaterThan, "duration", 1*time.Second),
								log.NewNumericLabelFilter(log.LabelFilterNotEqual, "status", 200.0),
							),
							log.NewStringLabelFilter(mustNewMatcher(labels.MatchNotEqual, "method", "POST")),
						),
					},
				},
			},
		},
		{
			in: `{app="foo"} |= "bar" | pattern "<foo> bar <buzz>" | (duration > 1s or status!= 200) and method!="POST"`,
			exp: &PipelineExpr{
				left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
				pipeline: MultiStageExpr{
					newLineFilterExpr(nil, labels.MatchEqual, "bar"),
					newLabelParserExpr(OpParserTypePattern, "<foo> bar <buzz>"),
					&LabelFilterExpr{
						LabelFilterer: log.NewAndLabelFilter(
							log.NewOrLabelFilter(
								log.NewDurationLabelFilter(log.LabelFilterGreaterThan, "duration", 1*time.Second),
								log.NewNumericLabelFilter(log.LabelFilterNotEqual, "status", 200.0),
							),
							log.NewStringLabelFilter(mustNewMatcher(labels.MatchNotEqual, "method", "POST")),
						),
					},
				},
			},
		},
		{
			in: `{app="foo"} |= "bar" | json | ( status_code < 500 and status_code > 200) or latency >= 250ms `,
			exp: &PipelineExpr{
				left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
				pipeline: MultiStageExpr{
					newLineFilterExpr(nil, labels.MatchEqual, "bar"),
					newLabelParserExpr(OpParserTypeJSON, ""),
					&LabelFilterExpr{
						LabelFilterer: log.NewOrLabelFilter(
							log.NewAndLabelFilter(
								log.NewNumericLabelFilter(log.LabelFilterLesserThan, "status_code", 500.0),
								log.NewNumericLabelFilter(log.LabelFilterGreaterThan, "status_code", 200.0),
							),
							log.NewDurationLabelFilter(log.LabelFilterGreaterThanOrEqual, "latency", 250*time.Millisecond),
						),
					},
				},
			},
		},
		{
			in: `{app="foo"} |= "bar" | json | ( status_code < 500 or status_code > 200) and latency >= 250ms `,
			exp: &PipelineExpr{
				left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
				pipeline: MultiStageExpr{
					newLineFilterExpr(nil, labels.MatchEqual, "bar"),
					newLabelParserExpr(OpParserTypeJSON, ""),
					&LabelFilterExpr{
						LabelFilterer: log.NewAndLabelFilter(
							log.NewOrLabelFilter(
								log.NewNumericLabelFilter(log.LabelFilterLesserThan, "status_code", 500.0),
								log.NewNumericLabelFilter(log.LabelFilterGreaterThan, "status_code", 200.0),
							),
							log.NewDurationLabelFilter(log.LabelFilterGreaterThanOrEqual, "latency", 250*time.Millisecond),
						),
					},
				},
			},
		},
		{
			in: `{app="foo"} |= "bar" | json |  status_code < 500 or status_code > 200 and latency >= 250ms `,
			exp: &PipelineExpr{
				left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
				pipeline: MultiStageExpr{
					newLineFilterExpr(nil, labels.MatchEqual, "bar"),
					newLabelParserExpr(OpParserTypeJSON, ""),
					&LabelFilterExpr{
						LabelFilterer: log.NewOrLabelFilter(
							log.NewNumericLabelFilter(log.LabelFilterLesserThan, "status_code", 500.0),
							log.NewAndLabelFilter(
								log.NewNumericLabelFilter(log.LabelFilterGreaterThan, "status_code", 200.0),
								log.NewDurationLabelFilter(log.LabelFilterGreaterThanOrEqual, "latency", 250*time.Millisecond),
							),
						),
					},
				},
			},
		},
		{
			in: `{app="foo"} |= "bar" | json | latency >= 250ms or ( status_code < 500 and status_code > 200)
				| foo="bar" buzz!="blip", blop=~"boop" or fuzz==5`,
			exp: &PipelineExpr{
				left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
				pipeline: MultiStageExpr{
					newLineFilterExpr(nil, labels.MatchEqual, "bar"),
					newLabelParserExpr(OpParserTypeJSON, ""),
					&LabelFilterExpr{
						LabelFilterer: log.NewOrLabelFilter(
							log.NewDurationLabelFilter(log.LabelFilterGreaterThanOrEqual, "latency", 250*time.Millisecond),
							log.NewAndLabelFilter(
								log.NewNumericLabelFilter(log.LabelFilterLesserThan, "status_code", 500.0),
								log.NewNumericLabelFilter(log.LabelFilterGreaterThan, "status_code", 200.0),
							),
						),
					},
					&LabelFilterExpr{
						LabelFilterer: log.NewAndLabelFilter(
							log.NewStringLabelFilter(mustNewMatcher(labels.MatchEqual, "foo", "bar")),
							log.NewAndLabelFilter(
								log.NewStringLabelFilter(mustNewMatcher(labels.MatchNotEqual, "buzz", "blip")),
								log.NewOrLabelFilter(
									log.NewStringLabelFilter(mustNewMatcher(labels.MatchRegexp, "blop", "boop")),
									log.NewNumericLabelFilter(log.LabelFilterEqual, "fuzz", 5),
								),
							),
						),
					},
				},
			},
		},
		{
			in: `{app="foo"} |= "bar" | line_format "blip{{ .foo }}blop"`,
			exp: &PipelineExpr{
				left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
				pipeline: MultiStageExpr{
					newLineFilterExpr(nil, labels.MatchEqual, "bar"),
					newLineFmtExpr("blip{{ .foo }}blop"),
				},
			},
		},
		{
			in: `{app="foo"} |= "bar" | json | latency >= 250ms or ( status_code < 500 and status_code > 200)
			| line_format "blip{{ .foo }}blop {{.status_code}}"`,
			exp: &PipelineExpr{
				left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
				pipeline: MultiStageExpr{
					newLineFilterExpr(nil, labels.MatchEqual, "bar"),
					newLabelParserExpr(OpParserTypeJSON, ""),
					&LabelFilterExpr{
						LabelFilterer: log.NewOrLabelFilter(
							log.NewDurationLabelFilter(log.LabelFilterGreaterThanOrEqual, "latency", 250*time.Millisecond),
							log.NewAndLabelFilter(
								log.NewNumericLabelFilter(log.LabelFilterLesserThan, "status_code", 500.0),
								log.NewNumericLabelFilter(log.LabelFilterGreaterThan, "status_code", 200.0),
							),
						),
					},
					newLineFmtExpr("blip{{ .foo }}blop {{.status_code}}"),
				},
			},
		},
		{
			in: `{app="foo"} |= "bar" | json | latency >= 250ms or ( status_code < 500 and status_code > 200)
			| line_format "blip{{ .foo }}blop {{.status_code}}" | label_format foo=bar,status_code="buzz{{.bar}}"`,
			exp: &PipelineExpr{
				left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
				pipeline: MultiStageExpr{
					newLineFilterExpr(nil, labels.MatchEqual, "bar"),
					newLabelParserExpr(OpParserTypeJSON, ""),
					&LabelFilterExpr{
						LabelFilterer: log.NewOrLabelFilter(
							log.NewDurationLabelFilter(log.LabelFilterGreaterThanOrEqual, "latency", 250*time.Millisecond),
							log.NewAndLabelFilter(
								log.NewNumericLabelFilter(log.LabelFilterLesserThan, "status_code", 500.0),
								log.NewNumericLabelFilter(log.LabelFilterGreaterThan, "status_code", 200.0),
							),
						),
					},
					newLineFmtExpr("blip{{ .foo }}blop {{.status_code}}"),
					newLabelFmtExpr([]log.LabelFmt{
						log.NewRenameLabelFmt("foo", "bar"),
						log.NewTemplateLabelFmt("status_code", "buzz{{.bar}}"),
					}),
				},
			},
		},
		{
			in: `count_over_time({app="foo"} |= "bar" | json | latency >= 250ms or ( status_code < 500 and status_code > 200)
			| line_format "blip{{ .foo }}blop {{.status_code}}" | label_format foo=bar,status_code="buzz{{.bar}}"[5m])`,
			exp: newRangeAggregationExpr(
				newLogRange(&PipelineExpr{
					left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
					pipeline: MultiStageExpr{
						newLineFilterExpr(nil, labels.MatchEqual, "bar"),
						newLabelParserExpr(OpParserTypeJSON, ""),
						&LabelFilterExpr{
							LabelFilterer: log.NewOrLabelFilter(
								log.NewDurationLabelFilter(log.LabelFilterGreaterThanOrEqual, "latency", 250*time.Millisecond),
								log.NewAndLabelFilter(
									log.NewNumericLabelFilter(log.LabelFilterLesserThan, "status_code", 500.0),
									log.NewNumericLabelFilter(log.LabelFilterGreaterThan, "status_code", 200.0),
								),
							),
						},
						newLineFmtExpr("blip{{ .foo }}blop {{.status_code}}"),
						newLabelFmtExpr([]log.LabelFmt{
							log.NewRenameLabelFmt("foo", "bar"),
							log.NewTemplateLabelFmt("status_code", "buzz{{.bar}}"),
						}),
					},
				},
					5*time.Minute,
					nil, nil),
				OpRangeTypeCount,
				nil,
				nil,
			),
		},
		{
			in:  "{app=~\"\xa0\xa1\"}",
			exp: nil,
			err: logqlmodel.NewParseError("invalid UTF-8 encoding", 1, 7),
		},
		{
			in: `sum_over_time({app="foo"} |= "bar" | json | latency >= 250ms or ( status_code < 500 and status_code > 200)
			| line_format "blip{{ .foo }}blop {{.status_code}}" | label_format foo=bar,status_code="buzz{{.bar}}"[5m])`,
			exp: nil,
			err: logqlmodel.NewParseError("invalid aggregation sum_over_time without unwrap", 0, 0),
		},
		{
			in:  `count_over_time({app="foo"} |= "foo" | json | unwrap foo [5m])`,
			exp: nil,
			err: logqlmodel.NewParseError("invalid aggregation count_over_time with unwrap", 0, 0),
		},
		{
			in: `{app="foo"} |= "bar" | json |  status_code < 500 or status_code > 200 and size >= 2.5KiB `,
			exp: &PipelineExpr{
				left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
				pipeline: MultiStageExpr{
					newLineFilterExpr(nil, labels.MatchEqual, "bar"),
					newLabelParserExpr(OpParserTypeJSON, ""),
					&LabelFilterExpr{
						LabelFilterer: log.NewOrLabelFilter(
							log.NewNumericLabelFilter(log.LabelFilterLesserThan, "status_code", 500.0),
							log.NewAndLabelFilter(
								log.NewNumericLabelFilter(log.LabelFilterGreaterThan, "status_code", 200.0),
								log.NewBytesLabelFilter(log.LabelFilterGreaterThanOrEqual, "size", 2560),
							),
						),
					},
				},
			},
		},
		{
			in: `stdvar_over_time({app="foo"} |= "bar" | json | latency >= 250ms or ( status_code < 500 and status_code > 200)
			| line_format "blip{{ .foo }}blop {{.status_code}}" | label_format foo=bar,status_code="buzz{{.bar}}" | unwrap foo [5m])`,
			exp: newRangeAggregationExpr(
				newLogRange(&PipelineExpr{
					left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
					pipeline: MultiStageExpr{
						newLineFilterExpr(nil, labels.MatchEqual, "bar"),
						newLabelParserExpr(OpParserTypeJSON, ""),
						&LabelFilterExpr{
							LabelFilterer: log.NewOrLabelFilter(
								log.NewDurationLabelFilter(log.LabelFilterGreaterThanOrEqual, "latency", 250*time.Millisecond),
								log.NewAndLabelFilter(
									log.NewNumericLabelFilter(log.LabelFilterLesserThan, "status_code", 500.0),
									log.NewNumericLabelFilter(log.LabelFilterGreaterThan, "status_code", 200.0),
								),
							),
						},
						newLineFmtExpr("blip{{ .foo }}blop {{.status_code}}"),
						newLabelFmtExpr([]log.LabelFmt{
							log.NewRenameLabelFmt("foo", "bar"),
							log.NewTemplateLabelFmt("status_code", "buzz{{.bar}}"),
						}),
					},
				},
					5*time.Minute,
					newUnwrapExpr("foo", ""),
					nil),
				OpRangeTypeStdvar, nil, nil,
			),
		},
		{
			in: `stdvar_over_time({app="foo"} |= "bar" | json | latency >= 250ms or ( status_code < 500 and status_code > 200)
			| line_format "blip{{ .foo }}blop {{.status_code}}" | label_format foo=bar,status_code="buzz{{.bar}}" | unwrap duration(foo) [5m])`,
			exp: newRangeAggregationExpr(
				newLogRange(&PipelineExpr{
					left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
					pipeline: MultiStageExpr{
						newLineFilterExpr(nil, labels.MatchEqual, "bar"),
						newLabelParserExpr(OpParserTypeJSON, ""),
						&LabelFilterExpr{
							LabelFilterer: log.NewOrLabelFilter(
								log.NewDurationLabelFilter(log.LabelFilterGreaterThanOrEqual, "latency", 250*time.Millisecond),
								log.NewAndLabelFilter(
									log.NewNumericLabelFilter(log.LabelFilterLesserThan, "status_code", 500.0),
									log.NewNumericLabelFilter(log.LabelFilterGreaterThan, "status_code", 200.0),
								),
							),
						},
						newLineFmtExpr("blip{{ .foo }}blop {{.status_code}}"),
						newLabelFmtExpr([]log.LabelFmt{
							log.NewRenameLabelFmt("foo", "bar"),
							log.NewTemplateLabelFmt("status_code", "buzz{{.bar}}"),
						}),
					},
				},
					5*time.Minute,
					newUnwrapExpr("foo", OpConvDuration),
					nil),
				OpRangeTypeStdvar, nil, nil,
			),
		},
		{
			in: `sum_over_time({namespace="tns"} |= "level=error" | json |foo>=5,bar<25ms| unwrap bytes(foo) [5m])`,
			exp: newRangeAggregationExpr(
				newLogRange(&PipelineExpr{
					left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "namespace", Value: "tns"}}),
					pipeline: MultiStageExpr{
						newLineFilterExpr(nil, labels.MatchEqual, "level=error"),
						newLabelParserExpr(OpParserTypeJSON, ""),
						&LabelFilterExpr{
							LabelFilterer: log.NewAndLabelFilter(
								log.NewNumericLabelFilter(log.LabelFilterGreaterThanOrEqual, "foo", 5),
								log.NewDurationLabelFilter(log.LabelFilterLesserThan, "bar", 25*time.Millisecond),
							),
						},
					},
				},
					5*time.Minute,
					newUnwrapExpr("foo", OpConvBytes),
					nil),
				OpRangeTypeSum, nil, nil,
			),
		},
		{
			in: `sum_over_time({namespace="tns"} |= "level=error" | json |foo>=5,bar<25ms| unwrap bytes(foo) [5m] offset 5m)`,
			exp: newRangeAggregationExpr(
				newLogRange(&PipelineExpr{
					left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "namespace", Value: "tns"}}),
					pipeline: MultiStageExpr{
						newLineFilterExpr(nil, labels.MatchEqual, "level=error"),
						newLabelParserExpr(OpParserTypeJSON, ""),
						&LabelFilterExpr{
							LabelFilterer: log.NewAndLabelFilter(
								log.NewNumericLabelFilter(log.LabelFilterGreaterThanOrEqual, "foo", 5),
								log.NewDurationLabelFilter(log.LabelFilterLesserThan, "bar", 25*time.Millisecond),
							),
						},
					},
				},
					5*time.Minute,
					newUnwrapExpr("foo", OpConvBytes),
					newOffsetExpr(5*time.Minute)),
				OpRangeTypeSum, nil, nil,
			),
		},
		{
			in: `sum_over_time({namespace="tns"} |= "level=error" | json |foo>=5,bar<25ms| unwrap latency [5m])`,
			exp: newRangeAggregationExpr(
				newLogRange(&PipelineExpr{
					left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "namespace", Value: "tns"}}),
					pipeline: MultiStageExpr{
						newLineFilterExpr(nil, labels.MatchEqual, "level=error"),
						newLabelParserExpr(OpParserTypeJSON, ""),
						&LabelFilterExpr{
							LabelFilterer: log.NewAndLabelFilter(
								log.NewNumericLabelFilter(log.LabelFilterGreaterThanOrEqual, "foo", 5),
								log.NewDurationLabelFilter(log.LabelFilterLesserThan, "bar", 25*time.Millisecond),
							),
						},
					},
				},
					5*time.Minute,
					newUnwrapExpr("latency", ""),
					nil),
				OpRangeTypeSum, nil, nil,
			),
		},
		{
			in: `sum_over_time({namespace="tns"} |= "level=error" | json |foo==5,bar<25ms| unwrap latency [5m])`,
			exp: newRangeAggregationExpr(
				newLogRange(&PipelineExpr{
					left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "namespace", Value: "tns"}}),
					pipeline: MultiStageExpr{
						newLineFilterExpr(nil, labels.MatchEqual, "level=error"),
						newLabelParserExpr(OpParserTypeJSON, ""),
						&LabelFilterExpr{
							LabelFilterer: log.NewAndLabelFilter(
								log.NewNumericLabelFilter(log.LabelFilterEqual, "foo", 5),
								log.NewDurationLabelFilter(log.LabelFilterLesserThan, "bar", 25*time.Millisecond),
							),
						},
					},
				},
					5*time.Minute,
					newUnwrapExpr("latency", ""),
					nil),
				OpRangeTypeSum, nil, nil,
			),
		},
		{
			in: `stddev_over_time({app="foo"} |= "bar" | unwrap bar [5m])`,
			exp: newRangeAggregationExpr(
				newLogRange(&PipelineExpr{
					left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
					pipeline: MultiStageExpr{
						newLineFilterExpr(nil, labels.MatchEqual, "bar"),
					},
				},
					5*time.Minute,
					newUnwrapExpr("bar", ""),
					nil),
				OpRangeTypeStddev, nil, nil,
			),
		},
		{
			in: `min_over_time({app="foo"} | unwrap bar [5m])`,
			exp: newRangeAggregationExpr(
				newLogRange(
					newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
					5*time.Minute,
					newUnwrapExpr("bar", ""),
					nil),
				OpRangeTypeMin, nil, nil,
			),
		},
		{
			in: `min_over_time({app="foo"} | unwrap bar [5m]) by ()`,
			exp: newRangeAggregationExpr(
				newLogRange(
					newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
					5*time.Minute,
					newUnwrapExpr("bar", ""),
					nil),
				OpRangeTypeMin, &grouping{}, nil,
			),
		},
		{
			in: `max_over_time({app="foo"} | unwrap bar [5m]) without ()`,
			exp: newRangeAggregationExpr(
				newLogRange(
					newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
					5*time.Minute,
					newUnwrapExpr("bar", ""),
					nil),
				OpRangeTypeMax, &grouping{without: true}, nil,
			),
		},
		{
			in: `max_over_time({app="foo"} | unwrap bar [5m]) without (foo,bar)`,
			exp: newRangeAggregationExpr(
				newLogRange(
					newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
					5*time.Minute,
					newUnwrapExpr("bar", ""),
					nil),
				OpRangeTypeMax, &grouping{without: true, groups: []string{"foo", "bar"}}, nil,
			),
		},
		{
			in: `max_over_time({app="foo"} | unwrap bar [5m] offset 5m) without (foo,bar)`,
			exp: newRangeAggregationExpr(
				newLogRange(
					newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
					5*time.Minute,
					newUnwrapExpr("bar", ""),
					newOffsetExpr(5*time.Minute)),
				OpRangeTypeMax, &grouping{without: true, groups: []string{"foo", "bar"}}, nil,
			),
		},
		{
			in: `max_over_time(({app="foo"} |= "bar" | json | latency >= 250ms or ( status_code < 500 and status_code > 200)
			| line_format "blip{{ .foo }}blop {{.status_code}}" | label_format foo=bar,status_code="buzz{{.bar}}" | unwrap foo )[5m])`,
			exp: newRangeAggregationExpr(
				newLogRange(&PipelineExpr{
					left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
					pipeline: MultiStageExpr{
						newLineFilterExpr(nil, labels.MatchEqual, "bar"),
						newLabelParserExpr(OpParserTypeJSON, ""),
						&LabelFilterExpr{
							LabelFilterer: log.NewOrLabelFilter(
								log.NewDurationLabelFilter(log.LabelFilterGreaterThanOrEqual, "latency", 250*time.Millisecond),
								log.NewAndLabelFilter(
									log.NewNumericLabelFilter(log.LabelFilterLesserThan, "status_code", 500.0),
									log.NewNumericLabelFilter(log.LabelFilterGreaterThan, "status_code", 200.0),
								),
							),
						},
						newLineFmtExpr("blip{{ .foo }}blop {{.status_code}}"),
						newLabelFmtExpr([]log.LabelFmt{
							log.NewRenameLabelFmt("foo", "bar"),
							log.NewTemplateLabelFmt("status_code", "buzz{{.bar}}"),
						}),
					},
				},
					5*time.Minute,
					newUnwrapExpr("foo", ""),
					nil),
				OpRangeTypeMax, nil, nil,
			),
		},
		{
			in: `quantile_over_time(0.99998,{app="foo"} |= "bar" | json | latency >= 250ms or ( status_code < 500 and status_code > 200)
			| line_format "blip{{ .foo }}blop {{.status_code}}" | label_format foo=bar,status_code="buzz{{.bar}}" | unwrap foo [5m])`,
			exp: newRangeAggregationExpr(
				newLogRange(&PipelineExpr{
					left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
					pipeline: MultiStageExpr{
						newLineFilterExpr(nil, labels.MatchEqual, "bar"),
						newLabelParserExpr(OpParserTypeJSON, ""),
						&LabelFilterExpr{
							LabelFilterer: log.NewOrLabelFilter(
								log.NewDurationLabelFilter(log.LabelFilterGreaterThanOrEqual, "latency", 250*time.Millisecond),
								log.NewAndLabelFilter(
									log.NewNumericLabelFilter(log.LabelFilterLesserThan, "status_code", 500.0),
									log.NewNumericLabelFilter(log.LabelFilterGreaterThan, "status_code", 200.0),
								),
							),
						},
						newLineFmtExpr("blip{{ .foo }}blop {{.status_code}}"),
						newLabelFmtExpr([]log.LabelFmt{
							log.NewRenameLabelFmt("foo", "bar"),
							log.NewTemplateLabelFmt("status_code", "buzz{{.bar}}"),
						}),
					},
				},
					5*time.Minute,
					newUnwrapExpr("foo", ""),
					nil),
				OpRangeTypeQuantile, nil, NewStringLabelFilter("0.99998"),
			),
		},
		{
			in: `quantile_over_time(0.99998,{app="foo"} |= "bar" | json | latency >= 250ms or ( status_code < 500 and status_code > 200)
			| line_format "blip{{ .foo }}blop {{.status_code}}" | label_format foo=bar,status_code="buzz{{.bar}}" | unwrap foo [5m]) by (namespace,instance)`,
			exp: newRangeAggregationExpr(
				newLogRange(&PipelineExpr{
					left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
					pipeline: MultiStageExpr{
						newLineFilterExpr(nil, labels.MatchEqual, "bar"),
						newLabelParserExpr(OpParserTypeJSON, ""),
						&LabelFilterExpr{
							LabelFilterer: log.NewOrLabelFilter(
								log.NewDurationLabelFilter(log.LabelFilterGreaterThanOrEqual, "latency", 250*time.Millisecond),
								log.NewAndLabelFilter(
									log.NewNumericLabelFilter(log.LabelFilterLesserThan, "status_code", 500.0),
									log.NewNumericLabelFilter(log.LabelFilterGreaterThan, "status_code", 200.0),
								),
							),
						},
						newLineFmtExpr("blip{{ .foo }}blop {{.status_code}}"),
						newLabelFmtExpr([]log.LabelFmt{
							log.NewRenameLabelFmt("foo", "bar"),
							log.NewTemplateLabelFmt("status_code", "buzz{{.bar}}"),
						}),
					},
				},
					5*time.Minute,
					newUnwrapExpr("foo", ""),
					nil),
				OpRangeTypeQuantile, &grouping{without: false, groups: []string{"namespace", "instance"}}, NewStringLabelFilter("0.99998"),
			),
		},
		{
			in: `quantile_over_time(0.99998,{app="foo"} |= "bar" | json | latency >= 250ms or ( status_code < 500 and status_code > 200)
			| line_format "blip{{ .foo }}blop {{.status_code}}" | label_format foo=bar,status_code="buzz{{.bar}}" | unwrap foo | __error__ !~".+"[5m]) by (namespace,instance)`,
			exp: newRangeAggregationExpr(
				newLogRange(&PipelineExpr{
					left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
					pipeline: MultiStageExpr{
						newLineFilterExpr(nil, labels.MatchEqual, "bar"),
						newLabelParserExpr(OpParserTypeJSON, ""),
						&LabelFilterExpr{
							LabelFilterer: log.NewOrLabelFilter(
								log.NewDurationLabelFilter(log.LabelFilterGreaterThanOrEqual, "latency", 250*time.Millisecond),
								log.NewAndLabelFilter(
									log.NewNumericLabelFilter(log.LabelFilterLesserThan, "status_code", 500.0),
									log.NewNumericLabelFilter(log.LabelFilterGreaterThan, "status_code", 200.0),
								),
							),
						},
						newLineFmtExpr("blip{{ .foo }}blop {{.status_code}}"),
						newLabelFmtExpr([]log.LabelFmt{
							log.NewRenameLabelFmt("foo", "bar"),
							log.NewTemplateLabelFmt("status_code", "buzz{{.bar}}"),
						}),
					},
				},
					5*time.Minute,
					newUnwrapExpr("foo", "").addPostFilter(log.NewStringLabelFilter(mustNewMatcher(labels.MatchNotRegexp, logqlmodel.ErrorLabel, ".+"))),
					nil),
				OpRangeTypeQuantile, &grouping{without: false, groups: []string{"namespace", "instance"}}, NewStringLabelFilter("0.99998"),
			),
		},
		{
			in: `sum without (foo) (
				quantile_over_time(0.99998,{app="foo"} |= "bar" | json | latency >= 250ms or ( status_code < 500 and status_code > 200)
					| line_format "blip{{ .foo }}blop {{.status_code}}" | label_format foo=bar,status_code="buzz{{.bar}}" | unwrap foo [5m]
								) by (namespace,instance)
					)`,
			exp: mustNewVectorAggregationExpr(
				newRangeAggregationExpr(
					newLogRange(&PipelineExpr{
						left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
						pipeline: MultiStageExpr{
							newLineFilterExpr(nil, labels.MatchEqual, "bar"),
							newLabelParserExpr(OpParserTypeJSON, ""),
							&LabelFilterExpr{
								LabelFilterer: log.NewOrLabelFilter(
									log.NewDurationLabelFilter(log.LabelFilterGreaterThanOrEqual, "latency", 250*time.Millisecond),
									log.NewAndLabelFilter(
										log.NewNumericLabelFilter(log.LabelFilterLesserThan, "status_code", 500.0),
										log.NewNumericLabelFilter(log.LabelFilterGreaterThan, "status_code", 200.0),
									),
								),
							},
							newLineFmtExpr("blip{{ .foo }}blop {{.status_code}}"),
							newLabelFmtExpr([]log.LabelFmt{
								log.NewRenameLabelFmt("foo", "bar"),
								log.NewTemplateLabelFmt("status_code", "buzz{{.bar}}"),
							}),
						},
					},
						5*time.Minute,
						newUnwrapExpr("foo", ""),
						nil),
					OpRangeTypeQuantile, &grouping{without: false, groups: []string{"namespace", "instance"}}, NewStringLabelFilter("0.99998"),
				),
				OpTypeSum,
				&grouping{without: true, groups: []string{"foo"}},
				nil,
			),
		},
		{
			in: `sum without (foo) (
				quantile_over_time(0.99998,{app="foo"} |= "bar" | json | latency >= 250ms or ( status_code < 500 and status_code > 200)
					| line_format "blip{{ .foo }}blop {{.status_code}}" | label_format foo=bar,status_code="buzz{{.bar}}" | unwrap foo [5m] offset 5m
								) by (namespace,instance)
					)`,
			exp: mustNewVectorAggregationExpr(
				newRangeAggregationExpr(
					newLogRange(&PipelineExpr{
						left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
						pipeline: MultiStageExpr{
							newLineFilterExpr(nil, labels.MatchEqual, "bar"),
							newLabelParserExpr(OpParserTypeJSON, ""),
							&LabelFilterExpr{
								LabelFilterer: log.NewOrLabelFilter(
									log.NewDurationLabelFilter(log.LabelFilterGreaterThanOrEqual, "latency", 250*time.Millisecond),
									log.NewAndLabelFilter(
										log.NewNumericLabelFilter(log.LabelFilterLesserThan, "status_code", 500.0),
										log.NewNumericLabelFilter(log.LabelFilterGreaterThan, "status_code", 200.0),
									),
								),
							},
							newLineFmtExpr("blip{{ .foo }}blop {{.status_code}}"),
							newLabelFmtExpr([]log.LabelFmt{
								log.NewRenameLabelFmt("foo", "bar"),
								log.NewTemplateLabelFmt("status_code", "buzz{{.bar}}"),
							}),
						},
					},
						5*time.Minute,
						newUnwrapExpr("foo", ""),
						newOffsetExpr(5*time.Minute)),
					OpRangeTypeQuantile, &grouping{without: false, groups: []string{"namespace", "instance"}}, NewStringLabelFilter("0.99998"),
				),
				OpTypeSum,
				&grouping{without: true, groups: []string{"foo"}},
				nil,
			),
		},
		{
			in: `sum without (foo) (
			quantile_over_time(0.99998,{app="foo"} |= "bar" | json | latency >= 250ms or ( status_code < 500 and status_code > 200)
				| line_format "blip{{ .foo }}blop {{.status_code}}" | label_format foo=bar,status_code="buzz{{.bar}}" | unwrap duration(foo) [5m]
							) by (namespace,instance)
				)`,
			exp: mustNewVectorAggregationExpr(
				newRangeAggregationExpr(
					newLogRange(&PipelineExpr{
						left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
						pipeline: MultiStageExpr{
							newLineFilterExpr(nil, labels.MatchEqual, "bar"),
							newLabelParserExpr(OpParserTypeJSON, ""),
							&LabelFilterExpr{
								LabelFilterer: log.NewOrLabelFilter(
									log.NewDurationLabelFilter(log.LabelFilterGreaterThanOrEqual, "latency", 250*time.Millisecond),
									log.NewAndLabelFilter(
										log.NewNumericLabelFilter(log.LabelFilterLesserThan, "status_code", 500.0),
										log.NewNumericLabelFilter(log.LabelFilterGreaterThan, "status_code", 200.0),
									),
								),
							},
							newLineFmtExpr("blip{{ .foo }}blop {{.status_code}}"),
							newLabelFmtExpr([]log.LabelFmt{
								log.NewRenameLabelFmt("foo", "bar"),
								log.NewTemplateLabelFmt("status_code", "buzz{{.bar}}"),
							}),
						},
					},
						5*time.Minute,
						newUnwrapExpr("foo", OpConvDuration),
						nil),
					OpRangeTypeQuantile, &grouping{without: false, groups: []string{"namespace", "instance"}}, NewStringLabelFilter("0.99998"),
				),
				OpTypeSum,
				&grouping{without: true, groups: []string{"foo"}},
				nil,
			),
		},
		{
			in: `sum without (foo) (
			quantile_over_time(.99998,{app="foo"} |= "bar" | json | latency >= 250ms or ( status_code < 500 and status_code > 200)
				| line_format "blip{{ .foo }}blop {{.status_code}}" | label_format foo=bar,status_code="buzz{{.bar}}" | unwrap duration(foo) [5m]
							) by (namespace,instance)
				)`,
			exp: mustNewVectorAggregationExpr(
				newRangeAggregationExpr(
					newLogRange(&PipelineExpr{
						left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
						pipeline: MultiStageExpr{
							newLineFilterExpr(nil, labels.MatchEqual, "bar"),
							newLabelParserExpr(OpParserTypeJSON, ""),
							&LabelFilterExpr{
								LabelFilterer: log.NewOrLabelFilter(
									log.NewDurationLabelFilter(log.LabelFilterGreaterThanOrEqual, "latency", 250*time.Millisecond),
									log.NewAndLabelFilter(
										log.NewNumericLabelFilter(log.LabelFilterLesserThan, "status_code", 500.0),
										log.NewNumericLabelFilter(log.LabelFilterGreaterThan, "status_code", 200.0),
									),
								),
							},
							newLineFmtExpr("blip{{ .foo }}blop {{.status_code}}"),
							newLabelFmtExpr([]log.LabelFmt{
								log.NewRenameLabelFmt("foo", "bar"),
								log.NewTemplateLabelFmt("status_code", "buzz{{.bar}}"),
							}),
						},
					},
						5*time.Minute,
						newUnwrapExpr("foo", OpConvDuration),
						nil),
					OpRangeTypeQuantile, &grouping{without: false, groups: []string{"namespace", "instance"}}, NewStringLabelFilter(".99998"),
				),
				OpTypeSum,
				&grouping{without: true, groups: []string{"foo"}},
				nil,
			),
		},
		{
			in: `sum without (foo) (
			quantile_over_time(.99998,{app="foo"} |= "bar" | json | latency >= 250ms or ( status_code < 500 and status_code > 200)
				| line_format "blip{{ .foo }}blop {{.status_code}}" | label_format foo=bar,status_code="buzz{{.bar}}" | unwrap duration_seconds(foo) [5m]
							) by (namespace,instance)
				)`,
			exp: mustNewVectorAggregationExpr(
				newRangeAggregationExpr(
					newLogRange(&PipelineExpr{
						left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
						pipeline: MultiStageExpr{
							newLineFilterExpr(nil, labels.MatchEqual, "bar"),
							newLabelParserExpr(OpParserTypeJSON, ""),
							&LabelFilterExpr{
								LabelFilterer: log.NewOrLabelFilter(
									log.NewDurationLabelFilter(log.LabelFilterGreaterThanOrEqual, "latency", 250*time.Millisecond),
									log.NewAndLabelFilter(
										log.NewNumericLabelFilter(log.LabelFilterLesserThan, "status_code", 500.0),
										log.NewNumericLabelFilter(log.LabelFilterGreaterThan, "status_code", 200.0),
									),
								),
							},
							newLineFmtExpr("blip{{ .foo }}blop {{.status_code}}"),
							newLabelFmtExpr([]log.LabelFmt{
								log.NewRenameLabelFmt("foo", "bar"),
								log.NewTemplateLabelFmt("status_code", "buzz{{.bar}}"),
							}),
						},
					},
						5*time.Minute,
						newUnwrapExpr("foo", OpConvDurationSeconds),
						nil),
					OpRangeTypeQuantile, &grouping{without: false, groups: []string{"namespace", "instance"}}, NewStringLabelFilter(".99998"),
				),
				OpTypeSum,
				&grouping{without: true, groups: []string{"foo"}},
				nil,
			),
		},
		{
			in: `topk(10,
				quantile_over_time(0.99998,{app="foo"} |= "bar" | json | latency >= 250ms or ( status_code < 500 and status_code > 200)
					| line_format "blip{{ .foo }}blop {{.status_code}}" | label_format foo=bar,status_code="buzz{{.bar}}" | unwrap foo [5m]
								) by (namespace,instance)
					)`,
			exp: mustNewVectorAggregationExpr(
				newRangeAggregationExpr(
					newLogRange(&PipelineExpr{
						left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
						pipeline: MultiStageExpr{
							newLineFilterExpr(nil, labels.MatchEqual, "bar"),
							newLabelParserExpr(OpParserTypeJSON, ""),
							&LabelFilterExpr{
								LabelFilterer: log.NewOrLabelFilter(
									log.NewDurationLabelFilter(log.LabelFilterGreaterThanOrEqual, "latency", 250*time.Millisecond),
									log.NewAndLabelFilter(
										log.NewNumericLabelFilter(log.LabelFilterLesserThan, "status_code", 500.0),
										log.NewNumericLabelFilter(log.LabelFilterGreaterThan, "status_code", 200.0),
									),
								),
							},
							newLineFmtExpr("blip{{ .foo }}blop {{.status_code}}"),
							newLabelFmtExpr([]log.LabelFmt{
								log.NewRenameLabelFmt("foo", "bar"),
								log.NewTemplateLabelFmt("status_code", "buzz{{.bar}}"),
							}),
						},
					},
						5*time.Minute,
						newUnwrapExpr("foo", ""),
						nil),
					OpRangeTypeQuantile, &grouping{without: false, groups: []string{"namespace", "instance"}}, NewStringLabelFilter("0.99998"),
				),
				OpTypeTopK,
				nil,
				NewStringLabelFilter("10"),
			),
		},
		{
			in: `
			sum by (foo,bar) (
				quantile_over_time(0.99998,{app="foo"} |= "bar" | json | latency >= 250ms or ( status_code < 500 and status_code > 200)
					| line_format "blip{{ .foo }}blop {{.status_code}}" | label_format foo=bar,status_code="buzz{{.bar}}" | unwrap foo [5m]
								) by (namespace,instance)
					)
					+
					avg(
						avg_over_time({app="foo"} |= "bar" | json | latency >= 250ms or ( status_code < 500 and status_code > 200)
							| line_format "blip{{ .foo }}blop {{.status_code}}" | label_format foo=bar,status_code="buzz{{.bar}}" | unwrap foo [5m]
										) by (namespace,instance)
							) by (foo,bar)
					`,
			exp: mustNewBinOpExpr(OpTypeAdd, BinOpOptions{ReturnBool: false},
				mustNewVectorAggregationExpr(
					newRangeAggregationExpr(
						newLogRange(&PipelineExpr{
							left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
							pipeline: MultiStageExpr{
								newLineFilterExpr(nil, labels.MatchEqual, "bar"),
								newLabelParserExpr(OpParserTypeJSON, ""),
								&LabelFilterExpr{
									LabelFilterer: log.NewOrLabelFilter(
										log.NewDurationLabelFilter(log.LabelFilterGreaterThanOrEqual, "latency", 250*time.Millisecond),
										log.NewAndLabelFilter(
											log.NewNumericLabelFilter(log.LabelFilterLesserThan, "status_code", 500.0),
											log.NewNumericLabelFilter(log.LabelFilterGreaterThan, "status_code", 200.0),
										),
									),
								},
								newLineFmtExpr("blip{{ .foo }}blop {{.status_code}}"),
								newLabelFmtExpr([]log.LabelFmt{
									log.NewRenameLabelFmt("foo", "bar"),
									log.NewTemplateLabelFmt("status_code", "buzz{{.bar}}"),
								}),
							},
						},
							5*time.Minute,
							newUnwrapExpr("foo", ""),
							nil),
						OpRangeTypeQuantile, &grouping{without: false, groups: []string{"namespace", "instance"}}, NewStringLabelFilter("0.99998"),
					),
					OpTypeSum,
					&grouping{groups: []string{"foo", "bar"}},
					nil,
				),
				mustNewVectorAggregationExpr(
					newRangeAggregationExpr(
						newLogRange(&PipelineExpr{
							left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
							pipeline: MultiStageExpr{
								newLineFilterExpr(nil, labels.MatchEqual, "bar"),
								newLabelParserExpr(OpParserTypeJSON, ""),
								&LabelFilterExpr{
									LabelFilterer: log.NewOrLabelFilter(
										log.NewDurationLabelFilter(log.LabelFilterGreaterThanOrEqual, "latency", 250*time.Millisecond),
										log.NewAndLabelFilter(
											log.NewNumericLabelFilter(log.LabelFilterLesserThan, "status_code", 500.0),
											log.NewNumericLabelFilter(log.LabelFilterGreaterThan, "status_code", 200.0),
										),
									),
								},
								newLineFmtExpr("blip{{ .foo }}blop {{.status_code}}"),
								newLabelFmtExpr([]log.LabelFmt{
									log.NewRenameLabelFmt("foo", "bar"),
									log.NewTemplateLabelFmt("status_code", "buzz{{.bar}}"),
								}),
							},
						},
							5*time.Minute,
							newUnwrapExpr("foo", ""),
							nil),
						OpRangeTypeAvg, &grouping{without: false, groups: []string{"namespace", "instance"}}, nil,
					),
					OpTypeAvg,
					&grouping{groups: []string{"foo", "bar"}},
					nil,
				),
			),
		},
		{
			in: `
			label_replace(
				sum by (foo,bar) (
					quantile_over_time(0.99998,{app="foo"} |= "bar" | json | latency >= 250ms or ( status_code < 500 and status_code > 200)
						| line_format "blip{{ .foo }}blop {{.status_code}}" | label_format foo=bar,status_code="buzz{{.bar}}" | unwrap foo [5m]
									) by (namespace,instance)
						)
						+
						avg(
							avg_over_time({app="foo"} |= "bar" | json | latency >= 250ms or ( status_code < 500 and status_code > 200)
								| line_format "blip{{ .foo }}blop {{.status_code}}" | label_format foo=bar,status_code="buzz{{.bar}}" | unwrap foo [5m]
											) by (namespace,instance)
								) by (foo,bar),
				"foo",
				"$1",
				"svc",
				"(.*)"
				)`,
			exp: mustNewLabelReplaceExpr(
				mustNewBinOpExpr(OpTypeAdd, BinOpOptions{ReturnBool: false},
					mustNewVectorAggregationExpr(
						newRangeAggregationExpr(
							newLogRange(&PipelineExpr{
								left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
								pipeline: MultiStageExpr{
									newLineFilterExpr(nil, labels.MatchEqual, "bar"),
									newLabelParserExpr(OpParserTypeJSON, ""),
									&LabelFilterExpr{
										LabelFilterer: log.NewOrLabelFilter(
											log.NewDurationLabelFilter(log.LabelFilterGreaterThanOrEqual, "latency", 250*time.Millisecond),
											log.NewAndLabelFilter(
												log.NewNumericLabelFilter(log.LabelFilterLesserThan, "status_code", 500.0),
												log.NewNumericLabelFilter(log.LabelFilterGreaterThan, "status_code", 200.0),
											),
										),
									},
									newLineFmtExpr("blip{{ .foo }}blop {{.status_code}}"),
									newLabelFmtExpr([]log.LabelFmt{
										log.NewRenameLabelFmt("foo", "bar"),
										log.NewTemplateLabelFmt("status_code", "buzz{{.bar}}"),
									}),
								},
							},
								5*time.Minute,
								newUnwrapExpr("foo", ""),
								nil),
							OpRangeTypeQuantile, &grouping{without: false, groups: []string{"namespace", "instance"}}, NewStringLabelFilter("0.99998"),
						),
						OpTypeSum,
						&grouping{groups: []string{"foo", "bar"}},
						nil,
					),
					mustNewVectorAggregationExpr(
						newRangeAggregationExpr(
							newLogRange(&PipelineExpr{
								left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
								pipeline: MultiStageExpr{
									newLineFilterExpr(nil, labels.MatchEqual, "bar"),
									newLabelParserExpr(OpParserTypeJSON, ""),
									&LabelFilterExpr{
										LabelFilterer: log.NewOrLabelFilter(
											log.NewDurationLabelFilter(log.LabelFilterGreaterThanOrEqual, "latency", 250*time.Millisecond),
											log.NewAndLabelFilter(
												log.NewNumericLabelFilter(log.LabelFilterLesserThan, "status_code", 500.0),
												log.NewNumericLabelFilter(log.LabelFilterGreaterThan, "status_code", 200.0),
											),
										),
									},
									newLineFmtExpr("blip{{ .foo }}blop {{.status_code}}"),
									newLabelFmtExpr([]log.LabelFmt{
										log.NewRenameLabelFmt("foo", "bar"),
										log.NewTemplateLabelFmt("status_code", "buzz{{.bar}}"),
									}),
								},
							},
								5*time.Minute,
								newUnwrapExpr("foo", ""),
								nil),
							OpRangeTypeAvg, &grouping{without: false, groups: []string{"namespace", "instance"}}, nil,
						),
						OpTypeAvg,
						&grouping{groups: []string{"foo", "bar"}},
						nil,
					),
				),
				"foo", "$1", "svc", "(.*)",
			),
		},
		{
			// ensure binary ops with two literals are reduced recursively
			in:  `1 + 1 + 1`,
			exp: &LiteralExpr{value: 3},
		},
		{
			// ensure binary ops with two literals are reduced when comparisons are used
			in:  `1 == 1`,
			exp: &LiteralExpr{value: 1},
		},
		{
			// ensure binary ops with two literals are reduced when comparisons are used
			in:  `1 != 1`,
			exp: &LiteralExpr{value: 0},
		},
		{
			// ensure binary ops with two literals are reduced when comparisons are used
			in:  `1 > 1`,
			exp: &LiteralExpr{value: 0},
		},
		{
			// ensure binary ops with two literals are reduced when comparisons are used
			in:  `1 >= 1`,
			exp: &LiteralExpr{value: 1},
		},
		{
			// ensure binary ops with two literals are reduced when comparisons are used
			in:  `1 < 1`,
			exp: &LiteralExpr{value: 0},
		},
		{
			// ensure binary ops with two literals are reduced when comparisons are used
			in:  `1 <= 1`,
			exp: &LiteralExpr{value: 1},
		},
		{
			// ensure binary ops with two literals are reduced recursively when comparisons are used
			in:  `1 >= 1 > 1`,
			exp: &LiteralExpr{value: 0},
		},
		{
			in:  `{foo="bar"} + {foo="bar"}`,
			err: logqlmodel.NewParseError(`unexpected type for left leg of binary operation (+): *logql.MatchersExpr`, 0, 0),
		},
		{
			in:  `sum(count_over_time({foo="bar"}[5m])) by (foo) - {foo="bar"}`,
			err: logqlmodel.NewParseError(`unexpected type for right leg of binary operation (-): *logql.MatchersExpr`, 0, 0),
		},
		{
			in:  `{foo="bar"} / sum(count_over_time({foo="bar"}[5m])) by (foo)`,
			err: logqlmodel.NewParseError(`unexpected type for left leg of binary operation (/): *logql.MatchersExpr`, 0, 0),
		},
		{
			in:  `sum(count_over_time({foo="bar"}[5m])) by (foo) or 1`,
			err: logqlmodel.NewParseError(`unexpected literal for right leg of logical/set binary operation (or): 1.000000`, 0, 0),
		},
		{
			in:  `1 unless sum(count_over_time({foo="bar"}[5m])) by (foo)`,
			err: logqlmodel.NewParseError(`unexpected literal for left leg of logical/set binary operation (unless): 1.000000`, 0, 0),
		},
		{
			in:  `sum(count_over_time({foo="bar"}[5m])) by (foo) + 1 or 1`,
			err: logqlmodel.NewParseError(`unexpected literal for right leg of logical/set binary operation (or): 1.000000`, 0, 0),
		},
		{
			in: `count_over_time({ foo ="bar" }[12m]) > count_over_time({ foo = "bar" }[12m])`,
			exp: &BinOpExpr{
				op: OpTypeGT,
				SampleExpr: &RangeAggregationExpr{
					left: &LogRange{
						left:     &MatchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
						interval: 12 * time.Minute,
					},
					operation: "count_over_time",
				},
				RHS: &RangeAggregationExpr{
					left: &LogRange{
						left:     &MatchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
						interval: 12 * time.Minute,
					},
					operation: "count_over_time",
				},
			},
		},
		{
			in: `count_over_time({ foo = "bar" }[12m]) > 1`,
			exp: &BinOpExpr{
				op: OpTypeGT,
				SampleExpr: &RangeAggregationExpr{
					left: &LogRange{
						left:     &MatchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
						interval: 12 * time.Minute,
					},
					operation: "count_over_time",
				},
				RHS: &LiteralExpr{value: 1},
			},
		},
		{
			// cannot compare metric & log queries
			in:  `count_over_time({ foo = "bar" }[12m]) > { foo = "bar" }`,
			err: logqlmodel.NewParseError("unexpected type for right leg of binary operation (>): *logql.MatchersExpr", 0, 0),
		},
		{
			in: `count_over_time({ foo = "bar" }[12m]) or count_over_time({ foo = "bar" }[12m]) > 1`,
			exp: &BinOpExpr{
				op: OpTypeOr,
				SampleExpr: &RangeAggregationExpr{
					left: &LogRange{
						left:     &MatchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
						interval: 12 * time.Minute,
					},
					operation: "count_over_time",
				},
				RHS: &BinOpExpr{
					op: OpTypeGT,
					SampleExpr: &RangeAggregationExpr{
						left: &LogRange{
							left:     &MatchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
							interval: 12 * time.Minute,
						},
						operation: "count_over_time",
					},
					RHS: &LiteralExpr{value: 1},
				},
			},
		},
		{
			// test associativity
			in:  `1 > 1 < 1`,
			exp: &LiteralExpr{value: 1},
		},
		{
			// bool modifiers are reduced-away between two literal legs
			in:  `1 > 1 > bool 1`,
			exp: &LiteralExpr{value: 0},
		},
		{
			// cannot lead with bool modifier
			in:  `bool 1 > 1 > bool 1`,
			err: logqlmodel.NewParseError("syntax error: unexpected bool", 1, 1),
		},
		{
			in:  `sum_over_time({namespace="tns"} |= "level=error" | json |foo>=5,bar<25ms| unwrap latency [5m]) by (foo)`,
			err: logqlmodel.NewParseError("grouping not allowed for sum_over_time aggregation", 0, 0),
		},
		{
			in:  `sum_over_time(50,{namespace="tns"} |= "level=error" | json |foo>=5,bar<25ms| unwrap latency [5m])`,
			err: logqlmodel.NewParseError("parameter 50 not supported for operation sum_over_time", 0, 0),
		},
		{
			in:  `quantile_over_time({namespace="tns"} |= "level=error" | json |foo>=5,bar<25ms| unwrap latency [5m])`,
			err: logqlmodel.NewParseError("parameter required for operation quantile_over_time", 0, 0),
		},
		{
			in:  `quantile_over_time(foo,{namespace="tns"} |= "level=error" | json |foo>=5,bar<25ms| unwrap latency [5m])`,
			err: logqlmodel.NewParseError("syntax error: unexpected IDENTIFIER, expecting NUMBER or { or (", 1, 20),
		},
		{
			in: `{app="foo"}
					# |= "bar"
					| json`,
			exp: &PipelineExpr{
				left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
				pipeline: MultiStageExpr{
					newLabelParserExpr(OpParserTypeJSON, ""),
				},
			},
		},
		{
			in: `{app="foo"}
					#
					|= "bar"
					| json`,
			exp: &PipelineExpr{
				left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
				pipeline: MultiStageExpr{
					newLineFilterExpr(nil, labels.MatchEqual, "bar"),
					newLabelParserExpr(OpParserTypeJSON, ""),
				},
			},
		},
		{
			in:  `{app="foo"} # |= "bar" | json`,
			exp: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
		},
		{
			in: `{app="foo"} | json #`,
			exp: &PipelineExpr{
				left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
				pipeline: MultiStageExpr{
					newLabelParserExpr(OpParserTypeJSON, ""),
				},
			},
		},
		{
			in:  `#{app="foo"} | json`,
			err: logqlmodel.NewParseError("syntax error: unexpected $end", 1, 20),
		},
		{
			in:  `{app="#"}`,
			exp: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "#"}}),
		},
		{
			in: `{app="foo"} |= "#"`,
			exp: &PipelineExpr{
				left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
				pipeline: MultiStageExpr{
					newLineFilterExpr(nil, labels.MatchEqual, "#"),
				},
			},
		},
		{
			in: `{app="foo"} | bar="#"`,
			exp: &PipelineExpr{
				left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
				pipeline: MultiStageExpr{
					&LabelFilterExpr{
						LabelFilterer: log.NewStringLabelFilter(mustNewMatcher(labels.MatchEqual, "bar", "#")),
					},
				},
			},
		},
		{
			in: `{app="foo"} | json bob="top.sub[\"index\"]"`,
			exp: &PipelineExpr{
				left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
				pipeline: MultiStageExpr{
					newJSONExpressionParser([]log.JSONExpression{
						log.NewJSONExpr("bob", `top.sub["index"]`),
					}),
				},
			},
		},
		{
			in: `{app="foo"} | json bob="top.params[0]"`,
			exp: &PipelineExpr{
				left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
				pipeline: MultiStageExpr{
					newJSONExpressionParser([]log.JSONExpression{
						log.NewJSONExpr("bob", `top.params[0]`),
					}),
				},
			},
		},
		{
			in: `{app="foo"} | json response_code="response.code", api_key="request.headers[\"X-API-KEY\"]"`,
			exp: &PipelineExpr{
				left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
				pipeline: MultiStageExpr{
					newJSONExpressionParser([]log.JSONExpression{
						log.NewJSONExpr("response_code", `response.code`),
						log.NewJSONExpr("api_key", `request.headers["X-API-KEY"]`),
					}),
				},
			},
		},
	} {
		t.Run(tc.in, func(t *testing.T) {
			ast, err := ParseExpr(tc.in)
			require.Equal(t, tc.err, err)
			require.Equal(t, tc.exp, ast)
		})
	}
}

func TestParseMatchers(t *testing.T) {
	tests := []struct {
		input   string
		want    []*labels.Matcher
		wantErr bool
	}{
		{
			`{app="foo",cluster=~".+bar"}`,
			[]*labels.Matcher{
				mustNewMatcher(labels.MatchEqual, "app", "foo"),
				mustNewMatcher(labels.MatchRegexp, "cluster", ".+bar"),
			},
			false,
		},
		{
			`{app!="foo",cluster=~".+bar",bar!~".?boo"}`,
			[]*labels.Matcher{
				mustNewMatcher(labels.MatchNotEqual, "app", "foo"),
				mustNewMatcher(labels.MatchRegexp, "cluster", ".+bar"),
				mustNewMatcher(labels.MatchNotRegexp, "bar", ".?boo"),
			},
			false,
		},
		{
			`{app!="foo",cluster=~".+bar",bar!~".?boo"`,
			nil,
			true,
		},
		{
			`{app!="foo",cluster=~".+bar",bar!~".?boo"} |= "test"`,
			nil,
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseMatchers(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseMatchers() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseMatchers() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsParseError(t *testing.T) {
	tests := []struct {
		name  string
		errFn func() error
		want  bool
	}{
		{
			"bad query",
			func() error {
				_, err := ParseExpr(`{foo`)
				return err
			},
			true,
		},
		{
			"other error",
			func() error {
				return errors.New("")
			},
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := errors.Is(tt.errFn(), logqlmodel.ErrParse); got != tt.want {
				t.Errorf("IsParseError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_PipelineCombined(t *testing.T) {
	query := `{job="cortex-ops/query-frontend"} |= "logging.go" | logfmt | line_format "{{.msg}}" | regexp "(?P<method>\\w+) (?P<path>[\\w|/]+) \\((?P<status>\\d+?)\\) (?P<duration>.*)" | (duration > 1s or status==200) and method="POST" | line_format "{{.duration}}|{{.method}}|{{.status}}"`

	expr, err := ParseLogSelector(query, true)
	require.Nil(t, err)

	p, err := expr.Pipeline()
	require.Nil(t, err)
	sp := p.ForStream(labels.Labels{})
	line, lbs, ok := sp.Process([]byte(`level=debug ts=2020-10-02T10:10:42.092268913Z caller=logging.go:66 traceID=a9d4d8a928d8db1 msg="POST /api/prom/api/v1/query_range (200) 1.5s"`))
	require.True(t, ok)
	require.Equal(
		t,
		labels.Labels{labels.Label{Name: "caller", Value: "logging.go:66"}, labels.Label{Name: "duration", Value: "1.5s"}, labels.Label{Name: "level", Value: "debug"}, labels.Label{Name: "method", Value: "POST"}, labels.Label{Name: "msg", Value: "POST /api/prom/api/v1/query_range (200) 1.5s"}, labels.Label{Name: "path", Value: "/api/prom/api/v1/query_range"}, labels.Label{Name: "status", Value: "200"}, labels.Label{Name: "traceID", Value: "a9d4d8a928d8db1"}, labels.Label{Name: "ts", Value: "2020-10-02T10:10:42.092268913Z"}},
		lbs.Labels(),
	)
	require.Equal(t, string([]byte(`1.5s|POST|200`)), string(line))
}

var c []*labels.Matcher

func Benchmark_ParseMatchers(b *testing.B) {
	s := `{cpu="10",endpoint="https",instance="10.253.57.87:9100",job="node-exporter",mode="idle",namespace="observability",pod="node-exporter-l454v",service="node-exporter"}`
	var err error
	for n := 0; n < b.N; n++ {
		c, err = ParseMatchers(s)
		require.NoError(b, err)
	}
}

var lbs labels.Labels

func Benchmark_CompareParseLabels(b *testing.B) {
	s := `{cpu="10",endpoint="https",instance="10.253.57.87:9100",job="node-exporter",mode="idle",namespace="observability",pod="node-exporter-l454v",service="node-exporter"}`
	var err error
	b.Run("logql", func(b *testing.B) {
		for n := 0; n < b.N; n++ {
			c, err = ParseMatchers(s)
			require.NoError(b, err)
		}
	})
	b.Run("promql", func(b *testing.B) {
		for n := 0; n < b.N; n++ {
			lbs, err = ParseLabels(s)
			require.NoError(b, err)
		}
	})
}

func TestParseSampleExpr_equalityMatcher(t *testing.T) {
	for _, tc := range []struct {
		in  string
		err error
	}{
		{
			in: `count_over_time({foo="bar"}[5m])`,
		},
		{
			in:  `count_over_time({foo!="bar"}[5m])`,
			err: logqlmodel.NewParseError(errAtleastOneEqualityMatcherRequired, 0, 0),
		},
		{
			in: `count_over_time({app="baz", foo!="bar"}[5m])`,
		},
		{
			in: `count_over_time({app=~".+"}[5m])`,
		},
		{
			in:  `count_over_time({app=~".*"}[5m])`,
			err: logqlmodel.NewParseError(errAtleastOneEqualityMatcherRequired, 0, 0),
		},
		{
			in: `count_over_time({app=~"bar|baz"}[5m])`,
		},
		{
			in:  `count_over_time({app!~"bar|baz"}[5m])`,
			err: logqlmodel.NewParseError(errAtleastOneEqualityMatcherRequired, 0, 0),
		},
		{
			in:  `1 + count_over_time({app=~".*"}[5m])`,
			err: logqlmodel.NewParseError(errAtleastOneEqualityMatcherRequired, 0, 0),
		},
		{
			in:  `1 + count_over_time({app=~".+"}[5m]) + count_over_time({app=~".*"}[5m])`,
			err: logqlmodel.NewParseError(errAtleastOneEqualityMatcherRequired, 0, 0),
		},
		{
			in: `1 + count_over_time({app=~".+"}[5m]) + count_over_time({app=~".+"}[5m])`,
		},
		{
			in:  `1 + count_over_time({app=~".+"}[5m]) + count_over_time({app=~".*"}[5m]) + 1`,
			err: logqlmodel.NewParseError(errAtleastOneEqualityMatcherRequired, 0, 0),
		},
		{
			in: `1 + count_over_time({app=~".+"}[5m]) + count_over_time({app=~".+"}[5m]) + 1`,
		},
	} {
		t.Run(tc.in, func(t *testing.T) {
			_, err := ParseSampleExpr(tc.in)
			require.Equal(t, tc.err, err)
		})
	}
}

func TestParseLogSelectorExpr_equalityMatcher(t *testing.T) {
	for _, tc := range []struct {
		in  string
		err error
	}{
		{
			in: `{foo="bar"}`,
		},
		{
			in:  `{foo!="bar"}`,
			err: logqlmodel.NewParseError(errAtleastOneEqualityMatcherRequired, 0, 0),
		},
		{
			in: `{app="baz", foo!="bar"}`,
		},
		{
			in: `{app=~".+"}`,
		},
		{
			in:  `{app=~".*"}`,
			err: logqlmodel.NewParseError(errAtleastOneEqualityMatcherRequired, 0, 0),
		},
		{
			in: `{foo=~"bar|baz"}`,
		},
		{
			in:  `{foo!~"bar|baz"}`,
			err: logqlmodel.NewParseError(errAtleastOneEqualityMatcherRequired, 0, 0),
		},
	} {
		t.Run(tc.in, func(t *testing.T) {
			_, err := ParseLogSelector(tc.in, true)
			require.Equal(t, tc.err, err)
		})
	}
}

func Test_match(t *testing.T) {
	tests := []struct {
		name    string
		input   []string
		want    [][]*labels.Matcher
		wantErr bool
	}{
		{"malformed", []string{`{a="1`}, nil, true},
		{"empty on nil input", nil, [][]*labels.Matcher{}, false},
		{"empty on empty input", []string{}, [][]*labels.Matcher{}, false},
		{
			"single",
			[]string{`{a="1"}`},
			[][]*labels.Matcher{
				{mustMatcher(labels.MatchEqual, "a", "1")},
			},
			false,
		},
		{
			"multiple groups",
			[]string{`{a="1"}`, `{b="2", c=~"3", d!="4"}`},
			[][]*labels.Matcher{
				{mustMatcher(labels.MatchEqual, "a", "1")},
				{
					mustMatcher(labels.MatchEqual, "b", "2"),
					mustMatcher(labels.MatchRegexp, "c", "3"),
					mustMatcher(labels.MatchNotEqual, "d", "4"),
				},
			},
			false,
		},
		{
			"errors on empty group",
			[]string{`{}`},
			nil,
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Match(tt.input)
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.Equal(t, tt.want, got)
			}
		})
	}
}

func mustMatcher(t labels.MatchType, n string, v string) *labels.Matcher {
	m, err := labels.NewMatcher(t, n, v)
	if err != nil {
		panic(err)
	}
	return m
}
