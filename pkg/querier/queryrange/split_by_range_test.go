package queryrange

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/syntax"
	"github.com/grafana/loki/pkg/querier/plan"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
)

func Test_RangeVectorSplit(t *testing.T) {
	srm := NewSplitByRangeMiddleware(log.NewNopLogger(), testEngineOpts, fakeLimits{
		maxSeries:    10000,
		queryTimeout: time.Second,
		splitDuration: map[string]time.Duration{
			"tenant": time.Minute,
		},
	}, nilShardingMetrics)

	ctx := user.InjectOrgID(context.TODO(), "tenant")

	for _, tc := range []struct {
		in         queryrangebase.Request
		subQueries []queryrangebase.RequestResponse
		expected   queryrangebase.Response
	}{
		{
			in: &LokiInstantRequest{
				Query:  `sum(bytes_over_time({app="foo"}[3m]))`,
				TimeTs: time.Unix(1, 0),
				Path:   "/loki/api/v1/query",
				Plan: &plan.QueryPlan{
					AST: syntax.MustParseExpr(`sum(bytes_over_time({app="foo"}[3m]))`),
				},
			},
			subQueries: []queryrangebase.RequestResponse{
				subQueryRequestResponse(`sum(bytes_over_time({app="foo"}[1m]))`, 1, 0),
				subQueryRequestResponse(`sum(bytes_over_time({app="foo"}[1m]))`, 2, 1*time.Minute),
				subQueryRequestResponse(`sum(bytes_over_time({app="foo"}[1m]))`, 3, 2*time.Minute),
			},
			expected: expectedMergedResponse(1 + 2 + 3),
		},
		{
			in: &LokiInstantRequest{
				Query:  `sum by (bar) (bytes_over_time({app="foo"}[3m]))`,
				TimeTs: time.Unix(1, 0),
				Path:   "/loki/api/v1/query",
				Plan: &plan.QueryPlan{
					AST: syntax.MustParseExpr(`sum by (bar) (bytes_over_time({app="foo"}[3m]))`),
				},
			},
			subQueries: []queryrangebase.RequestResponse{
				subQueryRequestResponse(`sum by (bar)(bytes_over_time({app="foo"}[1m]))`, 10, 0),
				subQueryRequestResponse(`sum by (bar)(bytes_over_time({app="foo"}[1m]))`, 20, 1*time.Minute),
				subQueryRequestResponse(`sum by (bar)(bytes_over_time({app="foo"}[1m]))`, 30, 2*time.Minute),
			},
			expected: expectedMergedResponse(10 + 20 + 30),
		},
		{
			in: &LokiInstantRequest{
				Query:  `sum(count_over_time({app="foo"}[3m]))`,
				TimeTs: time.Unix(1, 0),
				Path:   "/loki/api/v1/query",
				Plan: &plan.QueryPlan{
					AST: syntax.MustParseExpr(`sum(count_over_time({app="foo"}[3m]))`),
				},
			},
			subQueries: []queryrangebase.RequestResponse{
				subQueryRequestResponse(`sum(count_over_time({app="foo"}[1m]))`, 1, 0),
				subQueryRequestResponse(`sum(count_over_time({app="foo"}[1m]))`, 1, 1*time.Minute),
				subQueryRequestResponse(`sum(count_over_time({app="foo"}[1m]))`, 1, 2*time.Minute),
			},
			expected: expectedMergedResponse(1 + 1 + 1),
		},
		{
			in: &LokiInstantRequest{
				Query:  `sum by (bar) (count_over_time({app="foo"}[3m]))`,
				TimeTs: time.Unix(1, 0),
				Path:   "/loki/api/v1/query",
				Plan: &plan.QueryPlan{
					AST: syntax.MustParseExpr(`sum by (bar) (count_over_time({app="foo"}[3m]))`),
				},
			},
			subQueries: []queryrangebase.RequestResponse{
				subQueryRequestResponse(`sum by (bar)(count_over_time({app="foo"}[1m]))`, 0, 0),
				subQueryRequestResponse(`sum by (bar)(count_over_time({app="foo"}[1m]))`, 0, 1*time.Minute),
				subQueryRequestResponse(`sum by (bar)(count_over_time({app="foo"}[1m]))`, 0, 2*time.Minute),
			},
			expected: expectedMergedResponse(0 + 0 + 0),
		},
		{
			in: &LokiInstantRequest{
				Query:  `sum(sum_over_time({app="foo"} | unwrap bar [3m]))`,
				TimeTs: time.Unix(1, 0),
				Path:   "/loki/api/v1/query",
				Plan: &plan.QueryPlan{
					AST: syntax.MustParseExpr(`sum(sum_over_time({app="foo"} | unwrap bar [3m]))`),
				},
			},
			subQueries: []queryrangebase.RequestResponse{
				subQueryRequestResponse(`sum(sum_over_time({app="foo"} | unwrap bar[1m]))`, 1, 0),
				subQueryRequestResponse(`sum(sum_over_time({app="foo"} | unwrap bar[1m]))`, 2, time.Minute),
				subQueryRequestResponse(`sum(sum_over_time({app="foo"} | unwrap bar[1m]))`, 3, 2*time.Minute),
			},
			expected: expectedMergedResponse(1 + 2 + 3),
		},
		{
			in: &LokiInstantRequest{
				Query:  `sum by (bar) (sum_over_time({app="foo"} | unwrap bar [3m]))`,
				TimeTs: time.Unix(1, 0),
				Path:   "/loki/api/v1/query",
				Plan: &plan.QueryPlan{
					AST: syntax.MustParseExpr(`sum by (bar) (sum_over_time({app="foo"} | unwrap bar [3m]))`),
				},
			},
			subQueries: []queryrangebase.RequestResponse{
				subQueryRequestResponse(`sum by (bar)(sum_over_time({app="foo"} | unwrap bar[1m]))`, 1, 0),
				subQueryRequestResponse(`sum by (bar)(sum_over_time({app="foo"} | unwrap bar[1m]))`, 2, time.Minute),
				subQueryRequestResponse(`sum by (bar)(sum_over_time({app="foo"} | unwrap bar[1m]))`, 3, 2*time.Minute),
			},
			expected: expectedMergedResponse(1 + 2 + 3),
		},
	} {
		tc := tc
		t.Run(tc.in.GetQuery(), func(t *testing.T) {
			byTimeTs := make(map[int64]queryrangebase.RequestResponse)
			for _, v := range tc.subQueries {
				byTimeTs[v.Request.(*LokiInstantRequest).TimeTs.UnixNano()] = v
			}

			resp, err := srm.Wrap(queryrangebase.HandlerFunc(
				func(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
					// req should match with one of the subqueries.
					ts := req.(*LokiInstantRequest).TimeTs.UnixNano()
					subq, ok := byTimeTs[ts]
					if !ok { // every req **should** match with one of the subqueries
						return nil, fmt.Errorf("subquery request '%s-%d' not found", req.GetQuery(), ts)
					}

					// Assert subquery request
					assert.Equal(t, subq.Request.GetQuery(), req.GetQuery())
					assert.Equal(t, subq.Request, req)
					return subq.Response, nil

				})).Do(ctx, tc.in)
			require.NoError(t, err)
			require.Equal(t, tc.expected, resp.(*LokiPromResponse).Response)
		})
	}
}

