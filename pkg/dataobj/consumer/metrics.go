package consumer

import (
	"time"

	"go.uber.org/atomic"

	"github.com/prometheus/client_golang/prometheus"
)

type partitionOffsetMetrics struct {
	currentOffset prometheus.GaugeFunc
	lastOffset    atomic.Int64

	// Error counters
	commitFailures prometheus.Counter
	appendFailures prometheus.Counter

	// Request counters
	commitsTotal prometheus.Counter
	appendsTotal prometheus.Counter

	latestDelay     prometheus.Gauge     // Latest delta between record timestamp and current time
	processingDelay prometheus.Histogram // Processing delay histogram

	// Data volume metrics
	bytesProcessed prometheus.Counter
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
		appendsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_consumer_appends_total",
			Help: "Total number of appends",
		}),
		latestDelay: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "loki_dataobj_consumer_latest_processing_delay_seconds",
			Help: "Latest time difference bweteen record timestamp and processing time in seconds",
		}),
		processingDelay: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_dataobj_consumer_processing_delay_seconds",
			Help:                            "Time difference between record timestamp and processing time in seconds",
			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		bytesProcessed: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_consumer_bytes_processed_total",
			Help: "Total number of bytes processed from this partition",
		}),
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
		p.currentOffset,

		p.commitFailures,
		p.appendFailures,

		p.commitsTotal,
		p.appendsTotal,

		p.latestDelay,
		p.processingDelay,

		p.bytesProcessed,
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
		p.currentOffset,
		p.processingDelay,
		p.bytesProcessed,
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

func (p *partitionOffsetMetrics) incAppendsTotal() {
	p.appendsTotal.Inc()
}

func (p *partitionOffsetMetrics) incCommitsTotal() {
	p.commitsTotal.Inc()
}

func (p *partitionOffsetMetrics) observeProcessingDelay(recordTimestamp time.Time) {
	// Convert milliseconds to seconds and calculate delay
	if !recordTimestamp.IsZero() { // Only observe if timestamp is valid
		delay := time.Since(recordTimestamp).Seconds()

		p.latestDelay.Set(delay)
		p.processingDelay.Observe(delay)
	}
}

func (p *partitionOffsetMetrics) addBytesProcessed(bytes int64) {
	p.bytesProcessed.Add(float64(bytes))
}
