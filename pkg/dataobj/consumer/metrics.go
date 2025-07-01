package consumer

import (
	"time"

	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/util/constants"
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

	// Processing delay histogram
	processingDelay prometheus.Histogram

	// Data volume metrics
	bytesProcessed prometheus.Counter
}

func newPartitionOffsetMetrics() *partitionOffsetMetrics {
	subsystem := "dataobj_consumer"
	p := &partitionOffsetMetrics{
		commitFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Subsystem: subsystem,
			Name:      "commit_failures_total",
			Help:      "Total number of commit failures",
		}),
		appendFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Subsystem: subsystem,
			Name:      "append_failures_total",
			Help:      "Total number of append failures",
		}),
		appendsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Subsystem: subsystem,
			Name:      "appends_total",
			Help:      "Total number of appends",
		}),
		commitsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Subsystem: subsystem,
			Name:      "commits_total",
			Help:      "Total number of commits",
		}),
		processingDelay: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace:                       constants.Loki,
			Subsystem:                       subsystem,
			Name:                            "processing_delay_seconds",
			Help:                            "Time difference between record timestamp and processing time in seconds",
			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		bytesProcessed: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: constants.Loki,
			Subsystem: subsystem,
			Name:      "bytes_processed_total",
			Help:      "Total number of bytes processed from this partition",
		}),
	}

	p.currentOffset = prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Namespace: constants.Loki,
			Subsystem: subsystem,
			Name:      "current_offset",
			Help:      "The last consumed offset for this partition",
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
		p.currentOffset,
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
		p.processingDelay.Observe(time.Since(recordTimestamp).Seconds())
	}
}

func (p *partitionOffsetMetrics) addBytesProcessed(bytes int64) {
	p.bytesProcessed.Add(float64(bytes))
}
