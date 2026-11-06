package transport

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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

	fields := formatRequestHeaders(&h, safeHeadersToLog([]string{"X-Header-To-Log", "X-Header-Not-Present"}))

	expected := []interface{}{
		"header_x_header_to_log",
		"i should be logged!",
	}

	require.Equal(t, expected, fields)
}

func TestFormatRequestHeadersDropsUnsafeChars(t *testing.T) {
	h := http.Header{}
	h["User-Agent"] = []string{"  Grafana/12.0.0\n level=error msg=injected\x1b[31m  "}

	fields := formatRequestHeaders(&h, safeHeadersToLog([]string{"User-Agent"}))

	require.Equal(t, []interface{}{"header_user_agent", "Grafana/12.0.0 level=error msg=injected[31m"}, fields)
}

func TestFormatRequestHeadersTruncatesLongValues(t *testing.T) {
	atLimit := strings.Repeat("a", maxLoggedHeaderValueLength)
	// The two-byte é starts at the last byte inside the limit, so truncation cuts it in half.
	overLimit := strings.Repeat("a", maxLoggedHeaderValueLength-1) + "é" + strings.Repeat("b", 100)

	h := http.Header{}
	h.Set("X-At-Limit", atLimit)
	h.Set("X-Over-Limit", overLimit)

	fields := formatRequestHeaders(&h, safeHeadersToLog([]string{"X-At-Limit", "X-Over-Limit"}))

	require.Equal(t, []interface{}{
		"header_x_at_limit", atLimit,
		"header_x_over_limit", strings.Repeat("a", maxLoggedHeaderValueLength-1) + "...",
	}, fields)
}

func TestSafeHeadersToLogDropsSensitiveHeaders(t *testing.T) {
	headers := safeHeadersToLog([]string{"User-Agent", "Authorization", "cookie", "X-API-Key", " Proxy-Authorization ", "x-plugin-id"})

	require.Equal(t, []safeHeader{
		{name: "User-Agent", logKey: "header_user_agent"},
		{name: "X-Plugin-Id", logKey: "header_x_plugin_id"},
	}, headers)
}

func TestHandlerConfigValidate(t *testing.T) {
	require.NoError(t, (&HandlerConfig{}).Validate())
	require.NoError(t, (&HandlerConfig{LogQueryRequestHeaders: []string{"User-Agent", "x-plugin-id"}}).Validate())

	err := (&HandlerConfig{LogQueryRequestHeaders: []string{"User-Agent", "Authorization", "Cookie"}}).Validate()
	require.EqualError(t, err, "log_query_request_headers: rejected headers: Authorization, Cookie")
}

func TestHandlerDoesNotLogSensitiveHeaders(t *testing.T) {
	var buf bytes.Buffer
	cfg := HandlerConfig{
		LogQueryRequestHeaders: []string{"Authorization", "User-Agent"},
		MaxBodySize:            1024,
		QueryStatsEnabled:      true,
	}
	rt := roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})
	handler := NewHandler(cfg, rt, log.NewLogfmtLogger(&buf), prometheus.NewRegistry(), "test")

	req := httptest.NewRequest(http.MethodGet, "/loki/api/v1/query_range", nil)
	req = req.WithContext(user.InjectOrgID(req.Context(), "test"))
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("User-Agent", "Grafana/12.0.0")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	require.Contains(t, buf.String(), "header_user_agent=Grafana/12.0.0")
	require.NotContains(t, buf.String(), "header_authorization")
	require.NotContains(t, buf.String(), "secret")
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
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
