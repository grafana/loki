package syntax

import (
	"errors"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql/log"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
)

func NewStringLabelFilter(s string) *string {
	return &s
}

var ParseTestCases = []struct {
	in  string
	exp Expr
	err error
}{
	{
		// raw string
		in: "count_over_time({foo=~`bar\\w+`}[12h] |~ `error\\`)",
		exp: &RangeAggregationExpr{
			Operation: "count_over_time",
			Left: &LogRange{
				Left: &PipelineExpr{
					MultiStages: MultiStageExpr{
						newLineFilterExpr(log.LineMatchRegexp, "", "error\\"),
					},
					Left: &MatchersExpr{
						Mts: []*labels.Matcher{
							mustNewMatcher(labels.MatchRegexp, "foo", "bar\\w+"),
						},
					},
				},
				Interval: 12 * time.Hour,
			},
		},
	},
	{
		in: `{ foo = "bar" } | decolorize`,
		exp: newPipelineExpr(
			newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
			MultiStageExpr{
				newDecolorizeExpr(),
			},
		),
	},
	{
		// test [12h] before filter expr
		in: `count_over_time({foo="bar"}[12h] |= "error")`,
		exp: &RangeAggregationExpr{
			Operation: "count_over_time",
			Left: &LogRange{
				Left: newPipelineExpr(
					newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "foo", Value: "bar"}}),
					MultiStageExpr{
						newLineFilterExpr(log.LineMatchEqual, "", "error"),
					},
				),
				Interval: 12 * time.Hour,
			},
		},
	},
	{
		// test [12h] after filter expr
		in: `count_over_time({foo="bar"} |= "error" [12h])`,
		exp: &RangeAggregationExpr{
			Operation: "count_over_time",
			Left: &LogRange{
				Left: newPipelineExpr(
					newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "foo", Value: "bar"}}),
					MultiStageExpr{newLineFilterExpr(log.LineMatchEqual, "", "error")},
				),
				Interval: 12 * time.Hour,
			},
		},
	},
	{
		in:  `{foo="bar"}`,
		exp: &MatchersExpr{Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
	},
	{
		in:  `{ foo = "bar" }`,
		exp: &MatchersExpr{Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
	},
	{
		in: `{ namespace="buzz", foo != "bar" }`,
		exp: &MatchersExpr{Mts: []*labels.Matcher{
			mustNewMatcher(labels.MatchEqual, "namespace", "buzz"),
			mustNewMatcher(labels.MatchNotEqual, "foo", "bar"),
		}},
	},
	{
		in:  `{ foo =~ "bar" }`,
		exp: &MatchersExpr{Mts: []*labels.Matcher{mustNewMatcher(labels.MatchRegexp, "foo", "bar")}},
	},
	{
		in: `{ namespace="buzz", foo !~ "bar" }`,
		exp: &MatchersExpr{Mts: []*labels.Matcher{
			mustNewMatcher(labels.MatchEqual, "namespace", "buzz"),
			mustNewMatcher(labels.MatchNotRegexp, "foo", "bar"),
		}},
	},
	{
		in: `count_over_time({ foo = "bar" }[12m])`,
		exp: &RangeAggregationExpr{
			Left: &LogRange{
				Left:     &MatchersExpr{Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
				Interval: 12 * time.Minute,
			},
			Operation: "count_over_time",
		},
	},
	{
		in: `bytes_over_time({ foo = "bar" }[12m])`,
		exp: &RangeAggregationExpr{
			Left: &LogRange{
				Left:     &MatchersExpr{Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
				Interval: 12 * time.Minute,
			},
			Operation: OpRangeTypeBytes,
		},
	},
	{
		in: `bytes_rate({ foo = "bar" }[12m])`,
		exp: &RangeAggregationExpr{
			Left: &LogRange{
				Left:     &MatchersExpr{Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
				Interval: 12 * time.Minute,
			},
			Operation: OpRangeTypeBytesRate,
		},
	},
	{
		in: `rate({ foo = "bar" }[5h])`,
		exp: &RangeAggregationExpr{
			Left: &LogRange{
				Left:     &MatchersExpr{Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
				Interval: 5 * time.Hour,
			},
			Operation: "rate",
		},
	},
	{
		in: `{ foo = "bar" }|logfmt --strict`,
		exp: newPipelineExpr(
			newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
			MultiStageExpr{
				newLogfmtParserExpr([]string{OpStrict}),
			},
		),
	},
	{
		in: `{ foo = "bar" }|logfmt|rate="a"`, // rate should also be able to use it as IDENTIFIER
		exp: newPipelineExpr(
			newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
			MultiStageExpr{
				newLogfmtParserExpr(nil),
				newLabelFilterExpr(log.NewStringLabelFilter(mustNewMatcher(labels.MatchEqual, "rate", "a"))),
			},
		),
	},
	{
		in: `{ foo = "bar" }|logfmt|length>5d`,
		exp: newPipelineExpr(
			newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
			MultiStageExpr{
				newLogfmtParserExpr(nil),
				newLabelFilterExpr(log.NewDurationLabelFilter(log.LabelFilterGreaterThan, "length", 5*24*time.Hour)),
			},
		),
	},
	{
		in: `{ foo = "bar" }|logfmt --strict --keep-empty|length>5d`,
		exp: newPipelineExpr(
			newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
			MultiStageExpr{
				newLogfmtParserExpr([]string{OpStrict, OpKeepEmpty}),
				newLabelFilterExpr(log.NewDurationLabelFilter(log.LabelFilterGreaterThan, "length", 5*24*time.Hour)),
			},
		),
	},
	{
		in: `rate({ foo = "bar" }[5d])`,
		exp: &RangeAggregationExpr{
			Left: &LogRange{
				Left:     &MatchersExpr{Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
				Interval: 5 * 24 * time.Hour,
			},
			Operation: "rate",
		},
	},
	{
		in: `count_over_time({ foo = "bar" }[1w])`,
		exp: &RangeAggregationExpr{
			Left: &LogRange{
				Left:     &MatchersExpr{Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
				Interval: 7 * 24 * time.Hour,
			},
			Operation: "count_over_time",
		},
	},
	{
		in: `absent_over_time({ foo = "bar" }[1w])`,
		exp: &RangeAggregationExpr{
			Left: &LogRange{
				Left:     &MatchersExpr{Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
				Interval: 7 * 24 * time.Hour,
			},
			Operation: OpRangeTypeAbsent,
		},
	},
	{
		in: `sum(rate({ foo = "bar" }[5h]))`,
		exp: mustNewVectorAggregationExpr(&RangeAggregationExpr{
			Left: &LogRange{
				Left:     &MatchersExpr{Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
				Interval: 5 * time.Hour,
			},
			Operation: "rate",
		}, "sum", nil, nil),
	},
	{
		in: `sum(rate({ foo ="bar" }[1y]))`,
		exp: mustNewVectorAggregationExpr(&RangeAggregationExpr{
			Left: &LogRange{
				Left:     &MatchersExpr{Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
				Interval: 365 * 24 * time.Hour,
			},
			Operation: "rate",
		}, "sum", nil, nil),
	},
	{
		in: `avg(count_over_time({ foo = "bar" }[5h])) by (bar,foo)`,
		exp: mustNewVectorAggregationExpr(&RangeAggregationExpr{
			Left: &LogRange{
				Left:     &MatchersExpr{Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
				Interval: 5 * time.Hour,
			},
			Operation: "count_over_time",
		}, "avg", &Grouping{
			Without: false,
			Groups:  []string{"bar", "foo"},
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
					Left: &LogRange{
						Left:     &MatchersExpr{Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
						Interval: 5 * time.Hour,
					},
					Operation: "count_over_time",
				},
				"bar", "$1$2", "foo", "(.*).(.*)",
			),
			"avg", &Grouping{
				Without: false,
				Groups:  []string{"bar", "foo"},
			}, nil),
	},
	{
		in: `avg(count_over_time({ foo = "bar" }[5h])) by ()`,
		exp: mustNewVectorAggregationExpr(&RangeAggregationExpr{
			Left: &LogRange{
				Left:     &MatchersExpr{Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
				Interval: 5 * time.Hour,
			},
			Operation: "count_over_time",
		}, "avg", &Grouping{
			Without: false,
			Groups:  nil,
		}, nil),
	},
	{
		in: `max without (bar) (count_over_time({ foo = "bar" }[5h]))`,
		exp: mustNewVectorAggregationExpr(&RangeAggregationExpr{
			Left: &LogRange{
				Left:     &MatchersExpr{Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
				Interval: 5 * time.Hour,
			},
			Operation: "count_over_time",
		}, "max", &Grouping{
			Without: true,
			Groups:  []string{"bar"},
		}, nil),
	},
	{
		in: `max without () (count_over_time({ foo = "bar" }[5h]))`,
		exp: mustNewVectorAggregationExpr(&RangeAggregationExpr{
			Left: &LogRange{
				Left:     &MatchersExpr{Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
				Interval: 5 * time.Hour,
			},
			Operation: "count_over_time",
		}, "max", &Grouping{
			Without: true,
			Groups:  nil,
		}, nil),
	},
	{
		in: `topk(10,count_over_time({ foo = "bar" }[5h])) without (bar)`,
		exp: mustNewVectorAggregationExpr(&RangeAggregationExpr{
			Left: &LogRange{
				Left:     &MatchersExpr{Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
				Interval: 5 * time.Hour,
			},
			Operation: "count_over_time",
		}, "topk", &Grouping{
			Without: true,
			Groups:  []string{"bar"},
		}, NewStringLabelFilter("10")),
	},
	{
		in: `bottomk(30 ,sum(rate({ foo = "bar" }[5h])) by (foo))`,
		exp: mustNewVectorAggregationExpr(mustNewVectorAggregationExpr(&RangeAggregationExpr{
			Left: &LogRange{
				Left:     &MatchersExpr{Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
				Interval: 5 * time.Hour,
			},
			Operation: "rate",
		}, "sum", &Grouping{
			Groups:  []string{"foo"},
			Without: false,
		}, nil), "bottomk", nil,
			NewStringLabelFilter("30")),
	},
	{
		in: `max( sum(count_over_time({ foo = "bar" }[5h])) without (foo,bar) ) by (foo)`,
		exp: mustNewVectorAggregationExpr(mustNewVectorAggregationExpr(&RangeAggregationExpr{
			Left: &LogRange{
				Left:     &MatchersExpr{Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
				Interval: 5 * time.Hour,
			},
			Operation: "count_over_time",
		}, "sum", &Grouping{
			Groups:  []string{"foo", "bar"},
			Without: true,
		}, nil), "max", &Grouping{
			Groups:  []string{"foo"},
			Without: false,
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
		in:  `approx_topk(2, count_over_time({ foo = "bar" }[5h])) by (foo)`,
		err: logqlmodel.NewParseError("grouping not allowed for approx_topk aggregation", 0, 0),
	},
	{
		in:  `rate({ foo = "bar" }[5minutes])`,
		err: logqlmodel.NewParseError(`unknown unit "minutes" in duration "5minutes"`, 0, 21),
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
	// line filter for ip-matcher
	{
		in: `{foo="bar"} |= "baz" |= ip("123.123.123.123")`,
		exp: newPipelineExpr(
			newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
			MultiStageExpr{
				newNestedLineFilterExpr(
					newLineFilterExpr(log.LineMatchEqual, "", "baz"),
					newLineFilterExpr(log.LineMatchEqual, OpFilterIP, "123.123.123.123"),
				),
			},
		),
	},
	{
		in: `{ foo = "bar" , ip="foo"}|logfmt|= ip("127.0.0.1")|ip="2.3.4.5"|ip="abc"|ipaddr=ip("4.5.6.7")|ip=ip("6.7.8.9")`,
		exp: newPipelineExpr(
			newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar"), mustNewMatcher(labels.MatchEqual, "ip", "foo")}),
			MultiStageExpr{
				newLogfmtParserExpr(nil),
				newLineFilterExpr(log.LineMatchEqual, OpFilterIP, "127.0.0.1"),
				newLabelFilterExpr(log.NewStringLabelFilter(mustNewMatcher(labels.MatchEqual, "ip", "2.3.4.5"))),
				newLabelFilterExpr(log.NewStringLabelFilter(mustNewMatcher(labels.MatchEqual, "ip", "abc"))),
				newLabelFilterExpr(log.NewIPLabelFilter("4.5.6.7", "ipaddr", log.LabelFilterEqual)),
				newLabelFilterExpr(log.NewIPLabelFilter("6.7.8.9", "ip", log.LabelFilterEqual)),
			},
		),
	},
	{
		in: `{foo="bar"} |= ip("123.123.123.123")`,
		exp: newPipelineExpr(
			newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
			MultiStageExpr{
				newLineFilterExpr(log.LineMatchEqual, OpFilterIP, "123.123.123.123"),
			},
		),
	},
	{
		in: `{foo="bar"} |= ip("123.123.123.123")|= "baz"`,
		exp: newPipelineExpr(
			newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
			MultiStageExpr{
				newNestedLineFilterExpr(
					newLineFilterExpr(log.LineMatchEqual, OpFilterIP, "123.123.123.123"),
					newLineFilterExpr(log.LineMatchEqual, "", "baz"),
				),
			},
		),
	},
	{
		in: `{foo="bar"} |= ip("123.123.123.123")|= "baz" |=ip("123.123.123.123")`,
		exp: newPipelineExpr(
			newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
			MultiStageExpr{
				newNestedLineFilterExpr(
					newNestedLineFilterExpr(
						newLineFilterExpr(log.LineMatchEqual, OpFilterIP, "123.123.123.123"),
						newLineFilterExpr(log.LineMatchEqual, "", "baz"),
					),
					newLineFilterExpr(log.LineMatchEqual, OpFilterIP, "123.123.123.123"),
				),
			},
		),
	},
	{
		in: `{foo="bar"} |= "baz" |= ip("123.123.123.123")`,
		exp: newPipelineExpr(
			newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
			MultiStageExpr{
				newNestedLineFilterExpr(
					newLineFilterExpr(log.LineMatchEqual, "", "baz"),
					newLineFilterExpr(log.LineMatchEqual, OpFilterIP, "123.123.123.123"),
				),
			},
		),
	},
	{
		in: `{foo="bar"} != ip("123.123.123.123")`,
		exp: newPipelineExpr(
			newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
			MultiStageExpr{
				newLineFilterExpr(log.LineMatchNotEqual, OpFilterIP, "123.123.123.123"),
			},
		),
	},
	{
		in: `{foo="bar"} != ip("123.123.123.123")|= "baz"`,
		exp: newPipelineExpr(
			newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
			MultiStageExpr{
				newNestedLineFilterExpr(
					newLineFilterExpr(log.LineMatchNotEqual, OpFilterIP, "123.123.123.123"),
					newLineFilterExpr(log.LineMatchEqual, "", "baz"),
				),
			},
		),
	},
	{
		in: `{foo="bar"} != ip("123.123.123.123")|= "baz" !=ip("123.123.123.123")`,
		exp: newPipelineExpr(
			newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
			MultiStageExpr{
				newNestedLineFilterExpr(
					newNestedLineFilterExpr(
						newLineFilterExpr(log.LineMatchNotEqual, OpFilterIP, "123.123.123.123"),
						newLineFilterExpr(log.LineMatchEqual, "", "baz"),
					),
					newLineFilterExpr(log.LineMatchNotEqual, OpFilterIP, "123.123.123.123"),
				),
			},
		),
	},
	// label filter for ip-matcher
	{
		in:  `{ foo = "bar" }|logfmt|addr>=ip("1.2.3.4")`,
		err: logqlmodel.NewParseError("syntax error: unexpected ip", 1, 30),
	},
	{
		in:  `{ foo = "bar" }|logfmt|addr>ip("1.2.3.4")`,
		err: logqlmodel.NewParseError("syntax error: unexpected ip", 1, 29),
	},
	{
		in:  `{ foo = "bar" }|logfmt|addr<=ip("1.2.3.4")`,
		err: logqlmodel.NewParseError("syntax error: unexpected ip", 1, 30),
	},
	{
		in:  `{ foo = "bar" }|logfmt|addr<ip("1.2.3.4")`,
		err: logqlmodel.NewParseError("syntax error: unexpected ip", 1, 29),
	},
	{
		in: `{ foo = "bar" }|logfmt|addr=ip("1.2.3.4")`,
		exp: newPipelineExpr(
			newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
			MultiStageExpr{newLogfmtParserExpr(nil), newLabelFilterExpr(log.NewIPLabelFilter("1.2.3.4", "addr", log.LabelFilterEqual))},
		),
	},
	{
		in: `{ foo = "bar" }|logfmt|addr!=ip("1.2.3.4")`,
		exp: newPipelineExpr(
			newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
			MultiStageExpr{newLogfmtParserExpr(nil), newLabelFilterExpr(log.NewIPLabelFilter("1.2.3.4", "addr", log.LabelFilterNotEqual))},
		),
	},
	{
		in: `{ foo = "bar" }|logfmt|level="error"|addr=ip("1.2.3.4")`,
		exp: newPipelineExpr(
			newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
			MultiStageExpr{
				newLogfmtParserExpr(nil),
				newLabelFilterExpr(log.NewStringLabelFilter(mustNewMatcher(labels.MatchEqual, "level", "error"))),
				newLabelFilterExpr(log.NewIPLabelFilter("1.2.3.4", "addr", log.LabelFilterEqual)),
			},
		),
	},
	{
		in: `{ foo = "bar" }|logfmt|level="error"|addr!=ip("1.2.3.4")`,
		exp: newPipelineExpr(
			newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
			MultiStageExpr{
				newLogfmtParserExpr(nil),
				newLabelFilterExpr(log.NewStringLabelFilter(mustNewMatcher(labels.MatchEqual, "level", "error"))),
				newLabelFilterExpr(log.NewIPLabelFilter("1.2.3.4", "addr", log.LabelFilterNotEqual)),
			},
		),
	},
	{
		in: `{ foo = "bar" }|logfmt|remote_addr=ip("2.3.4.5")|level="error"|addr=ip("1.2.3.4")`, // chain label filters with ip matcher
		exp: newPipelineExpr(
			newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
			MultiStageExpr{
				newLogfmtParserExpr(nil),
				newLabelFilterExpr(log.NewIPLabelFilter("2.3.4.5", "remote_addr", log.LabelFilterEqual)),
				newLabelFilterExpr(log.NewStringLabelFilter(mustNewMatcher(labels.MatchEqual, "level", "error"))),
				newLabelFilterExpr(log.NewIPLabelFilter("1.2.3.4", "addr", log.LabelFilterEqual)),
			},
		),
	},
	{
		in: `{ foo = "bar" }|logfmt|remote_addr!=ip("2.3.4.5")|level="error"|addr!=ip("1.2.3.4")`, // chain label filters with ip matcher
		exp: newPipelineExpr(
			newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
			MultiStageExpr{
				newLogfmtParserExpr(nil),
				newLabelFilterExpr(log.NewIPLabelFilter("2.3.4.5", "remote_addr", log.LabelFilterNotEqual)),
				newLabelFilterExpr(log.NewStringLabelFilter(mustNewMatcher(labels.MatchEqual, "level", "error"))),
				newLabelFilterExpr(log.NewIPLabelFilter("1.2.3.4", "addr", log.LabelFilterNotEqual)),
			},
		),
	},
	{
		in: `{ foo = "bar" }|logfmt|remote_addr=ip("2.3.4.5")|level="error"|addr!=ip("1.2.3.4")`, // chain label filters with ip matcher
		exp: newPipelineExpr(
			newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
			MultiStageExpr{
				newLogfmtParserExpr(nil),
				newLabelFilterExpr(log.NewIPLabelFilter("2.3.4.5", "remote_addr", log.LabelFilterEqual)),
				newLabelFilterExpr(log.NewStringLabelFilter(mustNewMatcher(labels.MatchEqual, "level", "error"))),
				newLabelFilterExpr(log.NewIPLabelFilter("1.2.3.4", "addr", log.LabelFilterNotEqual)),
			},
		),
	},
	{
		in: `{ foo = "bar" }|logfmt|ip="2.3.4.5"`, // just using `ip` as a label name(identifier) should work
		exp: newPipelineExpr(
			newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
			MultiStageExpr{
				newLogfmtParserExpr(nil),
				newLabelFilterExpr(log.NewStringLabelFilter(mustNewMatcher(labels.MatchEqual, "ip", "2.3.4.5"))),
			},
		),
	},
	{
		in: `{ foo = "bar" }|logfmt|ip="2.3.4.5"|ip="abc"`, // just using `ip` as a label name should work with chaining
		exp: newPipelineExpr(
			newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
			MultiStageExpr{
				newLogfmtParserExpr(nil),
				newLabelFilterExpr(log.NewStringLabelFilter(mustNewMatcher(labels.MatchEqual, "ip", "2.3.4.5"))),
				newLabelFilterExpr(log.NewStringLabelFilter(mustNewMatcher(labels.MatchEqual, "ip", "abc"))),
			},
		),
	},
	{
		in: `{ foo = "bar" }|logfmt|ip="2.3.4.5"|ip="abc"|ipaddr=ip("4.5.6.7")`, // `ip` should work as both label name and filter in same query
		exp: newPipelineExpr(
			newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
			MultiStageExpr{
				newLogfmtParserExpr(nil),
				newLabelFilterExpr(log.NewStringLabelFilter(mustNewMatcher(labels.MatchEqual, "ip", "2.3.4.5"))),
				newLabelFilterExpr(log.NewStringLabelFilter(mustNewMatcher(labels.MatchEqual, "ip", "abc"))),
				newLabelFilterExpr(log.NewIPLabelFilter("4.5.6.7", "ipaddr", log.LabelFilterEqual)),
			},
		),
	},
	{
		in: `{ foo = "bar" }|logfmt|ip="2.3.4.5"|ip="abc"|ipaddr=ip("4.5.6.7")|ip=ip("6.7.8.9")`, // `ip` should work as both label name and filter in same query with same name.
		exp: newPipelineExpr(
			newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
			MultiStageExpr{
				newLogfmtParserExpr(nil),
				newLabelFilterExpr(log.NewStringLabelFilter(mustNewMatcher(labels.MatchEqual, "ip", "2.3.4.5"))),
				newLabelFilterExpr(log.NewStringLabelFilter(mustNewMatcher(labels.MatchEqual, "ip", "abc"))),
				newLabelFilterExpr(log.NewIPLabelFilter("4.5.6.7", "ipaddr", log.LabelFilterEqual)),
				newLabelFilterExpr(log.NewIPLabelFilter("6.7.8.9", "ip", log.LabelFilterEqual)),
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
		exp: &MatchersExpr{Mts: []*labels.Matcher{
			mustNewMatcher(labels.MatchEqual, "foo", "bar"),
			mustNewMatcher(labels.MatchNotEqual, "bar", "baz"),
		}},
	},
	{
		in: `{foo="bar"} |= "baz"`,
		exp: newPipelineExpr(
			newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
			MultiStageExpr{newLineFilterExpr(log.LineMatchEqual, "", "baz")},
		),
	},
	{
		in: `{foo="bar"} |= "baz" |~ "blip" != "flip" !~ "flap"`,
		exp: newPipelineExpr(
			newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
			MultiStageExpr{
				newNestedLineFilterExpr(
					newNestedLineFilterExpr(
						newNestedLineFilterExpr(
							newLineFilterExpr(log.LineMatchEqual, "", "baz"),
							newLineFilterExpr(log.LineMatchRegexp, "", "blip"),
						),
						newLineFilterExpr(log.LineMatchNotEqual, "", "flip"),
					),
					newLineFilterExpr(log.LineMatchNotRegexp, "", "flap"),
				),
			},
		),
	},
	{
		in: `count_over_time(({foo="bar"} |= "baz" |~ "blip" != "flip" !~ "flap")[5m])`,
		exp: newRangeAggregationExpr(
			&LogRange{
				Left: newPipelineExpr(
					newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
					MultiStageExpr{
						newNestedLineFilterExpr(
							newNestedLineFilterExpr(
								newNestedLineFilterExpr(
									newLineFilterExpr(log.LineMatchEqual, "", "baz"),
									newLineFilterExpr(log.LineMatchRegexp, "", "blip"),
								),
								newLineFilterExpr(log.LineMatchNotEqual, "", "flip"),
							),
							newLineFilterExpr(log.LineMatchNotRegexp, "", "flap"),
						),
					},
				),
				Interval: 5 * time.Minute,
			}, OpRangeTypeCount, nil, nil),
	},
	{
		in: `bytes_over_time(({foo="bar"} |= "baz" |~ "blip" != "flip" !~ "flap")[5m])`,
		exp: newRangeAggregationExpr(
			&LogRange{
				Left: newPipelineExpr(
					newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
					MultiStageExpr{
						newNestedLineFilterExpr(
							newNestedLineFilterExpr(
								newNestedLineFilterExpr(
									newLineFilterExpr(log.LineMatchEqual, "", "baz"),
									newLineFilterExpr(log.LineMatchRegexp, "", "blip"),
								),
								newLineFilterExpr(log.LineMatchNotEqual, "", "flip"),
							),
							newLineFilterExpr(log.LineMatchNotRegexp, "", "flap"),
						),
					},
				),
				Interval: 5 * time.Minute,
			}, OpRangeTypeBytes, nil, nil),
	},
	{
		in: `bytes_over_time(({foo="bar"} |= "baz" |~ "blip" != "flip" !~ "flap" | unpack)[5m])`,
		exp: newRangeAggregationExpr(
			&LogRange{
				Left: newPipelineExpr(
					newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
					MultiStageExpr{
						newNestedLineFilterExpr(
							newNestedLineFilterExpr(
								newNestedLineFilterExpr(
									newLineFilterExpr(log.LineMatchEqual, "", "baz"),
									newLineFilterExpr(log.LineMatchRegexp, "", "blip"),
								),
								newLineFilterExpr(log.LineMatchNotEqual, "", "flip"),
							),
							newLineFilterExpr(log.LineMatchNotRegexp, "", "flap"),
						),
						newLabelParserExpr(OpParserTypeUnpack, ""),
					},
				),
				Interval: 5 * time.Minute,
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
					Left: newPipelineExpr(
						newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
						MultiStageExpr{
							newNestedLineFilterExpr(
								newNestedLineFilterExpr(
									newNestedLineFilterExpr(
										newLineFilterExpr(log.LineMatchEqual, "", "baz"),
										newLineFilterExpr(log.LineMatchRegexp, "", "blip"),
									),
									newLineFilterExpr(log.LineMatchNotEqual, "", "flip"),
								),
								newLineFilterExpr(log.LineMatchNotRegexp, "", "flap"),
							),
						},
					),
					Interval: 5 * time.Minute,
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
				Left: newPipelineExpr(
					newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
					MultiStageExpr{
						newNestedLineFilterExpr(
							newNestedLineFilterExpr(
								newNestedLineFilterExpr(
									newLineFilterExpr(log.LineMatchEqual, "", "baz"),
									newLineFilterExpr(log.LineMatchRegexp, "", "blip"),
								),
								newLineFilterExpr(log.LineMatchNotEqual, "", "flip"),
							),
							newLineFilterExpr(log.LineMatchNotRegexp, "", "flap"),
						),
					},
				),
				Interval: 5 * time.Minute,
			}, OpRangeTypeCount, nil, nil),
			"sum",
			&Grouping{
				Without: false,
				Groups:  []string{"foo"},
			},
			nil),
	},
	{
		in: `sum(bytes_rate(({foo="bar"} |= "baz" |~ "blip" != "flip" !~ "flap")[5m])) by (foo)`,
		exp: mustNewVectorAggregationExpr(newRangeAggregationExpr(
			&LogRange{
				Left: newPipelineExpr(
					newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
					MultiStageExpr{
						newNestedLineFilterExpr(
							newNestedLineFilterExpr(
								newNestedLineFilterExpr(
									newLineFilterExpr(log.LineMatchEqual, "", "baz"),
									newLineFilterExpr(log.LineMatchRegexp, "", "blip"),
								),
								newLineFilterExpr(log.LineMatchNotEqual, "", "flip"),
							),
							newLineFilterExpr(log.LineMatchNotRegexp, "", "flap"),
						),
					},
				),
				Interval: 5 * time.Minute,
			}, OpRangeTypeBytesRate, nil, nil),
			"sum",
			&Grouping{
				Without: false,
				Groups:  []string{"foo"},
			},
			nil),
	},
	{
		in: `topk(5,count_over_time(({foo="bar"} |= "baz" |~ "blip" != "flip" !~ "flap")[5m])) without (foo)`,
		exp: mustNewVectorAggregationExpr(newRangeAggregationExpr(
			&LogRange{
				Left: newPipelineExpr(
					newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
					MultiStageExpr{
						newNestedLineFilterExpr(
							newNestedLineFilterExpr(
								newNestedLineFilterExpr(
									newLineFilterExpr(log.LineMatchEqual, "", "baz"),
									newLineFilterExpr(log.LineMatchRegexp, "", "blip"),
								),
								newLineFilterExpr(log.LineMatchNotEqual, "", "flip"),
							),
							newLineFilterExpr(log.LineMatchNotRegexp, "", "flap"),
						),
					},
				),
				Interval: 5 * time.Minute,
			}, OpRangeTypeCount, nil, nil),
			"topk",
			&Grouping{
				Without: true,
				Groups:  []string{"foo"},
			},
			NewStringLabelFilter("5")),
	},
	{
		in: `topk(5,sum(rate(({foo="bar"} |= "baz" |~ "blip" != "flip" !~ "flap")[5m])) by (app))`,
		exp: mustNewVectorAggregationExpr(
			mustNewVectorAggregationExpr(
				newRangeAggregationExpr(
					&LogRange{
						Left: newPipelineExpr(
							newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
							MultiStageExpr{
								newNestedLineFilterExpr(
									newNestedLineFilterExpr(
										newNestedLineFilterExpr(
											newLineFilterExpr(log.LineMatchEqual, "", "baz"),
											newLineFilterExpr(log.LineMatchRegexp, "", "blip"),
										),
										newLineFilterExpr(log.LineMatchNotEqual, "", "flip"),
									),
									newLineFilterExpr(log.LineMatchNotRegexp, "", "flap"),
								),
							},
						),
						Interval: 5 * time.Minute,
					}, OpRangeTypeRate, nil, nil),
				"sum",
				&Grouping{
					Without: false,
					Groups:  []string{"app"},
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
				Left: newPipelineExpr(
					newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
					MultiStageExpr{
						newNestedLineFilterExpr(
							newNestedLineFilterExpr(
								newNestedLineFilterExpr(
									newLineFilterExpr(log.LineMatchEqual, "", "baz"),
									newLineFilterExpr(log.LineMatchRegexp, "", "blip"),
								),
								newLineFilterExpr(log.LineMatchNotEqual, "", "flip"),
							),
							newLineFilterExpr(log.LineMatchNotRegexp, "", "flap"),
						),
					},
				),
				Interval: 5 * time.Minute,
			}, OpRangeTypeCount, nil, nil),
	},
	{
		in: `sum(count_over_time({foo="bar"}[5m] |= "baz" |~ "blip" != "flip" !~ "flap")) by (foo)`,
		exp: mustNewVectorAggregationExpr(newRangeAggregationExpr(
			&LogRange{
				Left: newPipelineExpr(
					newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
					MultiStageExpr{
						newNestedLineFilterExpr(
							newNestedLineFilterExpr(
								newNestedLineFilterExpr(
									newLineFilterExpr(log.LineMatchEqual, "", "baz"),
									newLineFilterExpr(log.LineMatchRegexp, "", "blip"),
								),
								newLineFilterExpr(log.LineMatchNotEqual, "", "flip"),
							),
							newLineFilterExpr(log.LineMatchNotRegexp, "", "flap"),
						),
					},
				),
				Interval: 5 * time.Minute,
			}, OpRangeTypeCount, nil, nil),
			"sum",
			&Grouping{
				Without: false,
				Groups:  []string{"foo"},
			},
			nil),
	},
	{
		in: `topk(5,count_over_time({foo="bar"}[5m] |= "baz" |~ "blip" != "flip" !~ "flap")) without (foo)`,
		exp: mustNewVectorAggregationExpr(newRangeAggregationExpr(
			&LogRange{
				Left: newPipelineExpr(
					newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
					MultiStageExpr{
						newNestedLineFilterExpr(
							newNestedLineFilterExpr(
								newNestedLineFilterExpr(
									newLineFilterExpr(log.LineMatchEqual, "", "baz"),
									newLineFilterExpr(log.LineMatchRegexp, "", "blip"),
								),
								newLineFilterExpr(log.LineMatchNotEqual, "", "flip"),
							),
							newLineFilterExpr(log.LineMatchNotRegexp, "", "flap"),
						),
					},
				),
				Interval: 5 * time.Minute,
			}, OpRangeTypeCount, nil, nil),
			"topk",
			&Grouping{
				Without: true,
				Groups:  []string{"foo"},
			},
			NewStringLabelFilter("5")),
	},
	{
		in: `topk(5,sum(rate({foo="bar"}[5m] |= "baz" |~ "blip" != "flip" !~ "flap")) by (app))`,
		exp: mustNewVectorAggregationExpr(
			mustNewVectorAggregationExpr(
				newRangeAggregationExpr(
					&LogRange{
						Left: newPipelineExpr(
							newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}),
							MultiStageExpr{
								newNestedLineFilterExpr(
									newNestedLineFilterExpr(
										newNestedLineFilterExpr(
											newLineFilterExpr(log.LineMatchEqual, "", "baz"),
											newLineFilterExpr(log.LineMatchRegexp, "", "blip"),
										),
										newLineFilterExpr(log.LineMatchNotEqual, "", "flip"),
									),
									newLineFilterExpr(log.LineMatchNotRegexp, "", "flap"),
								),
							},
						),
						Interval: 5 * time.Minute,
					}, OpRangeTypeRate, nil, nil),
				"sum",
				&Grouping{
					Without: false,
					Groups:  []string{"app"},
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
		err: logqlmodel.NewParseError("syntax error: unexpected $end, expecting STRING or ip", 1, 15),
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
			&BinOpOptions{
				VectorMatching: &VectorMatching{Card: CardOneToOne},
			},
			mustNewBinOpExpr(
				OpTypeDiv,
				&BinOpOptions{
					VectorMatching: &VectorMatching{Card: CardOneToOne},
				},
				mustNewVectorAggregationExpr(newRangeAggregationExpr(
					&LogRange{
						Left: &MatchersExpr{
							Mts: []*labels.Matcher{
								mustNewMatcher(labels.MatchEqual, "foo", "bar"),
							},
						},
						Interval: 5 * time.Minute,
					}, OpRangeTypeCount, nil, nil),
					"sum",
					&Grouping{
						Without: false,
						Groups:  []string{"foo"},
					},
					nil,
				),
				mustNewVectorAggregationExpr(newRangeAggregationExpr(
					&LogRange{
						Left: &MatchersExpr{
							Mts: []*labels.Matcher{
								mustNewMatcher(labels.MatchEqual, "foo", "bar"),
							},
						},
						Interval: 5 * time.Minute,
					}, OpRangeTypeCount, nil, nil),
					"sum",
					&Grouping{
						Without: false,
						Groups:  []string{"foo"},
					},
					nil,
				),
			),
			mustNewVectorAggregationExpr(newRangeAggregationExpr(
				&LogRange{
					Left: &MatchersExpr{
						Mts: []*labels.Matcher{
							mustNewMatcher(labels.MatchEqual, "foo", "bar"),
						},
					},
					Interval: 5 * time.Minute,
				}, OpRangeTypeCount, nil, nil),
				"sum",
				&Grouping{
					Without: false,
					Groups:  []string{"foo"},
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
			&BinOpOptions{
				VectorMatching: &VectorMatching{Card: CardOneToOne},
			},
			mustNewBinOpExpr(
				OpTypePow,
				&BinOpOptions{
					VectorMatching: &VectorMatching{Card: CardOneToOne},
				},
				mustNewVectorAggregationExpr(newRangeAggregationExpr(
					&LogRange{
						Left: &MatchersExpr{
							Mts: []*labels.Matcher{
								mustNewMatcher(labels.MatchEqual, "foo", "bar"),
							},
						},
						Interval: 5 * time.Minute,
					}, OpRangeTypeCount, nil, nil),
					"sum",
					&Grouping{
						Without: false,
						Groups:  []string{"foo"},
					},
					nil,
				),
				mustNewVectorAggregationExpr(newRangeAggregationExpr(
					&LogRange{
						Left: &MatchersExpr{
							Mts: []*labels.Matcher{
								mustNewMatcher(labels.MatchEqual, "foo", "bar"),
							},
						},
						Interval: 5 * time.Minute,
					}, OpRangeTypeCount, nil, nil),
					"sum",
					&Grouping{
						Without: false,
						Groups:  []string{"foo"},
					},
					nil,
				),
			),
			mustNewVectorAggregationExpr(newRangeAggregationExpr(
				&LogRange{
					Left: &MatchersExpr{
						Mts: []*labels.Matcher{
							mustNewMatcher(labels.MatchEqual, "foo", "bar"),
						},
					},
					Interval: 5 * time.Minute,
				}, OpRangeTypeCount, nil, nil),
				"sum",
				&Grouping{
					Without: false,
					Groups:  []string{"foo"},
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
			&BinOpOptions{
				VectorMatching: &VectorMatching{Card: CardOneToOne},
			},
			mustNewVectorAggregationExpr(newRangeAggregationExpr(
				&LogRange{
					Left: &MatchersExpr{
						Mts: []*labels.Matcher{
							mustNewMatcher(labels.MatchEqual, "foo", "bar"),
						},
					},
					Interval: 5 * time.Minute,
				}, OpRangeTypeCount, nil, nil),
				"sum",
				&Grouping{
					Without: false,
					Groups:  []string{"foo"},
				},
				nil,
			),
			mustNewBinOpExpr(
				OpTypeDiv,
				&BinOpOptions{
					VectorMatching: &VectorMatching{Card: CardOneToOne},
				},
				mustNewVectorAggregationExpr(newRangeAggregationExpr(
					&LogRange{
						Left: &MatchersExpr{
							Mts: []*labels.Matcher{
								mustNewMatcher(labels.MatchEqual, "foo", "bar"),
							},
						},
						Interval: 5 * time.Minute,
					}, OpRangeTypeCount, nil, nil),
					"sum",
					&Grouping{
						Without: false,
						Groups:  []string{"foo"},
					},
					nil,
				),
				mustNewVectorAggregationExpr(newRangeAggregationExpr(
					&LogRange{
						Left: &MatchersExpr{
							Mts: []*labels.Matcher{
								mustNewMatcher(labels.MatchEqual, "foo", "bar"),
							},
						},
						Interval: 5 * time.Minute,
					}, OpRangeTypeCount, nil, nil),
					"sum",
					&Grouping{
						Without: false,
						Groups:  []string{"foo"},
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
				&BinOpOptions{
					VectorMatching: &VectorMatching{Card: CardOneToOne},
				},
				newRangeAggregationExpr(
					&LogRange{
						Left: newPipelineExpr(
							newMatcherExpr([]*labels.Matcher{
								mustNewMatcher(labels.MatchEqual, "namespace", "tns"),
							}),
							MultiStageExpr{
								newLineFilterExpr(log.LineMatchEqual, "", "level=error"),
							}),
						Interval: 5 * time.Minute,
					}, OpRangeTypeCount, nil, nil),
				newRangeAggregationExpr(
					&LogRange{
						Left: &MatchersExpr{
							Mts: []*labels.Matcher{
								mustNewMatcher(labels.MatchEqual, "namespace", "tns"),
							},
						},
						Interval: 5 * time.Minute,
					}, OpRangeTypeCount, nil, nil)), OpTypeSum, &Grouping{Groups: []string{"job"}}, nil),
	},
	{
		in: `sum by (job) (
							count_over_time({namespace="tns"} |= "level=error"[5m])
						/
							count_over_time({namespace="tns"}[5m])
						) * 100`,
		exp: mustNewBinOpExpr(OpTypeMul, &BinOpOptions{
			VectorMatching: &VectorMatching{Card: CardOneToOne},
		}, mustNewVectorAggregationExpr(
			mustNewBinOpExpr(OpTypeDiv,
				&BinOpOptions{
					VectorMatching: &VectorMatching{Card: CardOneToOne},
				},
				newRangeAggregationExpr(
					&LogRange{
						Left: newPipelineExpr(
							newMatcherExpr([]*labels.Matcher{
								mustNewMatcher(labels.MatchEqual, "namespace", "tns"),
							}),
							MultiStageExpr{
								newLineFilterExpr(log.LineMatchEqual, "", "level=error"),
							}),
						Interval: 5 * time.Minute,
					}, OpRangeTypeCount, nil, nil),
				newRangeAggregationExpr(
					&LogRange{
						Left: &MatchersExpr{
							Mts: []*labels.Matcher{
								mustNewMatcher(labels.MatchEqual, "namespace", "tns"),
							},
						},
						Interval: 5 * time.Minute,
					}, OpRangeTypeCount, nil, nil)), OpTypeSum, &Grouping{Groups: []string{"job"}}, nil),
			mustNewLiteralExpr("100", false),
		),
	},
	{
		// reduces binop with two literalExprs
		in: `sum(count_over_time({foo="bar"}[5m])) by (foo) + 1 / 2`,
		exp: mustNewBinOpExpr(
			OpTypeAdd,
			&BinOpOptions{
				VectorMatching: &VectorMatching{Card: CardOneToOne},
			},
			mustNewVectorAggregationExpr(
				newRangeAggregationExpr(
					&LogRange{
						Left: &MatchersExpr{
							Mts: []*labels.Matcher{
								mustNewMatcher(labels.MatchEqual, "foo", "bar"),
							},
						},
						Interval: 5 * time.Minute,
					}, OpRangeTypeCount, nil, nil),
				"sum",
				&Grouping{
					Without: false,
					Groups:  []string{"foo"},
				},
				nil,
			),
			&LiteralExpr{Val: 0.5},
		),
	},
	{
		// test signs
		in: `1 + -2 / 1`,
		exp: mustNewBinOpExpr(
			OpTypeAdd,
			&BinOpOptions{
				VectorMatching: &VectorMatching{Card: CardOneToOne},
			},
			&LiteralExpr{Val: 1},
			mustNewBinOpExpr(OpTypeDiv, &BinOpOptions{
				VectorMatching: &VectorMatching{Card: CardOneToOne},
			}, &LiteralExpr{Val: -2}, &LiteralExpr{Val: 1}),
		),
	},
	{
		// test signs/ops with equal associativity
		in: `1 + 1 - -1`,
		exp: mustNewBinOpExpr(
			OpTypeSub,
			&BinOpOptions{
				VectorMatching: &VectorMatching{Card: CardOneToOne},
			},
			mustNewBinOpExpr(OpTypeAdd, &BinOpOptions{
				VectorMatching: &VectorMatching{Card: CardOneToOne},
			}, &LiteralExpr{Val: 1}, &LiteralExpr{Val: 1}),
			&LiteralExpr{Val: -1},
		),
	},
	{
		in: `{app="foo"} |= "bar" | json | latency >= 250ms or ( status_code < 500 and status_code > 200)`,
		exp: &PipelineExpr{
			Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
			MultiStages: MultiStageExpr{
				newLineFilterExpr(log.LineMatchEqual, "", "bar"),
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
			Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
			MultiStages: MultiStageExpr{
				newLineFilterExpr(log.LineMatchEqual, "", "bar"),
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
			Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
			MultiStages: MultiStageExpr{
				newLineFilterExpr(log.LineMatchEqual, "", "bar"),
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
			Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
			MultiStages: MultiStageExpr{
				newLineFilterExpr(log.LineMatchEqual, "", "bar"),
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
			Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
			MultiStages: MultiStageExpr{
				newLineFilterExpr(log.LineMatchEqual, "", "bar"),
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
			Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
			MultiStages: MultiStageExpr{
				newLineFilterExpr(log.LineMatchEqual, "", "bar"),
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
			Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
			MultiStages: MultiStageExpr{
				newLineFilterExpr(log.LineMatchEqual, "", "bar"),
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
			Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
			MultiStages: MultiStageExpr{
				newLineFilterExpr(log.LineMatchEqual, "", "bar"),
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
			Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
			MultiStages: MultiStageExpr{
				newLineFilterExpr(log.LineMatchEqual, "", "bar"),
				newLineFmtExpr("blip{{ .foo }}blop"),
			},
		},
	},
	{
		in: `{app="foo"} |= "bar" | json | latency >= 250ms or ( status_code < 500 and status_code > 200)
			| line_format "blip{{ .foo }}blop {{.status_code}}"`,
		exp: &PipelineExpr{
			Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
			MultiStages: MultiStageExpr{
				newLineFilterExpr(log.LineMatchEqual, "", "bar"),
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
			Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
			MultiStages: MultiStageExpr{
				newLineFilterExpr(log.LineMatchEqual, "", "bar"),
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
				Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
				MultiStages: MultiStageExpr{
					newLineFilterExpr(log.LineMatchEqual, "", "bar"),
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
			Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
			MultiStages: MultiStageExpr{
				newLineFilterExpr(log.LineMatchEqual, "", "bar"),
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
				Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
				MultiStages: MultiStageExpr{
					newLineFilterExpr(log.LineMatchEqual, "", "bar"),
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
				Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
				MultiStages: MultiStageExpr{
					newLineFilterExpr(log.LineMatchEqual, "", "bar"),
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
				Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "namespace", Value: "tns"}}),
				MultiStages: MultiStageExpr{
					newLineFilterExpr(log.LineMatchEqual, "", "level=error"),
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
				Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "namespace", Value: "tns"}}),
				MultiStages: MultiStageExpr{
					newLineFilterExpr(log.LineMatchEqual, "", "level=error"),
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
				Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "namespace", Value: "tns"}}),
				MultiStages: MultiStageExpr{
					newLineFilterExpr(log.LineMatchEqual, "", "level=error"),
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
				Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "namespace", Value: "tns"}}),
				MultiStages: MultiStageExpr{
					newLineFilterExpr(log.LineMatchEqual, "", "level=error"),
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
				Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
				MultiStages: MultiStageExpr{
					newLineFilterExpr(log.LineMatchEqual, "", "bar"),
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
			OpRangeTypeMin, &Grouping{}, nil,
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
			OpRangeTypeMax, &Grouping{Without: true}, nil,
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
			OpRangeTypeMax, &Grouping{Without: true, Groups: []string{"foo", "bar"}}, nil,
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
			OpRangeTypeMax, &Grouping{Without: true, Groups: []string{"foo", "bar"}}, nil,
		),
	},
	{
		in: `max_over_time({app="foo"} | unwrap bar [5m] offset -5m) without (foo,bar)`,
		exp: newRangeAggregationExpr(
			newLogRange(
				newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
				5*time.Minute,
				newUnwrapExpr("bar", ""),
				newOffsetExpr(-5*time.Minute)),
			OpRangeTypeMax, &Grouping{Without: true, Groups: []string{"foo", "bar"}}, nil,
		),
	},
	{
		in: `max_over_time(({app="foo"} |= "bar" | json | latency >= 250ms or ( status_code < 500 and status_code > 200)
			| line_format "blip{{ .foo }}blop {{.status_code}}" | label_format foo=bar,status_code="buzz{{.bar}}" | unwrap foo )[5m])`,
		exp: newRangeAggregationExpr(
			newLogRange(&PipelineExpr{
				Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
				MultiStages: MultiStageExpr{
					newLineFilterExpr(log.LineMatchEqual, "", "bar"),
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
				Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
				MultiStages: MultiStageExpr{
					newLineFilterExpr(log.LineMatchEqual, "", "bar"),
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
				Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
				MultiStages: MultiStageExpr{
					newLineFilterExpr(log.LineMatchEqual, "", "bar"),
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
			OpRangeTypeQuantile, &Grouping{Without: false, Groups: []string{"namespace", "instance"}}, NewStringLabelFilter("0.99998"),
		),
	},
	{
		in: `quantile_over_time(0.99998,{app="foo"} |= "bar" | json | latency >= 250ms or ( status_code < 500 and status_code > 200)
			| line_format "blip{{ .foo }}blop {{.status_code}}" | label_format foo=bar,status_code="buzz{{.bar}}" | unwrap foo | __error__ !~".+"[5m]) by (namespace,instance)`,
		exp: newRangeAggregationExpr(
			newLogRange(&PipelineExpr{
				Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
				MultiStages: MultiStageExpr{
					newLineFilterExpr(log.LineMatchEqual, "", "bar"),
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
			OpRangeTypeQuantile, &Grouping{Without: false, Groups: []string{"namespace", "instance"}}, NewStringLabelFilter("0.99998"),
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
					Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
					MultiStages: MultiStageExpr{
						newLineFilterExpr(log.LineMatchEqual, "", "bar"),
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
				OpRangeTypeQuantile, &Grouping{Without: false, Groups: []string{"namespace", "instance"}}, NewStringLabelFilter("0.99998"),
			),
			OpTypeSum,
			&Grouping{Without: true, Groups: []string{"foo"}},
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
					Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
					MultiStages: MultiStageExpr{
						newLineFilterExpr(log.LineMatchEqual, "", "bar"),
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
				OpRangeTypeQuantile, &Grouping{Without: false, Groups: []string{"namespace", "instance"}}, NewStringLabelFilter("0.99998"),
			),
			OpTypeSum,
			&Grouping{Without: true, Groups: []string{"foo"}},
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
					Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
					MultiStages: MultiStageExpr{
						newLineFilterExpr(log.LineMatchEqual, "", "bar"),
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
				OpRangeTypeQuantile, &Grouping{Without: false, Groups: []string{"namespace", "instance"}}, NewStringLabelFilter("0.99998"),
			),
			OpTypeSum,
			&Grouping{Without: true, Groups: []string{"foo"}},
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
					Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
					MultiStages: MultiStageExpr{
						newLineFilterExpr(log.LineMatchEqual, "", "bar"),
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
				OpRangeTypeQuantile, &Grouping{Without: false, Groups: []string{"namespace", "instance"}}, NewStringLabelFilter(".99998"),
			),
			OpTypeSum,
			&Grouping{Without: true, Groups: []string{"foo"}},
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
					Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
					MultiStages: MultiStageExpr{
						newLineFilterExpr(log.LineMatchEqual, "", "bar"),
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
				OpRangeTypeQuantile, &Grouping{Without: false, Groups: []string{"namespace", "instance"}}, NewStringLabelFilter(".99998"),
			),
			OpTypeSum,
			&Grouping{Without: true, Groups: []string{"foo"}},
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
					Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
					MultiStages: MultiStageExpr{
						newLineFilterExpr(log.LineMatchEqual, "", "bar"),
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
				OpRangeTypeQuantile, &Grouping{Without: false, Groups: []string{"namespace", "instance"}}, NewStringLabelFilter("0.99998"),
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
		exp: mustNewBinOpExpr(OpTypeAdd, &BinOpOptions{
			VectorMatching: &VectorMatching{Card: CardOneToOne}, ReturnBool: false,
		},
			mustNewVectorAggregationExpr(
				newRangeAggregationExpr(
					newLogRange(&PipelineExpr{
						Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
						MultiStages: MultiStageExpr{
							newLineFilterExpr(log.LineMatchEqual, "", "bar"),
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
					OpRangeTypeQuantile, &Grouping{Without: false, Groups: []string{"namespace", "instance"}}, NewStringLabelFilter("0.99998"),
				),
				OpTypeSum,
				&Grouping{Groups: []string{"foo", "bar"}},
				nil,
			),
			mustNewVectorAggregationExpr(
				newRangeAggregationExpr(
					newLogRange(&PipelineExpr{
						Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
						MultiStages: MultiStageExpr{
							newLineFilterExpr(log.LineMatchEqual, "", "bar"),
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
					OpRangeTypeAvg, &Grouping{Without: false, Groups: []string{"namespace", "instance"}}, nil,
				),
				OpTypeAvg,
				&Grouping{Groups: []string{"foo", "bar"}},
				nil,
			),
		),
	},
	{
		in: `
			sum by (foo,bar) (
				quantile_over_time(0.99998,{app="foo"} |= "bar" | json | latency >= 250ms or ( status_code < 500 and status_code > 200)
					| line_format "blip{{ .foo }}blop {{.status_code}}" | label_format foo=bar,status_code="buzz{{.bar}}" | unwrap foo [5m]
								) by (namespace,instance)
					)
					+ ignoring (bar)
					avg(
						avg_over_time({app="foo"} |= "bar" | json | latency >= 250ms or ( status_code < 500 and status_code > 200)
							| line_format "blip{{ .foo }}blop {{.status_code}}" | label_format foo=bar,status_code="buzz{{.bar}}" | unwrap foo [5m]
										) by (namespace,instance)
							) by (foo)
					`,
		exp: mustNewBinOpExpr(OpTypeAdd, &BinOpOptions{ReturnBool: false, VectorMatching: &VectorMatching{Card: CardOneToOne, On: false, MatchingLabels: []string{"bar"}}},
			mustNewVectorAggregationExpr(
				newRangeAggregationExpr(
					newLogRange(&PipelineExpr{
						Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
						MultiStages: MultiStageExpr{
							newLineFilterExpr(log.LineMatchEqual, "", "bar"),
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
					OpRangeTypeQuantile, &Grouping{Without: false, Groups: []string{"namespace", "instance"}}, NewStringLabelFilter("0.99998"),
				),
				OpTypeSum,
				&Grouping{Groups: []string{"foo", "bar"}},
				nil,
			),
			mustNewVectorAggregationExpr(
				newRangeAggregationExpr(
					newLogRange(&PipelineExpr{
						Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
						MultiStages: MultiStageExpr{
							newLineFilterExpr(log.LineMatchEqual, "", "bar"),
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
					OpRangeTypeAvg, &Grouping{Without: false, Groups: []string{"namespace", "instance"}}, nil,
				),
				OpTypeAvg,
				&Grouping{Groups: []string{"foo"}},
				nil,
			),
		),
	},
	{
		in: `
			sum by (foo,bar) (
				quantile_over_time(0.99998,{app="foo"} |= "bar" | json | latency >= 250ms or ( status_code < 500 and status_code > 200)
					| line_format "blip{{ .foo }}blop {{.status_code}}" | label_format foo=bar,status_code="buzz{{.bar}}" | unwrap foo [5m]
								) by (namespace,instance)
					)
					+ on (foo)
					avg(
						avg_over_time({app="foo"} |= "bar" | json | latency >= 250ms or ( status_code < 500 and status_code > 200)
							| line_format "blip{{ .foo }}blop {{.status_code}}" | label_format foo=bar,status_code="buzz{{.bar}}" | unwrap foo [5m]
										) by (namespace,instance)
							) by (foo)
					`,
		exp: mustNewBinOpExpr(OpTypeAdd, &BinOpOptions{ReturnBool: false, VectorMatching: &VectorMatching{Card: CardOneToOne, On: true, MatchingLabels: []string{"foo"}}},
			mustNewVectorAggregationExpr(
				newRangeAggregationExpr(
					newLogRange(&PipelineExpr{
						Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
						MultiStages: MultiStageExpr{
							newLineFilterExpr(log.LineMatchEqual, "", "bar"),
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
					OpRangeTypeQuantile, &Grouping{Without: false, Groups: []string{"namespace", "instance"}}, NewStringLabelFilter("0.99998"),
				),
				OpTypeSum,
				&Grouping{Groups: []string{"foo", "bar"}},
				nil,
			),
			mustNewVectorAggregationExpr(
				newRangeAggregationExpr(
					newLogRange(&PipelineExpr{
						Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
						MultiStages: MultiStageExpr{
							newLineFilterExpr(log.LineMatchEqual, "", "bar"),
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
					OpRangeTypeAvg, &Grouping{Without: false, Groups: []string{"namespace", "instance"}}, nil,
				),
				OpTypeAvg,
				&Grouping{Groups: []string{"foo"}},
				nil,
			),
		),
	},
	{
		in: `
			sum by (foo,bar) (
				quantile_over_time(0.99998,{app="foo"} |= "bar" | json | latency >= 250ms or ( status_code < 500 and status_code > 200)
					| line_format "blip{{ .foo }}blop {{.status_code}}" | label_format foo=bar,status_code="buzz{{.bar}}" | unwrap foo [5m]
								) by (namespace,instance)
					)
					+ ignoring (bar) group_left (foo)
					avg(
						avg_over_time({app="foo"} |= "bar" | json | latency >= 250ms or ( status_code < 500 and status_code > 200)
							| line_format "blip{{ .foo }}blop {{.status_code}}" | label_format foo=bar,status_code="buzz{{.bar}}" | unwrap foo [5m]
										) by (namespace,instance)
							) by (foo)
					`,
		exp: mustNewBinOpExpr(OpTypeAdd, &BinOpOptions{ReturnBool: false, VectorMatching: &VectorMatching{Card: CardManyToOne, Include: []string{"foo"}, On: false, MatchingLabels: []string{"bar"}}},
			mustNewVectorAggregationExpr(
				newRangeAggregationExpr(
					newLogRange(&PipelineExpr{
						Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
						MultiStages: MultiStageExpr{
							newLineFilterExpr(log.LineMatchEqual, "", "bar"),
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
					OpRangeTypeQuantile, &Grouping{Without: false, Groups: []string{"namespace", "instance"}}, NewStringLabelFilter("0.99998"),
				),
				OpTypeSum,
				&Grouping{Groups: []string{"foo", "bar"}},
				nil,
			),
			mustNewVectorAggregationExpr(
				newRangeAggregationExpr(
					newLogRange(&PipelineExpr{
						Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
						MultiStages: MultiStageExpr{
							newLineFilterExpr(log.LineMatchEqual, "", "bar"),
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
					OpRangeTypeAvg, &Grouping{Without: false, Groups: []string{"namespace", "instance"}}, nil,
				),
				OpTypeAvg,
				&Grouping{Groups: []string{"foo"}},
				nil,
			),
		),
	},
	{
		in: `
			sum by (app,machine) (count_over_time({app="foo"}[1m])) > bool on () group_right (app) sum by (app) (count_over_time({app="foo"}[1m]))
					`,
		exp: mustNewBinOpExpr(OpTypeGT, &BinOpOptions{ReturnBool: true, VectorMatching: &VectorMatching{Card: CardOneToMany, Include: []string{"app"}, On: true, MatchingLabels: nil}},
			mustNewVectorAggregationExpr(
				newRangeAggregationExpr(
					&LogRange{
						Left:     newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
						Interval: 1 * time.Minute,
					},
					OpRangeTypeCount, nil, nil,
				),
				OpTypeSum,
				&Grouping{Groups: []string{"app", "machine"}},
				nil,
			),
			mustNewVectorAggregationExpr(
				newRangeAggregationExpr(
					&LogRange{
						Left:     newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
						Interval: 1 * time.Minute,
					},
					OpRangeTypeCount, nil, nil,
				),
				OpTypeSum,
				&Grouping{Groups: []string{"app"}},
				nil,
			),
		),
	},
	{
		in: `
			sum by (app,machine) (count_over_time({app="foo"}[1m])) > bool on () group_right sum by (app) (count_over_time({app="foo"}[1m]))
					`,
		exp: mustNewBinOpExpr(OpTypeGT, &BinOpOptions{ReturnBool: true, VectorMatching: &VectorMatching{Card: CardOneToMany, Include: nil, On: true, MatchingLabels: nil}},
			mustNewVectorAggregationExpr(
				newRangeAggregationExpr(
					&LogRange{
						Left:     newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
						Interval: 1 * time.Minute,
					},
					OpRangeTypeCount, nil, nil,
				),
				OpTypeSum,
				&Grouping{Groups: []string{"app", "machine"}},
				nil,
			),
			mustNewVectorAggregationExpr(
				newRangeAggregationExpr(
					&LogRange{
						Left:     newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
						Interval: 1 * time.Minute,
					},
					OpRangeTypeCount, nil, nil,
				),
				OpTypeSum,
				&Grouping{Groups: []string{"app"}},
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
			mustNewBinOpExpr(OpTypeAdd, &BinOpOptions{VectorMatching: &VectorMatching{Card: CardOneToOne}, ReturnBool: false},
				mustNewVectorAggregationExpr(
					newRangeAggregationExpr(
						newLogRange(&PipelineExpr{
							Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
							MultiStages: MultiStageExpr{
								newLineFilterExpr(log.LineMatchEqual, "", "bar"),
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
						OpRangeTypeQuantile, &Grouping{Without: false, Groups: []string{"namespace", "instance"}}, NewStringLabelFilter("0.99998"),
					),
					OpTypeSum,
					&Grouping{Groups: []string{"foo", "bar"}},
					nil,
				),
				mustNewVectorAggregationExpr(
					newRangeAggregationExpr(
						newLogRange(&PipelineExpr{
							Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
							MultiStages: MultiStageExpr{
								newLineFilterExpr(log.LineMatchEqual, "", "bar"),
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
						OpRangeTypeAvg, &Grouping{Without: false, Groups: []string{"namespace", "instance"}}, nil,
					),
					OpTypeAvg,
					&Grouping{Groups: []string{"foo", "bar"}},
					nil,
				),
			),
			"foo", "$1", "svc", "(.*)",
		),
	},
	{
		// ensure binary ops with two literals are reduced recursively
		in:  `1 + 1 + 1`,
		exp: &LiteralExpr{Val: 3},
	},
	{
		// ensure binary ops with two literals are reduced when comparisons are used
		in:  `1 == 1`,
		exp: &LiteralExpr{Val: 1},
	},
	{
		// ensure binary ops with two literals are reduced when comparisons are used
		in:  `1 != 1`,
		exp: &LiteralExpr{Val: 0},
	},
	{
		// ensure binary ops with two literals are reduced when comparisons are used
		in:  `1 > 1`,
		exp: &LiteralExpr{Val: 0},
	},
	{
		// ensure binary ops with two literals are reduced when comparisons are used
		in:  `1 >= 1`,
		exp: &LiteralExpr{Val: 1},
	},
	{
		// ensure binary ops with two literals are reduced when comparisons are used
		in:  `1 < 1`,
		exp: &LiteralExpr{Val: 0},
	},
	{
		// ensure binary ops with two literals are reduced when comparisons are used
		in:  `1 <= 1`,
		exp: &LiteralExpr{Val: 1},
	},
	{
		// ensure binary ops with two literals are reduced recursively when comparisons are used
		in:  `1 >= 1 > 1`,
		exp: &LiteralExpr{Val: 0},
	},
	{
		in:  `{foo="bar"} + {foo="bar"}`,
		err: logqlmodel.NewParseError(`unexpected type for left leg of binary operation (+): *syntax.MatchersExpr`, 0, 0),
	},
	{
		in:  `sum(count_over_time({foo="bar"}[5m])) by (foo) - {foo="bar"}`,
		err: logqlmodel.NewParseError(`unexpected type for right leg of binary operation (-): *syntax.MatchersExpr`, 0, 0),
	},
	{
		in:  `{foo="bar"} / sum(count_over_time({foo="bar"}[5m])) by (foo)`,
		err: logqlmodel.NewParseError(`unexpected type for left leg of binary operation (/): *syntax.MatchersExpr`, 0, 0),
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
			Op: OpTypeGT,
			Opts: &BinOpOptions{
				ReturnBool:     false,
				VectorMatching: &VectorMatching{Card: CardOneToOne},
			},
			SampleExpr: &RangeAggregationExpr{
				Left: &LogRange{
					Left:     &MatchersExpr{Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
					Interval: 12 * time.Minute,
				},
				Operation: "count_over_time",
			},
			RHS: &RangeAggregationExpr{
				Left: &LogRange{
					Left:     &MatchersExpr{Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
					Interval: 12 * time.Minute,
				},
				Operation: "count_over_time",
			},
		},
	},
	{
		in: `count_over_time({ foo = "bar" }[12m]) > 1`,
		exp: &BinOpExpr{
			Op: OpTypeGT,
			Opts: &BinOpOptions{
				ReturnBool:     false,
				VectorMatching: &VectorMatching{Card: CardOneToOne},
			},
			SampleExpr: &RangeAggregationExpr{
				Left: &LogRange{
					Left:     &MatchersExpr{Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
					Interval: 12 * time.Minute,
				},
				Operation: "count_over_time",
			},
			RHS: &LiteralExpr{Val: 1},
		},
	},
	{
		// cannot compare metric & log queries
		in:  `count_over_time({ foo = "bar" }[12m]) > { foo = "bar" }`,
		err: logqlmodel.NewParseError("unexpected type for right leg of binary operation (>): *syntax.MatchersExpr", 0, 0),
	},
	{
		in: `count_over_time({ foo = "bar" }[12m]) or count_over_time({ foo = "bar" }[12m]) > 1`,
		exp: &BinOpExpr{
			Op: OpTypeOr,
			Opts: &BinOpOptions{
				ReturnBool:     false,
				VectorMatching: &VectorMatching{},
			},
			SampleExpr: &RangeAggregationExpr{
				Left: &LogRange{
					Left:     &MatchersExpr{Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
					Interval: 12 * time.Minute,
				},
				Operation: "count_over_time",
			},
			RHS: &BinOpExpr{
				Op: OpTypeGT,
				Opts: &BinOpOptions{
					ReturnBool:     false,
					VectorMatching: &VectorMatching{Card: CardOneToOne},
				},
				SampleExpr: &RangeAggregationExpr{
					Left: &LogRange{
						Left:     &MatchersExpr{Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
						Interval: 12 * time.Minute,
					},
					Operation: "count_over_time",
				},
				RHS: &LiteralExpr{Val: 1},
			},
		},
	},
	{
		// test associativity
		in:  `1 > 1 < 1`,
		exp: &LiteralExpr{Val: 1},
	},
	{
		// bool modifiers are reduced-away between two literal legs
		in:  `1 > 1 > bool 1`,
		exp: &LiteralExpr{Val: 0},
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
		in:  `vector(abc)`,
		err: logqlmodel.NewParseError("syntax error: unexpected IDENTIFIER, expecting NUMBER", 1, 8),
	},
	{
		in:  `vector(1)`,
		exp: &VectorExpr{Val: 1, err: nil},
	},
	{
		in:  `label_replace(vector(0), "foo", "bar", "", "")`,
		exp: mustNewLabelReplaceExpr(&VectorExpr{Val: 0, err: nil}, "foo", "bar", "", ""),
	},
	{
		in: `sum(vector(0))`,
		exp: &VectorAggregationExpr{
			Left:      &VectorExpr{Val: 0, err: nil},
			Grouping:  &Grouping{},
			Params:    0,
			Operation: "sum",
		},
	},
	{
		in: `{app="foo"}
					# |= "bar"
					| json`,
		exp: &PipelineExpr{
			Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
			MultiStages: MultiStageExpr{
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
			Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
			MultiStages: MultiStageExpr{
				newLineFilterExpr(log.LineMatchEqual, "", "bar"),
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
			Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
			MultiStages: MultiStageExpr{
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
			Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
			MultiStages: MultiStageExpr{
				newLineFilterExpr(log.LineMatchEqual, "", "#"),
			},
		},
	},
	{
		in: `{app="foo"} | bar="#"`,
		exp: &PipelineExpr{
			Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
			MultiStages: MultiStageExpr{
				&LabelFilterExpr{
					LabelFilterer: log.NewStringLabelFilter(mustNewMatcher(labels.MatchEqual, "bar", "#")),
				},
			},
		},
	},
	{
		in: `{app="foo"} | json bob="top.sub[\"index\"]"`,
		exp: &PipelineExpr{
			Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
			MultiStages: MultiStageExpr{
				newJSONExpressionParser([]log.LabelExtractionExpr{
					log.NewLabelExtractionExpr("bob", `top.sub["index"]`),
				}),
			},
		},
	},
	{
		in: `{app="foo"} | json bob="top.params[0]"`,
		exp: &PipelineExpr{
			Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
			MultiStages: MultiStageExpr{
				newJSONExpressionParser([]log.LabelExtractionExpr{
					log.NewLabelExtractionExpr("bob", `top.params[0]`),
				}),
			},
		},
	},
	{
		in: `{app="foo"} | json response_code="response.code", api_key="request.headers[\"X-API-KEY\"]"`,
		exp: &PipelineExpr{
			Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
			MultiStages: MultiStageExpr{
				newJSONExpressionParser([]log.LabelExtractionExpr{
					log.NewLabelExtractionExpr("response_code", `response.code`),
					log.NewLabelExtractionExpr("api_key", `request.headers["X-API-KEY"]`),
				}),
			},
		},
	},
	{
		in: `{app="foo"} | json response_code, api_key="request.headers[\"X-API-KEY\"]", layer7_something_specific="layer7_something_specific"`,
		exp: &PipelineExpr{
			Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
			MultiStages: MultiStageExpr{
				newJSONExpressionParser([]log.LabelExtractionExpr{
					log.NewLabelExtractionExpr("response_code", `response_code`),
					log.NewLabelExtractionExpr("api_key", `request.headers["X-API-KEY"]`),
					log.NewLabelExtractionExpr("layer7_something_specific", `layer7_something_specific`),
				}),
			},
		},
	},
	{
		in: `count_over_time({ foo ="bar" } | json layer7_something_specific="layer7_something_specific" [12m])`,
		exp: &RangeAggregationExpr{
			Left: &LogRange{
				Left: &PipelineExpr{
					MultiStages: MultiStageExpr{
						newJSONExpressionParser([]log.LabelExtractionExpr{
							log.NewLabelExtractionExpr("layer7_something_specific", `layer7_something_specific`),
						}),
					},
					Left: &MatchersExpr{Mts: []*labels.Matcher{mustNewMatcher(labels.MatchEqual, "foo", "bar")}},
				},
				Interval: 12 * time.Minute,
			},
			Operation: "count_over_time",
		},
	},
	{
		// binop always includes vector matching. Default is `without ()`,
		// the zero value.
		in: `
			sum(count_over_time({foo="bar"}[5m])) or vector(1)
			`,
		exp: mustNewBinOpExpr(
			OpTypeOr,
			&BinOpOptions{
				VectorMatching: &VectorMatching{Card: CardOneToOne},
			},
			mustNewVectorAggregationExpr(newRangeAggregationExpr(
				&LogRange{
					Left: &MatchersExpr{
						Mts: []*labels.Matcher{
							mustNewMatcher(labels.MatchEqual, "foo", "bar"),
						},
					},
					Interval: 5 * time.Minute,
				}, OpRangeTypeCount, nil, nil),
				"sum",
				&Grouping{},
				nil,
			),
			NewVectorExpr("1"),
		),
	},
	{
		in: `{app="foo"} | logfmt message="msg"`,
		exp: &PipelineExpr{
			Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
			MultiStages: MultiStageExpr{
				newLogfmtExpressionParser([]log.LabelExtractionExpr{
					log.NewLabelExtractionExpr("message", `msg`),
				}, nil),
			},
		},
	},
	{
		in: `{app="foo"} | logfmt msg`,
		exp: &PipelineExpr{
			Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
			MultiStages: MultiStageExpr{
				newLogfmtExpressionParser([]log.LabelExtractionExpr{
					log.NewLabelExtractionExpr("msg", `msg`),
				}, nil),
			},
		},
	},
	{
		in: `{app="foo"} | logfmt --strict msg`,
		exp: &PipelineExpr{
			Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
			MultiStages: MultiStageExpr{
				newLogfmtExpressionParser([]log.LabelExtractionExpr{
					log.NewLabelExtractionExpr("msg", `msg`),
				}, []string{OpStrict}),
			},
		},
	},
	{
		in: `{app="foo"} | logfmt --keep-empty msg, err `,
		exp: &PipelineExpr{
			Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
			MultiStages: MultiStageExpr{
				newLogfmtExpressionParser([]log.LabelExtractionExpr{
					log.NewLabelExtractionExpr("msg", `msg`),
					log.NewLabelExtractionExpr("err", `err`),
				}, []string{OpKeepEmpty}),
			},
		},
	},
	{
		in: `{app="foo"} | logfmt msg, err="error"`,
		exp: &PipelineExpr{
			Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
			MultiStages: MultiStageExpr{
				newLogfmtExpressionParser([]log.LabelExtractionExpr{
					log.NewLabelExtractionExpr("msg", `msg`),
					log.NewLabelExtractionExpr("err", `error`),
				}, nil),
			},
		},
	},
	{
		in: `{app="foo"} | logfmt --strict --keep-empty msg="message", apiKey="api_key"`,
		exp: &PipelineExpr{
			Left: newMatcherExpr([]*labels.Matcher{{Type: labels.MatchEqual, Name: "app", Value: "foo"}}),
			MultiStages: MultiStageExpr{
				newLogfmtExpressionParser([]log.LabelExtractionExpr{
					log.NewLabelExtractionExpr("msg", `message`),
					log.NewLabelExtractionExpr("apiKey", `api_key`),
				}, []string{OpStrict, OpKeepEmpty}),
			},
		},
	},
	{
		in: `{app="foo"} |= "foo" or "bar" |= "buzz" or "fizz"`,
		exp: &PipelineExpr{
			Left: newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "app", "foo")}),
			MultiStages: MultiStageExpr{
				&LineFilterExpr{
					Left: newOrLineFilter(
						&LineFilterExpr{
							LineFilter: LineFilter{
								Ty:    log.LineMatchEqual,
								Match: "foo",
							},
						},
						&LineFilterExpr{
							LineFilter: LineFilter{
								Ty:    log.LineMatchEqual,
								Match: "bar",
							},
						}),
					LineFilter: LineFilter{
						Ty:    log.LineMatchEqual,
						Match: "buzz",
					},
					Or: &LineFilterExpr{
						LineFilter: LineFilter{
							Ty:    log.LineMatchEqual,
							Match: "fizz",
						},
						IsOrChild: true,
					},
					IsOrChild: false,
				},
			},
		},
	},
	{
		in: `{app="foo"} |= "foo" or "bar" or "baz"`,
		exp: &PipelineExpr{
			Left: newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "app", "foo")}),
			MultiStages: MultiStageExpr{
				&LineFilterExpr{
					LineFilter: LineFilter{
						Ty:    log.LineMatchEqual,
						Match: "foo",
					},
					Or: newOrLineFilter(
						&LineFilterExpr{
							LineFilter: LineFilter{
								Ty:    log.LineMatchEqual,
								Match: "bar",
							},
							IsOrChild: true,
						},
						&LineFilterExpr{
							LineFilter: LineFilter{
								Ty:    log.LineMatchEqual,
								Match: "baz",
							},
							IsOrChild: true,
						}),
					IsOrChild: false,
				},
			},
		},
	},
	{
		in: `{app="foo"} |> "foo" or "bar" or "baz"`,
		exp: &PipelineExpr{
			Left: newMatcherExpr([]*labels.Matcher{mustNewMatcher(labels.MatchEqual, "app", "foo")}),
			MultiStages: MultiStageExpr{
				&LineFilterExpr{
					LineFilter: LineFilter{
						Ty:    log.LineMatchPattern,
						Match: "foo",
					},
					Or: newOrLineFilter(
						&LineFilterExpr{
							LineFilter: LineFilter{
								Ty:    log.LineMatchPattern,
								Match: "bar",
							},
							IsOrChild: true,
						},
						&LineFilterExpr{
							LineFilter: LineFilter{
								Ty:    log.LineMatchPattern,
								Match: "baz",
							},
							IsOrChild: true,
						}),
					IsOrChild: false,
				},
			},
		},
	},
}

func TestParse(t *testing.T) {
	for _, tc := range ParseTestCases {
		t.Run(tc.in, func(t *testing.T) {
			ast, err := ParseExpr(tc.in)
			require.Equal(t, tc.err, err)
			AssertExpressions(t, tc.exp, ast)
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
			got, err := ParseMatchers(tt.input, true)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseMatchers() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.want == nil {
				require.Nil(t, got)
			} else {
				AssertMatchers(t, tt.want, got)
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
	sp := p.ForStream(labels.EmptyLabels())
	line, lbs, matches := sp.Process(0, []byte(`level=debug ts=2020-10-02T10:10:42.092268913Z caller=logging.go:66 traceID=a9d4d8a928d8db1 msg="POST /api/prom/api/v1/query_range (200) 1.5s"`))
	require.True(t, matches)
	require.Equal(
		t,
		labels.FromStrings("caller", "logging.go:66", "duration", "1.5s", "level", "debug", "method", "POST", "msg", "POST /api/prom/api/v1/query_range (200) 1.5s", "path", "/api/prom/api/v1/query_range", "status", "200", "traceID", "a9d4d8a928d8db1", "ts", "2020-10-02T10:10:42.092268913Z"),
		lbs.Labels(),
	)
	require.Equal(t, string([]byte(`1.5s|POST|200`)), string(line))
}

func Benchmark_PipelineCombined(b *testing.B) {
	query := `{job="cortex-ops/query-frontend"} |= "logging.go" | logfmt | line_format "{{.msg}}" | regexp "(?P<method>\\w+) (?P<path>[\\w|/]+) \\((?P<status>\\d+?)\\) (?P<duration>.*)" | (duration > 1s or status==200) and method="POST" | line_format "{{.duration}}|{{.method}}|{{.status}}"`

	expr, err := ParseLogSelector(query, true)
	require.Nil(b, err)

	p, err := expr.Pipeline()
	require.Nil(b, err)
	sp := p.ForStream(labels.EmptyLabels())
	var (
		line    []byte
		lbs     log.LabelsResult
		matches bool
	)
	in := []byte(`level=debug ts=2020-10-02T10:10:42.092268913Z caller=logging.go:66 traceID=a9d4d8a928d8db1 msg="POST /api/prom/api/v1/query_range (200) 1.5s"`)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		line, lbs, matches = sp.Process(0, in)
	}
	require.True(b, matches)
	require.Equal(
		b,
		labels.FromStrings("caller", "logging.go:66", "duration", "1.5s", "level", "debug", "method", "POST", "msg", "POST /api/prom/api/v1/query_range (200) 1.5s", "path", "/api/prom/api/v1/query_range", "status", "200", "traceID", "a9d4d8a928d8db1", "ts", "2020-10-02T10:10:42.092268913Z"),
		lbs.Labels(),
	)
	require.Equal(b, string([]byte(`1.5s|POST|200`)), string(line))
}

func Benchmark_MetricPipelineCombined(b *testing.B) {
	query := `count_over_time({job="cortex-ops/query-frontend"} |= "logging.go" | logfmt | line_format "{{.msg}}" | regexp "(?P<method>\\w+) (?P<path>[\\w|/]+) \\((?P<status>\\d+?)\\) (?P<duration>.*)" | (duration > 1s or status==200) and method="POST" | line_format "{{.duration}}|{{.method}}|{{.status}}"[1m])`

	expr, err := ParseSampleExpr(query)
	require.Nil(b, err)

	p, err := expr.Extractor()
	require.Nil(b, err)
	sp := p.ForStream(labels.EmptyLabels())
	var (
		v       float64
		lbs     log.LabelsResult
		matches bool
	)
	in := []byte(`level=debug ts=2020-10-02T10:10:42.092268913Z caller=logging.go:66 traceID=a9d4d8a928d8db1 msg="POST /api/prom/api/v1/query_range (200) 1.5s"`)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v, lbs, matches = sp.Process(0, in)
	}
	require.True(b, matches)
	require.Equal(
		b,
		labels.FromStrings("caller", "logging.go:66", "duration", "1.5s", "level", "debug", "method", "POST", "msg", "POST /api/prom/api/v1/query_range (200) 1.5s", "path", "/api/prom/api/v1/query_range", "status", "200", "traceID", "a9d4d8a928d8db1", "ts", "2020-10-02T10:10:42.092268913Z"),
		lbs.Labels(),
	)
	require.Equal(b, 1.0, v)
}

var c []*labels.Matcher

func Benchmark_ParseMatchers(b *testing.B) {
	s := `{cpu="10",endpoint="https",instance="10.253.57.87:9100",job="node-exporter",mode="idle",namespace="observability",pod="node-exporter-l454v",service="node-exporter"}`
	var err error
	for n := 0; n < b.N; n++ {
		c, err = ParseMatchers(s, true)
		require.NoError(b, err)
	}
}

var lbs labels.Labels

func Benchmark_CompareParseLabels(b *testing.B) {
	s := `{cpu="10",endpoint="https",instance="10.253.57.87:9100",job="node-exporter",mode="idle",namespace="observability",pod="node-exporter-l454v",service="node-exporter"}`
	var err error
	b.Run("logql", func(b *testing.B) {
		for n := 0; n < b.N; n++ {
			c, err = ParseMatchers(s, true)
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
		{
			in:  `count without (rate({namespace="apps"}[15s]))`,
			err: logqlmodel.NewParseError("syntax error: unexpected RATE, expecting IDENTIFIER or )", 1, 16),
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

func TestParseLabels(t *testing.T) {
	for _, tc := range []struct {
		desc   string
		input  string
		output labels.Labels
	}{
		{
			desc:   "basic",
			input:  `{job="foo"}`,
			output: labels.FromStrings("job", "foo"),
		},
		{
			desc:   "strip empty label value",
			input:  `{job="foo", bar=""}`,
			output: labels.FromStrings("job", "foo"),
		},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			got, _ := ParseLabels(tc.input)
			require.Equal(t, tc.output, got)
		})
	}
}

func TestNoOpLabelToString(t *testing.T) {
	logExpr := `{container_name="app"} | foo=~".*"`
	l, err := ParseLogSelector(logExpr, false)
	require.NoError(t, err)
	require.Equal(t, `{container_name="app"} | foo=~".*"`, l.String())

	stages, err := l.(*PipelineExpr).MultiStages.stages()
	require.NoError(t, err)
	require.Len(t, stages, 0)
}

func TestParseSampleExpr_String(t *testing.T) {
	t.Run("it doesn't add escape characters when after getting parsed", func(t *testing.T) {
		query := `sum(rate({cluster="beep", namespace="boop"} | msg=~` + "`" + `.*?(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS) /loki/api/(?i)(\d+[a-z]|[a-z]+\d)\w*/query_range` + "`" + `[1d]))`
		expr, err := ParseSampleExpr(query)
		require.NoError(t, err)

		require.Equal(t, query, expr.String())
	})

	t.Run("it removes escape characters after being parsed", func(t *testing.T) {
		query := `{cluster="beep", namespace="boop"} | msg=~"\\w.*"`
		expr, err := ParseExpr(query)
		require.NoError(t, err)

		// escaping is hard: the result is {cluster="beep", namespace="boop"} | msg=~`\w.*` which is equivalent to the original
		require.Equal(t, "{cluster=\"beep\", namespace=\"boop\"} | msg=~`\\w.*`", expr.String())
	})
}
