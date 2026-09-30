package transport

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/NYTimes/gziphandler"
	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	qr "github.com/grafana/loki/v3/pkg/querier/queryrange"
	base "github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
)

func TestFormatRequestHeaders(t *testing.T) {
	h := http.Header{}
	h.Add("X-Header-To-Log", "i should be logged!")
	h.Add("X-Header-To-Not-Log", "i shouldn't be logged!")

	fields := formatRequestHeaders(&h, []string{"X-Header-To-Log", "X-Header-Not-Present"})

	expected := []interface{}{
		"header_x_header_to_log",
		"i should be logged!",
	}

	require.Equal(t, expected, fields)
}

func TestFrontendForwardsQueryUsageHeader(t *testing.T) {
	const header = "X-Loki-Query-Bytes-Processed"
	for _, instant := range []bool{false, true} {
		for _, zipped := range []bool{false, true} {
			t.Run(fmt.Sprintf("instant=%t/gzip=%t", instant, zipped), func(t *testing.T) {
				const n int64 = 123456789
				statistics := stats.Result{Summary: stats.Summary{TotalBytesProcessed: n}}
				var response base.Response = &qr.LokiResponse{Status: "success", Statistics: statistics, Data: qr.LokiData{ResultType: "streams"}}
				path, query := "/loki/api/v1/query_range", `{app="test"}`
				if instant {
					path, query = "/loki/api/v1/query", "vector(1)"
					response = &qr.LokiPromResponse{Statistics: statistics, Response: &base.PrometheusResponse{Status: "success", Data: base.PrometheusData{ResultType: "vector"}}}
				}
				next := base.HandlerFunc(func(context.Context, base.Request) (base.Response, error) { return response, nil })
				handler := NewHandler(HandlerConfig{MaxBodySize: 1024}, qr.NewSerializeRoundTripper(next, qr.DefaultCodec, true), log.NewNopLogger(), prometheus.NewRegistry(), "test")
				if zipped {
					wrap, err := gziphandler.NewGzipLevelAndMinSize(gzip.DefaultCompression, 1)
					require.NoError(t, err)
					handler = wrap(handler)
				}
				req := httptest.NewRequest(http.MethodGet, path+"?query="+url.QueryEscape(query)+"&start=1&end=2&step=1&time=2", nil)
				req = req.WithContext(user.InjectOrgID(req.Context(), "test"))
				req.Header.Set(header, "999")
				if zipped {
					req.Header.Set("Accept-Encoding", "gzip")
				}
				recorder := httptest.NewRecorder()
				handler.ServeHTTP(recorder, req)
				actual := recorder.Result()
				defer actual.Body.Close()
				require.Equal(t, http.StatusOK, actual.StatusCode, recorder.Body.String())
				require.Equal(t, "123456789", actual.Header.Get(header))
				var reader io.Reader = actual.Body
				if zipped {
					require.Equal(t, "gzip", actual.Header.Get("Content-Encoding"))
					gz, err := gzip.NewReader(reader)
					require.NoError(t, err)
					defer gz.Close()
					reader = gz
				}
				var body struct {
					Data struct {
						Stats struct {
							Summary struct {
								TotalBytesProcessed int64 `json:"totalBytesProcessed"`
							} `json:"summary"`
						} `json:"stats"`
					} `json:"data"`
				}
				require.NoError(t, json.NewDecoder(reader).Decode(&body))
				require.Equal(t, n, body.Data.Stats.Summary.TotalBytesProcessed)
			})
		}
	}
}

func TestFrontendQueryFailureDoesNotInventUsageHeader(t *testing.T) {
	next := base.HandlerFunc(func(context.Context, base.Request) (base.Response, error) { return nil, errors.New("query failed") })
	handler := NewHandler(HandlerConfig{MaxBodySize: 1024}, qr.NewSerializeRoundTripper(next, qr.DefaultCodec, true), log.NewNopLogger(), prometheus.NewRegistry(), "test")
	req := httptest.NewRequest(http.MethodGet, "/loki/api/v1/query?query=vector(1)&time=2", nil)
	req = req.WithContext(user.InjectOrgID(req.Context(), "test"))
	req.Header.Set("X-Loki-Query-Bytes-Processed", "999")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	require.GreaterOrEqual(t, recorder.Code, 500)
	require.Empty(t, recorder.Result().Header.Get("X-Loki-Query-Bytes-Processed"))
}
