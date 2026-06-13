package compactor

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

func TestMetricsRegistersChunksExpiredByIngestionTime(t *testing.T) {
	registry := prometheus.NewRegistry()
	metrics := newMetrics(registry)
	metrics.chunksExpiredByIngestionTimeTotal.WithLabelValues("test")

	families, err := registry.Gather()
	require.NoError(t, err)
	require.Contains(t, metricFamilyNames(families), "loki_compactor_chunks_expired_by_ingestion_time_total")
}

func metricFamilyNames(families []*dto.MetricFamily) []string {
	names := make([]string, 0, len(families))
	for _, family := range families {
		names = append(names, family.GetName())
	}

	return names
}
