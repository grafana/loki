package builder

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type metrics struct {
	lastOffset     prometheus.Gauge
	consumptionLag prometheus.Gauge
	receivedBytes  prometheus.Counter
	discardedBytes prometheus.Counter
	records        prometheus.Counter
	recordFailures prometheus.Counter
	sizeEstimate   prometheus.Gauge
}

func newMetrics(r prometheus.Registerer) *metrics {
	return &metrics{
		lastOffset: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "loki_dataobj_builder_last_offset",
			Help: "The last consumed offset.",
		}),
		consumptionLag: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "loki_dataobj_builder_consumption_lag_seconds",
			Help: "The time difference between the last consumed offset and the current time in seconds.",
		}),
		receivedBytes: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_builder_received_bytes_total",
			Help: "The sum of bytes in all Kafka records.",
		}),
		discardedBytes: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_builder_discarded_bytes_total",
			Help: "The sum of discarded bytes from corrupted or unprocessable Kafka records.",
		}),
		records: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_builder_records_total",
			Help: "Total number of records received.",
		}),
		recordFailures: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_builder_record_failures_total",
			Help: "Total number of records that failed to be processed.",
		}),
		sizeEstimate: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "loki_dataobj_builder_size_estimate_bytes",
			Help: "Current estimated size in bytes buffered across all window-specific builders for the partition.",
		}),
	}
}

func (m *metrics) setLastOffset(offset int64) {
	m.lastOffset.Set(float64(offset))
}

func (m *metrics) setConsumptionLag(d time.Duration) {
	secs := float64(d.Seconds())
	m.consumptionLag.Set(secs)
}

// requiredReplicasMetrics are the metrics of a requiredReplicasReporter.
type requiredReplicasMetrics struct {
	// requiredReplicas has no labels. It is a vector because a vector exports
	// no series until the first Set, so a restart never exports a false 0.
	requiredReplicas *prometheus.GaugeVec
	refreshFailures  prometheus.Counter
	lastRefresh      prometheus.Gauge
	refreshDuration  prometheus.Histogram
}

func newRequiredReplicasMetrics(reg prometheus.Registerer) *requiredReplicasMetrics {
	return &requiredReplicasMetrics{
		requiredReplicas: promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{
			Name: "loki_dataobj_builder_required_replicas",
			Help: "The number of builder replicas needed to consume every partition with unread records: the highest such partition plus 1, or 0 if there is none. The value stays the same while refreshes fail.",
		}, nil),
		refreshFailures: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_builder_required_replicas_refresh_failures_total",
			Help: "Total number of failed refreshes of loki_dataobj_builder_required_replicas.",
		}),
		lastRefresh: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Name: "loki_dataobj_builder_required_replicas_last_refresh_timestamp_seconds",
			Help: "The Unix time of the last successful refresh of loki_dataobj_builder_required_replicas.",
		}),
		refreshDuration: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_dataobj_builder_required_replicas_refresh_duration_seconds",
			Help:                            "Time taken to read the offsets of every partition and refresh loki_dataobj_builder_required_replicas, successful or not.",
			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
	}
}
