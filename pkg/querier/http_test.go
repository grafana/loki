package querier

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/mock"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logqlmodel"
	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/validation"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/tenant"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/user"
)

var (
	statsResultString = `"stats" : {
		"ingester" : {
			"store": {
				"chunk":{
					"compressedBytes": 1,
					"decompressedBytes": 2,
					"decompressedLines": 3,
					"headChunkBytes": 4,
					"headChunkLines": 5,
					"totalDuplicates": 8
				},
				"chunksDownloadTime": 0,
				"totalChunksRef": 0,
				"totalChunksDownloaded": 0
			},
			"totalBatches": 6,
			"totalChunksMatched": 7,
			"totalLinesSent": 9,
			"totalReached": 10
		},
		"querier": {
			"store" : {
				"chunk": {
					"compressedBytes": 11,
					"decompressedBytes": 12,
					"decompressedLines": 13,
					"headChunkBytes": 14,
					"headChunkLines": 15,
					"totalDuplicates": 19
				},
				"chunksDownloadTime": 16,
				"totalChunksRef": 17,
				"totalChunksDownloaded": 18
			}
		},
		"cache": {
			"chunk": {
				"entriesFound": 0,
				"entriesRequested": 0,
				"entriesStored": 0,
				"bytesReceived": 0,
				"bytesSent": 0,
				"requests": 0,
				"downloadTime": 0
			},
			"index": {
				"entriesFound": 0,
				"entriesRequested": 0,
				"entriesStored": 0,
				"bytesReceived": 0,
				"bytesSent": 0,
				"requests": 0,
				"downloadTime": 0
			},
		    "statsResult": {
				"entriesFound": 0,
				"entriesRequested": 0,
				"entriesStored": 0,
				"bytesReceived": 0,
				"bytesSent": 0,
				"requests": 0,
				"downloadTime": 0
			},
			"result": {
				"entriesFound": 0,
				"entriesRequested": 0,
				"entriesStored": 0,
				"bytesReceived": 0,
				"bytesSent": 0,
				"requests": 0,
				"downloadTime": 0
			}
		},
		"summary": {
			"bytesProcessedPerSecond": 20,
			"execTime": 22,
			"linesProcessedPerSecond": 23,
			"queueTime": 21,
			"shards": 0,
			"splits": 0,
			"subqueries": 0,
			"totalBytesProcessed": 24,
			"totalEntriesReturned": 10,
			"totalLinesProcessed": 25
		}
	}`
	statsResult = stats.Result{
		Summary: stats.Summary{
			BytesProcessedPerSecond: 20,
			QueueTime:               21,
			ExecTime:                22,
			LinesProcessedPerSecond: 23,
			TotalBytesProcessed:     24,
			TotalLinesProcessed:     25,
			TotalEntriesReturned:    10,
		},
		Querier: stats.Querier{
			Store: stats.Store{
				Chunk: stats.Chunk{
					CompressedBytes:   11,
					DecompressedBytes: 12,
					DecompressedLines: 13,
					HeadChunkBytes:    14,
					HeadChunkLines:    15,
					TotalDuplicates:   19,
				},
				ChunksDownloadTime:    16,
				TotalChunksRef:        17,
				TotalChunksDownloaded: 18,
			},
		},

		Ingester: stats.Ingester{
			Store: stats.Store{
				Chunk: stats.Chunk{
					CompressedBytes:   1,
					DecompressedBytes: 2,
					DecompressedLines: 3,
					HeadChunkBytes:    4,
					HeadChunkLines:    5,
					TotalDuplicates:   8,
				},
			},
			TotalBatches:       6,
			TotalChunksMatched: 7,
			TotalLinesSent:     9,
			TotalReached:       10,
		},

		Caches: stats.Caches{
			Chunk:  stats.Cache{},
			Index:  stats.Cache{},
			Result: stats.Cache{},
		},
	}
)

