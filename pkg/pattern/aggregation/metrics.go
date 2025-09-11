package aggregation

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/util/constants"
)

var (
	aggMetrics  *Metrics
	metricsOnce sync.Once
)

type Metrics struct {
	reg prometheus.Registerer

	// push operation
	pushErrors  *prometheus.CounterVec
	payloadSize *prometheus.HistogramVec

	// Batch metrics
	streamsPerPush  *prometheus.HistogramVec
	entriesPerPush  *prometheus.HistogramVec
	servicesTracked *prometheus.GaugeVec

	writeTimeout *prometheus.CounterVec

	// Pattern writing metrics
	PatternBytesWrittenTotal *prometheus.CounterVec
	PatternPayloadBytes      *prometheus.HistogramVec
	PatternWritesTotal       *prometheus.CounterVec
	PatternsActive           *prometheus.GaugeVec

	// Aggregated metrics writing metrics
	AggregatedMetricBytesWrittenTotal *prometheus.CounterVec
	AggregatedMetricsPayloadBytes     *prometheus.HistogramVec
}

func NewMetrics(r prometheus.Registerer) *Metrics {
	metricsOnce.Do(func() {
		aggMetrics = &Metrics{
			pushErrors: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
				Namespace: constants.Loki,
				Subsystem: "pattern_ingester",
				Name:      "push_errors_total",
				Help:      "Total number of errors when pushing metrics to Loki.",
			}, []string{"tenant_id", "error_type"}),

			// Batch metrics
			payloadSize: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
				Namespace: constants.Loki,
				Subsystem: "pattern_ingester",
				Name:      "push_payload_bytes",
				Help:      "Size of push payloads in bytes.",
				Buckets:   []float64{1024, 4096, 16384, 65536, 262144, 1048576},
			}, []string{"tenant_id"}),
			streamsPerPush: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
				Namespace: constants.Loki,
				Subsystem: "pattern_ingester",
				Name:      "streams_per_push",
				Help:      "Number of streams in each push request.",
				Buckets:   []float64{1, 5, 10, 25, 50, 100, 250, 500, 1000},
			}, []string{"tenant_id"}),
			entriesPerPush: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
				Namespace: constants.Loki,
				Subsystem: "pattern_ingester",
				Name:      "entries_per_push",
				Help:      "Number of entries in each push request.",
				Buckets:   []float64{10, 50, 100, 500, 1000, 5000, 10000},
			}, []string{"tenant_id"}),
			servicesTracked: promauto.With(r).NewGaugeVec(prometheus.GaugeOpts{
				Namespace: constants.Loki,
				Subsystem: "pattern_ingester",
				Name:      "services_tracked",
				Help:      "Number of unique services being tracked.",
			}, []string{"tenant_id"}),
			writeTimeout: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
				Namespace: constants.Loki,
				Subsystem: "pattern_ingester",
				Name:      "write_timeouts_total",
				Help:      "Total number of write timeouts.",
			}, []string{"tenant_id"}),

			// Pattern writing metrics
			PatternBytesWrittenTotal: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
				Namespace: constants.Loki,
				Subsystem: "pattern_ingester",
				Name:      "pattern_bytes_written_total",
				Help:      "Total bytes written for pattern payloads to Loki.",
			}, []string{"tenant_id", "service_name"}),

			PatternPayloadBytes: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
				Namespace: constants.Loki,
				Subsystem: "pattern_ingester",
				Name:      "pattern_payload_bytes",
				Help:      "Size distribution of pattern payloads written to Loki.",
				Buckets:   []float64{512, 2048, 8192, 32768, 131072, 524288},
			}, []string{"tenant_id", "service_name"}),

			PatternWritesTotal: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
				Namespace: constants.Loki,
				Subsystem: "pattern_ingester",
				Name:      "pattern_writes_total",
				Help:      "Total number of pattern write operations to Loki.",
			}, []string{"tenant_id", "service_name"}),

			PatternsActive: promauto.With(r).NewGaugeVec(prometheus.GaugeOpts{
				Namespace: constants.Loki,
				Subsystem: "pattern_ingester",
				Name:      "patterns_active",
				Help:      "Number of active patterns currently tracked in memory.",
			}, []string{"tenant_id", "service_name"}),

			// Aggregated metrics writing metrics
			AggregatedMetricBytesWrittenTotal: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
				Namespace: constants.Loki,
				Subsystem: "pattern_ingester",
				Name:      "aggregated_metric_bytes_written_total",
				Help:      "Total bytes written for aggregated metric payloads to Loki.",
			}, []string{"tenant_id", "service_name"}),

			AggregatedMetricsPayloadBytes: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
				Namespace: constants.Loki,
				Subsystem: "pattern_ingester",
				Name:      "aggregated_metrics_payload_bytes",
				Help:      "Size distribution of aggregated metric payloads written to Loki.",
				Buckets:   []float64{512, 2048, 8192, 32768, 131072, 524288},
			}, []string{"tenant_id", "service_name"}),
		}
	})

	return aggMetrics
}
