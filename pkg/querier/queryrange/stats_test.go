package queryrange

import (
	"context"
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

	// Do not collect stats if the `next` handler returns an error: the returned
	// `response` is nil, so there are no `response.statistics` to collect. A
	// failed query gets the dedicated usage line instead (see stats_partial_test.go).
	data = &queryData{}
	ctx = context.WithValue(context.Background(), ctxKey, data)
	_, err := StatsCollectorMiddleware().Wrap(queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		return nil, context.DeadlineExceeded
	})).Do(ctx, &LokiRequest{
		Query:   "foo",
		StartTs: now,
	})
	require.ErrorIs(t, err, context.DeadlineExceeded) // original error is still returned
	require.Equal(t, false, data.recorded)
	require.Nil(t, data.statistics)
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

func TestMergeIndexStatsRange_DoesNotDoubleCountNestedRanges(t *testing.T) {
	const matchers = `{foo="bar"}`
	hour := model.Time(time.Hour / time.Millisecond)
	full := indexStatsRange{from: 0, through: 2 * hour, matchers: matchers, bytes: 1000}
	firstSplit := indexStatsRange{from: 0, through: hour, matchers: matchers, bytes: 400}
	secondSplit := indexStatsRange{from: hour, through: 2 * hour, matchers: matchers, bytes: 600}
	otherMatchers := indexStatsRange{from: 0, through: 2 * hour, matchers: `{baz="qux"}`, bytes: 1000}

	for _, tc := range []struct {
		name  string
		input []indexStatsRange
		want  int64
	}{
		{
			name:  "full range then nested splits matches the covering haystack",
			input: []indexStatsRange{full, firstSplit, secondSplit},
			want:  1000,
		},
		{
			name:  "nested splits then covering range replaces the split sum",
			input: []indexStatsRange{firstSplit, secondSplit, full},
			want:  1000,
		},
		{
			name:  "non-overlapping splits are summed",
			input: []indexStatsRange{firstSplit, secondSplit},
			want:  1000,
		},
		{
			name:  "identical range is recorded once",
			input: []indexStatsRange{full, full},
			want:  1000,
		},
		{
			name:  "different matchers are summed",
			input: []indexStatsRange{full, otherMatchers},
			want:  2000,
		},
		{
			name:  "nested splits for one matcher do not drop another matcher",
			input: []indexStatsRange{full, firstSplit, secondSplit, otherMatchers},
			want:  2000,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var ranges []indexStatsRange
			for _, next := range tc.input {
				ranges = mergeIndexStatsRange(ranges, next)
			}
			require.Equal(t, tc.want, sumIndexStatsBytes(ranges))
		})
	}
}

func TestQueryData_RecordEstimatedQueryBytes_DoesNotDoubleCountNestedRanges(t *testing.T) {
	hour := model.Time(time.Hour / time.Millisecond)
	data := &queryData{}
	matchers := `{app="loki"}`

	data.recordEstimatedQueryBytes(&logproto.IndexStatsRequest{
		From:     0,
		Through:  2 * hour,
		Matchers: matchers,
	}, 60<<30)
	data.recordEstimatedQueryBytes(&logproto.IndexStatsRequest{
		From:     0,
		Through:  hour,
		Matchers: matchers,
	}, 30<<30)
	data.recordEstimatedQueryBytes(&logproto.IndexStatsRequest{
		From:     hour,
		Through:  2 * hour,
		Matchers: matchers,
	}, 30<<30)

	require.Equal(t, int64(60<<30), data.estimatedQueryBytes)
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

func TestStatsCollectorMiddleware_DoesNotDoubleCountNestedIndexStatsRanges(t *testing.T) {
	hour := model.Time(time.Hour / time.Millisecond)
	matchers := `{foo="bar"}`
	full := &logproto.IndexStatsRequest{From: 0, Through: 2 * hour, Matchers: matchers}
	firstSplit := &logproto.IndexStatsRequest{From: 0, Through: hour, Matchers: matchers}
	secondSplit := &logproto.IndexStatsRequest{From: hour, Through: 2 * hour, Matchers: matchers}
	bytesByReq := map[indexStatsRange]uint64{
		{from: full.From, through: full.Through, matchers: matchers}:               1000,
		{from: firstSplit.From, through: firstSplit.Through, matchers: matchers}:   400,
		{from: secondSplit.From, through: secondSplit.Through, matchers: matchers}: 600,
	}

	for _, tc := range []struct {
		name  string
		order []*logproto.IndexStatsRequest
	}{
		{
			name:  "full range then per-split stats",
			order: []*logproto.IndexStatsRequest{full, firstSplit, secondSplit},
		},
		{
			name:  "per-split stats then covering range",
			order: []*logproto.IndexStatsRequest{firstSplit, secondSplit, full},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := &queryData{}
			ctx := context.WithValue(context.Background(), ctxKey, data)
			mw := queryrangebase.MergeMiddlewares(
				StatsCollectorMiddleware(),
				IndexStatsContextCollectorMiddleware(),
			).Wrap(queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
				switch r := req.(type) {
				case *logproto.IndexStatsRequest:
					bytes, ok := bytesByReq[indexStatsRange{from: r.From, through: r.Through, matchers: r.Matchers}]
					if !ok {
						return nil, fmt.Errorf("unexpected index stats range %s %s %s", r.From, r.Through, r.Matchers)
					}
					return &IndexStatsResponse{
						Response: &logproto.IndexStatsResponse{Bytes: bytes},
					}, nil
				case *LokiRequest:
					return &LokiResponse{}, nil
				default:
					return nil, fmt.Errorf("unexpected request type %T", req)
				}
			}))

			for _, req := range tc.order {
				_, err := mw.Do(ctx, req)
				require.NoError(t, err)
			}
			require.Equal(t, int64(1000), data.estimatedQueryBytes)

			resp, err := mw.Do(ctx, &LokiRequest{Query: "foo", StartTs: time.Now()})
			require.NoError(t, err)
			lokiResp, ok := resp.(*LokiResponse)
			require.True(t, ok)
			require.Equal(t, int64(1000), lokiResp.Statistics.Summary.EstimatedQueryBytes)
		})
	}
}
