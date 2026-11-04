package metastore

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// tocOp is the op label of the ToC writer metrics.
type tocOp string

const (
	opWriteEntry tocOp = "write_entry"
	opReplace    tocOp = "replace_index_pointers"
)

// TocWriterMetrics instruments a [TableOfContentsWriter].
type TocWriterMetrics struct {
	writeAttemptSeconds *prometheus.HistogramVec
	writeSeconds        *prometheus.HistogramVec
}

// NewTocWriterMetrics creates the metrics of a [TableOfContentsWriter] and
// registers them with reg. If reg is nil, it does not register them. It panics
// if reg rejects a metric, for example one that reg holds with other labels.
func NewTocWriterMetrics(reg prometheus.Registerer) *TocWriterMetrics {
	factory := promauto.With(reg)
	metrics := &TocWriterMetrics{
		writeAttemptSeconds: factory.NewHistogramVec(prometheus.HistogramOpts{
			Name:                            "loki_metastore_toc_write_attempt_duration_seconds",
			Help:                            "Time taken by one attempt to write a ToC in seconds, by operation and result.",
			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}, []string{"op", "result"}),

		writeSeconds: factory.NewHistogramVec(prometheus.HistogramOpts{
			Name:                            "loki_metastore_toc_write_duration_seconds",
			Help:                            "Time taken to write a ToC in seconds, by operation and result. It includes every attempt and the backoff between them.",
			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}, []string{"op", "result"}),
	}

	// Initialize each series to 0, otherwise neither the rate nor increase
	// PromQL functions detect increases from 0 to 1.
	for _, op := range []tocOp{opWriteEntry, opReplace} {
		for _, result := range []changeResult{changeFailed, changeWritten, changeAlreadyApplied, changeSuperseded} {
			metrics.writeAttemptSeconds.WithLabelValues(string(op), string(result))
			metrics.writeSeconds.WithLabelValues(string(op), string(result))
		}
		// changeConflict is only reported for the attempts
		metrics.writeAttemptSeconds.WithLabelValues(string(op), string(changeConflict))
		// changeCancelled is only reported for the overall results
		metrics.writeSeconds.WithLabelValues(string(op), string(changeCancelled))
	}

	return metrics
}
