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

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql/stats"
)

func TestStatsCollectorMiddleware(t *testing.T) {
	// no stats
	var (
		data = &queryData{}
		now  = time.Now()
	)
	ctx := context.WithValue(context.Background(), ctxKey, data)
	_, _ = StatsCollectorMiddleware().Wrap(queryrange.HandlerFunc(func(ctx context.Context, r queryrange.Request) (queryrange.Response, error) {
		return nil, nil
	})).Do(ctx, &LokiRequest{
		Query:   "foo",
		StartTs: now,
	})
	require.Equal(t, "foo", data.params.Query())
	require.Equal(t, true, data.recorded)
	require.Equal(t, now, data.params.Start())
	require.Nil(t, data.statistics)

	// no context.
	data = &queryData{}
	_, _ = StatsCollectorMiddleware().Wrap(queryrange.HandlerFunc(func(ctx context.Context, r queryrange.Request) (queryrange.Response, error) {
		return nil, nil
	})).Do(context.Background(), &LokiRequest{
		Query:   "foo",
		StartTs: now,
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
		StartTs: now,
	})
	require.Equal(t, "foo", data.params.Query())
	require.Equal(t, true, data.recorded)
	require.Equal(t, now, data.params.Start())
	require.Equal(t, int32(10), data.statistics.Ingester.TotalReached)
}

func Test_StatsHTTP(t *testing.T) {
	for _, test := range []struct {
		name   string
		next   http.Handler
		expect func(t *testing.T, data *queryData)
	}{
		{
			"should not record metric if nothing is recorded",
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data := r.Context().Value(ctxKey).(*queryData)
				data.recorded = false
			}),
			func(t *testing.T, data *queryData) {
				t.Fail()
			},
		},
		{
			"empty statistics success",
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data := r.Context().Value(ctxKey).(*queryData)
				data.recorded = true
				data.params = paramsFromRequest(&LokiRequest{
					Query:     "foo",
					Direction: logproto.BACKWARD,
					Limit:     100,
				})
				data.statistics = nil
			}),
			func(t *testing.T, data *queryData) {
				require.Equal(t, fmt.Sprintf("%d", http.StatusOK), data.status)
				require.Equal(t, "foo", data.params.Query())
				require.Equal(t, logproto.BACKWARD, data.params.Direction())
				require.Equal(t, uint32(100), data.params.Limit())
				require.Equal(t, stats.Result{}, *data.statistics)
			},
		},
		{
			"statuscode",
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data := r.Context().Value(ctxKey).(*queryData)
				data.recorded = true
				data.params = paramsFromRequest(&LokiRequest{
					Query:     "foo",
					Direction: logproto.BACKWARD,
					Limit:     100,
				})
				data.statistics = &statsResult
				w.WriteHeader(http.StatusTeapot)
			}),
			func(t *testing.T, data *queryData) {
				require.Equal(t, fmt.Sprintf("%d", http.StatusTeapot), data.status)
				require.Equal(t, "foo", data.params.Query())
				require.Equal(t, logproto.BACKWARD, data.params.Direction())
				require.Equal(t, uint32(100), data.params.Limit())
				require.Equal(t, statsResult, *data.statistics)
			},
		},
		{
			"result",
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data := r.Context().Value(ctxKey).(*queryData)
				data.recorded = true
				data.params = paramsFromRequest(&LokiRequest{
					Query:     "foo",
					Direction: logproto.BACKWARD,
					Limit:     100,
				})
				data.statistics = &statsResult
				data.result = streams
				w.WriteHeader(http.StatusTeapot)
			}),
			func(t *testing.T, data *queryData) {
				require.Equal(t, fmt.Sprintf("%d", http.StatusTeapot), data.status)
				require.Equal(t, "foo", data.params.Query())
				require.Equal(t, logproto.BACKWARD, data.params.Direction())
				require.Equal(t, uint32(100), data.params.Limit())
				require.Equal(t, statsResult, *data.statistics)
				require.Equal(t, streams, data.result)
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			statsHTTPMiddleware(metricRecorderFn(func(data *queryData) {
				test.expect(t, data)
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
