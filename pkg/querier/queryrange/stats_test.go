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

	"github.com/prometheus/common/model"
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
					Direction: logproto.Direction_BACKWARD,
					Limit:     100,
				})
				data.statistics = nil
			}),
			func(t *testing.T, data *queryData) {
				require.Equal(t, fmt.Sprintf("%d", http.StatusOK), data.status)
				require.Equal(t, "foo", data.params.QueryString())
				require.Equal(t, logproto.Direction_BACKWARD, data.params.Direction())
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
					Direction: logproto.Direction_BACKWARD,
					Limit:     100,
				})
				data.statistics = &statsResult
				w.WriteHeader(http.StatusTeapot)
			}),
			func(t *testing.T, data *queryData) {
				require.Equal(t, fmt.Sprintf("%d", http.StatusTeapot), data.status)
				require.Equal(t, "foo", data.params.QueryString())
				require.Equal(t, logproto.Direction_BACKWARD, data.params.Direction())
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
					Direction: logproto.Direction_BACKWARD,
					Limit:     100,
				})
				data.statistics = &statsResult
				data.result = streams
				w.WriteHeader(http.StatusTeapot)
			}),
			func(t *testing.T, data *queryData) {
				require.Equal(t, fmt.Sprintf("%d", http.StatusTeapot), data.status)
				require.Equal(t, "foo", data.params.QueryString())
				require.Equal(t, logproto.Direction_BACKWARD, data.params.Direction())
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

func TestStatsCollectorMiddleware_PropagatesEstimatedQueryBytesFromIndexStats(t *testing.T) {
	data := &queryData{}
	ctx := context.WithValue(context.Background(), ctxKey, data)
	mw := StatsCollectorMiddleware().Wrap(queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		switch req.(type) {
		case *logproto.IndexStatsRequest:
			return &IndexStatsResponse{
				Response: &logproto.IndexStatsResponse{
					Bytes: 1024,
				},
			}, nil
		case *LokiRequest:
			return &LokiResponse{}, nil
		default:
			return nil, fmt.Errorf("unexpected request type %T", req)
		}
	}))

	req := &logproto.IndexStatsRequest{
		From:     model.Time(100),
		Through:  model.Time(200),
		Matchers: `{foo="bar"}`,
	}

	_, err := mw.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, int64(1024), data.estimatedQueryBytes)

	_, err = mw.Do(ctx, req)
	require.NoError(t, err)
	require.Equal(t, int64(1024), data.estimatedQueryBytes)

	_, err = mw.Do(ctx, &logproto.IndexStatsRequest{
		From:     model.Time(100),
		Through:  model.Time(200),
		Matchers: `{baz="qux"}`,
	})
	require.NoError(t, err)
	require.Equal(t, int64(2048), data.estimatedQueryBytes)

	resp, err := mw.Do(ctx, &LokiRequest{Query: "foo", StartTs: time.Now()})
	require.NoError(t, err)
	lokiResp, ok := resp.(*LokiResponse)
	require.True(t, ok)
	require.Equal(t, int64(2048), lokiResp.Statistics.Summary.EstimatedQueryBytes)
}

func TestStatsCollectorMiddleware_DoesNotOverwriteLargerEstimatedQueryBytes(t *testing.T) {
	data := &queryData{
		estimatedQueryBytes: 1024,
	}
	ctx := context.WithValue(context.Background(), ctxKey, data)
	mw := StatsCollectorMiddleware().Wrap(queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		switch req.(type) {
		case *LokiRequest:
			return &LokiResponse{
				Statistics: stats.Result{
					Summary: stats.Summary{
						EstimatedQueryBytes: 4096,
					},
				},
			}, nil
		default:
			return nil, fmt.Errorf("unexpected request type %T", req)
		}
	}))

	resp, err := mw.Do(ctx, &LokiRequest{Query: "foo", StartTs: time.Now()})
	require.NoError(t, err)
	lokiResp, ok := resp.(*LokiResponse)
	require.True(t, ok)
	require.Equal(t, int64(4096), lokiResp.Statistics.Summary.EstimatedQueryBytes)
}

func TestIndexStatsContextCollectorMiddleware_DedupesAcrossCollectorPaths(t *testing.T) {
	data := &queryData{}
	ctx := context.WithValue(context.Background(), ctxKey, data)
	indexReq := &logproto.IndexStatsRequest{
		From:     model.Time(100),
		Through:  model.Time(200),
		Matchers: `{foo="bar"}`,
	}

	mw := queryrangebase.MergeMiddlewares(
		StatsCollectorMiddleware(),
		IndexStatsContextCollectorMiddleware(),
	).Wrap(queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		switch req.(type) {
		case *logproto.IndexStatsRequest:
			return &IndexStatsResponse{
				Response: &logproto.IndexStatsResponse{
					Bytes: 1024,
				},
			}, nil
		case *LokiRequest:
			return &LokiResponse{}, nil
		default:
			return nil, fmt.Errorf("unexpected request type %T", req)
		}
	}))

	_, err := mw.Do(ctx, indexReq)
	require.NoError(t, err)
	require.Equal(t, int64(1024), data.estimatedQueryBytes)

	_, err = mw.Do(ctx, &logproto.IndexStatsRequest{
		From:     model.Time(100),
		Through:  model.Time(200),
		Matchers: `{bar="baz"}`,
	})
	require.NoError(t, err)
	require.Equal(t, int64(2048), data.estimatedQueryBytes)

	_, err = mw.Do(ctx, indexReq)
	require.NoError(t, err)
	require.Equal(t, int64(2048), data.estimatedQueryBytes)

	resp, err := mw.Do(ctx, &LokiRequest{Query: "foo", StartTs: time.Now()})
	require.NoError(t, err)
	lokiResp, ok := resp.(*LokiResponse)
	require.True(t, ok)
	require.Equal(t, int64(2048), lokiResp.Statistics.Summary.EstimatedQueryBytes)
}
