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

	chunks  *prometheus.GaugeVec
	samples *prometheus.CounterVec

	// push operation
	pushErrors    *prometheus.CounterVec
	pushRetries   *prometheus.CounterVec
	pushSuccesses *prometheus.CounterVec
	payloadSize   *prometheus.HistogramVec

	// Batch metrics
	streamsPerPush  *prometheus.HistogramVec
	entriesPerPush  *prometheus.HistogramVec
	servicesTracked *prometheus.GaugeVec

	writeTimeout *prometheus.CounterVec
}

func NewMetrics(r prometheus.Registerer) *Metrics {
	metricsOnce.Do(func() {
		aggMetrics = &Metrics{
			chunks: promauto.With(r).NewGaugeVec(prometheus.GaugeOpts{
				Namespace: constants.Loki,
				Subsystem: "pattern_ingester",
				Name:      "metric_chunks",
				Help:      "The total number of chunks in memory.",
			}, []string{"service_name"}),
			samples: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
				Namespace: constants.Loki,
				Subsystem: "pattern_ingester",
				Name:      "metric_samples",
				Help:      "The total number of samples in memory.",
			}, []string{"service_name"}),
			pushErrors: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
				Namespace: constants.Loki,
				Subsystem: "pattern_ingester",
				Name:      "push_errors_total",
				Help:      "Total number of errors when pushing metrics to Loki.",
			}, []string{"tenant_id", "error_type"}),

			pushRetries: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
				Namespace: constants.Loki,
				Subsystem: "pattern_ingester",
				Name:      "push_retries_total",
				Help:      "Total number of retries when pushing metrics to Loki.",
			}, []string{"tenant_id"}),

			pushSuccesses: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
				Namespace: constants.Loki,
				Subsystem: "pattern_ingester",
				Name:      "push_successes_total",
				Help:      "Total number of successful pushes to Loki.",
			}, []string{"tenant_id"}),

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
		}
	})

	return aggMetrics
}
