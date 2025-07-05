package indexing

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type indexBuilderMetrics struct {
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

func newIndexBuilderMetrics() *indexBuilderMetrics {
	p := &indexBuilderMetrics{
		commitFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "loki_index_builder_commit_failures_total",
			Help: "Total number of commit failures",
		}),
		appendFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "loki_index_builder_append_failures_total",
			Help: "Total number of append failures",
		}),
		appendsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "loki_index_builder_appends_total",
			Help: "Total number of appends",
		}),
		commitsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "loki_index_builder_commits_total",
			Help: "Total number of commits",
		}),
		processingDelay: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_index_builder_processing_delay_seconds",
			Help:                            "Time difference between record timestamp and processing time in seconds",
			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		bytesProcessed: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "loki_index_builder_bytes_processed_total",
			Help: "Total number of bytes processed from this partition",
		}),
	}

	return p
}

func (p *indexBuilderMetrics) register(reg prometheus.Registerer) error {
	collectors := []prometheus.Collector{
		p.commitFailures,
		p.appendFailures,
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

func (p *indexBuilderMetrics) unregister(reg prometheus.Registerer) {
	collectors := []prometheus.Collector{
		p.commitFailures,
		p.appendFailures,
		p.processingDelay,
		p.bytesProcessed,
	}

	for _, collector := range collectors {
		reg.Unregister(collector)
	}
}

func (p *indexBuilderMetrics) incCommitFailures() {
	p.commitFailures.Inc()
}

func (p *indexBuilderMetrics) incAppendFailures() {
	p.appendFailures.Inc()
}

func (p *indexBuilderMetrics) incAppendsTotal() {
	p.appendsTotal.Inc()
}

func (p *indexBuilderMetrics) incCommitsTotal() {
	p.commitsTotal.Inc()
}

func (p *indexBuilderMetrics) observeProcessingDelay(recordTimestamp time.Time) {
	// Convert milliseconds to seconds and calculate delay
	if !recordTimestamp.IsZero() { // Only observe if timestamp is valid
		p.processingDelay.Observe(time.Since(recordTimestamp).Seconds())
	}
}

func (p *indexBuilderMetrics) addBytesProcessed(bytes int64) {
	p.bytesProcessed.Add(float64(bytes))
}
