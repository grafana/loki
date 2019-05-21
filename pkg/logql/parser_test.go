package logql

import (
	"strings"
	"testing"
	"text/scanner"
	"time"

	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/require"
)

func TestLex(t *testing.T) {
	for _, tc := range []struct {
		input    string
		expected []int
	}{
		{`{foo="bar"}`, []int{OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE}},
		{`{ foo = "bar" }`, []int{OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE}},
		{`{ foo != "bar" }`, []int{OPEN_BRACE, IDENTIFIER, NEQ, STRING, CLOSE_BRACE}},
		{`{ foo =~ "bar" }`, []int{OPEN_BRACE, IDENTIFIER, RE, STRING, CLOSE_BRACE}},
		{`{ foo !~ "bar" }`, []int{OPEN_BRACE, IDENTIFIER, NRE, STRING, CLOSE_BRACE}},
		{`{ foo = "bar", bar != "baz" }`, []int{OPEN_BRACE, IDENTIFIER, EQ, STRING,
			COMMA, IDENTIFIER, NEQ, STRING, CLOSE_BRACE}},
		{`{ foo = "ba\"r" }`, []int{OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE}},
		{`rate({foo="bar"}[10s])`, []int{RATE, OPEN_PARENTHESIS, OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, DURATION, CLOSE_PARENTHESIS}},
		{`count_over_time({foo="bar"}[5m])`, []int{COUNT_OVER_TIME, OPEN_PARENTHESIS, OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, DURATION, CLOSE_PARENTHESIS}},
		{`sum(count_over_time({foo="bar"}[5m])) by (foo,bar)`, []int{SUM, OPEN_PARENTHESIS, COUNT_OVER_TIME, OPEN_PARENTHESIS, OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, DURATION, CLOSE_PARENTHESIS, CLOSE_PARENTHESIS, BY, OPEN_PARENTHESIS, IDENTIFIER, COMMA, IDENTIFIER, CLOSE_PARENTHESIS}},
		{`topk(3,count_over_time({foo="bar"}[5m])) by (foo,bar)`, []int{TOPK, OPEN_PARENTHESIS, IDENTIFIER, COMMA, COUNT_OVER_TIME, OPEN_PARENTHESIS, OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, DURATION, CLOSE_PARENTHESIS, CLOSE_PARENTHESIS, BY, OPEN_PARENTHESIS, IDENTIFIER, COMMA, IDENTIFIER, CLOSE_PARENTHESIS}},
		{`bottomk(10,sum(count_over_time({foo="bar"}[5m])) by (foo,bar))`, []int{BOTTOMK, OPEN_PARENTHESIS, IDENTIFIER, COMMA, SUM, OPEN_PARENTHESIS, COUNT_OVER_TIME, OPEN_PARENTHESIS, OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, DURATION, CLOSE_PARENTHESIS, CLOSE_PARENTHESIS, BY, OPEN_PARENTHESIS, IDENTIFIER, COMMA, IDENTIFIER, CLOSE_PARENTHESIS, CLOSE_PARENTHESIS}},
		{`sum(max(rate({foo="bar"}[5m])) by (foo,bar)) by (foo)`, []int{SUM, OPEN_PARENTHESIS, MAX, OPEN_PARENTHESIS, RATE, OPEN_PARENTHESIS, OPEN_BRACE, IDENTIFIER, EQ, STRING, CLOSE_BRACE, DURATION, CLOSE_PARENTHESIS, CLOSE_PARENTHESIS, BY, OPEN_PARENTHESIS, IDENTIFIER, COMMA, IDENTIFIER, CLOSE_PARENTHESIS, CLOSE_PARENTHESIS, BY, OPEN_PARENTHESIS, IDENTIFIER, CLOSE_PARENTHESIS}},
	} {
		t.Run(tc.input, func(t *testing.T) {
			actual := []int{}
			l := lexer{
				Scanner: scanner.Scanner{
					Mode: scanner.SkipComments | scanner.ScanStrings,
				},
			}
			l.Init(strings.NewReader(tc.input))
			var lval exprSymType
			for {
				tok := l.Lex(&lval)
				if tok == 0 {
					break
				}
				actual = append(actual, tok)
			}
			require.Equal(t, tc.expected, actual)
		})
	}
}

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
				msg:  "time: unknown unit minutes in duration 5minutes",
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
				msg:  "syntax error: unexpected {",
				line: 1,
				col:  5,
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
				msg:  "invalid parameter bottomk(he,",
				line: 0,
				col:  0,
			},
		},
		{
			in: `stddev({ foo !~ "bar" })`,
			err: ParseError{
				msg:  "syntax error: unexpected {",
				line: 1,
				col:  8,
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
				}, OpTypeCountOverTime),
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
				}, OpTypeCountOverTime),
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
				}, OpTypeCountOverTime),
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
						}, OpTypeRate),
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
				msg:  "syntax error: unexpected STRING, expecting != or !~ or |~ or |=",
				line: 1,
				col:  13,
			},
		},
		{
			in: `{foo="bar"} foo`,
			err: ParseError{
				msg:  "syntax error: unexpected IDENTIFIER, expecting != or !~ or |~ or |=",
				line: 1,
				col:  13,
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
