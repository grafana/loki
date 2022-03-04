package queryrange

import (
	"context"
	"fmt"
	"github.com/grafana/loki/pkg/loghttp"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"
)

func Test_RangeVectorSplit(t *testing.T) {
	srm, err := SplitByRangeMiddleware(log.NewNopLogger(), fakeLimits{
		maxSeries: 10000,
		splits: map[string]time.Duration{
			"tenant": time.Minute,
		},
	}, nilShardingMetrics)
	require.NoError(t, err)

	ctx := user.InjectOrgID(context.TODO(), "tenant")

	for _, tc := range []struct {
		in         queryrangebase.Request
		subQueries []queryrangebase.RequestResponse
		expected   queryrangebase.Response
	}{
		{
			in: &LokiInstantRequest{
				Query:  `sum(rate({app="foo"}[3m]))`,
				TimeTs: time.Unix(1, 0),
				Path:   "/loki/api/v1/query",
			},
			subQueries: []queryrangebase.RequestResponse{
				subQueryRequestResponse(`sum(rate({app="foo"}[1m]))`, 1),
				subQueryRequestResponse(`sum(rate({app="foo"}[1m] offset 1m0s))`, 2),
				subQueryRequestResponse(`sum(rate({app="foo"}[1m] offset 2m0s))`, 3),
			},
			expected: expectedMergedResponse(1 + 2 + 3),
		},
		{
			in: &LokiInstantRequest{
				Query:  `sum(rate({app="foo"}[3m])) by (bar)`,
				TimeTs: time.Unix(1, 0),
				Path:   "/loki/api/v1/query",
			},
			subQueries: []queryrangebase.RequestResponse{
				subQueryRequestResponse(`sum by(bar)(rate({app="foo"}[1m]))`, 1),
				subQueryRequestResponse(`sum by(bar)(rate({app="foo"}[1m] offset 1m0s))`, 2),
				subQueryRequestResponse(`sum by(bar)(rate({app="foo"}[1m] offset 2m0s))`, 3),
			},
			expected: expectedMergedResponse(1 + 2 + 3),
		},
		{
			in: &LokiInstantRequest{
				Query:  `count(rate({app="foo"}[3m]))`,
				TimeTs: time.Unix(1, 0),
				Path:   "/loki/api/v1/query",
			},
			subQueries: []queryrangebase.RequestResponse{
				subQueryRequestResponse(`count(rate({app="foo"}[1m]))`, 1),
				subQueryRequestResponse(`count(rate({app="foo"}[1m] offset 1m0s))`, 1),
				subQueryRequestResponse(`count(rate({app="foo"}[1m] offset 2m0s))`, 1),
			},
			expected: expectedMergedResponse(1 + 1 + 1),
		},
		{
			in: &LokiInstantRequest{
				Query:  `count(rate({app="foo"}[3m])) by (bar)`,
				TimeTs: time.Unix(1, 0),
				Path:   "/loki/api/v1/query",
			},
			subQueries: []queryrangebase.RequestResponse{
				subQueryRequestResponse(`count by(bar)(rate({app="foo"}[1m]))`, 1),
				subQueryRequestResponse(`count by(bar)(rate({app="foo"}[1m] offset 1m0s))`, 1),
				subQueryRequestResponse(`count by(bar)(rate({app="foo"}[1m] offset 2m0s))`, 1),
			},
			expected: expectedMergedResponse(1 + 1 + 1),
		},
		{
			in: &LokiInstantRequest{
				Query:  `max(rate({app="foo"}[3m]))`,
				TimeTs: time.Unix(1, 0),
				Path:   "/loki/api/v1/query",
			},
			subQueries: []queryrangebase.RequestResponse{
				subQueryRequestResponse(`max(rate({app="foo"}[1m]))`, 10),
				subQueryRequestResponse(`max(rate({app="foo"}[1m] offset 1m0s))`, 1),
				subQueryRequestResponse(`max(rate({app="foo"}[1m] offset 2m0s))`, -1),
			},
			expected: expectedMergedResponse(10),
		},
		{
			in: &LokiInstantRequest{
				Query:  `max(rate({app="foo"}[3m])) by (bar)`,
				TimeTs: time.Unix(1, 0),
				Path:   "/loki/api/v1/query",
			},
			subQueries: []queryrangebase.RequestResponse{
				subQueryRequestResponse(`max by(bar)(rate({app="foo"}[1m]))`, 10),
				subQueryRequestResponse(`max by(bar)(rate({app="foo"}[1m] offset 1m0s))`, 100),
				subQueryRequestResponse(`max by(bar)(rate({app="foo"}[1m] offset 2m0s))`, -200),
			},
			expected: expectedMergedResponse(100),
		},
		{
			in: &LokiInstantRequest{
				Query:  `min(rate({app="foo"}[3m]))`,
				TimeTs: time.Unix(1, 0),
				Path:   "/loki/api/v1/query",
			},
			subQueries: []queryrangebase.RequestResponse{
				subQueryRequestResponse(`min(rate({app="foo"}[1m]))`, 10),
				subQueryRequestResponse(`min(rate({app="foo"}[1m] offset 1m0s))`, 1),
				subQueryRequestResponse(`min(rate({app="foo"}[1m] offset 2m0s))`, -1),
			},
			expected: expectedMergedResponse(-1),
		},
		{
			in: &LokiInstantRequest{
				Query:  `min(rate({app="foo"}[3m])) by (bar)`,
				TimeTs: time.Unix(1, 0),
				Path:   "/loki/api/v1/query",
			},
			subQueries: []queryrangebase.RequestResponse{
				subQueryRequestResponse(`min by(bar)(rate({app="foo"}[1m]))`, 10),
				subQueryRequestResponse(`min by(bar)(rate({app="foo"}[1m] offset 1m0s))`, 100),
				subQueryRequestResponse(`min by(bar)(rate({app="foo"}[1m] offset 2m0s))`, -200),
			},
			expected: expectedMergedResponse(-200),
		},
		{
			in: &LokiInstantRequest{
				Query:  `bytes_over_time({app="foo"}[3m])`,
				TimeTs: time.Unix(1, 0),
				Path:   "/loki/api/v1/query",
			},
			subQueries: []queryrangebase.RequestResponse{
				subQueryRequestResponse(`bytes_over_time({app="foo"}[1m])`, 10),
				subQueryRequestResponse(`bytes_over_time({app="foo"}[1m] offset 1m0s)`, 20),
				subQueryRequestResponse(`bytes_over_time({app="foo"}[1m] offset 2m0s)`, -200),
			},
			expected: expectedMergedResponse(10 + 20 - 200),
		},
		{
			in: &LokiInstantRequest{
				Query:  `bytes_rate({app="foo"}[3m])`,
				TimeTs: time.Unix(1, 0),
				Path:   "/loki/api/v1/query",
			},
			subQueries: []queryrangebase.RequestResponse{
				subQueryRequestResponse(`bytes_rate({app="foo"}[1m])`, 10),
				subQueryRequestResponse(`bytes_rate({app="foo"}[1m] offset 1m0s)`, 20),
				subQueryRequestResponse(`bytes_rate({app="foo"}[1m] offset 2m0s)`, -5),
			},
			expected: expectedMergedResponse(10 + 20 - 5),
		},
		{
			in: &LokiInstantRequest{
				Query:  `count_over_time({app="foo"}[3m])`,
				TimeTs: time.Unix(1, 0),
				Path:   "/loki/api/v1/query",
			},
			subQueries: []queryrangebase.RequestResponse{
				subQueryRequestResponse(`count_over_time({app="foo"}[1m])`, 0),
				subQueryRequestResponse(`count_over_time({app="foo"}[1m] offset 1m0s)`, 0),
				subQueryRequestResponse(`count_over_time({app="foo"}[1m] offset 2m0s)`, 2),
			},
			expected: expectedMergedResponse(0 + 0 + 2),
		},
		{
			in: &LokiInstantRequest{
				Query:  `rate({app="foo"}[3m])`,
				TimeTs: time.Unix(1, 0),
				Path:   "/loki/api/v1/query",
			},
			subQueries: []queryrangebase.RequestResponse{
				subQueryRequestResponse(`rate({app="foo"}[1m])`, 1),
				subQueryRequestResponse(`rate({app="foo"}[1m] offset 1m0s)`, 2),
				subQueryRequestResponse(`rate({app="foo"}[1m] offset 2m0s)`, 3),
			},
			expected: expectedMergedResponse(1 + 2 + 3),
		},
		{
			in: &LokiInstantRequest{
				Query:  `sum_over_time({app="foo"} | unwrap bar [3m])`,
				TimeTs: time.Unix(1, 0),
				Path:   "/loki/api/v1/query",
			},
			subQueries: []queryrangebase.RequestResponse{
				subQueryRequestResponse(`sum_over_time({app="foo"} | unwrap bar[1m])`, 1),
				subQueryRequestResponse(`sum_over_time({app="foo"} | unwrap bar[1m] offset 1m0s)`, 2),
				subQueryRequestResponse(`sum_over_time({app="foo"} | unwrap bar[1m] offset 2m0s)`, 3),
			},
			expected: expectedMergedResponse(1 + 2 + 3),
		},
		{
			in: &LokiInstantRequest{
				Query:  `min_over_time({app="foo"} | unwrap bar [3m])`,
				TimeTs: time.Unix(1, 0),
				Path:   "/loki/api/v1/query",
			},
			subQueries: []queryrangebase.RequestResponse{
				subQueryRequestResponse(`min_over_time({app="foo"} | unwrap bar[1m])`, 1),
				subQueryRequestResponse(`min_over_time({app="foo"} | unwrap bar[1m] offset 1m0s)`, 2),
				subQueryRequestResponse(`min_over_time({app="foo"} | unwrap bar[1m] offset 2m0s)`, 3),
			},
			expected: expectedMergedResponse(1),
		},
		{
			in: &LokiInstantRequest{
				Query:  `min_over_time({app="foo"} | unwrap bar [3m]) by (bar)`,
				TimeTs: time.Unix(1, 0),
				Path:   "/loki/api/v1/query",
			},
			subQueries: []queryrangebase.RequestResponse{
				subQueryRequestResponse(`min_over_time({app="foo"} | unwrap bar[1m]) by(bar)`, 1),
				subQueryRequestResponse(`min_over_time({app="foo"} | unwrap bar[1m] offset 1m0s) by(bar)`, 2),
				subQueryRequestResponse(`min_over_time({app="foo"} | unwrap bar[1m] offset 2m0s) by(bar)`, 3),
			},
			expected: expectedMergedResponse(1),
		},
		{
			in: &LokiInstantRequest{
				Query:  `max_over_time({app="foo"} | unwrap bar [3m])`,
				TimeTs: time.Unix(1, 0),
				Path:   "/loki/api/v1/query",
			},
			subQueries: []queryrangebase.RequestResponse{
				subQueryRequestResponse(`max_over_time({app="foo"} | unwrap bar[1m])`, 1),
				subQueryRequestResponse(`max_over_time({app="foo"} | unwrap bar[1m] offset 1m0s)`, 2),
				subQueryRequestResponse(`max_over_time({app="foo"} | unwrap bar[1m] offset 2m0s)`, 3),
			},
			expected: expectedMergedResponse(3),
		},
		{
			in: &LokiInstantRequest{
				Query:  `max_over_time({app="foo"} | unwrap bar [3m]) by (bar)`,
				TimeTs: time.Unix(1, 0),
				Path:   "/loki/api/v1/query",
			},
			subQueries: []queryrangebase.RequestResponse{
				subQueryRequestResponse(`max_over_time({app="foo"} | unwrap bar[1m]) by(bar)`, 1),
				subQueryRequestResponse(`max_over_time({app="foo"} | unwrap bar[1m] offset 1m0s) by(bar)`, 2),
				subQueryRequestResponse(`max_over_time({app="foo"} | unwrap bar[1m] offset 2m0s) by(bar)`, 3),
			},
			expected: expectedMergedResponse(3),
		},
		{
			in: &LokiInstantRequest{
				Query:  `rate({app="foo"}[3m]) + rate({app="bar"}[2m])`,
				TimeTs: time.Unix(1, 0),
				Path:   "/loki/api/v1/query",
			},
			subQueries: []queryrangebase.RequestResponse{
				subQueryRequestResponse(`rate({app="foo"}[1m])`, 1),
				subQueryRequestResponse(`rate({app="foo"}[1m] offset 1m0s)`, 2),
				subQueryRequestResponse(`rate({app="foo"}[1m] offset 2m0s)`, 3),

				subQueryRequestResponse(`rate({app="bar"}[1m])`, 4),
				subQueryRequestResponse(`rate({app="bar"}[1m] offset 1m0s)`, 5),
			},
			expected: expectedMergedResponse(1 + 2 + 3 + 4 + 5),
		},
		{
			in: &LokiInstantRequest{
				Query:  `min(rate({app="foo"}[5m])) / count(rate({app="bar"}[10m]))`,
				TimeTs: time.Unix(1, 0),
				Path:   "/loki/api/v1/query",
			},
			subQueries: []queryrangebase.RequestResponse{
				subQueryRequestResponse(`min(rate({app="foo"}[1m]))`, 1),
				subQueryRequestResponse(`min(rate({app="foo"}[1m] offset 4m0s))`, 2),
				subQueryRequestResponse(`min(rate({app="foo"}[1m] offset 3m0s))`, 3),
				subQueryRequestResponse(`min(rate({app="foo"}[1m] offset 2m0s))`, 4),
				subQueryRequestResponse(`min(rate({app="foo"}[1m] offset 1m0s))`, 5),

				subQueryRequestResponse(`count(rate({app="bar"}[1m]))`, 10),
				subQueryRequestResponse(`count(rate({app="bar"}[1m] offset 9m0s))`, 10),
				subQueryRequestResponse(`count(rate({app="bar"}[1m] offset 8m0s))`, 10),
				subQueryRequestResponse(`count(rate({app="bar"}[1m] offset 7m0s))`, 10),
				subQueryRequestResponse(`count(rate({app="bar"}[1m] offset 6m0s))`, 10),
				subQueryRequestResponse(`count(rate({app="bar"}[1m] offset 5m0s))`, 10),
				subQueryRequestResponse(`count(rate({app="bar"}[1m] offset 4m0s))`, 10),
				subQueryRequestResponse(`count(rate({app="bar"}[1m] offset 3m0s))`, 10),
				subQueryRequestResponse(`count(rate({app="bar"}[1m] offset 2m0s))`, 10),
				subQueryRequestResponse(`count(rate({app="bar"}[1m] offset 1m0s))`, 10),
			},
			expected: expectedMergedResponse(float64(1) / float64(100)),
		},
		{
			in: &LokiInstantRequest{
				Query:  `topk(2, rate({app="foo"}[3m]))`,
				TimeTs: time.Unix(1, 0),
				Path:   "/loki/api/v1/query",
			},
			subQueries: []queryrangebase.RequestResponse{
				subQueryRequestResponse(`rate({app="foo"}[1m])`, 1),
				subQueryRequestResponse(`rate({app="foo"}[1m] offset 1m0s)`, 2),
				subQueryRequestResponse(`rate({app="foo"}[1m] offset 2m0s)`, 3),
			},
			expected: expectedMergedResponse(1 + 2 + 3),
		},
		{
			in: &LokiInstantRequest{
				Query:  `quantile_over_time(0.95, {app="foo"} | unwrap bar[3m])`,
				TimeTs: time.Unix(1, 0),
				Path:   "/loki/api/v1/query",
			},
			subQueries: []queryrangebase.RequestResponse{
				subQueryRequestResponse(`quantile_over_time(0.95, {app="foo"} | unwrap bar[3m])`, 1),
			},
			expected: &queryrangebase.PrometheusResponse{
				Status: loghttp.QueryStatusSuccess,
				Data: queryrangebase.PrometheusData{
					ResultType: loghttp.ResultTypeVector,
					Result: []queryrangebase.SampleStream{
						{
							Labels: []logproto.LabelAdapter{
								{Name: "app", Value: "foo"},
							},
							Samples: []logproto.LegacySample{
								{TimestampMs: 1000, Value: 1},
							},
						},
					},
				},
			},
		},
		{
			in: &LokiInstantRequest{
				Query:  `sum(avg_over_time({app="foo"} | unwrap bar[3m]))`,
				TimeTs: time.Unix(1, 0),
				Path:   "/loki/api/v1/query",
			},
			subQueries: []queryrangebase.RequestResponse{
				subQueryRequestResponse(`sum(avg_over_time({app="foo"} | unwrap bar[3m]))`, 1),
			},
			expected: &queryrangebase.PrometheusResponse{
				Status: loghttp.QueryStatusSuccess,
				Data: queryrangebase.PrometheusData{
					ResultType: loghttp.ResultTypeVector,
					Result: []queryrangebase.SampleStream{
						{
							Labels: []logproto.LabelAdapter{
								{Name: "app", Value: "foo"},
							},
							Samples: []logproto.LegacySample{
								{TimestampMs: 1000, Value: 1},
							},
						},
					},
				},
			},
		},
		{
			in: &LokiInstantRequest{
				Query:  `rate({app="foo"}[1m])`,
				TimeTs: time.Unix(1, 0),
				Path:   "/loki/api/v1/query",
			},
			subQueries: []queryrangebase.RequestResponse{
				subQueryRequestResponse(`rate({app="foo"}[1m])`, 1),
			},
			expected: &queryrangebase.PrometheusResponse{
				Status: loghttp.QueryStatusSuccess,
				Data: queryrangebase.PrometheusData{
					ResultType: loghttp.ResultTypeVector,
					Result: []queryrangebase.SampleStream{
						{
							Labels: []logproto.LabelAdapter{
								{Name: "app", Value: "foo"},
							},
							Samples: []logproto.LegacySample{
								{TimestampMs: 1000, Value: 1},
							},
						},
					},
				},
			},
		},
	} {
		tc := tc
		t.Run(tc.in.GetQuery(), func(t *testing.T) {
			resp, err := srm.Wrap(queryrangebase.HandlerFunc(
				func(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
					// Assert subquery request
					for _, reqResp := range tc.subQueries {
						if req.GetQuery() == reqResp.Request.GetQuery() {
							require.Equal(t, reqResp.Request, req)
							// return the test data subquery response
							return reqResp.Response, nil
						}
					}

					return nil, fmt.Errorf("subquery request '" + req.GetQuery() + "' not found")
				})).Do(ctx, tc.in)
			require.NoError(t, err)
			require.Equal(t, tc.expected, resp.(*LokiPromResponse).Response)
		})
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
