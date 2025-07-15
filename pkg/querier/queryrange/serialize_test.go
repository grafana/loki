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
