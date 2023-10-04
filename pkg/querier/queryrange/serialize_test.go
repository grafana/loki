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

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logqlmodel"
	"github.com/grafana/loki/pkg/querier/queryrange/queryrangebase"
)

func TestResponseFormat(t *testing.T) {
	for _, tc := range []struct {
		url             string
		accept          string
		result          logqlmodel.Result
		expectedRespone string
	}{
		{
			url: "/api/prom/query",
			result: logqlmodel.Result{
				Data: logqlmodel.Streams{
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
				Statistics: statsResult,
			},
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
			result: logqlmodel.Result{
				Data: logqlmodel.Streams{
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
				Statistics: statsResult,
			},
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
	} {
		t.Run(fmt.Sprintf("%s returns the expected format", tc.url), func(t *testing.T) {
			handler := queryrangebase.HandlerFunc(func(ctx context.Context, r queryrangebase.Request) (queryrangebase.Response, error) {
				// TODO: return tc.response instead.
				params, err := ParamsFromRequest(r)
				if err != nil {
					return nil, err
				}
				return ResultToResponse(tc.result, params)
			})
			httpHandler := NewSerializeHTTPHandler(handler, DefaultCodec)

			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tc.url+
				"?start=0"+
				"&end=1"+
				"&query=%7Bfoo%3D%22bar%22%7D", nil)
			req = req.WithContext(user.InjectOrgID(context.Background(), "1"))
			httpHandler.ServeHTTP(w, req)

			require.Equalf(t, http.StatusOK, w.Code, "unexpected response: %s", w.Body.String())
			require.JSONEq(t, tc.expectedRespone, w.Body.String())
		})
	}
}
