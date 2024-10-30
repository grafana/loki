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

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/querier/plan"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
)

func Test_RangeVectorSplitAlign(t *testing.T) {
	var (
		twelve34 = time.Date(1970, 1, 1, 12, 34, 0, 0, time.UTC) // 1970 12:34:00 UTC
		twelve   = time.Date(1970, 1, 1, 12, 00, 0, 0, time.UTC) // 1970 12:00:00 UTC
		eleven   = twelve.Add(-1 * time.Hour)                    // 1970 11:00:00 UTC
		ten      = eleven.Add(-1 * time.Hour)                    // 1970 10:00:00 UTC
	)

	for _, tc := range []struct {
		name            string
		in              queryrangebase.Request
		subQueries      []queryrangebase.RequestResponse
		expected        queryrangebase.Response
		splitByInterval time.Duration
	}{
		{
			name:            "sum_splitBy_aligned_with_query_time",
			splitByInterval: 1 * time.Minute,
			in: &LokiInstantRequest{
				Query:  `sum(bytes_over_time({app="foo"}[3m]))`,
				TimeTs: time.Unix(180, 0),
				Path:   "/loki/api/v1/query",
				Plan: &plan.QueryPlan{
					AST: syntax.MustParseExpr(`sum(bytes_over_time({app="foo"}[3m]))`),
				},
			},
			subQueries: []queryrangebase.RequestResponse{
				subQueryRequestResponseWithQueryTime(`sum(bytes_over_time({app="foo"}[1m]))`, 1, time.Unix(60, 0)),
				subQueryRequestResponseWithQueryTime(`sum(bytes_over_time({app="foo"}[1m]))`, 2, time.Unix(120, 0)),
				subQueryRequestResponseWithQueryTime(`sum(bytes_over_time({app="foo"}[1m]))`, 3, time.Unix(180, 0)),
			},
			expected: expectedMergedResponseWithTime(1+2+3, time.Unix(180, 0)), // original `TimeTs` of the query.
		},
		{
			name:            "sum_splitBy_not_aligned_query_time",
			splitByInterval: 1 * time.Hour,
			in: &LokiInstantRequest{
				Query:  `sum(bytes_over_time({app="foo"}[3h]))`,
				TimeTs: twelve34,
				Path:   "/loki/api/v1/query",
				Plan: &plan.QueryPlan{
					AST: syntax.MustParseExpr(`sum(bytes_over_time({app="foo"}[3h]))`),
				},
			},
			subQueries: []queryrangebase.RequestResponse{
				subQueryRequestResponseWithQueryTime(`sum(bytes_over_time({app="foo"}[34m]))`, 1, twelve34),
				subQueryRequestResponseWithQueryTime(`sum(bytes_over_time({app="foo"}[1h]))`, 2, twelve),
				subQueryRequestResponseWithQueryTime(`sum(bytes_over_time({app="foo"}[1h]))`, 3, eleven),
				subQueryRequestResponseWithQueryTime(`sum(bytes_over_time({app="foo"}[26m]))`, 4, ten),
			},
			expected: expectedMergedResponseWithTime(1+2+3+4, twelve34), // original `TimeTs` of the query.
		},
		{
			name:            "sum_aggregation_splitBy_aligned_with_query_time",
			splitByInterval: 1 * time.Minute,
			in: &LokiInstantRequest{
				Query:  `sum by (bar) (bytes_over_time({app="foo"}[3m]))`,
				TimeTs: time.Unix(180, 0),
				Path:   "/loki/api/v1/query",
				Plan: &plan.QueryPlan{
					AST: syntax.MustParseExpr(`sum by (bar) (bytes_over_time({app="foo"}[3m]))`),
				},
			},
			subQueries: []queryrangebase.RequestResponse{
				subQueryRequestResponseWithQueryTime(`sum by (bar)(bytes_over_time({app="foo"}[1m]))`, 10, time.Unix(60, 0)),
				subQueryRequestResponseWithQueryTime(`sum by (bar)(bytes_over_time({app="foo"}[1m]))`, 20, time.Unix(120, 0)),
				subQueryRequestResponseWithQueryTime(`sum by (bar)(bytes_over_time({app="foo"}[1m]))`, 30, time.Unix(180, 0)),
			},
			expected: expectedMergedResponseWithTime(10+20+30, time.Unix(180, 0)),
		},
		{
			name:            "sum_aggregation_splitBy_not_aligned_with_query_time",
			splitByInterval: 1 * time.Hour,
			in: &LokiInstantRequest{
				Query:  `sum by (bar) (bytes_over_time({app="foo"}[3h]))`,
				TimeTs: twelve34,
				Path:   "/loki/api/v1/query",
				Plan: &plan.QueryPlan{
					AST: syntax.MustParseExpr(`sum by (bar) (bytes_over_time({app="foo"}[3h]))`),
				},
			},
			subQueries: []queryrangebase.RequestResponse{
				subQueryRequestResponseWithQueryTime(`sum by (bar)(bytes_over_time({app="foo"}[34m]))`, 10, twelve34), // 12:34:00
				subQueryRequestResponseWithQueryTime(`sum by (bar)(bytes_over_time({app="foo"}[1h]))`, 20, twelve),    // 12:00:00 aligned
				subQueryRequestResponseWithQueryTime(`sum by (bar)(bytes_over_time({app="foo"}[1h]))`, 30, eleven),    // 11:00:00 aligned
				subQueryRequestResponseWithQueryTime(`sum by (bar)(bytes_over_time({app="foo"}[26m]))`, 40, ten),      // 10:00:00
			},
			expected: expectedMergedResponseWithTime(10+20+30+40, twelve34),
		},
		{
			name:            "count_over_time_aligned_with_query_time",
			splitByInterval: 1 * time.Minute,
			in: &LokiInstantRequest{
				Query:  `sum(count_over_time({app="foo"}[3m]))`,
				TimeTs: time.Unix(180, 0),
				Path:   "/loki/api/v1/query",
				Plan: &plan.QueryPlan{
					AST: syntax.MustParseExpr(`sum(count_over_time({app="foo"}[3m]))`),
				},
			},
			subQueries: []queryrangebase.RequestResponse{
				subQueryRequestResponseWithQueryTime(`sum(count_over_time({app="foo"}[1m]))`, 1, time.Unix(60, 0)),
				subQueryRequestResponseWithQueryTime(`sum(count_over_time({app="foo"}[1m]))`, 1, time.Unix(120, 0)),
				subQueryRequestResponseWithQueryTime(`sum(count_over_time({app="foo"}[1m]))`, 1, time.Unix(180, 0)),
			},
			expected: expectedMergedResponseWithTime(1+1+1, time.Unix(180, 0)),
		},
		{
			name:            "count_over_time_not_aligned_with_query_time",
			splitByInterval: 1 * time.Hour,
			in: &LokiInstantRequest{
				Query:  `sum(count_over_time({app="foo"}[3h]))`,
				TimeTs: twelve34,
				Path:   "/loki/api/v1/query",
				Plan: &plan.QueryPlan{
					AST: syntax.MustParseExpr(`sum(count_over_time({app="foo"}[3h]))`),
				},
			},
			subQueries: []queryrangebase.RequestResponse{
				subQueryRequestResponseWithQueryTime(`sum(count_over_time({app="foo"}[34m]))`, 1, twelve34),
				subQueryRequestResponseWithQueryTime(`sum(count_over_time({app="foo"}[1h]))`, 1, twelve),
				subQueryRequestResponseWithQueryTime(`sum(count_over_time({app="foo"}[1h]))`, 1, eleven),
				subQueryRequestResponseWithQueryTime(`sum(count_over_time({app="foo"}[26m]))`, 1, ten),
			},
			expected: expectedMergedResponseWithTime(1+1+1+1, twelve34),
		},
		{
			name:            "sum_agg_count_over_time_align_with_query_time",
			splitByInterval: 1 * time.Minute,
			in: &LokiInstantRequest{
				Query:  `sum by (bar) (count_over_time({app="foo"}[3m]))`,
				TimeTs: time.Unix(180, 0),
				Path:   "/loki/api/v1/query",
				Plan: &plan.QueryPlan{
					AST: syntax.MustParseExpr(`sum by (bar) (count_over_time({app="foo"}[3m]))`),
				},
			},
			subQueries: []queryrangebase.RequestResponse{
				subQueryRequestResponseWithQueryTime(`sum by (bar)(count_over_time({app="foo"}[1m]))`, 0, time.Unix(60, 0)),
				subQueryRequestResponseWithQueryTime(`sum by (bar)(count_over_time({app="foo"}[1m]))`, 0, time.Unix(120, 0)),
				subQueryRequestResponseWithQueryTime(`sum by (bar)(count_over_time({app="foo"}[1m]))`, 0, time.Unix(180, 0)),
			},
			expected: expectedMergedResponseWithTime(0+0+0, time.Unix(180, 0)),
		},
		{
			name:            "sum_agg_count_over_time_not_align_with_query_time",
			splitByInterval: 1 * time.Hour,
			in: &LokiInstantRequest{
				Query:  `sum by (bar) (count_over_time({app="foo"}[3h]))`,
				TimeTs: twelve34,
				Path:   "/loki/api/v1/query",
				Plan: &plan.QueryPlan{
					AST: syntax.MustParseExpr(`sum by (bar) (count_over_time({app="foo"}[3h]))`),
				},
			},
			subQueries: []queryrangebase.RequestResponse{
				subQueryRequestResponseWithQueryTime(`sum by (bar)(count_over_time({app="foo"}[34m]))`, 0, twelve34),
				subQueryRequestResponseWithQueryTime(`sum by (bar)(count_over_time({app="foo"}[1h]))`, 0, twelve),
				subQueryRequestResponseWithQueryTime(`sum by (bar)(count_over_time({app="foo"}[1h]))`, 0, eleven),
				subQueryRequestResponseWithQueryTime(`sum by (bar)(count_over_time({app="foo"}[26m]))`, 0, ten),
			},
			expected: expectedMergedResponseWithTime(0+0+0+0, twelve34),
		},
		{
			name:            "sum_over_time_aligned_with_query_time",
			splitByInterval: 1 * time.Minute,
			in: &LokiInstantRequest{
				Query:  `sum(sum_over_time({app="foo"} | unwrap bar [3m]))`,
				TimeTs: time.Unix(180, 0),
				Path:   "/loki/api/v1/query",
				Plan: &plan.QueryPlan{
					AST: syntax.MustParseExpr(`sum(sum_over_time({app="foo"} | unwrap bar [3m]))`),
				},
			},
			subQueries: []queryrangebase.RequestResponse{
				subQueryRequestResponseWithQueryTime(`sum(sum_over_time({app="foo"} | unwrap bar[1m]))`, 1, time.Unix(60, 0)),
				subQueryRequestResponseWithQueryTime(`sum(sum_over_time({app="foo"} | unwrap bar[1m]))`, 2, time.Unix(120, 0)),
				subQueryRequestResponseWithQueryTime(`sum(sum_over_time({app="foo"} | unwrap bar[1m]))`, 3, time.Unix(180, 0)),
			},
			expected: expectedMergedResponseWithTime(1+2+3, time.Unix(180, 0)),
		},
		{
			name:            "sum_over_time_not_aligned_with_query_time",
			splitByInterval: 1 * time.Hour,
			in: &LokiInstantRequest{
				Query:  `sum(sum_over_time({app="foo"} | unwrap bar [3h]))`,
				TimeTs: twelve34,
				Path:   "/loki/api/v1/query",
				Plan: &plan.QueryPlan{
					AST: syntax.MustParseExpr(`sum(sum_over_time({app="foo"} | unwrap bar [3h]))`),
				},
			},
			subQueries: []queryrangebase.RequestResponse{
				subQueryRequestResponseWithQueryTime(`sum(sum_over_time({app="foo"} | unwrap bar[34m]))`, 1, twelve34),
				subQueryRequestResponseWithQueryTime(`sum(sum_over_time({app="foo"} | unwrap bar[1h]))`, 2, twelve),
				subQueryRequestResponseWithQueryTime(`sum(sum_over_time({app="foo"} | unwrap bar[1h]))`, 3, eleven),
				subQueryRequestResponseWithQueryTime(`sum(sum_over_time({app="foo"} | unwrap bar[26m]))`, 4, ten),
			},
			expected: expectedMergedResponseWithTime(1+2+3+4, twelve34),
		},
		{
			name:            "sum_agg_sum_over_time_aligned_with_query_time",
			splitByInterval: 1 * time.Minute,
			in: &LokiInstantRequest{
				Query:  `sum by (bar) (sum_over_time({app="foo"} | unwrap bar [3m]))`,
				TimeTs: time.Unix(180, 0),
				Path:   "/loki/api/v1/query",
				Plan: &plan.QueryPlan{
					AST: syntax.MustParseExpr(`sum by (bar) (sum_over_time({app="foo"} | unwrap bar [3m]))`),
				},
			},
			subQueries: []queryrangebase.RequestResponse{
				subQueryRequestResponseWithQueryTime(`sum by (bar)(sum_over_time({app="foo"} | unwrap bar[1m]))`, 1, time.Unix(60, 0)),
				subQueryRequestResponseWithQueryTime(`sum by (bar)(sum_over_time({app="foo"} | unwrap bar[1m]))`, 2, time.Unix(120, 0)),
				subQueryRequestResponseWithQueryTime(`sum by (bar)(sum_over_time({app="foo"} | unwrap bar[1m]))`, 3, time.Unix(180, 0)),
			},
			expected: expectedMergedResponseWithTime(1+2+3, time.Unix(180, 0)),
		},
		{
			name:            "sum_agg_sum_over_time_not_aligned_with_query_time",
			splitByInterval: 1 * time.Hour,
			in: &LokiInstantRequest{
				Query:  `sum by (bar) (sum_over_time({app="foo"} | unwrap bar [3h]))`,
				TimeTs: twelve34,
				Path:   "/loki/api/v1/query",
				Plan: &plan.QueryPlan{
					AST: syntax.MustParseExpr(`sum by (bar) (sum_over_time({app="foo"} | unwrap bar [3h]))`),
				},
			},
			subQueries: []queryrangebase.RequestResponse{
				subQueryRequestResponseWithQueryTime(`sum by (bar)(sum_over_time({app="foo"} | unwrap bar[34m]))`, 1, twelve34),
				subQueryRequestResponseWithQueryTime(`sum by (bar)(sum_over_time({app="foo"} | unwrap bar[1h]))`, 2, twelve),
				subQueryRequestResponseWithQueryTime(`sum by (bar)(sum_over_time({app="foo"} | unwrap bar[1h]))`, 3, eleven),
				subQueryRequestResponseWithQueryTime(`sum by (bar)(sum_over_time({app="foo"} | unwrap bar[26m]))`, 4, ten),
			},
			expected: expectedMergedResponseWithTime(1+2+3+4, twelve34),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srm := NewSplitByRangeMiddleware(log.NewNopLogger(), testEngineOpts, fakeLimits{
				maxSeries:    10000,
				queryTimeout: time.Second,
				instantMetricSplitDuration: map[string]time.Duration{
					"tenant": tc.splitByInterval,
				},
			}, true, nilShardingMetrics) // enable splitAlign

			ctx := user.InjectOrgID(context.TODO(), "tenant")

			byTimeTs := make(map[int64]queryrangebase.RequestResponse)
			for _, v := range tc.subQueries {
				key := v.Request.(*LokiInstantRequest).TimeTs.UnixNano()
				byTimeTs[key] = v
			}

			resp, err := srm.Wrap(queryrangebase.HandlerFunc(
				func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
					// req should match with one of the subqueries.
					ts := req.(*LokiInstantRequest).TimeTs
					subq, ok := byTimeTs[ts.UnixNano()]
					if !ok { // every req **should** match with one of the subqueries
						return nil, fmt.Errorf("subquery request '%s-%s' not found", req.GetQuery(), ts)
					}

					// Assert subquery request
					assert.Equal(t, subq.Request.GetQuery(), req.GetQuery())
					assert.Equal(t, subq.Request, req)
					return subq.Response, nil

				})).Do(ctx, tc.in)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, resp.(*LokiPromResponse).Response)
		})
	}
}

