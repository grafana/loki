package metastore

import (
	"context"
	"errors"

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
			Name:                            "loki_metastore_toc_replay_seconds",
			Help:                            "Time taken to replay existing Table of Contents data into the new builder in seconds",
			Buckets:                         nil,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		tocEncodingTime: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_metastore_toc_encoding_seconds",
			Help:                            "Time taken to add the new entries & encode the a single Table of Contents metastore file in seconds",
			Buckets:                         nil,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		tocProcessingTime: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_metastore_toc_processing_seconds",
			Help:                            "Total time taken to update all Table of Contents files for a metastore WriteEntry operation in seconds",
			Buckets:                         nil,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		tocWriteFailures: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "loki_metastore_toc_writes_total",
			Help: "Total number of metastore writes",
		}, []string{"status"}),
	}

	// Initialize each status to 0, otherwise neither the rate nor increase
	// PromQL functions detect increases from 0 to 1.
	metrics.tocWriteFailures.WithLabelValues(string(statusSuccess)).Add(0)
	metrics.tocWriteFailures.WithLabelValues(string(statusFailure)).Add(0)

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

func (p *tocMetrics) incTableOfContentsWrites(status status) {
	p.tocWriteFailures.WithLabelValues(string(status)).Inc()
}

// Values of the diverged label of the duplicate sections metric.
const (
	divergedFalse = "false"
	divergedTrue  = "true"
)

// Values of the result label of the get indexes duration metric.
const (
	resultSuccess          = "success"
	resultError            = "error"
	resultCanceled         = "canceled"
	resultDeadlineExceeded = "deadline_exceeded"
)

func getIndexesResult(err error) string {
	switch {
	case err == nil:
		return resultSuccess
	case errors.Is(err, context.Canceled):
		return resultCanceled
	case errors.Is(err, context.DeadlineExceeded):
		return resultDeadlineExceeded
	default:
		return resultError
	}
}

type ObjectMetastoreMetrics struct {
	getIndexesTotalDuration             *prometheus.HistogramVec
	indexObjectsTotal                   prometheus.Histogram
	streamFilterTotalDuration           prometheus.Histogram
	streamFilterSections                prometheus.Histogram
	streamFilterStreamsReadDuration     prometheus.Histogram
	streamFilterPointersReadDuration    prometheus.Histogram
	estimateSectionsTotalDuration       prometheus.Histogram
	estimateSectionsPointerReadDuration prometheus.Histogram
	estimateSectionsSections            prometheus.Histogram
	resolvedSectionsTotalDuration       prometheus.Histogram
	resolvedSectionsTotal               prometheus.Histogram
	resolvedSectionsRatio               prometheus.Histogram

	indexReadFlowTotal        *prometheus.CounterVec
	indexReadRowsPerObject    *prometheus.HistogramVec
	resolvedSectionsPerObject prometheus.Histogram
	duplicateSectionsTotal    *prometheus.CounterVec
}

