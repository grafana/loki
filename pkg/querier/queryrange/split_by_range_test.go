package queryrange

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/grafana/loki/pkg/loghttp"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
)

func Test_RangeVectorSplit(t *testing.T) {
	srm := NewSplitByRangeMiddleware(log.NewNopLogger(), testEngineOpts, fakeLimits{
		maxSeries:    10000,
		queryTimeout: time.Second,
		splits: map[string]time.Duration{
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
			},
			subQueries: []queryrangebase.RequestResponse{
				subQueryRequestResponse(`sum by (bar)(sum_over_time({app="foo"} | unwrap bar[1m]))`, 1),
				subQueryRequestResponse(`sum by (bar)(sum_over_time({app="foo"} | unwrap bar[1m] offset 1m0s))`, 2),
				subQueryRequestResponse(`sum by (bar)(sum_over_time({app="foo"} | unwrap bar[1m] offset 2m0s))`, 3),
			},
			expected: expectedMergedResponse(1 + 2 + 3),
		},
	} {
		tc := tc
		t.Run(tc.in.GetQuery(), func(t *testing.T) {
			resp, err := srm.Wrap(queryrangebase.HandlerFunc(
				func(ctx context.Context, _ bool, req queryrangebase.Request) (queryrangebase.Response, error) {
					// Assert subquery request
					for _, reqResp := range tc.subQueries {
						if req.GetQuery() == reqResp.Request.GetQuery() {
							require.Equal(t, reqResp.Request, req)
							// return the test data subquery response
							return reqResp.Response, nil
						}
					}

					return nil, fmt.Errorf("subquery request '" + req.GetQuery() + "' not found")
				})).Do(ctx, false, tc.in)
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