func Test_RangeVectorSplit(t *testing.T) {
	srm := NewSplitByRangeMiddleware(log.NewNopLogger(), testEngineOpts, fakeLimits{
		maxSeries:    10000,
		queryTimeout: time.Second,
		instantMetricSplitDuration: map[string]time.Duration{
			"tenant": time.Minute,
		},
	}, false, nilShardingMetrics)

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
				subQueryRequestResponse(`sum(bytes_over_time({app="foo"}[1m]))`, 1),
				subQueryRequestResponse(`sum(bytes_over_time({app="foo"}[1m] offset 1m0s))`, 2),
				subQueryRequestResponse(`sum(bytes_over_time({app="foo"}[1m] offset 2m0s))`, 3),
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
				subQueryRequestResponse(`sum by (bar)(bytes_over_time({app="foo"}[1m]))`, 10),
				subQueryRequestResponse(`sum by (bar)(bytes_over_time({app="foo"}[1m] offset 1m0s))`, 20),
				subQueryRequestResponse(`sum by (bar)(bytes_over_time({app="foo"}[1m] offset 2m0s))`, 30),
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
				subQueryRequestResponse(`sum(count_over_time({app="foo"}[1m]))`, 1),
				subQueryRequestResponse(`sum(count_over_time({app="foo"}[1m] offset 1m0s))`, 1),
				subQueryRequestResponse(`sum(count_over_time({app="foo"}[1m] offset 2m0s))`, 1),
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
				subQueryRequestResponse(`sum by (bar)(count_over_time({app="foo"}[1m]))`, 0),
				subQueryRequestResponse(`sum by (bar)(count_over_time({app="foo"}[1m] offset 1m0s))`, 0),
				subQueryRequestResponse(`sum by (bar)(count_over_time({app="foo"}[1m] offset 2m0s))`, 0),
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
				subQueryRequestResponse(`sum(sum_over_time({app="foo"} | unwrap bar[1m]))`, 1),
				subQueryRequestResponse(`sum(sum_over_time({app="foo"} | unwrap bar[1m] offset 1m0s))`, 2),
				subQueryRequestResponse(`sum(sum_over_time({app="foo"} | unwrap bar[1m] offset 2m0s))`, 3),
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
				subQueryRequestResponse(`sum by (bar)(sum_over_time({app="foo"} | unwrap bar[1m]))`, 1),
				subQueryRequestResponse(`sum by (bar)(sum_over_time({app="foo"} | unwrap bar[1m] offset 1m0s))`, 2),
				subQueryRequestResponse(`sum by (bar)(sum_over_time({app="foo"} | unwrap bar[1m] offset 2m0s))`, 3),
			},
			expected: expectedMergedResponse(1 + 2 + 3),
		},
	} {
		t.Run(tc.in.GetQuery(), func(t *testing.T) {
			resp, err := srm.Wrap(queryrangebase.HandlerFunc(
				func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
					// Assert subquery request
					for _, reqResp := range tc.subQueries {
						if req.GetQuery() == reqResp.Request.GetQuery() {
							require.Equal(t, reqResp.Request, req)
							// return the test data subquery response
							return reqResp.Response, nil
						}
					}

					return nil, fmt.Errorf("%s", "subquery request '"+req.GetQuery()+"' not found")
				})).Do(ctx, tc.in)
			require.NoError(t, err)
			require.Equal(t, tc.expected, resp.(*LokiPromResponse).Response)
		})
	}
}

