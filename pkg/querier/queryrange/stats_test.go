package queryrange

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	strings "strings"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/querier/queryrange"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/logql"
	"github.com/grafana/loki/pkg/logql/stats"
)

func TestStatsCollectorMiddleware(t *testing.T) {
	// no stats
	data := &queryData{}
	ctx := context.WithValue(context.Background(), ctxKey, data)
	_, _ = StatsCollectorMiddleware().Wrap(queryrange.HandlerFunc(func(ctx context.Context, r queryrange.Request) (queryrange.Response, error) {
		return nil, nil
	})).Do(ctx, &LokiRequest{
		Query:   "foo",
		StartTs: time.Now(),
	})
	require.Equal(t, "foo", data.query)
	require.Equal(t, true, data.recorded)
	require.Equal(t, logql.RangeType, data.rangeType)
	require.Nil(t, data.statistics)

	// no context.
	data = &queryData{}
	_, _ = StatsCollectorMiddleware().Wrap(queryrange.HandlerFunc(func(ctx context.Context, r queryrange.Request) (queryrange.Response, error) {
		return nil, nil
	})).Do(context.Background(), &LokiRequest{
		Query:   "foo",
		StartTs: time.Now(),
	})
	require.Equal(t, false, data.recorded)

	// stats
	data = &queryData{}
	ctx = context.WithValue(context.Background(), ctxKey, data)
	_, _ = StatsCollectorMiddleware().Wrap(queryrange.HandlerFunc(func(ctx context.Context, r queryrange.Request) (queryrange.Response, error) {
		return &LokiPromResponse{
			Statistics: stats.Result{
				Ingester: stats.Ingester{
					TotalReached: 10,
				},
			},
		}, nil
	})).Do(ctx, &LokiRequest{
		Query:   "foo",
		StartTs: time.Now(),
	})
	require.Equal(t, "foo", data.query)
	require.Equal(t, true, data.recorded)
	require.Equal(t, logql.RangeType, data.rangeType)
	require.Equal(t, int32(10), data.statistics.Ingester.TotalReached)
}

func Test_StatsHTTP(t *testing.T) {
	for _, test := range []struct {
		name   string
		next   http.Handler
		expect func(t *testing.T, status, query string, rangeType logql.QueryRangeType, s stats.Result)
	}{
		{
			"should not record metric if nothing is recorded",
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data := r.Context().Value(ctxKey).(*queryData)
				data.recorded = false
			}),
			func(t *testing.T, status, query string, rangeType logql.QueryRangeType, s stats.Result) {
				t.Fail()
			},
		},
		{
			"empty statistics success",
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data := r.Context().Value(ctxKey).(*queryData)
				data.recorded = true
				data.rangeType = logql.RangeType
				data.query = "foo"
				data.statistics = nil
			}),
			func(t *testing.T, status, query string, rangeType logql.QueryRangeType, s stats.Result) {
				require.Equal(t, fmt.Sprintf("%d", http.StatusOK), status)
				require.Equal(t, logql.RangeType, rangeType)
				require.Equal(t, "foo", query)
				require.Equal(t, stats.Result{}, s)
			},
		},
		{
			"statuscode",
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data := r.Context().Value(ctxKey).(*queryData)
				data.recorded = true
				data.rangeType = logql.RangeType
				data.query = "foo"
				data.statistics = &statsResult
				w.WriteHeader(http.StatusTeapot)
			}),
			func(t *testing.T, status, query string, rangeType logql.QueryRangeType, s stats.Result) {
				require.Equal(t, fmt.Sprintf("%d", http.StatusTeapot), status)
				require.Equal(t, logql.RangeType, rangeType)
				require.Equal(t, "foo", query)
				require.Equal(t, statsResult, s)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			statsHTTPMiddleware(metricRecorderFn(func(status, query string, rangeType logql.QueryRangeType, stats stats.Result) {
				test.expect(t, status, query, rangeType, stats)
			})).Wrap(test.next).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/foo", strings.NewReader("")))
		})
	}
}

func Test_StatsUpdateResult(t *testing.T) {
	resp, err := StatsCollectorMiddleware().Wrap(queryrange.HandlerFunc(func(c context.Context, r queryrange.Request) (queryrange.Response, error) {
		time.Sleep(20 * time.Millisecond)
		return &LokiResponse{}, nil
	})).Do(context.Background(), &LokiRequest{
		Query: "foo",
		EndTs: time.Now(),
	})
	require.NoError(t, err)
	require.GreaterOrEqual(t, resp.(*LokiResponse).Statistics.Summary.ExecTime, (20 * time.Millisecond).Seconds())
}
