package compactor

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type metrics struct {
	compactTablesFileIngestLatencyMs prometheus.Histogram
	compactTablesFilesIngested       prometheus.Counter
}

func newMetrics(r prometheus.Registerer) *metrics {
	m := metrics{
		compactTablesFileIngestLatencyMs: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "compact_tables_file_ingest_latency_ms",
		}),
		compactTablesFilesIngested: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: "loki_boltdb_shipper",
			Name:      "compact_tables_files_ingested",
		}),
	}

	return &m
}