// subQueryRequestResponse returns a RequestResponse containing the expected subQuery instant request
// and a response containing a sample value returned from the following wrapper
func subQueryRequestResponseWithQueryTime(expectedSubQuery string, sampleValue float64, exec time.Time) queryrangebase.RequestResponse {
	return queryrangebase.RequestResponse{
		Request: &LokiInstantRequest{
			Query:  expectedSubQuery,
			TimeTs: exec,
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
}

// subQueryRequestResponse returns a RequestResponse containing the expected subQuery instant request
// and a response containing a sample value returned from the following wrapper
func subQueryRequestResponse(expectedSubQuery string, sampleValue float64) queryrangebase.RequestResponse {
	return queryrangebase.RequestResponse{
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

func expectedMergedResponseWithTime(expectedSampleValue float64, exec time.Time) *queryrangebase.PrometheusResponse {
	return &queryrangebase.PrometheusResponse{
		Status: loghttp.QueryStatusSuccess,
		Data: queryrangebase.PrometheusData{
			ResultType: loghttp.ResultTypeVector,
			Result: []queryrangebase.SampleStream{
				{
					Labels: []logproto.LabelAdapter{},
					Samples: []logproto.LegacySample{
						{TimestampMs: exec.UnixMilli(), Value: expectedSampleValue},
					},
				},
			},
		},
	}
}
