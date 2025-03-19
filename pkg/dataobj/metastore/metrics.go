package metastore

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type status string

const (
	statusSuccess status = "success"
	statusFailure status = "failure"
)

type metastoreMetrics struct {
	metastoreProcessingTime prometheus.Histogram
	metastoreReplayTime     prometheus.Histogram
	metastoreEncodingTime   prometheus.Histogram
	metastoreWriteFailures  *prometheus.CounterVec
}

func newMetastoreMetrics() *metastoreMetrics {
	metrics := &metastoreMetrics{
		metastoreReplayTime: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_dataobj_consumer_metastore_replay_seconds",
			Help:                            "Time taken to replay existing metastore data into the in-memory builder in seconds",
			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		metastoreEncodingTime: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_dataobj_consumer_metastore_encoding_seconds",
			Help:                            "Time taken to add the new metadata & encode the new metastore data object in seconds",
			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		metastoreProcessingTime: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_dataobj_consumer_metastore_processing_seconds",
			Help:                            "Total time taken to update all metastores for a flushed dataobj in seconds",
			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		metastoreWriteFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "loki_dataobj_consumer_metastore_writes_total",
			Help: "Total number of metastore write failures",
		}, []string{"status"}),
	}

	return metrics
}

func (p *metastoreMetrics) register(reg prometheus.Registerer) error {
	collectors := []prometheus.Collector{
		p.metastoreReplayTime,
		p.metastoreEncodingTime,
		p.metastoreProcessingTime,
		p.metastoreWriteFailures,
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

func (p *metastoreMetrics) unregister(reg prometheus.Registerer) {
	collectors := []prometheus.Collector{
		p.metastoreReplayTime,
		p.metastoreEncodingTime,
		p.metastoreProcessingTime,
		p.metastoreWriteFailures,
	}

	for _, collector := range collectors {
		reg.Unregister(collector)
	}
}

func (p *metastoreMetrics) incMetastoreWrites(status status) {
	p.metastoreWriteFailures.WithLabelValues(string(status)).Inc()
}

func (p *metastoreMetrics) observeMetastoreReplay(recordTimestamp time.Time) {
	if !recordTimestamp.IsZero() { // Only observe if timestamp is valid
		p.metastoreReplayTime.Observe(time.Since(recordTimestamp).Seconds())
	}
}

func (p *metastoreMetrics) observeMetastoreEncoding(recordTimestamp time.Time) {
	if !recordTimestamp.IsZero() { // Only observe if timestamp is valid
		p.metastoreEncodingTime.Observe(time.Since(recordTimestamp).Seconds())
	}
}

func (p *metastoreMetrics) observeMetastoreProcessing(recordTimestamp time.Time) {
	if !recordTimestamp.IsZero() { // Only observe if timestamp is valid
		p.metastoreProcessingTime.Observe(time.Since(recordTimestamp).Seconds())
	}
}
