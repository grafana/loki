package queryrange

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	strings "strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
)

func TestStatsCollectorMiddleware(t *testing.T) {
	// no stats
	var (
		data = &queryData{}
		now  = time.Now()
	)
	ctx := context.WithValue(context.Background(), ctxKey, data)
	_, _ = StatsCollectorMiddleware().Wrap(queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		return nil, nil
	})).Do(ctx, &LokiRequest{
		Query:   "foo",
		StartTs: now,
	})
	require.Equal(t, "foo", data.params.QueryString())
	require.Equal(t, true, data.recorded)
	require.Equal(t, now, data.params.Start())
	require.Nil(t, data.statistics)

	// no context.
	data = &queryData{}
	_, _ = StatsCollectorMiddleware().Wrap(queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		return nil, nil
	})).Do(context.Background(), &LokiRequest{
		Query:   "foo",
		StartTs: now,
	})
	require.Equal(t, false, data.recorded)

	// stats
	data = &queryData{}
	ctx = context.WithValue(context.Background(), ctxKey, data)
	_, _ = StatsCollectorMiddleware().Wrap(queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
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
	require.Equal(t, "foo", data.params.QueryString())
	require.Equal(t, true, data.recorded)
	require.Equal(t, now, data.params.Start())
	require.Equal(t, int32(10), data.statistics.Ingester.TotalReached)

	// Do not collect stats if the `next` handler returns error.
	// Rationale being, in that case returned `response` will be nil and there won't be any `response.statistics` to collect.
	data = &queryData{}
	ctx = context.WithValue(context.Background(), ctxKey, data)
	_, _ = StatsCollectorMiddleware().Wrap(queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		return nil, errors.New("request timedout")
	})).Do(ctx, &LokiRequest{
		Query:   "foo",
		StartTs: now,
	})
	require.Equal(t, false, data.recorded)
}

func Test_StatsHTTP(t *testing.T) {
	for _, test := range []struct {
		name   string
		next   http.Handler
		expect func(t *testing.T, data *queryData)
	}{
		{
			"should not record metric if nothing is recorded",
			http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				data := r.Context().Value(ctxKey).(*queryData)
				data.recorded = false
			}),
			func(t *testing.T, _ *queryData) {
				t.Fail()
			},
		},
		{
			"empty statistics success",
			http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				data := r.Context().Value(ctxKey).(*queryData)
				data.recorded = true
				data.params, _ = ParamsFromRequest(&LokiRequest{
					Query:     "foo",
					Direction: logproto.BACKWARD,
					Limit:     100,
				})
				data.statistics = nil
			}),
			func(t *testing.T, data *queryData) {
				require.Equal(t, fmt.Sprintf("%d", http.StatusOK), data.status)
				require.Equal(t, "foo", data.params.QueryString())
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
				data.params, _ = ParamsFromRequest(&LokiRequest{
					Query:     "foo",
					Direction: logproto.BACKWARD,
					Limit:     100,
				})
				data.statistics = &statsResult
				w.WriteHeader(http.StatusTeapot)
			}),
			func(t *testing.T, data *queryData) {
				require.Equal(t, fmt.Sprintf("%d", http.StatusTeapot), data.status)
				require.Equal(t, "foo", data.params.QueryString())
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
				data.params, _ = ParamsFromRequest(&LokiRequest{
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
				require.Equal(t, "foo", data.params.QueryString())
				require.Equal(t, logproto.BACKWARD, data.params.Direction())
				require.Equal(t, uint32(100), data.params.Limit())
				require.Equal(t, statsResult, *data.statistics)
				require.Equal(t, streams, data.result)
			},
		},
		{
			"volume request",
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data := r.Context().Value(ctxKey).(*queryData)
				data.recorded = true
				data.params, _ = ParamsFromRequest(&logproto.VolumeRequest{
					Matchers: "foo",
					Limit:    100,
				})
				data.statistics = &statsResult
				data.result = streams
				w.WriteHeader(http.StatusTeapot)
			}),
			func(t *testing.T, data *queryData) {
				require.Equal(t, fmt.Sprintf("%d", http.StatusTeapot), data.status)
				require.Equal(t, "foo", data.params.QueryString())
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
	resp, err := StatsCollectorMiddleware().Wrap(queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		time.Sleep(20 * time.Millisecond)
		return &LokiResponse{}, nil
	})).Do(context.Background(), &LokiRequest{
		Query: "foo",
		EndTs: time.Now(),
	})
	require.NoError(t, err)
	require.GreaterOrEqual(t, resp.(*LokiResponse).Statistics.Summary.ExecTime, (20 * time.Millisecond).Seconds())
}