func NewObjectMetastoreMetrics(reg prometheus.Registerer) *ObjectMetastoreMetrics {
	metrics := &ObjectMetastoreMetrics{
		getIndexesTotalDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:                            "loki_metastore_get_indexes_duration_seconds",
			Help:                            "Time taken to list the index objects for a Metastore query window in seconds",
			Buckets:                         nil,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}, []string{"result"}),
		indexObjectsTotal: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_metastore_index_objects_total",
			Help:                            "Total number of objects to be searched for a Metastore query",
			Buckets:                         nil,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		streamFilterTotalDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_metastore_stream_filter_total_duration_seconds",
			Help:                            "Total time taken to lookup streams for a Metastore query in seconds",
			Buckets:                         nil,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		streamFilterSections: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_metastore_stream_filter_sections_total",
			Help:                            "Total number of sections resolved for a Metastore query when listing sections from stream matchers",
			Buckets:                         nil,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		streamFilterStreamsReadDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_metastore_stream_filter_streams_read_duration_seconds",
			Help:                            "Total time taken to read one streams section during a Metastore query when listing sections from stream matchers in seconds",
			Buckets:                         nil,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		streamFilterPointersReadDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_metastore_stream_filter_pointers_read_duration_seconds",
			Help:                            "Total time taken to read one pointers section during a Metastore query when listing sections from stream matchers in seconds",
			Buckets:                         nil,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		estimateSectionsTotalDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_metastore_estimate_sections_total_duration_seconds",
			Help:                            "Total time taken to check section membership for a Metastore query when listing sections from AMQ filters in seconds",
			Buckets:                         nil,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		estimateSectionsPointerReadDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_metastore_estimate_sections_pointer_read_duration_seconds",
			Help:                            "Total time taken to read one pointers section during a Metastore query when listing sections from AMQ filters in seconds",
			Buckets:                         nil,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		estimateSectionsSections: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_metastore_estimate_sections_sections_total",
			Help:                            "Total number of sections resolved for a Metastore query when listing sections from AMQ filters",
			Buckets:                         nil,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		resolvedSectionsTotalDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_metastore_resolved_sections_total_duration_seconds",
			Help:                            "Total time taken to resolve sections for a Metastore query",
			Buckets:                         nil,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		resolvedSectionsTotal: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_metastore_resolved_sections_total",
			Help:                            "Total number of sections resolved for a Metastore query",
			Buckets:                         nil,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		resolvedSectionsRatio: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_metastore_resolved_sections_ratio",
			Help:                            "Ratio of sections resolved for a Metastore query between stream filters and then intersecting with section estimates",
			Buckets:                         nil,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		indexReadFlowTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "loki_metastore_index_read_flow_total",
			Help: "Total number of index objects routed to each read flow",
		}, []string{"flow"}),
		indexReadRowsPerObject: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:                            "loki_metastore_index_read_rows_per_object",
			Help:                            "Number of index rows read while resolving a single index object",
			Buckets:                         nil,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}, []string{"flow"}),
		resolvedSectionsPerObject: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_metastore_resolved_sections_per_object",
			Help:                            "Number of sections resolved from a single index object",
			Buckets:                         nil,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}),
		duplicateSectionsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "loki_metastore_duplicate_sections_total",
			Help: "Total number of duplicate sections found when more than one index object describes the same section. A section in N index objects counts N-1 with diverged=false. The copies of a diverged section disagree on the streams, the row count or the size. That fails the section lookup at the first one, so diverged=true counts at most one for each lookup",
		}, []string{"diverged"}),
	}

	// Report both outcomes from the start, so that a rate over diverged duplicates reads as
	// zero rather than going missing until the first one happens.
	metrics.duplicateSectionsTotal.WithLabelValues(divergedFalse)
	metrics.duplicateSectionsTotal.WithLabelValues(divergedTrue)

	for _, result := range []string{resultSuccess, resultError, resultCanceled, resultDeadlineExceeded} {
		metrics.getIndexesTotalDuration.WithLabelValues(result)
	}

	metrics.register(reg)

	return metrics
}

func (p *ObjectMetastoreMetrics) register(reg prometheus.Registerer) {
	if reg == nil {
		return
	}

	collectors := []prometheus.Collector{
		p.getIndexesTotalDuration,
		p.indexObjectsTotal,
		p.streamFilterTotalDuration,
		p.streamFilterSections,
		p.streamFilterStreamsReadDuration,
		p.streamFilterPointersReadDuration,
		p.estimateSectionsTotalDuration,
		p.estimateSectionsPointerReadDuration,
		p.estimateSectionsSections,
		p.resolvedSectionsTotalDuration,
		p.resolvedSectionsTotal,
		p.resolvedSectionsRatio,
		p.indexReadFlowTotal,
		p.indexReadRowsPerObject,
		p.resolvedSectionsPerObject,
		p.duplicateSectionsTotal,
	}

	for _, collector := range collectors {
		if err := reg.Register(collector); err != nil {
			if _, ok := err.(prometheus.AlreadyRegisteredError); !ok {
				panic(err)
			}
		}
	}
}
