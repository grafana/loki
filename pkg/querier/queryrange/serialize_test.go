package queryrange

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logqlmodel"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
)

// mockCodec replaces the encoding step of DefaultCodec with a fixed result.
type mockCodec struct {
	queryrangebase.Codec
	resp *http.Response
	err  error
}

func (c mockCodec) EncodeResponse(context.Context, *http.Request, queryrangebase.Response) (*http.Response, error) {
	return c.resp, c.err
}

func serializeTestRequest(t *testing.T) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/loki/api/v1/query_range?start=0&end=1&query=%7Bfoo%3D%22bar%22%7D", nil)
	return req.WithContext(user.InjectOrgID(t.Context(), "loki"))
}

// TestSerializeHTTPHandlerEncodeError asserts that a failed encoding yields an
// error response only, and not an error appended to a partially written body.
func TestSerializeHTTPHandlerEncodeError(t *testing.T) {
	handler := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		return &LokiResponse{}, nil
	})
	codec := mockCodec{Codec: DefaultCodec, err: errors.New("encoding failed")}

	w := httptest.NewRecorder()
	NewSerializeHTTPHandler(handler, codec).ServeHTTP(w, serializeTestRequest(t))

	res := w.Result()
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)

	require.Equal(t, http.StatusInternalServerError, res.StatusCode)
	require.Equal(t, "text/plain; charset=utf-8", res.Header.Get("Content-Type"))
	require.Equal(t, "encoding failed", string(body))
}

// TestSerializeHTTPHandlerWritesEncodedResponse asserts that the status code and
// all headers of the encoded response reach the client unmodified.
func TestSerializeHTTPHandlerWritesEncodedResponse(t *testing.T) {
	handler := queryrangebase.HandlerFunc(func(_ context.Context, _ queryrangebase.Request) (queryrangebase.Response, error) {
		return &LokiResponse{}, nil
	})
	codec := mockCodec{
		Codec: DefaultCodec,
		resp: &http.Response{
			StatusCode: http.StatusPartialContent, // explicitly a non-200 status
			Header: http.Header{
				"Content-Type": []string{"application/json; charset=UTF-8"},
				"Warning":      []string{"199 - first", "199 - second"},
			},
			Body: io.NopCloser(bytes.NewBufferString(`{"status":"success"}`)),
		},
	}

	w := httptest.NewRecorder()
	NewSerializeHTTPHandler(handler, codec).ServeHTTP(w, serializeTestRequest(t))

	res := w.Result()
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	require.NoError(t, err)

	require.Equal(t, http.StatusPartialContent, res.StatusCode)
	require.Equal(t, "application/json; charset=UTF-8", res.Header.Get("Content-Type"))
	require.Equal(t, []string{"199 - first", "199 - second"}, res.Header.Values("Warning"))
	require.JSONEq(t, `{"status":"success"}`, string(body))
}

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
