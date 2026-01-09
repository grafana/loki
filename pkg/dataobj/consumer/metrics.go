package consumer

import (
	"time"

	"go.uber.org/atomic"

	"github.com/prometheus/client_golang/prometheus"
)

// Flush reason constants
const (
	FlushReasonMaxAge      = "max_age"
	FlushReasonBuilderFull = "builder_full"
	FlushReasonIdle        = "idle"
)

type partitionOffsetMetrics struct {
	currentOffset prometheus.GaugeFunc
	lastOffset    atomic.Int64

	// Error counters
	commitFailures prometheus.Counter
	appendFailures prometheus.Counter
	flushesTotal   *prometheus.CounterVec

	// Request counters
	commitsTotal prometheus.Counter

	latestDelay      prometheus.Gauge // Latest delta between record timestamp and current time
	processedRecords prometheus.Counter
	processedBytes   prometheus.Counter

	flushDuration prometheus.Histogram
}

func newPartitionOffsetMetrics() *partitionOffsetMetrics {
	p := &partitionOffsetMetrics{
		commitFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_consumer_commit_failures_total",
			Help: "Total number of commit failures",
		}),
		appendFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_consumer_append_failures_total",
			Help: "Total number of append failures",
		}),
		commitsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_consumer_commits_total",
			Help: "Total number of commits",
		}),
		latestDelay: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "loki_dataobj_consumer_latest_processing_delay_seconds",
			Help: "Latest time difference bweteen record timestamp and processing time in seconds",
		}),
		processedRecords: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_consumer_processed_records_total",
			Help: "Total number of records processed.",
		}),
		processedBytes: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_consumer_processed_bytes_total",
			Help: "Total number of bytes processed.",
		}),
		flushDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name: "loki_dataobj_consumer_flush_duration_seconds",
			Help: "Time taken to flush a data object (build, sort, upload, etc).",

			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		flushesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "loki_dataobj_consumer_flushes_total",
			Help: "Total number of data objects flushed.",
		}, []string{"reason"}),
	}

	p.currentOffset = prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name: "loki_dataobj_consumer_current_offset",
			Help: "The last consumed offset for this partition",
		},
		p.getCurrentOffset,
	)

	return p
}

func (p *partitionOffsetMetrics) getCurrentOffset() float64 {
	return float64(p.lastOffset.Load())
}

func (p *partitionOffsetMetrics) register(reg prometheus.Registerer) error {
	collectors := []prometheus.Collector{
		p.commitFailures,
		p.appendFailures,
		p.flushesTotal,
		p.latestDelay,
		p.processedRecords,
		p.processedBytes,
		p.currentOffset,
		p.flushDuration,
	}

	for _, collector := range collectors {
		if err := reg.Register(collector); err != nil {
			if _, ok := err.(prometheus.AlreadyRegisteredError); !ok {
				return err
			}
		}
	}
	return nil
}

func (p *partitionOffsetMetrics) unregister(reg prometheus.Registerer) {
	collectors := []prometheus.Collector{
		p.commitFailures,
		p.appendFailures,
		p.flushesTotal,
		p.latestDelay,
		p.processedRecords,
		p.processedBytes,
		p.currentOffset,
		p.flushDuration,
	}

	for _, collector := range collectors {
		reg.Unregister(collector)
	}
}

func (p *partitionOffsetMetrics) updateOffset(offset int64) {
	p.lastOffset.Store(offset)
}

func (p *partitionOffsetMetrics) incCommitFailures() {
	p.commitFailures.Inc()
}

func (p *partitionOffsetMetrics) incAppendFailures() {
	p.appendFailures.Inc()
}

func (p *partitionOffsetMetrics) incCommitsTotal() {
	p.commitsTotal.Inc()
}

func (p *partitionOffsetMetrics) incFlushesTotal(reason string) {
	p.flushesTotal.WithLabelValues(reason).Inc()
}

func (p *partitionOffsetMetrics) observeProcessingDelay(recordTimestamp time.Time) {
	// Convert milliseconds to seconds and calculate delay
	if !recordTimestamp.IsZero() { // Only observe if timestamp is valid
		delay := time.Since(recordTimestamp).Seconds()

		p.latestDelay.Set(delay)
	}
}
