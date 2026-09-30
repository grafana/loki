package queryrange

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/NYTimes/gziphandler"
	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
)

func TestResponseFormat(t *testing.T) {
	for _, tc := range []struct {
		url             string
		accept          string
		response        queryrangebase.Response
		expectedCode    int
		expectedRespone string
	}{
		{
			url: "/api/prom/query",
			response: &LokiResponse{
				Direction: logproto.BACKWARD,
				Limit:     200,
				Data: LokiData{
					ResultType: loghttp.ResultTypeStream,
					Result: logqlmodel.Streams{
						logproto.Stream{
							Entries: []logproto.Entry{
								{
									Timestamp: time.Unix(0, 123456789012345).UTC(),
									Line:      "super line",
								},
							},
							Labels: `{foo="bar"}`,
						},
					},
				},
				Status:     "success",
				Statistics: statsResult,
			},
			expectedCode: http.StatusOK,
			expectedRespone: `{
				` + statsResultString + `
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
			response: &LokiResponse{
				Direction: logproto.BACKWARD,
				Limit:     200,
				Data: LokiData{
					ResultType: loghttp.ResultTypeStream,
					Result: logqlmodel.Streams{
						logproto.Stream{
							Entries: []logproto.Entry{
								{
									Timestamp: time.Unix(0, 123456789012345).UTC(),
									Line:      "super line",
								},
							},
							Labels: `{foo="bar"}`,
						},
					},
				},
				Status:     "success",
				Statistics: statsResult,
			},
			expectedCode: http.StatusOK,
			expectedRespone: `{
				"status": "success",
				"data": {
				  "resultType": "streams",
				` + statsResultString + `
				  "result": [{
					"stream": {"foo": "bar"},
					"values": [
					  ["123456789012345", "super line"]
					]
				  }]
				}
			}`,
		},
		{
			url:             "/loki/wrong/path",
			response:        nil,
			expectedCode:    http.StatusNotFound,
			expectedRespone: "unknown request path: /loki/wrong/path",
		},
	} {
		t.Run(fmt.Sprintf("%s returns the expected format", tc.url), func(t *testing.T) {
			handler := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
				return tc.response, nil
			})
			httpHandler := NewSerializeHTTPHandler(handler, DefaultCodec)

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tc.url+
				"?start=0"+
				"&end=1"+
				"&query=%7Bfoo%3D%22bar%22%7D", nil)
			req = req.WithContext(user.InjectOrgID(context.Background(), "1"))
			httpHandler.ServeHTTP(w, req)

			require.Equalf(t, tc.expectedCode, w.Code, "unexpected response: %s", w.Body.String())
			if tc.expectedCode/100 == 2 {
				require.JSONEq(t, tc.expectedRespone, w.Body.String())
			} else {
				require.Equal(t, tc.expectedRespone, w.Body.String())
			}
		})
	}
}

func TestSerializeRoundTripperStripsClientHintRanges(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), "1")
	start := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	httpRequest, err := DefaultCodec.EncodeRequest(ctx, &LokiRequest{
		Query:     `{foo="bar"}`,
		Limit:     100,
		Path:      "/loki/api/v1/query_range",
		StartTs:   start,
		EndTs:     start.Add(time.Hour),
		Direction: logproto.BACKWARD,
		HintRanges: []logproto.HintTimeRange{{
			Start: start.Add(5 * time.Minute),
			End:   start.Add(10 * time.Minute),
		}},
	})
	require.NoError(t, err)

	var got *LokiRequest
	next := queryrangebase.HandlerFunc(func(_ context.Context, request queryrangebase.Request) (queryrangebase.Response, error) {
		got = request.(*LokiRequest)
		return &LokiResponse{
			Status: loghttp.QueryStatusSuccess,
			Data: LokiData{
				ResultType: loghttp.ResultTypeStream,
				Result:     []logproto.Stream{},
			},
		}, nil
	})

	response, err := NewSerializeRoundTripper(next, DefaultCodec, false).RoundTrip(httpRequest)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, response.Body.Close()) })
	require.Empty(t, got.HintRanges)
}

func TestSerializeHTTPHandlerStripsClientHintRanges(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), "1")
	start := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	httpRequest, err := DefaultCodec.EncodeRequest(ctx, &LokiRequest{
		Query:     `{foo="bar"}`,
		Limit:     100,
		Path:      "/loki/api/v1/query_range",
		StartTs:   start,
		EndTs:     start.Add(time.Hour),
		Direction: logproto.BACKWARD,
		HintRanges: []logproto.HintTimeRange{{
			Start: start.Add(5 * time.Minute),
			End:   start.Add(10 * time.Minute),
		}},
	})
	require.NoError(t, err)

	var got *LokiRequest
	next := queryrangebase.HandlerFunc(func(_ context.Context, request queryrangebase.Request) (queryrangebase.Response, error) {
		got = request.(*LokiRequest)
		return &LokiResponse{
			Status: loghttp.QueryStatusSuccess,
			Data: LokiData{
				ResultType: loghttp.ResultTypeStream,
				Result:     []logproto.Stream{},
			},
		}, nil
	})

	w := httptest.NewRecorder()
	NewSerializeHTTPHandler(next, DefaultCodec).ServeHTTP(w, httpRequest)
	require.Equal(t, http.StatusOK, w.Code)
	require.Empty(t, got.HintRanges)
}

func TestQueryUsageHeaderHTTP(t *testing.T) {
	for _, tc := range []struct{ name, path, query, kind string }{
		{"range logs", "/loki/api/v1/query_range", `{app="test"}`, "logs"},
		{"instant metrics", "/loki/api/v1/query", "vector(1)", "vector"},
		{"range metrics", "/loki/api/v1/query_range", "vector(1)", "matrix"},
		{"fields", "/loki/api/v1/detected_fields", `{app="test"}`, "fields"},
		{"field values", "/loki/api/v1/detected_field/foo/values", `{app="test"}`, "fields"},
	} {
		for _, zipped := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/gzip=%t", tc.name, zipped), func(t *testing.T) {
				const n int64 = 123456789
				response := usageHeaderResponse(tc.kind, n)
				if tc.kind == "fields" {
					response = &DetectedFieldsResponse{Response: &logproto.DetectedFieldsResponse{}, Headers: withQueryBytesProcessed(nil, strconv.FormatInt(n, 10))}
				}
				next := queryrangebase.HandlerFunc(func(context.Context, queryrangebase.Request) (queryrangebase.Response, error) { return response, nil })
				var handler http.Handler = NewSerializeHTTPHandler(next, DefaultCodec)
				if zipped {
					wrap, err := gziphandler.NewGzipLevelAndMinSize(gzip.DefaultCompression, 1)
					require.NoError(t, err)
					handler = wrap(handler)
				}
				req := httptest.NewRequest(http.MethodGet, tc.path+"?query="+url.QueryEscape(tc.query)+"&start=1&end=2&time=2&step=1", nil)
				req = req.WithContext(user.InjectOrgID(req.Context(), "test"))
				req.Header.Set(queryBytesProcessedHeader, "999")
				if zipped {
					req.Header.Set("Accept-Encoding", "gzip")
				}
				recorder := httptest.NewRecorder()
				handler.ServeHTTP(recorder, req)
				actual := recorder.Result()
				defer actual.Body.Close()
				require.Equal(t, http.StatusOK, actual.StatusCode, recorder.Body.String())
				require.Equal(t, strconv.FormatInt(n, 10), actual.Header.Get(queryBytesProcessedHeader), "must be present before headers are committed")
				var reader io.Reader = actual.Body
				if zipped {
					require.Equal(t, "gzip", actual.Header.Get("Content-Encoding"))
					gz, err := gzip.NewReader(reader)
					require.NoError(t, err)
					defer gz.Close()
					reader = gz
				}
				actualBody, err := io.ReadAll(reader)
				require.NoError(t, err)
				expected, err := encodeResponseJSON(req.Context(), loghttp.VersionV1, response, httpreq.EncodingFlags{})
				require.NoError(t, err)
				defer expected.Body.Close()
				expectedBody, err := io.ReadAll(expected.Body)
				require.NoError(t, err)
				require.JSONEq(t, string(expectedBody), string(actualBody))
			})
		}
	}
}

func TestQueryFailureDoesNotInventUsageHeader(t *testing.T) {
	next := queryrangebase.HandlerFunc(func(context.Context, queryrangebase.Request) (queryrangebase.Response, error) {
		return nil, errors.New("query failed")
	})
	handler := NewSerializeHTTPHandler(next, DefaultCodec)
	req := httptest.NewRequest(http.MethodGet, "/loki/api/v1/query?query=vector(1)&time=2", nil)
	req = req.WithContext(user.InjectOrgID(req.Context(), "test"))
	req.Header.Set("X-Loki-Query-Bytes-Processed", "999")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	require.GreaterOrEqual(t, recorder.Code, 500)
	require.Empty(t, recorder.Result().Header.Get("X-Loki-Query-Bytes-Processed"))
}
