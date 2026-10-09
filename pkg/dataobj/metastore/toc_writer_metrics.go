package metastore

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Values of the op label of the ToC writer metrics.
const (
	opWriteEntry = "write_entry"
	opReplace    = "replace_index_pointers"
)

// status is the result label of the ToC writer metrics. statusSkipped means
// the ToC needed no change, so the writer did not write it.
type status string

const (
	statusSuccess status = "success"
	statusFailure status = "failure"
	statusSkipped status = "skipped"
)

// TocWriterMetrics instruments a [TableOfContentsWriter].
type TocWriterMetrics struct {
	changeAttemptSeconds *prometheus.HistogramVec
	changeTotalSeconds   *prometheus.HistogramVec
}

// NewTocWriterMetrics creates the metrics of a [TableOfContentsWriter] and
// registers them with reg. If reg is nil, it does not register them.
func NewTocWriterMetrics(reg prometheus.Registerer) *TocWriterMetrics {
	factory := promauto.With(reg)
	metrics := &TocWriterMetrics{
		changeAttemptSeconds: factory.NewHistogramVec(prometheus.HistogramOpts{
			Name:                            "loki_metastore_toc_change_attempt_seconds",
			Help:                            "Time taken by one attempt to change a ToC in seconds, by operation and result.",
			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}, []string{"op", "result"}),

		changeTotalSeconds: factory.NewHistogramVec(prometheus.HistogramOpts{
			Name:                            "loki_metastore_toc_change_total_seconds",
			Help:                            "Time taken to change a ToC in seconds, by operation and result. It includes every attempt and the backoff between them.",
			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: 0,
		}, []string{"op", "result"}),
	}

	// Initialize each series to 0, otherwise neither the rate nor increase
	// PromQL functions detect increases from 0 to 1.
	for _, op := range []string{opWriteEntry, opReplace} {
		for _, s := range []status{statusSuccess, statusFailure, statusSkipped} {
			metrics.changeAttemptSeconds.WithLabelValues(op, string(s))
			metrics.changeTotalSeconds.WithLabelValues(op, string(s))
		}
	}

	return metrics
}
