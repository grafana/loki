package logql

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logql/log"
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
			exp: &rangeAggregationExpr{
				operation: "count_over_time",
				left: &logRange{
					left: &pipelineExpr{
						pipeline: MultiStageExpr{
							newLineFilterExpr(nil, labels.MatchRegexp, "error\\"),
						},
						left: &matchersExpr{
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
			exp: &rangeAggregationExpr{
				operation: "count_over_time",
				left: &logRange{
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
			exp: &rangeAggregationExpr{
				operation: "count_over_time",
				left: &logRange{
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
			exp: &matchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
		},
		{
			in:  `{ foo = "bar" }`,
			exp: &matchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
		},
		{
			in:  `{ foo != "bar" }`,
			exp: &matchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchNotEqual, "foo", "bar")}},
		},
		{
			in:  `{ foo =~ "bar" }`,
			exp: &matchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchRegexp, "foo", "bar")}},
		},
		{
			in:  `{ foo !~ "bar" }`,
			exp: &matchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchNotRegexp, "foo", "bar")}},
		},
		{
			in: `count_over_time({ foo !~ "bar" }[12m])`,
			exp: &rangeAggregationExpr{
				left: &logRange{
					left:     &matchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchNotRegexp, "foo", "bar")}},
					interval: 12 * time.Minute,
				},
				operation: "count_over_time",
			},
		},
		{
			in: `bytes_over_time({ foo !~ "bar" }[12m])`,
			exp: &rangeAggregationExpr{
				left: &logRange{
					left:     &matchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchNotRegexp, "foo", "bar")}},
					interval: 12 * time.Minute,
				},
				operation: OpRangeTypeBytes,
			},
		},
		{
			in: `bytes_rate({ foo !~ "bar" }[12m])`,
			exp: &rangeAggregationExpr{
				left: &logRange{
					left:     &matchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchNotRegexp, "foo", "bar")}},
					interval: 12 * time.Minute,
				},
				operation: OpRangeTypeBytesRate,
			},
		},
		{
			in: `rate({ foo !~ "bar" }[5h])`,
			exp: &rangeAggregationExpr{
				left: &logRange{
					left:     &matchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchNotRegexp, "foo", "bar")}},
					interval: 5 * time.Hour,
				},
				operation: "rate",
			},
		},
		{
			in: `rate({ foo !~ "bar" }[5d])`,
			exp: &rangeAggregationExpr{
				left: &logRange{
					left:     &matchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchNotRegexp, "foo", "bar")}},
					interval: 5 * 24 * time.Hour,
				},
				operation: "rate",
			},
		},
		{
			in: `count_over_time({ foo !~ "bar" }[1w])`,
			exp: &rangeAggregationExpr{
				left: &logRange{
					left:     &matchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchNotRegexp, "foo", "bar")}},
					interval: 7 * 24 * time.Hour,
				},
				operation: "count_over_time",
			},
		},
		{
			in: `absent_over_time({ foo !~ "bar" }[1w])`,
			exp: &rangeAggregationExpr{
				left: &logRange{
					left:     &matchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchNotRegexp, "foo", "bar")}},
					interval: 7 * 24 * time.Hour,
				},
				operation: OpRangeTypeAbsent,
			},
		},
		{
			in: `sum(rate({ foo !~ "bar" }[5h]))`,
			exp: mustNewVectorAggregationExpr(&rangeAggregationExpr{
				left: &logRange{
					left:     &matchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchNotRegexp, "foo", "bar")}},
					interval: 5 * time.Hour,
				},
				operation: "rate",
			}, "sum", nil, nil),
		},
		{
			in: `sum(rate({ foo !~ "bar" }[1y]))`,
			exp: mustNewVectorAggregationExpr(&rangeAggregationExpr{
				left: &logRange{
					left:     &matchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchNotRegexp, "foo", "bar")}},
					interval: 365 * 24 * time.Hour,
				},
				operation: "rate",
			}, "sum", nil, nil),
		},
		{
			in: `avg(count_over_time({ foo !~ "bar" }[5h])) by (bar,foo)`,
			exp: mustNewVectorAggregationExpr(&rangeAggregationExpr{
				left: &logRange{
					left:     &matchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchNotRegexp, "foo", "bar")}},
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
						count_over_time({ foo !~ "bar" }[5h]),
						"bar",
						"$1$2",
						"foo",
						"(.*).(.*)"
					)
				) by (bar,foo)`,
			exp: mustNewVectorAggregationExpr(
				mustNewLabelReplaceExpr(
					&rangeAggregationExpr{
						left: &logRange{
							left:     &matchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchNotRegexp, "foo", "bar")}},
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
			in: `avg(count_over_time({ foo !~ "bar" }[5h])) by ()`,
			exp: mustNewVectorAggregationExpr(&rangeAggregationExpr{
				left: &logRange{
					left:     &matchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchNotRegexp, "foo", "bar")}},
					interval: 5 * time.Hour,
				},
				operation: "count_over_time",
			}, "avg", &grouping{
				without: false,
				groups:  nil,
			}, nil),
		},
		{
			in: `max without (bar) (count_over_time({ foo !~ "bar" }[5h]))`,
			exp: mustNewVectorAggregationExpr(&rangeAggregationExpr{
				left: &logRange{
					left:     &matchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchNotRegexp, "foo", "bar")}},
					interval: 5 * time.Hour,
				},
				operation: "count_over_time",
			}, "max", &grouping{
				without: true,
				groups:  []string{"bar"},
			}, nil),
		},
		{
			in: `max without () (count_over_time({ foo !~ "bar" }[5h]))`,
			exp: mustNewVectorAggregationExpr(&rangeAggregationExpr{
				left: &logRange{
					left:     &matchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchNotRegexp, "foo", "bar")}},
					interval: 5 * time.Hour,
				},
				operation: "count_over_time",
			}, "max", &grouping{
				without: true,
				groups:  nil,
			}, nil),
		},
		{
			in: `topk(10,count_over_time({ foo !~ "bar" }[5h])) without (bar)`,
			exp: mustNewVectorAggregationExpr(&rangeAggregationExpr{
				left: &logRange{
					left:     &matchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchNotRegexp, "foo", "bar")}},
					interval: 5 * time.Hour,
				},
				operation: "count_over_time",
			}, "topk", &grouping{
				without: true,
				groups:  []string{"bar"},
			}, NewStringLabelFilter("10")),
		},
		{
			in: `bottomk(30 ,sum(rate({ foo !~ "bar" }[5h])) by (foo))`,
			exp: mustNewVectorAggregationExpr(mustNewVectorAggregationExpr(&rangeAggregationExpr{
				left: &logRange{
					left:     &matchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchNotRegexp, "foo", "bar")}},
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
			in: `max( sum(count_over_time({ foo !~ "bar" }[5h])) without (foo,bar) ) by (foo)`,
			exp: mustNewVectorAggregationExpr(mustNewVectorAggregationExpr(&rangeAggregationExpr{
				left: &logRange{
					left:     &matchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchNotRegexp, "foo", "bar")}},
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
			in: `unk({ foo !~ "bar" }[5m])`,
			err: ParseError{
				msg:  "syntax error: unexpected IDENTIFIER",
				line: 1,
				col:  1,
			},
		},
		{
			in: `absent_over_time({ foo !~ "bar" }[5h]) by (foo)`,
			err: ParseError{
				msg:  "grouping not allowed for absent_over_time aggregation",
				line: 0,
				col:  0,
			},
		},
		{
			in: `rate({ foo !~ "bar" }[5minutes])`,
			err: ParseError{
				msg:  `not a valid duration string: "5minutes"`,
				line: 0,
				col:  22,
			},
		},
		{
			in: `label_replace(rate({ foo !~ "bar" }[5m]),"")`,
			err: ParseError{
				msg:  `syntax error: unexpected ), expecting ,`,
				line: 1,
				col:  44,
			},
		},
		{
			in: `label_replace(rate({ foo !~ "bar" }[5m]),"foo","$1","bar","^^^^x43\\q")`,
			err: ParseError{
				msg:  "invalid regex in label_replace: error parsing regexp: invalid escape sequence: `\\q`",
				line: 0,
				col:  0,
			},
		},
		{
			in: `rate({ foo !~ "bar" }[5)`,
			err: ParseError{
				msg:  "missing closing ']' in duration",
				line: 0,
				col:  22,
			},
		},
		{
			in: `min({ foo !~ "bar" }[5m])`,
			err: ParseError{
				msg:  "syntax error: unexpected RANGE",
				line: 0,
				col:  21,
			},
		},
		{
			in: `sum(3 ,count_over_time({ foo !~ "bar" }[5h]))`,
			err: ParseError{
				msg:  "unsupported parameter for operation sum(3,",
				line: 0,
				col:  0,
			},
		},
		{
			in: `topk(count_over_time({ foo !~ "bar" }[5h]))`,
			err: ParseError{
				msg:  "parameter required for operation topk",
				line: 0,
				col:  0,
			},
		},
		{
			in: `bottomk(he,count_over_time({ foo !~ "bar" }[5h]))`,
			err: ParseError{
				msg:  "syntax error: unexpected IDENTIFIER",
				line: 1,
				col:  9,
			},
		},
		{
			in: `bottomk(1.2,count_over_time({ foo !~ "bar" }[5h]))`,
			err: ParseError{
				msg:  "invalid parameter bottomk(1.2,",
				line: 0,
				col:  0,
			},
		},
		{
			in: `stddev({ foo !~ "bar" })`,
			err: ParseError{
				msg:  "syntax error: unexpected )",
				line: 1,
				col:  24,
			},
		},
		{
			in: `{ foo = "bar", bar != "baz" }`,
			exp: &matchersExpr{matchers: []*labels.Matcher{
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
				&logRange{
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
				&logRange{
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
					&logRange{
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
				&logRange{
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
				&logRange{
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
				&logRange{
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
						&logRange{
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
				&logRange{
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
				&logRange{
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
				&logRange{
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
						&logRange{
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
			in: `{foo="bar}`,
			err: ParseError{
				msg:  "literal not terminated",
				line: 1,
				col:  6,
			},
		},
		{
			in: `{foo="bar"`,
			err: ParseError{
				msg:  "syntax error: unexpected $end, expecting } or ,",
				line: 1,
				col:  11,
			},
		},

		{
			in: `{foo="bar"} |~`,
			err: ParseError{
				msg:  "syntax error: unexpected $end, expecting STRING",
				line: 1,
				col:  15,
			},
		},

		{
			in: `{foo="bar"} "foo"`,
			err: ParseError{
				msg:  "syntax error: unexpected STRING",
				line: 1,
				col:  13,
			},
		},
		{
			in: `{foo="bar"} foo`,
			err: ParseError{
				msg:  "syntax error: unexpected IDENTIFIER",
				line: 1,
				col:  13,
			},
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
						&logRange{
							left: &matchersExpr{
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
						&logRange{
							left: &matchersExpr{
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
					&logRange{
						left: &matchersExpr{
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
						&logRange{
							left: &matchersExpr{
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
						&logRange{
							left: &matchersExpr{
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
					&logRange{
						left: &matchersExpr{
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
					&logRange{
						left: &matchersExpr{
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
						&logRange{
							left: &matchersExpr{
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
						&logRange{
							left: &matchersExpr{
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
						&logRange{
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
						&logRange{
							left: &matchersExpr{
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
						&logRange{
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
						&logRange{
							left: &matchersExpr{
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
						&logRange{
							left: &matchersExpr{
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
				&literalExpr{value: 0.5},
			),
		},
		{
			// test signs
			in: `1 + -2 / 1`,
			exp: mustNewBinOpExpr(
				OpTypeAdd,
				BinOpOptions{},
				&literalExpr{value: 1},
				mustNewBinOpExpr(OpTypeDiv, BinOpOptions{}, &literalExpr{value: -2}, &literalExpr{value: 1}),
			),
		},
		{
			// test signs/ops with equal associativity
			in: `1 + 1 - -1`,
			exp: mustNewBinOpExpr(
				OpTypeSub,
				BinOpOptions{},
				mustNewBinOpExpr(OpTypeAdd, BinOpOptions{}, &literalExpr{value: 1}, &literalExpr{value: 1}),
				&literalExpr{value: -1},
			),
		},
		{
			in: `{app="foo"} |= "bar" | json | latency >= 250ms or ( status_code < 500 and status_code > 200)`,
			exp: &pipelineExpr{
				left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
				pipeline: MultiStageExpr{
					newLineFilterExpr(nil, labels.MatchEqual, "bar"),
					newLabelParserExpr(OpParserTypeJSON, ""),
					&labelFilterExpr{
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
			exp: &pipelineExpr{
				left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
				pipeline: MultiStageExpr{
					newLineFilterExpr(nil, labels.MatchEqual, "bar"),
					newLabelParserExpr(OpParserTypeJSON, ""),
					&labelFilterExpr{
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
			exp: &pipelineExpr{
				left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
				pipeline: MultiStageExpr{
					newLineFilterExpr(nil, labels.MatchEqual, "bar"),
					newLabelParserExpr(OpParserTypeJSON, ""),
					&labelFilterExpr{
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
			exp: &pipelineExpr{
				left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
				pipeline: MultiStageExpr{
					newLineFilterExpr(nil, labels.MatchEqual, "bar"),
					newLabelParserExpr(OpParserTypeJSON, ""),
					&labelFilterExpr{
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
			exp: &pipelineExpr{
				left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
				pipeline: MultiStageExpr{
					newLineFilterExpr(nil, labels.MatchEqual, "bar"),
					newLabelParserExpr(OpParserTypeJSON, ""),
					&labelFilterExpr{
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
			exp: &pipelineExpr{
				left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
				pipeline: MultiStageExpr{
					newLineFilterExpr(nil, labels.MatchEqual, "bar"),
					newLabelParserExpr(OpParserTypeJSON, ""),
					&labelFilterExpr{
						LabelFilterer: log.NewOrLabelFilter(
							log.NewDurationLabelFilter(log.LabelFilterGreaterThanOrEqual, "latency", 250*time.Millisecond),
							log.NewAndLabelFilter(
								log.NewNumericLabelFilter(log.LabelFilterLesserThan, "status_code", 500.0),
								log.NewNumericLabelFilter(log.LabelFilterGreaterThan, "status_code", 200.0),
							),
						),
					},
					&labelFilterExpr{
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
			exp: &pipelineExpr{
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
			exp: &pipelineExpr{
				left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
				pipeline: MultiStageExpr{
					newLineFilterExpr(nil, labels.MatchEqual, "bar"),
					newLabelParserExpr(OpParserTypeJSON, ""),
					&labelFilterExpr{
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
			exp: &pipelineExpr{
				left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
				pipeline: MultiStageExpr{
					newLineFilterExpr(nil, labels.MatchEqual, "bar"),
					newLabelParserExpr(OpParserTypeJSON, ""),
					&labelFilterExpr{
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
				newLogRange(&pipelineExpr{
					left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
					pipeline: MultiStageExpr{
						newLineFilterExpr(nil, labels.MatchEqual, "bar"),
						newLabelParserExpr(OpParserTypeJSON, ""),
						&labelFilterExpr{
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
					nil),
				OpRangeTypeCount,
				nil, nil,
			),
		},
		{
			in: `sum_over_time({app="foo"} |= "bar" | json | latency >= 250ms or ( status_code < 500 and status_code > 200)
			| line_format "blip{{ .foo }}blop {{.status_code}}" | label_format foo=bar,status_code="buzz{{.bar}}"[5m])`,
			exp: nil,
			err: ParseError{msg: "invalid aggregation sum_over_time without unwrap"},
		},
		{
			in:  `count_over_time({app="foo"} |= "foo" | json | unwrap foo [5m])`,
			exp: nil,
			err: ParseError{msg: "invalid aggregation count_over_time with unwrap"},
		},
		{
			in: `{app="foo"} |= "bar" | json |  status_code < 500 or status_code > 200 and size >= 2.5KiB `,
			exp: &pipelineExpr{
				left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
				pipeline: MultiStageExpr{
					newLineFilterExpr(nil, labels.MatchEqual, "bar"),
					newLabelParserExpr(OpParserTypeJSON, ""),
					&labelFilterExpr{
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
				newLogRange(&pipelineExpr{
					left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
					pipeline: MultiStageExpr{
						newLineFilterExpr(nil, labels.MatchEqual, "bar"),
						newLabelParserExpr(OpParserTypeJSON, ""),
						&labelFilterExpr{
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
					newUnwrapExpr("foo", "")),
				OpRangeTypeStdvar, nil, nil,
			),
		}, {
			in: `stdvar_over_time({app="foo"} |= "bar" | json | latency >= 250ms or ( status_code < 500 and status_code > 200)
			| line_format "blip{{ .foo }}blop {{.status_code}}" | label_format foo=bar,status_code="buzz{{.bar}}" | unwrap duration(foo) [5m])`,
			exp: newRangeAggregationExpr(
				newLogRange(&pipelineExpr{
					left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
					pipeline: MultiStageExpr{
						newLineFilterExpr(nil, labels.MatchEqual, "bar"),
						newLabelParserExpr(OpParserTypeJSON, ""),
						&labelFilterExpr{
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
					newUnwrapExpr("foo", OpConvDuration)),
				OpRangeTypeStdvar, nil, nil,
			),
		},
		{
			in: `sum_over_time({namespace="tns"} |= "level=error" | json |foo>=5,bar<25ms| unwrap bytes(foo) [5m])`,
			exp: newRangeAggregationExpr(
				newLogRange(&pipelineExpr{
					left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "namespace", Value: "tns"}}),
					pipeline: MultiStageExpr{
						newLineFilterExpr(nil, labels.MatchEqual, "level=error"),
						newLabelParserExpr(OpParserTypeJSON, ""),
						&labelFilterExpr{
							LabelFilterer: log.NewAndLabelFilter(
								log.NewNumericLabelFilter(log.LabelFilterGreaterThanOrEqual, "foo", 5),
								log.NewDurationLabelFilter(log.LabelFilterLesserThan, "bar", 25*time.Millisecond),
							),
						},
					},
				},
					5*time.Minute,
					newUnwrapExpr("foo", OpConvBytes)),
				OpRangeTypeSum, nil, nil,
			),
		},
		{
			in: `sum_over_time({namespace="tns"} |= "level=error" | json |foo>=5,bar<25ms| unwrap latency [5m])`,
			exp: newRangeAggregationExpr(
				newLogRange(&pipelineExpr{
					left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "namespace", Value: "tns"}}),
					pipeline: MultiStageExpr{
						newLineFilterExpr(nil, labels.MatchEqual, "level=error"),
						newLabelParserExpr(OpParserTypeJSON, ""),
						&labelFilterExpr{
							LabelFilterer: log.NewAndLabelFilter(
								log.NewNumericLabelFilter(log.LabelFilterGreaterThanOrEqual, "foo", 5),
								log.NewDurationLabelFilter(log.LabelFilterLesserThan, "bar", 25*time.Millisecond),
							),
						},
					},
				},
					5*time.Minute,
					newUnwrapExpr("latency", "")),
				OpRangeTypeSum, nil, nil,
			),
		},
		{
			in: `sum_over_time({namespace="tns"} |= "level=error" | json |foo==5,bar<25ms| unwrap latency [5m])`,
			exp: newRangeAggregationExpr(
				newLogRange(&pipelineExpr{
					left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "namespace", Value: "tns"}}),
					pipeline: MultiStageExpr{
						newLineFilterExpr(nil, labels.MatchEqual, "level=error"),
						newLabelParserExpr(OpParserTypeJSON, ""),
						&labelFilterExpr{
							LabelFilterer: log.NewAndLabelFilter(
								log.NewNumericLabelFilter(log.LabelFilterEqual, "foo", 5),
								log.NewDurationLabelFilter(log.LabelFilterLesserThan, "bar", 25*time.Millisecond),
							),
						},
					},
				},
					5*time.Minute,
					newUnwrapExpr("latency", "")),
				OpRangeTypeSum, nil, nil,
			),
		},
		{
			in: `stddev_over_time({app="foo"} |= "bar" | unwrap bar [5m])`,
			exp: newRangeAggregationExpr(
				newLogRange(&pipelineExpr{
					left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
					pipeline: MultiStageExpr{
						newLineFilterExpr(nil, labels.MatchEqual, "bar"),
					},
				},
					5*time.Minute,
					newUnwrapExpr("bar", "")),
				OpRangeTypeStddev, nil, nil,
			),
		},
		{
			in: `min_over_time({app="foo"} | unwrap bar [5m])`,
			exp: newRangeAggregationExpr(
				newLogRange(
					newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
					5*time.Minute,
					newUnwrapExpr("bar", "")),
				OpRangeTypeMin, nil, nil,
			),
		},
		{
			in: `min_over_time({app="foo"} | unwrap bar [5m]) by ()`,
			exp: newRangeAggregationExpr(
				newLogRange(
					newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
					5*time.Minute,
					newUnwrapExpr("bar", "")),
				OpRangeTypeMin, &grouping{}, nil,
			),
		},
		{
			in: `max_over_time({app="foo"} | unwrap bar [5m]) without ()`,
			exp: newRangeAggregationExpr(
				newLogRange(
					newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
					5*time.Minute,
					newUnwrapExpr("bar", "")),
				OpRangeTypeMax, &grouping{without: true}, nil,
			),
		},
		{
			in: `max_over_time({app="foo"} | unwrap bar [5m]) without (foo,bar)`,
			exp: newRangeAggregationExpr(
				newLogRange(
					newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
					5*time.Minute,
					newUnwrapExpr("bar", "")),
				OpRangeTypeMax, &grouping{without: true, groups: []string{"foo", "bar"}}, nil,
			),
		},
		{
			in: `max_over_time(({app="foo"} |= "bar" | json | latency >= 250ms or ( status_code < 500 and status_code > 200)
			| line_format "blip{{ .foo }}blop {{.status_code}}" | label_format foo=bar,status_code="buzz{{.bar}}" | unwrap foo )[5m])`,
			exp: newRangeAggregationExpr(
				newLogRange(&pipelineExpr{
					left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
					pipeline: MultiStageExpr{
						newLineFilterExpr(nil, labels.MatchEqual, "bar"),
						newLabelParserExpr(OpParserTypeJSON, ""),
						&labelFilterExpr{
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
					newUnwrapExpr("foo", "")),
				OpRangeTypeMax, nil, nil,
			),
		},
		{
			in: `quantile_over_time(0.99998,{app="foo"} |= "bar" | json | latency >= 250ms or ( status_code < 500 and status_code > 200)
			| line_format "blip{{ .foo }}blop {{.status_code}}" | label_format foo=bar,status_code="buzz{{.bar}}" | unwrap foo [5m])`,
			exp: newRangeAggregationExpr(
				newLogRange(&pipelineExpr{
					left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
					pipeline: MultiStageExpr{
						newLineFilterExpr(nil, labels.MatchEqual, "bar"),
						newLabelParserExpr(OpParserTypeJSON, ""),
						&labelFilterExpr{
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
					newUnwrapExpr("foo", "")),
				OpRangeTypeQuantile, nil, NewStringLabelFilter("0.99998"),
			),
		},
		{
			in: `quantile_over_time(0.99998,{app="foo"} |= "bar" | json | latency >= 250ms or ( status_code < 500 and status_code > 200)
			| line_format "blip{{ .foo }}blop {{.status_code}}" | label_format foo=bar,status_code="buzz{{.bar}}" | unwrap foo [5m]) by (namespace,instance)`,
			exp: newRangeAggregationExpr(
				newLogRange(&pipelineExpr{
					left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
					pipeline: MultiStageExpr{
						newLineFilterExpr(nil, labels.MatchEqual, "bar"),
						newLabelParserExpr(OpParserTypeJSON, ""),
						&labelFilterExpr{
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
					newUnwrapExpr("foo", "")),
				OpRangeTypeQuantile, &grouping{without: false, groups: []string{"namespace", "instance"}}, NewStringLabelFilter("0.99998"),
			),
		},
		{
			in: `quantile_over_time(0.99998,{app="foo"} |= "bar" | json | latency >= 250ms or ( status_code < 500 and status_code > 200)
			| line_format "blip{{ .foo }}blop {{.status_code}}" | label_format foo=bar,status_code="buzz{{.bar}}" | unwrap foo | __error__ !~".+"[5m]) by (namespace,instance)`,
			exp: newRangeAggregationExpr(
				newLogRange(&pipelineExpr{
					left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
					pipeline: MultiStageExpr{
						newLineFilterExpr(nil, labels.MatchEqual, "bar"),
						newLabelParserExpr(OpParserTypeJSON, ""),
						&labelFilterExpr{
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
					newUnwrapExpr("foo", "").addPostFilter(log.NewStringLabelFilter(mustNewMatcher(labels.MatchNotRegexp, log.ErrorLabel, ".+")))),
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
					newLogRange(&pipelineExpr{
						left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
						pipeline: MultiStageExpr{
							newLineFilterExpr(nil, labels.MatchEqual, "bar"),
							newLabelParserExpr(OpParserTypeJSON, ""),
							&labelFilterExpr{
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
						newUnwrapExpr("foo", "")),
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
					newLogRange(&pipelineExpr{
						left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
						pipeline: MultiStageExpr{
							newLineFilterExpr(nil, labels.MatchEqual, "bar"),
							newLabelParserExpr(OpParserTypeJSON, ""),
							&labelFilterExpr{
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
						newUnwrapExpr("foo", OpConvDuration)),
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
					newLogRange(&pipelineExpr{
						left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
						pipeline: MultiStageExpr{
							newLineFilterExpr(nil, labels.MatchEqual, "bar"),
							newLabelParserExpr(OpParserTypeJSON, ""),
							&labelFilterExpr{
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
						newUnwrapExpr("foo", OpConvDuration)),
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
					newLogRange(&pipelineExpr{
						left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
						pipeline: MultiStageExpr{
							newLineFilterExpr(nil, labels.MatchEqual, "bar"),
							newLabelParserExpr(OpParserTypeJSON, ""),
							&labelFilterExpr{
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
						newUnwrapExpr("foo", OpConvDurationSeconds)),
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
					newLogRange(&pipelineExpr{
						left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
						pipeline: MultiStageExpr{
							newLineFilterExpr(nil, labels.MatchEqual, "bar"),
							newLabelParserExpr(OpParserTypeJSON, ""),
							&labelFilterExpr{
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
						newUnwrapExpr("foo", "")),
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
						newLogRange(&pipelineExpr{
							left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
							pipeline: MultiStageExpr{
								newLineFilterExpr(nil, labels.MatchEqual, "bar"),
								newLabelParserExpr(OpParserTypeJSON, ""),
								&labelFilterExpr{
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
							newUnwrapExpr("foo", "")),
						OpRangeTypeQuantile, &grouping{without: false, groups: []string{"namespace", "instance"}}, NewStringLabelFilter("0.99998"),
					),
					OpTypeSum,
					&grouping{groups: []string{"foo", "bar"}},
					nil,
				),
				mustNewVectorAggregationExpr(
					newRangeAggregationExpr(
						newLogRange(&pipelineExpr{
							left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
							pipeline: MultiStageExpr{
								newLineFilterExpr(nil, labels.MatchEqual, "bar"),
								newLabelParserExpr(OpParserTypeJSON, ""),
								&labelFilterExpr{
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
							newUnwrapExpr("foo", "")),
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
							newLogRange(&pipelineExpr{
								left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
								pipeline: MultiStageExpr{
									newLineFilterExpr(nil, labels.MatchEqual, "bar"),
									newLabelParserExpr(OpParserTypeJSON, ""),
									&labelFilterExpr{
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
								newUnwrapExpr("foo", "")),
							OpRangeTypeQuantile, &grouping{without: false, groups: []string{"namespace", "instance"}}, NewStringLabelFilter("0.99998"),
						),
						OpTypeSum,
						&grouping{groups: []string{"foo", "bar"}},
						nil,
					),
					mustNewVectorAggregationExpr(
						newRangeAggregationExpr(
							newLogRange(&pipelineExpr{
								left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
								pipeline: MultiStageExpr{
									newLineFilterExpr(nil, labels.MatchEqual, "bar"),
									newLabelParserExpr(OpParserTypeJSON, ""),
									&labelFilterExpr{
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
								newUnwrapExpr("foo", "")),
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
			exp: &literalExpr{value: 3},
		},
		{
			// ensure binary ops with two literals are reduced when comparisons are used
			in:  `1 == 1`,
			exp: &literalExpr{value: 1},
		},
		{
			// ensure binary ops with two literals are reduced when comparisons are used
			in:  `1 != 1`,
			exp: &literalExpr{value: 0},
		},
		{
			// ensure binary ops with two literals are reduced when comparisons are used
			in:  `1 > 1`,
			exp: &literalExpr{value: 0},
		},
		{
			// ensure binary ops with two literals are reduced when comparisons are used
			in:  `1 >= 1`,
			exp: &literalExpr{value: 1},
		},
		{
			// ensure binary ops with two literals are reduced when comparisons are used
			in:  `1 < 1`,
			exp: &literalExpr{value: 0},
		},
		{
			// ensure binary ops with two literals are reduced when comparisons are used
			in:  `1 <= 1`,
			exp: &literalExpr{value: 1},
		},
		{
			// ensure binary ops with two literals are reduced recursively when comparisons are used
			in:  `1 >= 1 > 1`,
			exp: &literalExpr{value: 0},
		},
		{
			in: `{foo="bar"} + {foo="bar"}`,
			err: ParseError{
				msg:  `unexpected type for left leg of binary operation (+): *logql.matchersExpr`,
				line: 0,
				col:  0,
			},
		},
		{
			in: `sum(count_over_time({foo="bar"}[5m])) by (foo) - {foo="bar"}`,
			err: ParseError{
				msg:  `unexpected type for right leg of binary operation (-): *logql.matchersExpr`,
				line: 0,
				col:  0,
			},
		},
		{
			in: `{foo="bar"} / sum(count_over_time({foo="bar"}[5m])) by (foo)`,
			err: ParseError{
				msg:  `unexpected type for left leg of binary operation (/): *logql.matchersExpr`,
				line: 0,
				col:  0,
			},
		},
		{
			in: `sum(count_over_time({foo="bar"}[5m])) by (foo) or 1`,
			err: ParseError{
				msg:  `unexpected literal for right leg of logical/set binary operation (or): 1.000000`,
				line: 0,
				col:  0,
			},
		},
		{
			in: `1 unless sum(count_over_time({foo="bar"}[5m])) by (foo)`,
			err: ParseError{
				msg:  `unexpected literal for left leg of logical/set binary operation (unless): 1.000000`,
				line: 0,
				col:  0,
			},
		},
		{
			in: `sum(count_over_time({foo="bar"}[5m])) by (foo) + 1 or 1`,
			err: ParseError{
				msg:  `unexpected literal for right leg of logical/set binary operation (or): 1.000000`,
				line: 0,
				col:  0,
			},
		},
		{
			in: `count_over_time({ foo != "bar" }[12m]) > count_over_time({ foo = "bar" }[12m])`,
			exp: &binOpExpr{
				op: OpTypeGT,
				SampleExpr: &rangeAggregationExpr{
					left: &logRange{
						left:     &matchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchNotEqual, "foo", "bar")}},
						interval: 12 * time.Minute,
					},
					operation: "count_over_time",
				},
				RHS: &rangeAggregationExpr{
					left: &logRange{
						left:     &matchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
						interval: 12 * time.Minute,
					},
					operation: "count_over_time",
				},
			},
		},
		{
			in: `count_over_time({ foo != "bar" }[12m]) > 1`,
			exp: &binOpExpr{
				op: OpTypeGT,
				SampleExpr: &rangeAggregationExpr{
					left: &logRange{
						left:     &matchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchNotEqual, "foo", "bar")}},
						interval: 12 * time.Minute,
					},
					operation: "count_over_time",
				},
				RHS: &literalExpr{value: 1},
			},
		},
		{
			// cannot compare metric & log queries
			in: `count_over_time({ foo != "bar" }[12m]) > { foo = "bar" }`,
			err: ParseError{
				msg: "unexpected type for right leg of binary operation (>): *logql.matchersExpr",
			},
		},
		{
			in: `count_over_time({ foo != "bar" }[12m]) or count_over_time({ foo = "bar" }[12m]) > 1`,
			exp: &binOpExpr{
				op: OpTypeOr,
				SampleExpr: &rangeAggregationExpr{
					left: &logRange{
						left:     &matchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchNotEqual, "foo", "bar")}},
						interval: 12 * time.Minute,
					},
					operation: "count_over_time",
				},
				RHS: &binOpExpr{
					op: OpTypeGT,
					SampleExpr: &rangeAggregationExpr{
						left: &logRange{
							left:     &matchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
							interval: 12 * time.Minute,
						},
						operation: "count_over_time",
					},
					RHS: &literalExpr{value: 1},
				},
			},
		},
		{
			// test associativity
			in:  `1 > 1 < 1`,
			exp: &literalExpr{value: 1},
		},
		{
			// bool modifiers are reduced-away between two literal legs
			in:  `1 > 1 > bool 1`,
			exp: &literalExpr{value: 0},
		},
		{
			// cannot lead with bool modifier
			in: `bool 1 > 1 > bool 1`,
			err: ParseError{
				msg:  "syntax error: unexpected bool",
				line: 1,
				col:  1,
			},
		},
		{
			in:  `sum_over_time({namespace="tns"} |= "level=error" | json |foo>=5,bar<25ms| unwrap latency [5m]) by (foo)`,
			err: ParseError{msg: "grouping not allowed for sum_over_time aggregation"},
		},
		{
			in:  `sum_over_time(50,{namespace="tns"} |= "level=error" | json |foo>=5,bar<25ms| unwrap latency [5m])`,
			err: ParseError{msg: "parameter 50 not supported for operation sum_over_time"},
		},
		{
			in:  `quantile_over_time({namespace="tns"} |= "level=error" | json |foo>=5,bar<25ms| unwrap latency [5m])`,
			err: ParseError{msg: "parameter required for operation quantile_over_time"},
		},
		{
			in:  `quantile_over_time(foo,{namespace="tns"} |= "level=error" | json |foo>=5,bar<25ms| unwrap latency [5m])`,
			err: ParseError{msg: "syntax error: unexpected IDENTIFIER, expecting NUMBER or { or (", line: 1, col: 20},
		},
		{
			in: `{app="foo"}
					# |= "bar"
					| json`,
			exp: &pipelineExpr{
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
			exp: &pipelineExpr{
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
			exp: &pipelineExpr{
				left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
				pipeline: MultiStageExpr{
					newLabelParserExpr(OpParserTypeJSON, ""),
				},
			},
		},
		{
			in:  `#{app="foo"} | json`,
			err: ParseError{msg: "syntax error: unexpected $end", line: 1, col: 20},
		},
		{
			in:  `{app="#"}`,
			exp: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "#"}}),
		},
		{
			in: `{app="foo"} |= "#"`,
			exp: &pipelineExpr{
				left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
				pipeline: MultiStageExpr{
					newLineFilterExpr(nil, labels.MatchEqual, "#"),
				},
			},
		},
		{
			in: `{app="foo"} | bar="#"`,
			exp: &pipelineExpr{
				left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
				pipeline: MultiStageExpr{
					&labelFilterExpr{
						LabelFilterer: log.NewStringLabelFilter(mustNewMatcher(labels.MatchEqual, "bar", "#")),
					},
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
			if got := errors.Is(tt.errFn(), ErrParse); got != tt.want {
				t.Errorf("IsParseError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_PipelineCombined(t *testing.T) {
	query := `{job="cortex-ops/query-frontend"} |= "logging.go" | logfmt | line_format "{{.msg}}" | regexp "(?P<method>\\w+) (?P<path>[\\w|/]+) \\((?P<status>\\d+?)\\) (?P<duration>.*)" | (duration > 1s or status==200) and method="POST" | line_format "{{.duration}}|{{.method}}|{{.status}}"`

	expr, err := ParseLogSelector(query)
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
