package transport

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/NYTimes/gziphandler"
	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logql/model/stats"
	qr "github.com/grafana/loki/v3/pkg/querier/queryrange"
	base "github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
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
	const n int64 = 123456789
	response := &qr.LokiResponse{
		Status:     "success",
		Statistics: stats.Result{Summary: stats.Summary{TotalBytesProcessed: n}},
		Data:       qr.LokiData{ResultType: "streams"},
	}
	next := base.HandlerFunc(func(context.Context, base.Request) (base.Response, error) {
		return response, nil
	})
	handler := NewHandler(HandlerConfig{MaxBodySize: 1024}, qr.NewSerializeRoundTripper(next, qr.DefaultCodec, true), log.NewNopLogger(), prometheus.NewRegistry(), "test")
	wrap, err := gziphandler.NewGzipLevelAndMinSize(gzip.DefaultCompression, 1)
	require.NoError(t, err)
	handler = wrap(handler)

	// Exercise codec encoding, frontend header forwarding and HTTP compression together.
	req := httptest.NewRequest(http.MethodGet, "/loki/api/v1/query_range?query="+url.QueryEscape(`{app="test"}`)+"&start=1&end=2", nil)
	req = req.WithContext(user.InjectOrgID(req.Context(), "test"))
	req.Header.Set("Accept-Encoding", "gzip")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	actual := recorder.Result()
	defer actual.Body.Close()
	require.Equal(t, http.StatusOK, actual.StatusCode, recorder.Body.String())
	require.Equal(t, "123456789", actual.Header.Get(httpreq.LokiQueryBytesProcessedHeader))
	require.Equal(t, "gzip", actual.Header.Get("Content-Encoding"))

	gz, err := gzip.NewReader(actual.Body)
	require.NoError(t, err)
	defer gz.Close()
	var body struct {
		Data struct {
			Stats stats.Result `json:"stats"`
		} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(gz).Decode(&body))
	require.Equal(t, n, body.Data.Stats.Summary.TotalBytesProcessed)
}
