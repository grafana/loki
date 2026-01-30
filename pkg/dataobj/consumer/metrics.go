package consumer

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
	commits        prometheus.Counter
	commitFailures prometheus.Counter

	// Deprecated, will be removed in two weeklies.
	latestDelay      prometheus.Gauge
	currentOffset    prometheus.Gauge
	processedBytes   prometheus.Counter
	processedRecords prometheus.Counter
}

func newMetrics(r prometheus.Registerer) *metrics {
	return &metrics{
		lastOffset: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "loki_dataobj_consumer_last_offset",
			Help: "The last consumed offset.",
		}),
		consumptionLag: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "loki_dataobj_consumer_consumption_lag_seconds",
			Help: "The time difference between the last consumed offset and the current time in seconds.",
		}),
		receivedBytes: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_consumer_received_bytes_total",
			Help: "The sum of bytes in all Kafka records.",
		}),
		discardedBytes: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_consumer_discarded_bytes_total",
			Help: "The sum of discarded bytes from corrupted or unprocessable Kafka records.",
		}),
		records: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_consumer_records_total",
			Help: "Total number of records received.",
		}),
		recordFailures: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_consumer_record_failures_total",
			Help: "Total number of records that failed to be processed.",
		}),
		commits: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_consumer_commits_total",
			Help: "Total number of commits",
		}),
		commitFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_consumer_commit_failures_total",
			Help: "Total number of commit failures",
		}),
		// TODO(grobinson): Remove after two minor releases.
		latestDelay: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "loki_dataobj_consumer_latest_processing_delay_seconds",
			Help: "Latest time difference bweteen record timestamp and processing time in seconds",
		}),
		currentOffset: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "loki_dataobj_consumer_current_offset",
			Help: "The last consumed offset.",
		}),
		processedBytes: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_consumer_processed_bytes_total",
			Help: "The sum of bytes in all Kafka records.",
		}),
		processedRecords: promauto.With(r).NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_consumer_processed_records_total",
			Help: "The total number of Kafka records.",
		}),
	}
}

func (m *metrics) setLastOffset(offset int64) {
	m.lastOffset.Set(float64(offset))
	m.currentOffset.Set(float64(offset))
}

func (m *metrics) setConsumptionLag(d time.Duration) {
	secs := float64(d.Seconds())
	m.consumptionLag.Set(secs)
	m.latestDelay.Set(secs)
}
