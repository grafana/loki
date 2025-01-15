package builder

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/util/constants"
)

const (
	metricsSubsystem = "bloombuilder"

	statusSuccess = "success"
	statusFailure = "failure"
)

type Metrics struct {
	running        prometheus.Gauge
	processingTask prometheus.Gauge

	taskStarted   prometheus.Counter
	taskCompleted *prometheus.CounterVec
	taskDuration  *prometheus.HistogramVec

	blocksReused  prometheus.Counter
	blocksCreated prometheus.Counter
	metasCreated  prometheus.Counter

	seriesPerTask prometheus.Histogram
	bytesPerTask  prometheus.Histogram

	chunkSize prometheus.Histogram
}

func NewMetrics(r prometheus.Registerer) *Metrics {
	return &Metrics{
		running: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: constants.Loki,
			Subsystem: metricsSubsystem,
			Name:      "running",
			Help:      "Value will be 1 if the bloom builder is currently running on this instance",
		}),
		processingTask: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Namespace: constants.Loki,
			Subsystem: metricsSubsystem,
			Name:      "processing_task",
			Help:      "Value will be 1 if the bloom builder is currently processing a task",
		}),

		taskStarted: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Subsystem: metricsSubsystem,
			Name:      "task_started_total",
			Help:      "Total number of task started",
		}),
		taskCompleted: promauto.With(r).NewCounterVec(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Subsystem: metricsSubsystem,
			Name:      "task_completed_total",
			Help:      "Total number of task completed",
		}, []string{"status"}),
		taskDuration: promauto.With(r).NewHistogramVec(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Subsystem: metricsSubsystem,
			Name:      "task_duration_seconds",
			Help:      "Time spent processing a task.",
			// Buckets in seconds:
			Buckets: append(
				// 1s --> 1h (steps of 10 minutes)
				prometheus.LinearBuckets(1, 600, 6),
				// 1h --> 24h (steps of 1 hour)
				prometheus.LinearBuckets(3600, 3600, 24)...,
			),
		}, []string{"status"}),

		blocksReused: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Subsystem: metricsSubsystem,
			Name:      "blocks_reused_total",
			Help:      "Number of overlapping bloom blocks reused when creating new blocks",
		}),
		blocksCreated: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Subsystem: metricsSubsystem,
			Name:      "blocks_created_total",
			Help:      "Number of blocks created",
		}),
		metasCreated: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Subsystem: metricsSubsystem,
			Name:      "metas_created_total",
			Help:      "Number of metas created",
		}),

		seriesPerTask: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Subsystem: metricsSubsystem,
			Name:      "series_per_task",
			Help:      "Number of series during task processing. Includes series which copied from other blocks and don't need to be indexed",
			// Up to 10M series per tenant, way more than what we expect given our max_global_streams_per_user limits
			Buckets: prometheus.ExponentialBucketsRange(1, 10e6, 10),
		}),
		bytesPerTask: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Subsystem: metricsSubsystem,
			Name:      "bytes_per_task",
			Help:      "Number of source bytes from chunks added during a task processing.",
			// 1KB -> 100GB, 10 buckets
			Buckets: prometheus.ExponentialBucketsRange(1<<10, 100<<30, 10),
		}),

		chunkSize: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Namespace: constants.Loki,
			Subsystem: metricsSubsystem,
			Name:      "chunk_series_size",
			Help:      "Uncompressed size of chunks in a series",
			// 256B -> 100GB, 10 buckets
			Buckets: prometheus.ExponentialBucketsRange(256, 100<<30, 10),
		}),
	}
}
