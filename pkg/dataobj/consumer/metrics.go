package consumer

import (
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type partitionOffsetMetrics struct {
	currentOffset prometheus.GaugeFunc
	lastOffset    int64

	// Error counters
	flushFailures          prometheus.Counter
	commitFailures         prometheus.Counter
	metastoreWriteFailures prometheus.Counter
	appendFailures         prometheus.Counter

	// Processing delay histogram
	processingDelay     prometheus.Histogram
	metastoreReplay     prometheus.Histogram
	metastoreEncoding   prometheus.Histogram
	metastoreProcessing prometheus.Histogram
}

func newPartitionOffsetMetrics() *partitionOffsetMetrics {
	p := &partitionOffsetMetrics{
		flushFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_consumer_flush_failures_total",
			Help: "Total number of flush failures",
		}),
		commitFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_consumer_commit_failures_total",
			Help: "Total number of commit failures",
		}),
		appendFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_consumer_append_failures_total",
			Help: "Total number of append failures",
		}),
		metastoreWriteFailures: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_consumer_metastore_write_failures_total",
			Help: "Total number of metastore write failures",
		}),
		processingDelay: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_dataobj_consumer_processing_delay_seconds",
			Help:                            "Time difference between record timestamp and processing time in seconds",
			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		metastoreReplay: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_dataobj_consumer_metastore_replay_seconds",
			Help:                            "Time taken to replay existing metastore data into the in-memory builder in seconds",
			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		metastoreEncoding: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_dataobj_consumer_metastore_encoding_seconds",
			Help:                            "Time taken to add the new metadata & encode the new metastore data object in seconds",
			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		metastoreProcessing: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_dataobj_consumer_metastore_processing_seconds",
			Help:                            "Total time taken to update all metastores for a flushed dataobj in seconds",
			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
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
	return float64(atomic.LoadInt64(&p.lastOffset))
}

func (p *partitionOffsetMetrics) register(reg prometheus.Registerer) error {
	collectors := []prometheus.Collector{
		p.currentOffset,
		p.flushFailures,
		p.commitFailures,
		p.appendFailures,
		p.processingDelay,
		p.metastoreReplay,
		p.metastoreEncoding,
		p.metastoreProcessing,
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
		p.currentOffset,
		p.flushFailures,
		p.commitFailures,
		p.appendFailures,
		p.processingDelay,
		p.metastoreReplay,
		p.metastoreEncoding,
		p.metastoreProcessing,
	}

	for _, collector := range collectors {
		reg.Unregister(collector)
	}
}

func (p *partitionOffsetMetrics) updateOffset(offset int64) {
	atomic.StoreInt64(&p.lastOffset, offset)
}

func (p *partitionOffsetMetrics) incFlushFailures() {
	p.flushFailures.Inc()
}

func (p *partitionOffsetMetrics) incCommitFailures() {
	p.commitFailures.Inc()
}

func (p *partitionOffsetMetrics) incAppendFailures() {
	p.appendFailures.Inc()
}

func (p *partitionOffsetMetrics) incMetastoreWriteFailures() {
	p.metastoreWriteFailures.Inc()
}

func (p *partitionOffsetMetrics) observeProcessingDelay(recordTimestamp time.Time) {
	// Convert milliseconds to seconds and calculate delay
	if !recordTimestamp.IsZero() { // Only observe if timestamp is valid
		p.processingDelay.Observe(time.Since(recordTimestamp).Seconds())
	}
}

func (p *partitionOffsetMetrics) observeMetastoreReplay(recordTimestamp time.Time) {
	if !recordTimestamp.IsZero() { // Only observe if timestamp is valid
		p.metastoreReplay.Observe(time.Since(recordTimestamp).Seconds())
	}
}

func (p *partitionOffsetMetrics) observeMetastoreEncoding(recordTimestamp time.Time) {
	if !recordTimestamp.IsZero() { // Only observe if timestamp is valid
		p.metastoreEncoding.Observe(time.Since(recordTimestamp).Seconds())
	}
}

func (p *partitionOffsetMetrics) observeMetastoreProcessing(recordTimestamp time.Time) {
	if !recordTimestamp.IsZero() { // Only observe if timestamp is valid
		p.metastoreProcessing.Observe(time.Since(recordTimestamp).Seconds())
	}
}
