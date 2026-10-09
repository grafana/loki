package metastore

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// status is the result label of the ToC writer metrics. statusSkipped means
// the ToC already held the entry's path, so WriteEntry did not write it.
type status string

const (
	statusSuccess status = "success"
	statusFailure status = "failure"
	statusSkipped status = "skipped"
)

// TocWriterMetrics instruments a [TableOfContentsWriter].
type TocWriterMetrics struct {
	writeEntryAttemptSeconds *prometheus.HistogramVec
	writeEntryTotalSeconds   *prometheus.HistogramVec
}

// NewTocWriterMetrics creates the metrics of a [TableOfContentsWriter] and
// registers them with reg. If reg is nil, it does not register them.
func NewTocWriterMetrics(reg prometheus.Registerer) *TocWriterMetrics {
	factory := promauto.With(reg)
	metrics := &TocWriterMetrics{
		writeEntryAttemptSeconds: factory.NewHistogramVec(prometheus.HistogramOpts{
			Name:                            "loki_metastore_toc_write_entry_attempt_seconds",
			Help:                            "Time taken by one attempt to write an entry to a ToC in seconds, by result.",
			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}, []string{"result"}),

		writeEntryTotalSeconds: factory.NewHistogramVec(prometheus.HistogramOpts{
			Name:                            "loki_metastore_toc_write_entry_total_seconds",
			Help:                            "Time taken to write an entry to a ToC in seconds, by result. It includes every attempt and the backoff between them.",
			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}, []string{"result"}),
	}

	// Initialize each status to 0, otherwise neither the rate nor increase
	// PromQL functions detect increases from 0 to 1.
	metrics.writeEntryAttemptSeconds.WithLabelValues(string(statusSuccess))
	metrics.writeEntryAttemptSeconds.WithLabelValues(string(statusFailure))
	metrics.writeEntryAttemptSeconds.WithLabelValues(string(statusSkipped))
	metrics.writeEntryTotalSeconds.WithLabelValues(string(statusSuccess))
	metrics.writeEntryTotalSeconds.WithLabelValues(string(statusFailure))
	metrics.writeEntryTotalSeconds.WithLabelValues(string(statusSkipped))

	return metrics
}
