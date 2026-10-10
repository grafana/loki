package queryrange

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	strings "strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/grafana/dskit/user"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/util"
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

func TestStatsCollectorMiddleware_InternalRequestIsNotRecorded(t *testing.T) {
	data := &queryData{}
	ctx := context.WithValue(context.Background(), ctxKey, data)
	mw := StatsCollectorMiddleware().Wrap(queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		switch req.(type) {
		case *logproto.IndexStatsRequest:
			return &IndexStatsResponse{Response: &logproto.IndexStatsResponse{Bytes: 1024}}, nil
		case *LokiRequest:
			return nil, context.Canceled
		default:
			return nil, fmt.Errorf("unexpected request type %T", req)
		}
	}))

	_, err := mw.Do(WithInternalRequest(ctx), &logproto.IndexStatsRequest{
		From:     model.Time(100),
		Through:  model.Time(200),
		Matchers: `{foo="bar"}`,
	})
	require.NoError(t, err)
	require.False(t, data.recorded)
	require.Equal(t, int64(1024), data.estimatedQueryBytes)

	_, err = mw.Do(ctx, &LokiRequest{Query: "foo", StartTs: time.Now()})
	require.ErrorIs(t, err, context.Canceled)
	require.False(t, data.recorded, "a failed query must not log the internal index stats request")
}

func TestStatsCollectorMiddleware_RecordsStatsAddedAfterCollection(t *testing.T) {
	data := &queryData{}
	ctx := context.WithValue(context.Background(), ctxKey, data)
	mw := StatsCollectorMiddleware().Wrap(queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		return &LokiResponse{}, nil
	}))

	resp, err := mw.Do(ctx, &LokiRequest{Query: "foo", StartTs: time.Now()})
	require.NoError(t, err)

	// Middlewares outside the tripperware, such as the logline hint prefetch,
	// attach stats to the response after StatsCollectorMiddleware returns.
	resp.(*LokiResponse).Statistics.Index.LoglineHintStatus = "ok"
	require.Equal(t, "ok", data.statistics.Index.LoglineHintStatus)
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

func TestStatsCollectorMiddleware_SeriesLimitIncludesCompletedUsage(t *testing.T) {
	ctx := user.InjectOrgID(context.WithValue(context.Background(), ctxKey, &queryData{}), "test")
	lines := captureFailedQueryUsage(t)
	query := `sum by (series) (count_over_time({app="test"}[1m]))`
	request := &LokiRequest{Query: query, StartTs: time.Unix(0, 0), EndTs: time.Unix(3600, 0), Step: 1000}

	// Collect the first response, reject the second, and leave the third pending.
	first, rejected, pending := &LokiRequest{}, &LokiRequest{}, &LokiRequest{}
	inputs := []*lokiResult{
		{req: first, ch: make(chan *packedResp)},
		{req: rejected, ch: make(chan *packedResp)},
		// Observe completion without letting the ordered collector consume this response.
		{req: pending, ch: make(chan *packedResp, 1)},
	}

	// Delay rejection until the pending response has registered its statistics.
	pendingStarted := make(chan struct{})
	var returned atomic.Int64
	split := &splitByInterval{next: queryrangebase.HandlerFunc(func(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
		defer returned.Add(1)
		switch req {
		case pending:
			close(pendingStarted)
			return metricResponseWithScanUsage(200, "a"), nil
		case first:
			return metricResponseWithScanUsage(100, "a"), nil
		default:
			select {
			case <-pendingStarted:
			case <-ctx.Done():
				return nil, ctx.Err()
			}

			// Registration happens before sending to the buffered channel. Wait for
			// that send so the test cannot race finalization against registration.
			ticker := time.NewTicker(time.Millisecond)
			defer ticker.Stop()
			timeout := time.NewTimer(5 * time.Second)
			defer timeout.Stop()
			for len(inputs[2].ch) == 0 {
				select {
				case <-ticker.C:
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-timeout.C:
					return nil, errors.New("pending response did not finish")
				}
			}
			return metricResponseWithScanUsage(50, "b"), nil
		}
	})}

	// Run the real splitter and series limiter inside the root stats collector.
	handler := StatsCollectorMiddleware().Wrap(queryrangebase.HandlerFunc(func(ctx context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		responses, err := split.Process(ctx, 3, 0, inputs, 1)
		if err != nil {
			if responses != nil {
				return nil, errors.New("failed collection returned responses")
			}
			return nil, err
		}
		return DefaultCodec.MergeResponse(responses...)
	}))

	// The failed root query must report usage from all three responses once.
	response, err := handler.Do(ctx, request)
	require.ErrorContains(t, err, "maximum number of series")
	require.Nil(t, response)
	require.Equal(t, int64(3), returned.Load())

	line := lines.only(t)
	requireFailedQueryUsageShape(t, line)
	require.Equal(t, util.HumanizeBytes(350), line["total_bytes"], "100 collected + 50 rejected + 200 pending")
}

