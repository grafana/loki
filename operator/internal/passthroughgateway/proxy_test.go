package passthroughgateway

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func BenchmarkIsWritePath(b *testing.B) {
	benchmarks := []struct {
		name string
		path string
	}{
		{"write_loki_push", "/loki/api/v1/push"},
		{"write_prom_push", "/api/prom/push"},
		{"write_otlp", "/otlp/v1/logs"},
		{"write_with_suffix", "/loki/api/v1/push?foo=bar"},
		{"read_query", "/loki/api/v1/query"},
		{"read_labels", "/loki/api/v1/labels"},
		{"read_series", "/loki/api/v1/series"},
		{"read_tail", "/loki/api/v1/tail"},
		{"read_long_path", "/loki/api/v1/query_range?query=rate({app='test'}[5m])&start=1234567890&end=1234567899"},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = isWritePath(bm.path)
			}
		})
	}
}

func TestInstrumentedHandler(t *testing.T) {
	tt := []struct {
		name          string
		method        string
		path          string
		handlerStatus int
	}{
		{
			name:          "read request with GET",
			method:        http.MethodGet,
			path:          "/loki/api/v1/query",
			handlerStatus: http.StatusOK,
		},
		{
			name:          "write request with POST",
			method:        http.MethodPost,
			path:          "/loki/api/v1/push",
			handlerStatus: http.StatusNoContent,
		},
		{
			name:          "write request otlp",
			method:        http.MethodPost,
			path:          "/otlp/v1/logs",
			handlerStatus: http.StatusOK,
		},
		{
			name:          "read request with error status",
			method:        http.MethodGet,
			path:          "/loki/api/v1/labels",
			handlerStatus: http.StatusInternalServerError,
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			registry := prometheus.NewPedanticRegistry()
			metrics := NewMetrics(registry)

			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.handlerStatus)
			})

			instrumented := InstrumentedHandler(handler, metrics)

			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()

			instrumented.ServeHTTP(rec, req)

			require.Equal(t, tc.handlerStatus, rec.Code)

			expectedTotal := fmt.Sprintf(`
# HELP lokistack_gateway_requests_total Total number of requests processed by the LokiStack gateway.
# TYPE lokistack_gateway_requests_total counter
lokistack_gateway_requests_total{code="%d",method="%s"} 1
`, tc.handlerStatus, tc.method)
			err := testutil.CollectAndCompare(metrics.RequestsTotal, strings.NewReader(expectedTotal))
			require.NoError(t, err)

			durationCount := testutil.CollectAndCount(metrics.RequestDuration)
			require.Equal(t, 1, durationCount)

			err = testutil.CollectAndCompare(metrics.RequestsInFlight, strings.NewReader(`
# HELP lokistack_gateway_requests_in_flight Current number of requests being processed by the LokiStack gateway.
# TYPE lokistack_gateway_requests_in_flight gauge
lokistack_gateway_requests_in_flight 0
`))
			require.NoError(t, err)
		})
	}
}
