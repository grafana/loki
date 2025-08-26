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

type tocMetrics struct {
	tocProcessingTime prometheus.Histogram
	tocReplayTime     prometheus.Histogram
	tocEncodingTime   prometheus.Histogram
	tocWriteFailures  *prometheus.CounterVec
}

func newTableOfContentsMetrics() *tocMetrics {
	metrics := &tocMetrics{
		tocReplayTime: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_dataobj_consumer_metastore_replay_seconds",
			Help:                            "Time taken to replay existing metastore data into the in-memory builder in seconds",
			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		tocEncodingTime: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_dataobj_consumer_metastore_encoding_seconds",
			Help:                            "Time taken to add the new metadata & encode the new metastore data object in seconds",
			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		tocProcessingTime: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_dataobj_consumer_metastore_processing_seconds",
			Help:                            "Total time taken to update all metastores for a flushed dataobj in seconds",
			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		tocWriteFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "loki_dataobj_consumer_metastore_writes_total",
			Help: "Total number of metastore writes",
		}, []string{"status"}),
	}

	return metrics
}

func (p *tocMetrics) register(reg prometheus.Registerer) error {
	collectors := []prometheus.Collector{
		p.tocReplayTime,
		p.tocEncodingTime,
		p.tocProcessingTime,
		p.tocWriteFailures,
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

func (p *tocMetrics) unregister(reg prometheus.Registerer) {
	collectors := []prometheus.Collector{
		p.tocReplayTime,
		p.tocEncodingTime,
		p.tocProcessingTime,
		p.tocWriteFailures,
	}

	for _, collector := range collectors {
		reg.Unregister(collector)
	}
}

func (p *tocMetrics) incMetastoreWrites(status status) {
	p.tocWriteFailures.WithLabelValues(string(status)).Inc()
}

func (p *tocMetrics) observeMetastoreReplay(recordTimestamp time.Time) {
	if !recordTimestamp.IsZero() { // Only observe if timestamp is valid
		p.tocReplayTime.Observe(time.Since(recordTimestamp).Seconds())
	}
}

func (p *tocMetrics) observeMetastoreEncoding(recordTimestamp time.Time) {
	if !recordTimestamp.IsZero() { // Only observe if timestamp is valid
		p.tocEncodingTime.Observe(time.Since(recordTimestamp).Seconds())
	}
}

func (p *tocMetrics) observeMetastoreProcessing(recordTimestamp time.Time) {
	if !recordTimestamp.IsZero() { // Only observe if timestamp is valid
		p.tocProcessingTime.Observe(time.Since(recordTimestamp).Seconds())
	}
}

type objectMetastoreMetrics struct {
	streamFilterTotalDuration           prometheus.Histogram
	streamFilterPaths                   prometheus.Histogram
	streamFilterSections                prometheus.Histogram
	streamFilterStreamsReadDuration     prometheus.Histogram
	streamFilterPointersReadDuration    prometheus.Histogram
	estimateSectionsTotalDuration       prometheus.Histogram
	estimateSectionsPointerReadDuration prometheus.Histogram
	estimateSectionsPaths               prometheus.Histogram
	estimateSectionsSections            prometheus.Histogram
	resolvedSectionsTotalDuration       prometheus.Histogram
	resolvedSectionsTotal               prometheus.Histogram
	resolvedSectionsRatio               prometheus.Histogram
}

func newObjectMetastoreMetrics() *objectMetastoreMetrics {
	metrics := &objectMetastoreMetrics{
		streamFilterTotalDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_metastore_stream_filter_total_duration_seconds",
			Help:                            "Total time taken to lookup streams for a Metastore query in seconds",
			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		streamFilterPaths: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_metastore_stream_filter_paths_total",
			Help:                            "Total number of paths to be searched for a Metastore query",
			Buckets:                         []float64{0, 10, 20, 30, 50, 70, 100, 150, 200, 1000},
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		streamFilterSections: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_metastore_stream_filter_sections_total",
			Help:                            "Total number of sections resolved for a Metastore query when listing sections from stream matchers",
			Buckets:                         []float64{0, 10, 20, 30, 50, 70, 100, 150, 200},
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		streamFilterStreamsReadDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_metastore_stream_filter_streams_read_duration_seconds",
			Help:                            "Total time taken to read one streams section during a Metastore query when listing sections from stream matchers in seconds",
			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		streamFilterPointersReadDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_metastore_stream_filter_pointers_read_duration_seconds",
			Help:                            "Total time taken to read one pointers section during a Metastore query when listing sections from stream matchers in seconds",
			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		estimateSectionsTotalDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_metastore_estimate_sections_total_duration_seconds",
			Help:                            "Total time taken to check section membership for a Metastore query when listing sections from AMQ filters in seconds",
			Buckets:                         []float64{0, 10, 20, 30, 50, 70, 100, 150, 200},
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		estimateSectionsPaths: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_metastore_estimate_sections_paths_total",
			Help:                            "Total number of paths to be searched for a Metastore query when listing sections from AMQ filters",
			Buckets:                         []float64{0, 10, 20, 30, 50, 70, 100, 150, 200, 1000},
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		estimateSectionsPointerReadDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_metastore_estimate_sections_pointer_read_duration_seconds",
			Help:                            "Total time taken to read one pointers section during a Metastore query when listing sections from AMQ filters in seconds",
			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		estimateSectionsSections: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_metastore_estimate_sections_sections_total",
			Help:                            "Total number of sections resolved for a Metastore query when listing sections from AMQ filters",
			Buckets:                         []float64{0, 10, 20, 30, 50, 70, 100, 150, 200},
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		resolvedSectionsTotalDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_metastore_resolved_sections_total_duration_seconds",
			Help:                            "Total time taken to resolve sections for a Metastore query",
			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		resolvedSectionsTotal: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_metastore_resolved_sections_total",
			Help:                            "Total number of sections resolved for a Metastore query",
			Buckets:                         []float64{0, 10, 20, 30, 50, 70, 100, 150, 200},
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		resolvedSectionsRatio: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_metastore_resolved_sections_ratio",
			Help:                            "Ratio of sections resolved for a Metastore query between stream filters and then intersecting with section estimates",
			Buckets:                         []float64{0.0, 0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0},
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
	}

	return metrics
}

func (p *objectMetastoreMetrics) register(reg prometheus.Registerer) {
	reg.MustRegister(p.streamFilterTotalDuration)
	reg.MustRegister(p.streamFilterPaths)
	reg.MustRegister(p.streamFilterSections)
	reg.MustRegister(p.streamFilterStreamsReadDuration)
	reg.MustRegister(p.streamFilterPointersReadDuration)
	reg.MustRegister(p.estimateSectionsTotalDuration)
	reg.MustRegister(p.estimateSectionsPointerReadDuration)
	reg.MustRegister(p.estimateSectionsSections)
	reg.MustRegister(p.resolvedSectionsTotal)
	reg.MustRegister(p.resolvedSectionsRatio)
}