// TestQueryUsageHTTPFlow exercises HTTP decoding, real middleware/collection
// channels, response encoding, and the final usage log. Only downstream query
// execution is controlled by the test; no remote workers or log shipping run here.
func TestQueryUsageHTTPFlow(t *testing.T) {
	// The three intervals scan 100, 200, and 300 bytes. Workers that fail in
	// this test return no statistics. Usage from workers that finish after
	// the query returns is excluded.
	for _, tc := range []struct {
		name              string
		limit             int
		downstreamErr     error
		cancelClient      bool
		lateResponse      bool
		exceedSeriesLimit bool
		mergeErr          error
		wantStatus        int
		wantBytes         int64
		wantEntries       int
		wantSplits        int64
	}{
		{
			name: "complete", limit: 1000,
			wantStatus: http.StatusOK, wantBytes: 600, wantEntries: 3, wantSplits: 3,
		},
		{
			name: "line limit", limit: 1,
			wantStatus: http.StatusOK, wantBytes: 600, wantEntries: 1, wantSplits: 3,
		},
		{
			name: "line limit after two splits", limit: 2,
			wantStatus: http.StatusOK, wantBytes: 600, wantEntries: 2, wantSplits: 3,
		},
		{
			name: "failure", limit: 1000, downstreamErr: errors.New("downstream query failed"),
			wantStatus: http.StatusInternalServerError, wantBytes: 400,
		},
		{
			name: "downstream cancellation", limit: 1000, downstreamErr: context.Canceled,
			wantStatus: 499, wantBytes: 400,
		},
		{
			// The first interval waits for cancellation and returns no stats.
			name: "client cancellation", limit: 1000, cancelClient: true,
			wantStatus: 499, wantBytes: 500,
		},
		{
			name: "merge failure", limit: 1000, mergeErr: errors.New("final response merge failed"),
			wantStatus: http.StatusInternalServerError, wantBytes: 600,
		},
		{
			name: "series limit", limit: 1000, exceedSeriesLimit: true,
			wantStatus: http.StatusBadRequest, wantBytes: 600,
		},
		{
			// The second interval finishes after usage is finalized.
			name: "late response", limit: 1, lateResponse: true,
			wantStatus: http.StatusOK, wantBytes: 400, wantEntries: 1, wantSplits: 2,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			failedUsage := captureFailedQueryUsage(t)
			synctest.Test(t, func(t *testing.T) {
				// Split the request into three hourly intervals.
				start := time.Unix(0, 0)
				interval := time.Hour
				const intervalCount = 3

				query := `{app="test"}`
				if tc.exceedSeriesLimit {
					query = `sum by (series) (count_over_time({app="test"}[1m]))`
				}

				// Hold the first response so later intervals finish before collection resumes.
				releaseFirst := make(chan struct{})
				releaseLate := make(chan struct{})
				defer close(releaseLate)

				var calls atomic.Int64
				leaf := queryrangebase.HandlerFunc(func(ctx context.Context, request queryrangebase.Request) (queryrangebase.Response, error) {
					index := int(request.GetStart().Sub(start) / interval)
					calls.Add(1)

					if tc.lateResponse && index == 1 {
						// Simulate a worker returning successfully after the parent
						// has canceled it and finalized the HTTP response.
						<-releaseLate
					}
					if tc.cancelClient && index == 0 {
						<-ctx.Done()
						return nil, ctx.Err()
					}

					if index == 0 || (tc.exceedSeriesLimit && index == 1) {
						select {
						case <-releaseFirst:
						case <-ctx.Done():
							return nil, ctx.Err()
						}
					}

					if index == 1 && tc.downstreamErr != nil {
						return nil, tc.downstreamErr
					}

					bytesScanned := int64(100 * (index + 1))
					if tc.exceedSeriesLimit {
						series := "a"
						if index == 1 {
							series = "b"
						}
						return metricResponseWithScanUsage(bytesScanned, series), nil
					}

					response := logResponseWithScanUsage(bytesScanned)
					response.Limit = request.(*LokiRequest).Limit
					response.Direction = request.(*LokiRequest).Direction
					return response, nil
				})

				// Use the real interval splitter, with a failing merger for the merge-error case.
				var merger queryrangebase.Merger = DefaultCodec
				if tc.mergeErr != nil {
					merger = &responseMergerWithInjectedError{err: tc.mergeErr}
				}

				limits := WithSplitByLimits(fakeLimits{maxSeries: 1, maxQueryParallelism: intervalCount}, interval)
				processing := SplitByIntervalMiddleware(testSchemas, limits, merger, newDefaultSplitter(fakeLimits{}, nil), nilMetrics).Wrap(leaf)

				// Wrap processing in the HTTP stack and capture successful usage totals.
				var successfulUsage []int64
				handler := statsHTTPMiddleware(metricRecorderFn(func(data *queryData) {
					successfulUsage = append(successfulUsage, data.statistics.Summary.TotalBytesProcessed)
				})).Wrap(NewSerializeHTTPHandler(StatsCollectorMiddleware().Wrap(processing), DefaultCodec))

				params := url.Values{
					"query": {query}, "start": {"0"}, "end": {"10800"},
					"step": {"1"}, "direction": {"forward"}, "limit": {strconv.Itoa(tc.limit)},
				}
				request := httptest.NewRequest(http.MethodGet, "/loki/api/v1/query_range?"+params.Encode(), nil)
				ctx, cancel := context.WithCancel(user.InjectOrgID(request.Context(), "test"))
				defer cancel()
				request = request.WithContext(ctx)
				response := httptest.NewRecorder()

				// Start the request while the first response is still held by the test.
				done := make(chan struct{})
				go func() {
					defer close(done)
					handler.ServeHTTP(response, request)
				}()

				// Workers register completed statistics before blocking on real,
				// unbuffered delivery channels. No sleeps or production test hooks.
				synctest.Wait()
				require.Equal(t, int64(intervalCount), calls.Load())
				select {
				case <-done:
					t.Fatal("request returned before the first response was released")
				default:
				}

				// The other workers are now blocked on delivery or a test channel.
				// Release the first response, or cancel the client, to let the query finish.
				if tc.cancelClient {
					cancel()
				} else {
					close(releaseFirst)
				}
				<-done

				// Late work completes after the HTTP result, so it must not change usage.
				if tc.lateResponse {
					releaseLate <- struct{}{}
				}
				synctest.Wait()

				// Check the HTTP status and the corresponding success or failure accounting.
				require.Equal(t, tc.wantStatus, response.Code, response.Body.String())
				if tc.wantStatus == http.StatusOK {
					require.Empty(t, failedUsage.all())
					require.Equal(t, []int64{tc.wantBytes}, successfulUsage)

					// Successful response statistics must agree with the recorded usage.
					var body struct {
						Data struct {
							Stats  stats.Result `json:"stats"`
							Result []struct {
								Values []json.RawMessage `json:"values"`
							} `json:"result"`
						} `json:"data"`
					}
					require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
					require.Equal(t, tc.wantBytes, body.Data.Stats.Summary.TotalBytesProcessed)

					require.Equal(t, tc.wantSplits, body.Data.Stats.Summary.Splits,
						"each completed interval counts once, including discarded responses")

					// Recovered scan usage must not add discarded entries to the response.
					entries := 0
					for _, stream := range body.Data.Result {
						entries += len(stream.Values)
					}
					require.Equal(t, tc.wantEntries, entries)
				} else {
					require.Empty(t, successfulUsage, "failures must not emit a successful usage record")

					line := failedUsage.only(t)
					requireFailedQueryUsageShape(t, line)
					require.Equal(t, util.HumanizeBytes(uint64(tc.wantBytes)), line["total_bytes"])
					require.Equal(t, strconv.Itoa(tc.wantStatus), line["status"])
				}
			})
		})
	}
}