func TestTailHandler(t *testing.T) {
	tenant.WithDefaultResolver(tenant.NewMultiResolver())

	defaultLimits := defaultLimitsTestConfig()
	limits, err := validation.NewOverrides(defaultLimits, nil)
	require.NoError(t, err)

	api := NewQuerierAPI(mockQuerierConfig(), nil, limits, log.NewNopLogger())

	req, err := http.NewRequest("GET", "/", nil)
	ctx := user.InjectOrgID(req.Context(), "1|2")
	req = req.WithContext(ctx)
	require.NoError(t, err)

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(api.TailHandler)

	handler.ServeHTTP(rr, req)
	require.Equal(t, http.StatusBadRequest, rr.Code)
	require.Equal(t, "multiple org IDs present\n", rr.Body.String())
}

type slowConnectionSimulator struct {
	sleepFor   time.Duration
	deadline   time.Duration
	didTimeout bool
}

func (s *slowConnectionSimulator) ServeHTTP(_ http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := ctx.Err(); err != nil {
		panic(fmt.Sprintf("context already errored: %s", err))

	}
	time.Sleep(s.sleepFor)

	select {
	case <-ctx.Done():
		switch ctx.Err() {
		case context.DeadlineExceeded:
			s.didTimeout = true
		case context.Canceled:
			panic("context already canceled")
		}
	case <-time.After(s.deadline):
	}
}

func TestQueryWrapperMiddleware(t *testing.T) {
	tenant.WithDefaultResolver(tenant.NewMultiResolver())
	shortestTimeout := time.Millisecond * 5

	t.Run("request timeout is the shortest one", func(t *testing.T) {
		defaultLimits := defaultLimitsTestConfig()
		limits, err := validation.NewOverrides(defaultLimits, nil)
		require.NoError(t, err)
		api := NewQuerierAPI(mockQuerierConfig(), nil, limits, log.NewNopLogger())

		// request timeout is 5ms but it sleeps for 100ms, so timeout injected in the request is expected.
		connSimulator := &slowConnectionSimulator{
			sleepFor: time.Millisecond * 100,
			deadline: shortestTimeout,
		}

		api.cfg.QueryTimeout = time.Millisecond * 10
		midl := WrapQuerySpanAndTimeout("mycall", api).Wrap(connSimulator)

		req, err := http.NewRequest("GET", "/loki/api/v1/label", nil)
		ctx, cancelFunc := context.WithTimeout(user.InjectOrgID(req.Context(), "fake"), shortestTimeout)
		defer cancelFunc()
		req = req.WithContext(ctx)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		srv := http.HandlerFunc(midl.ServeHTTP)

		srv.ServeHTTP(rr, req)
		require.Equal(t, http.StatusOK, rr.Code)

		select {
		case <-ctx.Done():
			break
		case <-time.After(shortestTimeout):
			require.FailNow(t, "should have timed out before %s", shortestTimeout)
		default:
			require.FailNow(t, "timeout expected")
		}

		require.True(t, connSimulator.didTimeout)
	})

	t.Run("old querier:query_timeout is configured to supersede all others", func(t *testing.T) {
		defaultLimits := defaultLimitsTestConfig()
		defaultLimits.QueryTimeout = model.Duration(shortestTimeout)
		limits, err := validation.NewOverrides(defaultLimits, nil)
		require.NoError(t, err)
		api := NewQuerierAPI(mockQuerierConfig(), nil, limits, log.NewNopLogger())

		// configure old querier:query_timeout parameter.
		// although it is longer than the limits timeout, it should supersede it.
		api.cfg.QueryTimeout = time.Millisecond * 100

		// although limits:query_timeout is shorter than querier:query_timeout,
		// limits:query_timeout should be ignored.
		// here we configure it to sleep for 100ms and we want it to timeout at the 100ms.
		connSimulator := &slowConnectionSimulator{
			sleepFor: api.cfg.QueryTimeout,
			deadline: time.Millisecond * 200,
		}

		midl := WrapQuerySpanAndTimeout("mycall", api).Wrap(connSimulator)

		req, err := http.NewRequest("GET", "/loki/api/v1/label", nil)
		ctx, cancelFunc := context.WithTimeout(user.InjectOrgID(req.Context(), "fake"), time.Millisecond*200)
		defer cancelFunc()
		req = req.WithContext(ctx)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		srv := http.HandlerFunc(midl.ServeHTTP)

		srv.ServeHTTP(rr, req)
		require.Equal(t, http.StatusOK, rr.Code)

		select {
		case <-ctx.Done():
			require.FailNow(t, fmt.Sprintf("should timeout in %s", api.cfg.QueryTimeout))
		case <-time.After(shortestTimeout):
			// didn't use the limits timeout (i.e: shortest one), exactly what we want.
			break
		case <-time.After(api.cfg.QueryTimeout):
			require.FailNow(t, fmt.Sprintf("should timeout in %s", api.cfg.QueryTimeout))
		}

		require.True(t, connSimulator.didTimeout)
	})

	t.Run("new limits query timeout is configured to supersede all others", func(t *testing.T) {
		defaultLimits := defaultLimitsTestConfig()
		defaultLimits.QueryTimeout = model.Duration(shortestTimeout)

		limits, err := validation.NewOverrides(defaultLimits, nil)
		require.NoError(t, err)
		api := NewQuerierAPI(mockQuerierConfig(), nil, limits, log.NewNopLogger())

		connSimulator := &slowConnectionSimulator{
			sleepFor: time.Millisecond * 100,
			deadline: shortestTimeout,
		}

		midl := WrapQuerySpanAndTimeout("mycall", api).Wrap(connSimulator)

		req, err := http.NewRequest("GET", "/loki/api/v1/label", nil)
		ctx, cancelFunc := context.WithTimeout(user.InjectOrgID(req.Context(), "fake"), time.Millisecond*100)
		defer cancelFunc()
		req = req.WithContext(ctx)
		require.NoError(t, err)

		rr := httptest.NewRecorder()
		srv := http.HandlerFunc(midl.ServeHTTP)

		srv.ServeHTTP(rr, req)
		require.Equal(t, http.StatusOK, rr.Code)

		select {
		case <-ctx.Done():
			break
		case <-time.After(shortestTimeout):
			require.FailNow(t, "should have timed out before %s", shortestTimeout)
		}

		require.True(t, connSimulator.didTimeout)
	})
}

