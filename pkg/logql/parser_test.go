package logql

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/require"
)

func newString(s string) *string {
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
					left: &filterExpr{
						ty:    labels.MatchRegexp,
						match: "error\\",
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
					left: &filterExpr{
						ty:    labels.MatchEqual,
						match: "error",
						left: &matchersExpr{
							matchers: []*labels.Matcher{
								mustNewMatcher(labels.MatchEqual, "foo", "bar"),
							},
						},
					},
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
					left: &filterExpr{
						ty:    labels.MatchEqual,
						match: "error",
						left: &matchersExpr{
							matchers: []*labels.Matcher{
								mustNewMatcher(labels.MatchEqual, "foo", "bar"),
							},
						},
					},
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
			}, newString("10")),
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
				newString("30")),
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
			in: `rate({ foo !~ "bar" }[5minutes])`,
			err: ParseError{
				msg:  `not a valid duration string: "5minutes"`,
				line: 0,
				col:  22,
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
				msg:  "syntax error: unexpected DURATION",
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
			exp: &filterExpr{
				left:  &matchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
				ty:    labels.MatchEqual,
				match: "baz",
			},
		},
		{
			in: `{foo="bar"} |= "baz" |~ "blip" != "flip" !~ "flap"`,
			exp: &filterExpr{
				left: &filterExpr{
					left: &filterExpr{
						left: &filterExpr{
							left:  &matchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
							ty:    labels.MatchEqual,
							match: "baz",
						},
						ty:    labels.MatchRegexp,
						match: "blip",
					},
					ty:    labels.MatchNotEqual,
					match: "flip",
				},
				ty:    labels.MatchNotRegexp,
				match: "flap",
			},
		},
		{
			in: `count_over_time(({foo="bar"} |= "baz" |~ "blip" != "flip" !~ "flap")[5m])`,
			exp: newRangeAggregationExpr(
				&logRange{
					left: &filterExpr{
						left: &filterExpr{
							left: &filterExpr{
								left: &filterExpr{
									left:  &matchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
									ty:    labels.MatchEqual,
									match: "baz",
								},
								ty:    labels.MatchRegexp,
								match: "blip",
							},
							ty:    labels.MatchNotEqual,
							match: "flip",
						},
						ty:    labels.MatchNotRegexp,
						match: "flap",
					},
					interval: 5 * time.Minute,
				}, OpRangeTypeCount),
		},
		{
			in: `bytes_over_time(({foo="bar"} |= "baz" |~ "blip" != "flip" !~ "flap")[5m])`,
			exp: newRangeAggregationExpr(
				&logRange{
					left: &filterExpr{
						left: &filterExpr{
							left: &filterExpr{
								left: &filterExpr{
									left:  &matchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
									ty:    labels.MatchEqual,
									match: "baz",
								},
								ty:    labels.MatchRegexp,
								match: "blip",
							},
							ty:    labels.MatchNotEqual,
							match: "flip",
						},
						ty:    labels.MatchNotRegexp,
						match: "flap",
					},
					interval: 5 * time.Minute,
				}, OpRangeTypeBytes),
		},
		{
			in: `sum(count_over_time(({foo="bar"} |= "baz" |~ "blip" != "flip" !~ "flap")[5m])) by (foo)`,
			exp: mustNewVectorAggregationExpr(newRangeAggregationExpr(
				&logRange{
					left: &filterExpr{
						left: &filterExpr{
							left: &filterExpr{
								left: &filterExpr{
									left:  &matchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
									ty:    labels.MatchEqual,
									match: "baz",
								},
								ty:    labels.MatchRegexp,
								match: "blip",
							},
							ty:    labels.MatchNotEqual,
							match: "flip",
						},
						ty:    labels.MatchNotRegexp,
						match: "flap",
					},
					interval: 5 * time.Minute,
				}, OpRangeTypeCount),
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
					left: &filterExpr{
						left: &filterExpr{
							left: &filterExpr{
								left: &filterExpr{
									left:  &matchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
									ty:    labels.MatchEqual,
									match: "baz",
								},
								ty:    labels.MatchRegexp,
								match: "blip",
							},
							ty:    labels.MatchNotEqual,
							match: "flip",
						},
						ty:    labels.MatchNotRegexp,
						match: "flap",
					},
					interval: 5 * time.Minute,
				}, OpRangeTypeBytesRate),
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
					left: &filterExpr{
						left: &filterExpr{
							left: &filterExpr{
								left: &filterExpr{
									left:  &matchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
									ty:    labels.MatchEqual,
									match: "baz",
								},
								ty:    labels.MatchRegexp,
								match: "blip",
							},
							ty:    labels.MatchNotEqual,
							match: "flip",
						},
						ty:    labels.MatchNotRegexp,
						match: "flap",
					},
					interval: 5 * time.Minute,
				}, OpRangeTypeCount),
				"topk",
				&grouping{
					without: true,
					groups:  []string{"foo"},
				},
				newString("5")),
		},
		{
			in: `topk(5,sum(rate(({foo="bar"} |= "baz" |~ "blip" != "flip" !~ "flap")[5m])) by (app))`,
			exp: mustNewVectorAggregationExpr(
				mustNewVectorAggregationExpr(
					newRangeAggregationExpr(
						&logRange{
							left: &filterExpr{
								left: &filterExpr{
									left: &filterExpr{
										left: &filterExpr{
											left:  &matchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
											ty:    labels.MatchEqual,
											match: "baz",
										},
										ty:    labels.MatchRegexp,
										match: "blip",
									},
									ty:    labels.MatchNotEqual,
									match: "flip",
								},
								ty:    labels.MatchNotRegexp,
								match: "flap",
							},
							interval: 5 * time.Minute,
						}, OpRangeTypeRate),
					"sum",
					&grouping{
						without: false,
						groups:  []string{"app"},
					},
					nil),
				"topk",
				nil,
				newString("5")),
		},
		{
			in: `count_over_time({foo="bar"}[5m] |= "baz" |~ "blip" != "flip" !~ "flap")`,
			exp: newRangeAggregationExpr(
				&logRange{
					left: &filterExpr{
						left: &filterExpr{
							left: &filterExpr{
								left: &filterExpr{
									left:  &matchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
									ty:    labels.MatchEqual,
									match: "baz",
								},
								ty:    labels.MatchRegexp,
								match: "blip",
							},
							ty:    labels.MatchNotEqual,
							match: "flip",
						},
						ty:    labels.MatchNotRegexp,
						match: "flap",
					},
					interval: 5 * time.Minute,
				}, OpRangeTypeCount),
		},
		{
			in: `sum(count_over_time({foo="bar"}[5m] |= "baz" |~ "blip" != "flip" !~ "flap")) by (foo)`,
			exp: mustNewVectorAggregationExpr(newRangeAggregationExpr(
				&logRange{
					left: &filterExpr{
						left: &filterExpr{
							left: &filterExpr{
								left: &filterExpr{
									left:  &matchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
									ty:    labels.MatchEqual,
									match: "baz",
								},
								ty:    labels.MatchRegexp,
								match: "blip",
							},
							ty:    labels.MatchNotEqual,
							match: "flip",
						},
						ty:    labels.MatchNotRegexp,
						match: "flap",
					},
					interval: 5 * time.Minute,
				}, OpRangeTypeCount),
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
					left: &filterExpr{
						left: &filterExpr{
							left: &filterExpr{
								left: &filterExpr{
									left:  &matchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
									ty:    labels.MatchEqual,
									match: "baz",
								},
								ty:    labels.MatchRegexp,
								match: "blip",
							},
							ty:    labels.MatchNotEqual,
							match: "flip",
						},
						ty:    labels.MatchNotRegexp,
						match: "flap",
					},
					interval: 5 * time.Minute,
				}, OpRangeTypeCount),
				"topk",
				&grouping{
					without: true,
					groups:  []string{"foo"},
				},
				newString("5")),
		},
		{
			in: `topk(5,sum(rate({foo="bar"}[5m] |= "baz" |~ "blip" != "flip" !~ "flap")) by (app))`,
			exp: mustNewVectorAggregationExpr(
				mustNewVectorAggregationExpr(
					newRangeAggregationExpr(
						&logRange{
							left: &filterExpr{
								left: &filterExpr{
									left: &filterExpr{
										left: &filterExpr{
											left:  &matchersExpr{matchers: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
											ty:    labels.MatchEqual,
											match: "baz",
										},
										ty:    labels.MatchRegexp,
										match: "blip",
									},
									ty:    labels.MatchNotEqual,
									match: "flip",
								},
								ty:    labels.MatchNotRegexp,
								match: "flap",
							},
							interval: 5 * time.Minute,
						}, OpRangeTypeRate),
					"sum",
					&grouping{
						without: false,
						groups:  []string{"app"},
					},
					nil),
				"topk",
				nil,
				newString("5")),
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
						}, OpRangeTypeCount),
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
						}, OpRangeTypeCount),
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
					}, OpRangeTypeCount),
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
						}, OpRangeTypeCount),
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
						}, OpRangeTypeCount),
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
					}, OpRangeTypeCount),
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
					}, OpRangeTypeCount),
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
						}, OpRangeTypeCount),
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
						}, OpRangeTypeCount),
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
							left: &filterExpr{
								left: &matchersExpr{
									matchers: []*labels.Matcher{
										mustNewMatcher(labels.MatchEqual, "namespace", "tns"),
									},
								},
								match: "level=error",
								ty:    labels.MatchEqual,
							},
							interval: 5 * time.Minute,
						}, OpRangeTypeCount),
					newRangeAggregationExpr(
						&logRange{
							left: &matchersExpr{
								matchers: []*labels.Matcher{
									mustNewMatcher(labels.MatchEqual, "namespace", "tns"),
								},
							},
							interval: 5 * time.Minute,
						}, OpRangeTypeCount)), OpTypeSum, &grouping{groups: []string{"job"}}, nil),
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
							left: &filterExpr{
								left: &matchersExpr{
									matchers: []*labels.Matcher{
										mustNewMatcher(labels.MatchEqual, "namespace", "tns"),
									},
								},
								match: "level=error",
								ty:    labels.MatchEqual,
							},
							interval: 5 * time.Minute,
						}, OpRangeTypeCount),
					newRangeAggregationExpr(
						&logRange{
							left: &matchersExpr{
								matchers: []*labels.Matcher{
									mustNewMatcher(labels.MatchEqual, "namespace", "tns"),
								},
							},
							interval: 5 * time.Minute,
						}, OpRangeTypeCount)), OpTypeSum, &grouping{groups: []string{"job"}}, nil),
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
						}, OpRangeTypeCount),
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
			if got := IsParseError(tt.errFn()); got != tt.want {
				t.Errorf("IsParseError() = %v, want %v", got, tt.want)
			}
		})
	}
}
