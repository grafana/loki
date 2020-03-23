package logql

import (
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/querier/astmapper"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/stretchr/testify/require"
)

func TestMapSampleExpr(t *testing.T) {
	m, err := NewShardMapper(2)
	require.Nil(t, err)

	for _, tc := range []struct {
		in  SampleExpr
		out SampleExpr
	}{
		{
			in: &rangeAggregationExpr{
				operation: OpTypeRate,
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
				SampleExpr: DownstreamSampleExpr{
					shard: &astmapper.ShardAnnotation{
						Shard: 0,
						Of:    2,
					},
					SampleExpr: &rangeAggregationExpr{
						operation: OpTypeRate,
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
					SampleExpr: DownstreamSampleExpr{
						shard: &astmapper.ShardAnnotation{
							Shard: 1,
							Of:    2,
						},
						SampleExpr: &rangeAggregationExpr{
							operation: OpTypeRate,
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
			require.Equal(t, tc.out, m.mapSampleExpr(tc.in))
		})

	}
}

func TestMapping(t *testing.T) {
	m, err := NewShardMapper(2)
	require.Nil(t, err)

	for _, tc := range []struct {
		in   string
		expr Expr
		err  error
	}{
		{
			in: `{foo="bar"}`,
			expr: &ConcatLogSelectorExpr{
				LogSelectorExpr: DownstreamLogSelectorExpr{
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
					LogSelectorExpr: DownstreamLogSelectorExpr{
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
				LogSelectorExpr: DownstreamLogSelectorExpr{
					shard: &astmapper.ShardAnnotation{
						Shard: 0,
						Of:    2,
					},
					LogSelectorExpr: &filterExpr{
						match: "error",
						ty:    labels.MatchEqual,
						left: &matchersExpr{
							matchers: []*labels.Matcher{
								mustNewMatcher(labels.MatchEqual, "foo", "bar"),
							},
						},
					},
				},
				next: &ConcatLogSelectorExpr{
					LogSelectorExpr: DownstreamLogSelectorExpr{
						shard: &astmapper.ShardAnnotation{
							Shard: 1,
							Of:    2,
						},
						LogSelectorExpr: &filterExpr{
							match: "error",
							ty:    labels.MatchEqual,
							left: &matchersExpr{
								matchers: []*labels.Matcher{
									mustNewMatcher(labels.MatchEqual, "foo", "bar"),
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
				SampleExpr: DownstreamSampleExpr{
					shard: &astmapper.ShardAnnotation{
						Shard: 0,
						Of:    2,
					},
					SampleExpr: &rangeAggregationExpr{
						operation: OpTypeRate,
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
					SampleExpr: DownstreamSampleExpr{
						shard: &astmapper.ShardAnnotation{
							Shard: 1,
							Of:    2,
						},
						SampleExpr: &rangeAggregationExpr{
							operation: OpTypeRate,
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
	} {
		t.Run(tc.in, func(t *testing.T) {
			ast, err := ParseExpr(tc.in)
			require.Equal(t, tc.err, err)

			mapped, err := m.Map(ast)
			require.Equal(t, tc.err, err)
			require.Equal(t, tc.expr, mapped)
		})
	}
}
