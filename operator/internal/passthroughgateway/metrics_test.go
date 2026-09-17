package passthroughgateway

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestMetricsRequestsTotal(t *testing.T) {
	registry := prometheus.NewPedanticRegistry()
	metrics := newMetrics(registry)

	metrics.RequestsTotal.WithLabelValues("GET", "read", "200").Inc()
	metrics.RequestsTotal.WithLabelValues("POST", "write", "201").Inc()

	expected := `
# HELP lokistack_gateway_requests_total Total number of requests processed by the LokiStack gateway.
# TYPE lokistack_gateway_requests_total counter
lokistack_gateway_requests_total{method="GET",route="read",status_code="200"} 1
lokistack_gateway_requests_total{method="POST",route="write",status_code="201"} 1
`

	err := testutil.CollectAndCompare(metrics.RequestsTotal, strings.NewReader(expected))
	require.NoError(t, err)
}

func TestMetricsRequestDuration(t *testing.T) {
	registry := prometheus.NewPedanticRegistry()
	metrics := newMetrics(registry)

	metrics.RequestDuration.WithLabelValues("GET", "/api/logs").Observe(0.5)

	count := testutil.CollectAndCount(metrics.RequestDuration)
	require.Equal(t, 1, count)
}

func TestMetricsRequestsInFlight(t *testing.T) {
	registry := prometheus.NewPedanticRegistry()
	metrics := newMetrics(registry)

	metrics.RequestsInFlight.WithLabelValues("read").Inc()
	metrics.RequestsInFlight.WithLabelValues("write").Inc()
	metrics.RequestsInFlight.WithLabelValues("read").Dec()

	expected := `
# HELP lokistack_gateway_requests_in_flight Current number of requests being processed by the LokiStack gateway.
# TYPE lokistack_gateway_requests_in_flight gauge
lokistack_gateway_requests_in_flight{route="read"} 0
lokistack_gateway_requests_in_flight{route="write"} 1
`

	err := testutil.CollectAndCompare(metrics.RequestsInFlight, strings.NewReader(expected))
	require.NoError(t, err)
}
