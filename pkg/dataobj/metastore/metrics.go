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
			Help: "Total number of metastore writes",
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

type objectMetastoreMetrics struct {
	streamLookupTotalDuration          prometheus.Histogram
	streamLookupPaths                  prometheus.Histogram
	streamLookupSections               prometheus.Histogram
	streamLookupStreamReadDuration     prometheus.Histogram
	streamLookupPointerReadDuration    prometheus.Histogram
	checkMembershipTotalDuration       prometheus.Histogram
	checkMembershipPointerReadDuration prometheus.Histogram
	checkMembershipPaths               prometheus.Histogram
	checkMembershipSections            prometheus.Histogram
	resolvedSections                   prometheus.Histogram
	resolvedSectionsRatio              prometheus.Histogram
}

func newObjectMetastoreMetrics(reg prometheus.Registerer) *objectMetastoreMetrics {
	metrics := &objectMetastoreMetrics{
		streamLookupTotalDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_metastore_stream_lookup_total_duration_seconds",
			Help:                            "Total time taken to lookup streams for a Metastore query in seconds",
			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		streamLookupPaths: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_metastore_stream_lookup_paths_total",
			Help:                            "Total number of paths to be searched for a Metastore query",
			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		streamLookupStreamReadDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_metastore_stream_lookup_stream_read_duration_seconds",
			Help:                            "Total time taken to read one streams section during a Metastore query when listing sections from stream matchers in seconds",
			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		streamLookupPointerReadDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_metastore_stream_lookup_pointer_read_duration_seconds",
			Help:                            "Total time taken to read one pointers section during a Metastore query when listing sections from stream matchers in seconds",
			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		checkMembershipTotalDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_metastore_check_membership_total_duration_seconds",
			Help:                            "Total time taken to check section membership for a Metastore query when listing sections from AMQ filters in seconds",
			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		checkMembershipPointerReadDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_metastore_check_membership_pointer_read_duration_seconds",
			Help:                            "Total time taken to read one pointers section during a Metastore query when listing sections from AMQ filters in seconds",
			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		checkMembershipSections: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_metastore_check_membership_sections_total",
			Help:                            "Total number of sections resolved for a Metastore query when listing sections from AMQ filters",
			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		resolvedSections: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_metastore_resolved_sections_total",
			Help:                            "Total number of sections resolved for a Metastore query",
			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		resolvedSectionsRatio: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_metastore_predicate_amq_ratio",
			Help:                            "Ratio of sections resolved for a Metastore query between stream selectors and then intersecting with AMQ filters",
			Buckets:                         []float64{0.0, 0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0},
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
	}

	return metrics
}

func (p *objectMetastoreMetrics) register(reg prometheus.Registerer) {
	reg.MustRegister(p.streamLookupTotalDuration)
	reg.MustRegister(p.streamLookupPaths)
	reg.MustRegister(p.streamLookupStreamReadDuration)
	reg.MustRegister(p.streamLookupPointerReadDuration)
	reg.MustRegister(p.checkMembershipTotalDuration)
	reg.MustRegister(p.checkMembershipPointerReadDuration)
	reg.MustRegister(p.checkMembershipSections)
	reg.MustRegister(p.resolvedSections)
	reg.MustRegister(p.resolvedSectionsRatio)
}
