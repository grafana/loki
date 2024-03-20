//go:build integration

package integration

import (
	"testing"

	"github.com/stretchr/testify/require"
)

var exampleMetricOutput = `
# HELP loki_compactor_delete_requests_processed_total Number of delete requests processed per user
# TYPE loki_compactor_delete_requests_processed_total counter
loki_compactor_delete_requests_processed_total{user="eEWxEcgwRQcf"} 1
# HELP loki_compactor_delete_requests_received_total Number of delete requests received per user
# TYPE loki_compactor_delete_requests_received_total counter
loki_compactor_delete_requests_received_total{user="eEWxEcgwRQcf"} 1
# HELP loki_compactor_load_pending_requests_attempts_total Number of attempts that were made to load pending requests with status
# TYPE loki_compactor_load_pending_requests_attempts_total counter
loki_compactor_load_pending_requests_attempts_total{status="success"} 57
# HELP loki_compactor_oldest_pending_delete_request_age_seconds Age of oldest pending delete request in seconds, since they are over their cancellation period
# TYPE loki_compactor_oldest_pending_delete_request_age_seconds gauge
loki_compactor_oldest_pending_delete_request_age_seconds 0
# HELP loki_compactor_pending_delete_requests_count Count of delete requests which are over their cancellation period and have not finished processing yet
# TYPE loki_compactor_pending_delete_requests_count gauge
loki_compactor_pending_delete_requests_count 0
`

func TestExtractCounterMetric(t *testing.T) {
	val, labels, err := extractMetric("loki_compactor_oldest_pending_delete_request_age_seconds", exampleMetricOutput)
	require.NoError(t, err)
	require.NotNil(t, labels)
	require.Len(t, labels, 0)
	require.Equal(t, float64(0), val)

	val, labels, err = extractMetric("loki_compactor_delete_requests_processed_total", exampleMetricOutput)
	require.NoError(t, err)
	require.NotNil(t, labels)
	require.Len(t, labels, 1)
	require.Contains(t, labels, "user")
	require.Equal(t, labels["user"], "eEWxEcgwRQcf")
	require.Equal(t, float64(1), val)
}
