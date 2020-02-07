package queryrange

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/querier/queryrange"
	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logql/stats"
	"github.com/stretchr/testify/require"
)

func TestStatsMiddleware(t *testing.T) {
	md := StatsMiddleware()
	for name, test := range map[string]struct {
		queryrange.Handler
		queryrange.Request
		expect func(t *testing.T, status, query string, rangeType logql.QueryRangeType, stats stats.Result)
	}{
		"Prometheus instant query nil": {
			Handler: queryrange.HandlerFunc(func(c context.Context, r queryrange.Request) (queryrange.Response, error) {
				return &LokiPromResponse{}, nil
			}),
			Request: &LokiRequest{
				Query: "foo",
			},
			expect: func(t *testing.T, status, query string, rangeType logql.QueryRangeType, stats stats.Result) {
				require.Equal(t, "", status)
				require.Equal(t, "foo", query)
				require.Equal(t, logql.InstantType, rangeType)
				require.NotEmpty(t, stats.Summary.ExecTime)
			},
		},
		"Prometheus instant query": {
			Handler: queryrange.HandlerFunc(func(c context.Context, r queryrange.Request) (queryrange.Response, error) {
				return &LokiPromResponse{
					Response: &queryrange.PrometheusResponse{
						Status: loghttp.QueryStatusSuccess,
					},
				}, nil
			}),
			Request: &LokiRequest{
				Query: "foo",
			},
			expect: func(t *testing.T, status, query string, rangeType logql.QueryRangeType, stats stats.Result) {
				require.Equal(t, loghttp.QueryStatusSuccess, status)
				require.Equal(t, "foo", query)
				require.Equal(t, logql.InstantType, rangeType)
				require.NotEmpty(t, stats.Summary.ExecTime)
			},
		},
		"Loki range query": {
			Handler: queryrange.HandlerFunc(func(c context.Context, r queryrange.Request) (queryrange.Response, error) {
				return &LokiResponse{
					Status: loghttp.QueryStatusFail,
					Statistics: stats.Result{
						Store: stats.Store{
							DecompressedBytes: 20 * 1024,
						},
					},
				}, nil
			}),
			Request: &LokiRequest{
				Query: "foo",
				EndTs: time.Now(),
			},
			expect: func(t *testing.T, status, query string, rangeType logql.QueryRangeType, stats stats.Result) {
				require.Equal(t, loghttp.QueryStatusFail, status)
				require.Equal(t, "foo", query)
				require.Equal(t, logql.RangeType, rangeType)
				require.NotEmpty(t, stats.Summary.ExecTime)
				require.Equal(t, int64(20*1024), stats.Store.DecompressedBytes)
			},
		},
		"error": {
			Handler: queryrange.HandlerFunc(func(c context.Context, r queryrange.Request) (queryrange.Response, error) {
				return nil, errors.New("")
			}),
			Request: &LokiRequest{
				Query: "foo",
				EndTs: time.Now(),
			},
			expect: func(t *testing.T, status, query string, rangeType logql.QueryRangeType, stats stats.Result) {
				require.Equal(t, loghttp.QueryStatusFail, status)
				require.Equal(t, "foo", query)
				require.Equal(t, logql.RangeType, rangeType)
				require.NotEmpty(t, stats.Summary.ExecTime)
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			RecordMetrics = func(status, query string, rangeType logql.QueryRangeType, stats stats.Result) {
				test.expect(t, status, query, rangeType, stats)
			}
			_, _ = md.Wrap(test.Handler).Do(context.Background(), test.Request)
		})

	}

}