func TestQueryUsageHTTPFlow_ShardFailureBeforeIntervalLineLimit(t *testing.T) {
	failedUsage := captureFailedQueryUsage(t)
	synctest.Test(t, func(t *testing.T) {
		start := time.Unix(0, 0)
		shardsFinished := make(chan struct{})
		shardFailure := errors.New("second shard failed")
		var intervalErr error

		// Let the later interval finish its shard execution before the first
		// interval returns one entry and satisfies the root query's line limit.
		limits := WithSplitByLimits(fakeLimits{maxQueryParallelism: 2}, time.Hour)
		processing := SplitByIntervalMiddleware(testSchemas, limits, DefaultCodec, newDefaultSplitter(limits, nil), nilMetrics).Wrap(queryrangebase.HandlerFunc(func(ctx context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
			if req.GetStart().Equal(start) {
				select {
				case <-shardsFinished:
					return logResponseWithScanUsage(100), nil
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			defer close(shardsFinished)

			// Identify the two shards by their start times. Run them serially
			// so the first shard's 200 bytes reach the accumulator before the
			// second shard fails and the evaluator records partial usage.
			queries := make([]logql.DownstreamQuery, 2)
			for i := range queries {
				params, err := logql.NewLiteralParams(`{app="test"}`, start.Add(time.Hour).Add(time.Duration(i)*time.Second), start.Add(2*time.Hour), 0, 0, logproto.FORWARD, 1, nil, nil)
				require.NoError(t, err)
				queries[i].Params = params
			}
			next := queryrangebase.HandlerFunc(func(_ context.Context, shardReq queryrangebase.Request) (queryrangebase.Response, error) {
				if shardReq.GetStart().Equal(start.Add(time.Hour)) {
					return logResponseWithScanUsage(200), nil
				}
				return nil, shardFailure
			})
			downstream := DownstreamHandler{limits: fakeLimits{maxQueryParallelism: 1}, next: next}.Downstreamer(ctx)
			_, err := logql.NewDownstreamEvaluator(downstream).Downstream(ctx, queries, logql.NewStreamAccumulator(queries[0].Params))
			intervalErr = err
			return nil, err
		}))

		// Exercise the HTTP stack and capture usage for the successful root query.
		var successfulUsage []stats.Result
		handler := statsHTTPMiddleware(metricRecorderFn(func(data *queryData) {
			successfulUsage = append(successfulUsage, *data.statistics)
		})).Wrap(NewSerializeHTTPHandler(StatsCollectorMiddleware().Wrap(processing), DefaultCodec))
		params := url.Values{
			"query": {`{app="test"}`}, "start": {"0"}, "end": {"7200"},
			"step": {"1"}, "direction": {"forward"}, "limit": {"1"},
		}
		request := httptest.NewRequest(http.MethodGet, "/loki/api/v1/query_range?"+params.Encode(), nil)
		request = request.WithContext(user.InjectOrgID(request.Context(), "test"))
		response := httptest.NewRecorder()

		handler.ServeHTTP(response, request)
		synctest.Wait()

		// The later interval failed, but the root succeeded. Its usage includes
		// the returned interval's 100 bytes and the failed interval's 200 bytes.
		require.ErrorIs(t, intervalErr, shardFailure)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		require.Empty(t, failedUsage.all(), "a failed interval must not emit a failed root-query record")
		require.Len(t, successfulUsage, 1)
		require.Equal(t, int64(300), successfulUsage[0].Summary.TotalBytesProcessed)
		require.Equal(t, int64(1), successfulUsage[0].Summary.TotalEntriesReturned)
		require.Equal(t, int64(1), successfulUsage[0].Summary.Splits, "failed shard usage must not add another time partition")

		// Serialized statistics must match recorded usage, and only the
		// successful interval's entry should be returned to the client.
		var body struct {
			Data struct {
				Stats  stats.Result `json:"stats"`
				Result []struct {
					Values []json.RawMessage `json:"values"`
				} `json:"result"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
		require.Equal(t, successfulUsage[0].Summary, body.Data.Stats.Summary)
		require.Len(t, body.Data.Result, 1)
		require.Len(t, body.Data.Result[0].Values, 1)
	})
}

func TestQueryUsageHTTPFlow_DiscardedShardsOnFailure(t *testing.T) {
	for _, seriesLimit := range []bool{false, true} {
		t.Run(fmt.Sprintf("seriesLimit=%t", seriesLimit), func(t *testing.T) {
			failedUsage := captureFailedQueryUsage(t)
			synctest.Test(t, func(t *testing.T) {
				// The failed query must report 100 bytes from the accumulator plus
				// 200 from the completed shard whose delivery is discarded.
				query := `{app="test"}`
				wantStatus, wantBytes := http.StatusInternalServerError, uint64(300)
				if seriesLimit {
					query = `sum by (series) (count_over_time({app="test"}[1m]))`
					// The response rejected by the series limiter scanned another 50 bytes.
					wantStatus, wantBytes = http.StatusBadRequest, 350
				}

				// Give each shard a distinct start time so the handler can identify it.
				queries := make([]logql.DownstreamQuery, 3)
				for i := range queries {
					params, err := logql.NewLiteralParams(query, time.Unix(int64(i), 0), time.Unix(int64(i+1), 0), 0, 0, logproto.FORWARD, 2, nil, nil)
					require.NoError(t, err)
					queries[i].Params = params
				}

				// Control completion order: hold shard 0 in the accumulator, let shard 1
				// finish, then fail shard 2 before allowing collection to continue.
				firstReceived := make(chan struct{})
				releaseFirst := make(chan struct{})
				defer close(releaseFirst)
				releaseFailure := make(chan struct{})
				defer close(releaseFailure)

				// Return controlled shard responses through the real downstream handler.
				var next queryrangebase.Handler = queryrangebase.HandlerFunc(func(_ context.Context, req queryrangebase.Request) (queryrangebase.Response, error) {
					switch req.GetStart().Unix() {
					case 0:
						if seriesLimit {
							return metricResponseWithScanUsage(100, "a"), nil
						}
						return logResponseWithScanUsage(100), nil

					case 1:
						<-firstReceived
						if seriesLimit {
							return metricResponseWithScanUsage(200, "a"), nil
						}
						return logResponseWithScanUsage(200), nil

					default:
						<-releaseFailure
						if seriesLimit {
							// The earlier responses share series "a" and fit the limit.
							// Only this new series makes the real limiter reject a response.
							return metricResponseWithScanUsage(50, "b"), nil
						}
						return nil, errors.New("shard execution failed")
					}
				})

				if seriesLimit {
					next = newSeriesLimiter(1).Wrap(next)
				}

				// Connect the downstream handler to the evaluator. Pause the accumulator
				// on shard 0 so shard 1 cannot be collected before shard 2 fails.
				processing := queryrangebase.HandlerFunc(func(ctx context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
					downstream := DownstreamHandler{limits: fakeLimits{}, next: next}.Downstreamer(ctx)

					var accumulator logql.Accumulator = logql.NewStreamAccumulator(queries[0].Params)
					if seriesLimit {
						accumulator = logql.NewBufferedAccumulator(len(queries))
					}

					acc := &gatedShardAccumulator{
						Accumulator:         accumulator,
						firstResultReceived: firstReceived,
						releaseFirstResult:  releaseFirst,
					}

					_, err := logql.NewDownstreamEvaluator(downstream).Downstream(ctx, queries, acc)
					return nil, err
				})

				// Exercise HTTP decoding, statistics collection, and failed-query logging.
				successfulUsageRecords := 0
				handler := statsHTTPMiddleware(metricRecorderFn(func(*queryData) {
					successfulUsageRecords++
				})).Wrap(NewSerializeHTTPHandler(StatsCollectorMiddleware().Wrap(processing), DefaultCodec))

				params := url.Values{"query": {query}, "start": {"0"}, "end": {"3600"}, "direction": {"forward"}, "limit": {"2"}}
				request := httptest.NewRequest(http.MethodGet, "/loki/api/v1/query_range?"+params.Encode(), nil)
				request = request.WithContext(user.InjectOrgID(request.Context(), "test"))
				response := httptest.NewRecorder()

				// Start the request and wait until the completed second shard is blocked.
				done := make(chan struct{})
				go func() {
					defer close(done)
					handler.ServeHTTP(response, request)
				}()

				synctest.Wait() // Shard 1 is complete but cannot reach the blocked accumulator.

				// Trigger the sibling failure before releasing the accumulator. This
				// forces shard 1 to lose delivery, so the tracker must retain its usage.
				releaseFailure <- struct{}{}
				synctest.Wait() // The sibling error cancels shard 1's pending delivery.

				releaseFirst <- struct{}{}
				<-done
				synctest.Wait()

				// Verify one failed usage record contains all completed work, with no
				// successful usage record emitted for the failed request.
				require.Equal(t, wantStatus, response.Code)
				require.Zero(t, successfulUsageRecords)

				line := failedUsage.only(t)
				requireFailedQueryUsageShape(t, line)
				require.Equal(t, util.HumanizeBytes(wantBytes), line["total_bytes"])
				require.Equal(t, strconv.Itoa(wantStatus), line["status"])
			})
		})
	}
}
