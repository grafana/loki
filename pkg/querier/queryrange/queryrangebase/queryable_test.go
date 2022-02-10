package queryrangebase

import (
	"context"
	"testing"

	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/querier/astmapper"
)

func TestSelect(t *testing.T) {
	var testExpr = []struct {
		name    string
		querier *ShardedQuerier
		fn      func(*testing.T, *ShardedQuerier)
	}{
		{
			name: "errors non embedded query",
			querier: mkQuerier(
				nil,
			),
			fn: func(t *testing.T, q *ShardedQuerier) {
				set := q.Select(false, nil)
				require.EqualError(t, set.Err(), nonEmbeddedErrMsg)
			},
		},
		{
			name: "replaces query",
			querier: mkQuerier(mockHandlerWith(
				&PrometheusResponse{},
				nil,
			)),
			fn: func(t *testing.T, q *ShardedQuerier) {

				expected := &PrometheusResponse{
					Status: "success",
					Data: PrometheusData{
						ResultType: string(parser.ValueTypeVector),
					},
				}

				// override handler func to assert new query has been substituted
				q.Handler = HandlerFunc(
					func(ctx context.Context, req Request) (Response, error) {
						require.Equal(t, `http_requests_total{cluster="prod"}`, req.GetQuery())
						return expected, nil
					},
				)

				encoded, err := astmapper.JSONCodec.Encode([]string{`http_requests_total{cluster="prod"}`})
				require.Nil(t, err)
				set := q.Select(
					false,
					nil,
					exactMatch("__name__", astmapper.EmbeddedQueriesMetricName),
					exactMatch(astmapper.QueryLabel, encoded),
				)
				require.Nil(t, set.Err())
			},
		},
		{
			name: "propagates response error",
			querier: mkQuerier(mockHandlerWith(
				&PrometheusResponse{
					Error: "SomeErr",
				},
				nil,
			)),
			fn: func(t *testing.T, q *ShardedQuerier) {
				encoded, err := astmapper.JSONCodec.Encode([]string{`http_requests_total{cluster="prod"}`})
				require.Nil(t, err)
				set := q.Select(
					false,
					nil,
					exactMatch("__name__", astmapper.EmbeddedQueriesMetricName),
					exactMatch(astmapper.QueryLabel, encoded),
				)
				require.EqualError(t, set.Err(), "SomeErr")
			},
		},
		{
			name: "returns SeriesSet",
			querier: mkQuerier(mockHandlerWith(
				&PrometheusResponse{
					Data: PrometheusData{
						ResultType: string(parser.ValueTypeVector),
						Result: []SampleStream{
							{
								Labels: []logproto.LabelAdapter{
									{Name: "a", Value: "a1"},
									{Name: "b", Value: "b1"},
								},
								Samples: []logproto.LegacySample{
									{
										Value:       1,
										TimestampMs: 1,
									},
									{
										Value:       2,
										TimestampMs: 2,
									},
								},
							},
							{
								Labels: []logproto.LabelAdapter{
									{Name: "a", Value: "a1"},
									{Name: "b", Value: "b1"},
								},
								Samples: []logproto.LegacySample{
									{
										Value:       8,
										TimestampMs: 1,
									},
									{
										Value:       9,
										TimestampMs: 2,
									},
								},
							},
						},
					},
				},
				nil,
			)),
			fn: func(t *testing.T, q *ShardedQuerier) {
				encoded, err := astmapper.JSONCodec.Encode([]string{`http_requests_total{cluster="prod"}`})
				require.Nil(t, err)
				set := q.Select(
					false,
					nil,
					exactMatch("__name__", astmapper.EmbeddedQueriesMetricName),
					exactMatch(astmapper.QueryLabel, encoded),
				)
				require.Nil(t, set.Err())
				require.Equal(
					t,
					NewSeriesSet([]SampleStream{
						{
							Labels: []logproto.LabelAdapter{
								{Name: "a", Value: "a1"},
								{Name: "b", Value: "b1"},
							},
							Samples: []logproto.LegacySample{
								{
									Value:       1,
									TimestampMs: 1,
								},
								{
									Value:       2,
									TimestampMs: 2,
								},
							},
						},
						{
							Labels: []logproto.LabelAdapter{
								{Name: "a", Value: "a1"},
								{Name: "b", Value: "b1"},
							},
							Samples: []logproto.LegacySample{
								{
									Value:       8,
									TimestampMs: 1,
								},
								{
									Value:       9,
									TimestampMs: 2,
								},
							},
						},
					}),
					set,
				)
			},
		},
	}

	for _, c := range testExpr {
		t.Run(c.name, func(t *testing.T) {
			c.fn(t, c.querier)
		})
	}
}

func TestSelectConcurrent(t *testing.T) {
	for _, c := range []struct {
		name    string
		queries []string
		err     error
	}{
		{
			name: "concats queries",
			queries: []string{
				`sum by(__cortex_shard__) (rate(bar1{__cortex_shard__="0_of_3",baz="blip"}[1m]))`,
				`sum by(__cortex_shard__) (rate(bar1{__cortex_shard__="1_of_3",baz="blip"}[1m]))`,
				`sum by(__cortex_shard__) (rate(bar1{__cortex_shard__="2_of_3",baz="blip"}[1m]))`,
			},
			err: nil,
		},
		{
			name: "errors",
			queries: []string{
				`sum by(__cortex_shard__) (rate(bar1{__cortex_shard__="0_of_3",baz="blip"}[1m]))`,
				`sum by(__cortex_shard__) (rate(bar1{__cortex_shard__="1_of_3",baz="blip"}[1m]))`,
				`sum by(__cortex_shard__) (rate(bar1{__cortex_shard__="2_of_3",baz="blip"}[1m]))`,
			},
			err: errors.Errorf("some-err"),
		},
	} {

		t.Run(c.name, func(t *testing.T) {
			// each request will return a single samplestream
			querier := mkQuerier(mockHandlerWith(&PrometheusResponse{
				Data: PrometheusData{
					ResultType: string(parser.ValueTypeVector),
					Result: []SampleStream{
						{
							Labels: []logproto.LabelAdapter{
								{Name: "a", Value: "1"},
							},
							Samples: []logproto.LegacySample{
								{
									Value:       1,
									TimestampMs: 1,
								},
							},
						},
					},
				},
			}, c.err))

			encoded, err := astmapper.JSONCodec.Encode(c.queries)
			require.Nil(t, err)
			set := querier.Select(
				false,
				nil,
				exactMatch("__name__", astmapper.EmbeddedQueriesMetricName),
				exactMatch(astmapper.QueryLabel, encoded),
			)

			if c.err != nil {
				require.EqualError(t, set.Err(), c.err.Error())
				return
			}

			var ct int
			for set.Next() {
				ct++
			}
			require.Equal(t, len(c.queries), ct)
		})
	}
}

func exactMatch(k, v string) *labels.Matcher {
	m, err := labels.NewMatcher(labels.MatchEqual, k, v)
	if err != nil {
		panic(err)
	}
	return m

}

func mkQuerier(handler Handler) *ShardedQuerier {
	return &ShardedQuerier{Ctx: context.Background(), Req: &PrometheusRequest{}, Handler: handler, ResponseHeaders: map[string][]string{}}
}