func TestSeriesHandler(t *testing.T) {
	t.Run("instant queries set a step of 0", func(t *testing.T) {
		ret := func() *logproto.SeriesResponse {
			return &logproto.SeriesResponse{
				Series: []logproto.SeriesIdentifier{
					{
						Labels: map[string]string{
							"a": "1",
							"b": "2",
						},
					},
					{
						Labels: map[string]string{
							"c": "3",
							"d": "4",
						},
					},
				},
			}
		}
		expected := `{"status":"success","data":[{"a":"1","b":"2"},{"c":"3","d":"4"}]}`

		querier := newQuerierMock()
		querier.On("Series", mock.Anything, mock.Anything).Return(ret, nil)
		api := setupAPI(querier)

		req := httptest.NewRequest(http.MethodGet, "/loki/api/v1/series"+
			"?start=0"+
			"&end=1"+
			"&step=42"+
			"&query=%7Bfoo%3D%22bar%22%7D", nil)
		res := makeRequest(t, api.SeriesHandler, req)

		require.Equalf(t, 200, res.Code, "response was not HTTP OK: %s", res.Body.String())
		require.JSONEq(t, expected, res.Body.String())
	})
}
func TestSeriesVolumeHandler(t *testing.T) {
	ret := &logproto.VolumeResponse{
		Volumes: []logproto.Volume{
			{Name: `{foo="bar"}`, Volume: 38},
		},
	}

	t.Run("shared beavhior between range and instant queries", func(t *testing.T) {
		for _, tc := range []struct {
			mode    string
			handler func(api *QuerierAPI) http.HandlerFunc
		}{
			{mode: "instant", handler: func(api *QuerierAPI) http.HandlerFunc { return api.SeriesVolumeInstantHandler }},
			{mode: "range", handler: func(api *QuerierAPI) http.HandlerFunc { return api.SeriesVolumeRangeHandler }},
		} {
			t.Run(fmt.Sprintf("%s queries return label volumes from the querier", tc.mode), func(t *testing.T) {
				querier := newQuerierMock()
				querier.On("SeriesVolume", mock.Anything, mock.Anything).Return(ret, nil)
				api := setupAPI(querier)

				req := httptest.NewRequest(http.MethodGet, "/series_volume"+
					"?start=0"+
					"&end=1"+
					"&query=%7Bfoo%3D%22bar%22%7D", nil)

				w := makeRequest(t, tc.handler(api), req)

				calls := querier.GetMockedCallsByMethod("SeriesVolume")
				require.Len(t, calls, 1)

				request := calls[0].Arguments[1].(*logproto.VolumeRequest)
				require.Equal(t, `{foo="bar"}`, request.Matchers)

				require.Equal(
					t,
					`{"volumes":[{"name":"{foo=\"bar\"}","volume":38}]}`,
					strings.TrimSpace(w.Body.String()),
				)
				require.Equal(t, http.StatusOK, w.Result().StatusCode)
			})

			t.Run(fmt.Sprintf("%s queries return nothing when a store doesn't support label volumes", tc.mode), func(t *testing.T) {
				querier := newQuerierMock()
				querier.On("SeriesVolume", mock.Anything, mock.Anything).Return(nil, nil)
				api := setupAPI(querier)

				req := httptest.NewRequest(http.MethodGet, "/series_volume?start=0&end=1&query=%7Bfoo%3D%22bar%22%7D", nil)
				w := makeRequest(t, tc.handler(api), req)

				calls := querier.GetMockedCallsByMethod("SeriesVolume")
				require.Len(t, calls, 1)

				require.Equal(t, strings.TrimSpace(w.Body.String()), `{"volumes":[]}`)
				require.Equal(t, http.StatusOK, w.Result().StatusCode)
			})

			t.Run(fmt.Sprintf("%s queries return error when there's an error in the querier", tc.mode), func(t *testing.T) {
				err := errors.New("something bad")
				querier := newQuerierMock()
				querier.On("SeriesVolume", mock.Anything, mock.Anything).Return(nil, err)

				api := setupAPI(querier)

				req := httptest.NewRequest(http.MethodGet, "/series_volume?start=0&end=1&query=%7Bfoo%3D%22bar%22%7D", nil)
				w := makeRequest(t, tc.handler(api), req)

				calls := querier.GetMockedCallsByMethod("SeriesVolume")
				require.Len(t, calls, 1)

				require.Equal(t, strings.TrimSpace(w.Body.String()), `something bad`)
				require.Equal(t, http.StatusInternalServerError, w.Result().StatusCode)
			})
		}
	})

	t.Run("instant queries set a step of 0", func(t *testing.T) {
		querier := newQuerierMock()
		querier.On("SeriesVolume", mock.Anything, mock.Anything).Return(ret, nil)
		api := setupAPI(querier)

		req := httptest.NewRequest(http.MethodGet, "/series_volume"+
			"?start=0"+
			"&end=1"+
			"&step=42"+
			"&query=%7Bfoo%3D%22bar%22%7D", nil)
		makeRequest(t, api.SeriesVolumeInstantHandler, req)

		calls := querier.GetMockedCallsByMethod("SeriesVolume")
		require.Len(t, calls, 1)

		request := calls[0].Arguments[1].(*logproto.VolumeRequest)
		require.Equal(t, int64(0), request.Step)
	})

	t.Run("range queries parse step from request", func(t *testing.T) {
		querier := newQuerierMock()
		querier.On("SeriesVolume", mock.Anything, mock.Anything).Return(ret, nil)
		api := setupAPI(querier)

		req := httptest.NewRequest(http.MethodGet, "/series_volume"+
			"?start=0"+
			"&end=1"+
			"&step=42"+
			"&query=%7Bfoo%3D%22bar%22%7D", nil)
		makeRequest(t, api.SeriesVolumeRangeHandler, req)

		calls := querier.GetMockedCallsByMethod("SeriesVolume")
		require.Len(t, calls, 1)

		request := calls[0].Arguments[1].(*logproto.VolumeRequest)
		require.Equal(t, (42 * time.Second).Milliseconds(), request.Step)
	})

	t.Run("range queries provide default step when not provided", func(t *testing.T) {
		querier := newQuerierMock()
		querier.On("SeriesVolume", mock.Anything, mock.Anything).Return(ret, nil)
		api := setupAPI(querier)

		req := httptest.NewRequest(http.MethodGet, "/series_volume"+
			"?start=0"+
			"&end=1"+
			"&query=%7Bfoo%3D%22bar%22%7D", nil)
		makeRequest(t, api.SeriesVolumeRangeHandler, req)

		calls := querier.GetMockedCallsByMethod("SeriesVolume")
		require.Len(t, calls, 1)

		request := calls[0].Arguments[1].(*logproto.VolumeRequest)
		require.Equal(t, time.Second.Milliseconds(), request.Step)
	})
}

