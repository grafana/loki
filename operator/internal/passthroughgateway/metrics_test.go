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
	metrics := NewMetrics(registry)

	metrics.RequestsTotal.WithLabelValues("GET", "200").Inc()
	metrics.RequestsTotal.WithLabelValues("POST", "201").Inc()

	expected := `
# HELP lokistack_gateway_requests_total Total number of requests processed by the LokiStack gateway.
# TYPE lokistack_gateway_requests_total counter
lokistack_gateway_requests_total{code="200",method="GET"} 1
lokistack_gateway_requests_total{code="201",method="POST"} 1
`

	err := testutil.CollectAndCompare(metrics.RequestsTotal, strings.NewReader(expected))
	require.NoError(t, err)
}

func TestMetricsRequestDuration(t *testing.T) {
	registry := prometheus.NewPedanticRegistry()
	metrics := NewMetrics(registry)

	metrics.RequestDuration.WithLabelValues("GET", "/api/logs").Observe(0.5)

	count := testutil.CollectAndCount(metrics.RequestDuration)
	require.Equal(t, 1, count)
}

func TestMetricsRequestsInFlight(t *testing.T) {
	registry := prometheus.NewPedanticRegistry()
	metrics := NewMetrics(registry)

	metrics.RequestsInFlight.Inc()
	metrics.RequestsInFlight.Inc()
	metrics.RequestsInFlight.Dec()

	expected := `
# HELP lokistack_gateway_requests_in_flight Current number of requests being processed by the LokiStack gateway.
# TYPE lokistack_gateway_requests_in_flight gauge
lokistack_gateway_requests_in_flight 1
`

	err := testutil.CollectAndCompare(metrics.RequestsInFlight, strings.NewReader(expected))
	require.NoError(t, err)
}