// subQueryRequestResponse returns a RequestResponse containing the expected subQuery instant request
// and a response containing a sample value returned from the following wrapper
func subQueryRequestResponse(expectedSubQuery string, sampleValue float64, offset time.Duration) queryrangebase.RequestResponse {
	res := queryrangebase.RequestResponse{
		Request: &LokiInstantRequest{
			Query:  expectedSubQuery,
			TimeTs: time.Unix(1, 0),
			Path:   "/loki/api/v1/query",
			Plan: &plan.QueryPlan{
				AST: syntax.MustParseExpr(expectedSubQuery),
			},
		},
		Response: &LokiPromResponse{
			Response: &queryrangebase.PrometheusResponse{
				Status: loghttp.QueryStatusSuccess,
				Data: queryrangebase.PrometheusData{
					ResultType: loghttp.ResultTypeVector,
					Result: []queryrangebase.SampleStream{
						{
							Labels: []logproto.LabelAdapter{
								{Name: "app", Value: "foo"},
							},
							Samples: []logproto.LegacySample{
								{TimestampMs: 1000, Value: sampleValue},
							},
						},
					},
				},
			},
		},
	}
	// The `TimeTs` (exec time) of instant (sub)query is adjusted with given offset.
	// Because subqueries would no longer contains the offset.
	if offset != 0 {
		ts := res.Request.(*LokiInstantRequest).TimeTs
		ts = ts.Add(-offset)
		res.Request.(*LokiInstantRequest).TimeTs = ts
	}
	return res
}

// expectedMergedResponse returns the expected middleware Prometheus response with the samples
// as the expectedSampleValue
func expectedMergedResponse(expectedSampleValue float64) *queryrangebase.PrometheusResponse {
	return &queryrangebase.PrometheusResponse{
		Status: loghttp.QueryStatusSuccess,
		Data: queryrangebase.PrometheusData{
			ResultType: loghttp.ResultTypeVector,
			Result: []queryrangebase.SampleStream{
				{
					Labels: []logproto.LabelAdapter{},
					Samples: []logproto.LegacySample{
						{TimestampMs: 1000, Value: expectedSampleValue},
					},
				},
			},
		},
	}
}