func TestResponseFormat(t *testing.T) {
	for _, tc := range []struct {
		url             string
		accept          string
		handler         func(api *QuerierAPI) http.HandlerFunc
		result          logqlmodel.Result
		expectedRespone string
	}{
		{
			url: "/api/prom/query",
			handler: func(api *QuerierAPI) http.HandlerFunc {
				return api.LogQueryHandler
			},
			result: logqlmodel.Result{
				Data: logqlmodel.Streams{
					logproto.Stream{
						Entries: []logproto.Entry{
							{
								Timestamp: time.Unix(0, 123456789012345),
								Line:      "super line",
							},
						},
						Labels: `{foo="bar"}`,
					},
				},
				Statistics: statsResult,
			},
			expectedRespone: `{
				` + statsResultString + `,
				"streams": [
				  {
				    "labels": "{foo=\"bar\"}",
				    "entries": [
				      {
				        "line": "super line",
				        "ts": "1970-01-02T10:17:36.789012345Z"
				      }
				    ]
				  }
				]
			}`,
		},
		{
			url: "/loki/api/v1/query_range",
			handler: func(api *QuerierAPI) http.HandlerFunc {
				return api.RangeQueryHandler
			},
			result: logqlmodel.Result{
				Data: logqlmodel.Streams{
					logproto.Stream{
						Entries: []logproto.Entry{
							{
								Timestamp: time.Unix(0, 123456789012345),
								Line:      "super line",
							},
						},
						Labels: `{foo="bar"}`,
					},
				},
				Statistics: statsResult,
			},
			expectedRespone: `{
				"status": "success",
				"data": {
				  "resultType": "streams",
				` + statsResultString + `,
				  "result": [{
					"stream": {"foo": "bar"},
					"values": [
					  ["123456789012345", "super line"]
					]
				  }]
				}
			}`,
		},
	} {
		t.Run(fmt.Sprintf("%s returns the expected format", tc.url), func(t *testing.T) {
			engine := newEngineMock()
			engine.On("Query", mock.Anything, mock.Anything).Return(queryMock{tc.result})
			api := setupAPIWithEngine(engine)

			req := httptest.NewRequest(http.MethodGet, tc.url+
				"?start=0"+
				"&end=1"+
				"&query=%7Bfoo%3D%22bar%22%7D", nil)
			req = req.WithContext(user.InjectOrgID(context.Background(), "1"))

			w := makeRequest(t, tc.handler(api), req)

			require.Equalf(t, http.StatusOK, w.Code, "unexpected response: %s", w.Body.String())
			require.JSONEq(t, tc.expectedRespone, w.Body.String())
		})
	}
}

func makeRequest(t *testing.T, handler http.HandlerFunc, req *http.Request) *httptest.ResponseRecorder {
	err := req.ParseForm()
	require.NoError(t, err)

	w := httptest.NewRecorder()
	handler(w, req)
	return w
}

func setupAPI(querier *querierMock) *QuerierAPI {
	api := NewQuerierAPI(Config{}, querier, nil, log.NewNopLogger())
	return api
}

func setupAPIWithEngine(engine *engineMock) *QuerierAPI {
	limits, _ := validation.NewOverrides(validation.Limits{}, mockTenantLimits{})
	api := NewQuerierAPI(Config{}, nil, limits, log.NewNopLogger())
	api.engine = engine
	return api
}
