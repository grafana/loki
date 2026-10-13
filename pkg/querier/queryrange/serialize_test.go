package queryrange

import (
	"context"
	"fmt"
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

// requestWithHintRanges returns a range request that carries hint ranges, as a client could send it.
func requestWithHintRanges() *LokiRequest {
	start := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	return &LokiRequest{
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
	}
}

// successResponseFor returns a successful response of the type that the codec expects for the request.
func successResponseFor(req queryrangebase.Request) queryrangebase.Response {
	if _, ok := req.(*LokiInstantRequest); ok {
		return &LokiPromResponse{Response: &queryrangebase.PrometheusResponse{
			Status: loghttp.QueryStatusSuccess,
			Data:   queryrangebase.PrometheusData{ResultType: loghttp.ResultTypeVector},
		}}
	}
	return &LokiResponse{
		Status: loghttp.QueryStatusSuccess,
		Data:   LokiData{ResultType: loghttp.ResultTypeStream, Result: []logproto.Stream{}},
	}
}

func TestSerializeRoundTripperStripsClientHints(t *testing.T) {
	ctx := user.InjectOrgID(t.Context(), "1")

	roundTrip := func(t *testing.T, req queryrangebase.Request) queryrangebase.Request {
		t.Helper()

		httpRequest, err := DefaultCodec.EncodeRequest(ctx, req)
		require.NoError(t, err)

		var got queryrangebase.Request
		next := queryrangebase.HandlerFunc(func(_ context.Context, request queryrangebase.Request) (queryrangebase.Response, error) {
			got = request
			return successResponseFor(request), nil
		})

		response, err := NewSerializeRoundTripper(next, DefaultCodec, false).RoundTrip(httpRequest)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, response.Body.Close()) })
		return got
	}

	t.Run("clears the hint ranges a client sends", func(t *testing.T) {
		got := roundTrip(t, requestWithHintRanges())

		require.Empty(t, got.(*LokiRequest).HintRanges)
	})

	t.Run("clears the data-object options a client sends with a range request", func(t *testing.T) {
		got := roundTrip(t, dataObjRangeRequest(true))

		require.Nil(t, got.(*LokiRequest).DataObjOptions)
	})

	t.Run("clears the data-object options a client sends with an instant request", func(t *testing.T) {
		got := roundTrip(t, dataObjInstantRequest(true))

		require.Nil(t, got.(*LokiInstantRequest).DataObjOptions)
	})
}

func TestSerializeHTTPHandlerStripsClientHints(t *testing.T) {
	ctx := user.InjectOrgID(t.Context(), "1")

	serve := func(t *testing.T, req queryrangebase.Request) queryrangebase.Request {
		t.Helper()

		httpRequest, err := DefaultCodec.EncodeRequest(ctx, req)
		require.NoError(t, err)

		var got queryrangebase.Request
		next := queryrangebase.HandlerFunc(func(_ context.Context, request queryrangebase.Request) (queryrangebase.Response, error) {
			got = request
			return successResponseFor(request), nil
		})

		w := httptest.NewRecorder()
		NewSerializeHTTPHandler(next, DefaultCodec).ServeHTTP(w, httpRequest)
		require.Equal(t, http.StatusOK, w.Code)
		return got
	}

	t.Run("clears the hint ranges a client sends", func(t *testing.T) {
		got := serve(t, requestWithHintRanges())

		require.Empty(t, got.(*LokiRequest).HintRanges)
	})

	t.Run("clears the data-object options a client sends with a range request", func(t *testing.T) {
		got := serve(t, dataObjRangeRequest(true))

		require.Nil(t, got.(*LokiRequest).DataObjOptions)
	})

	t.Run("clears the data-object options a client sends with an instant request", func(t *testing.T) {
		got := serve(t, dataObjInstantRequest(true))

		require.Nil(t, got.(*LokiInstantRequest).DataObjOptions)
	})
}
