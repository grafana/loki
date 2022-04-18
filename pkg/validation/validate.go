package validation

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/pkg/util/flagext"
)

const (
	ReasonLabel = "reason"
	// InvalidLabels is a reason for discarding log lines which have labels that cannot be parsed.
	InvalidLabels = "invalid_labels"
	MissingLabels = "missing_labels"

	MissingLabelsErrorMsg = "error at least one label pair is required per stream"
	InvalidLabelsErrorMsg = "Error parsing labels '%s' with error: %s"
	// RateLimited is one of the values for the reason to discard samples.
	// Declared here to avoid duplication in ingester and distributor.
	RateLimited         = "rate_limited"
	RateLimitedErrorMsg = "Ingestion rate limit exceeded for user %s (limit: %d bytes/sec) while attempting to ingest '%d' lines totaling '%d' bytes, reduce log volume or contact your Loki administrator to see if the limit can be increased"
	// LineTooLong is a reason for discarding too long log lines.
	LineTooLong         = "line_too_long"
	LineTooLongErrorMsg = "Max entry size '%d' bytes exceeded for stream '%s' while adding an entry with length '%d' bytes"
	// StreamLimit is a reason for discarding lines when we can't create a new stream
	// because the limit of active streams has been reached.
	StreamLimit         = "stream_limit"
	StreamLimitErrorMsg = "Maximum active stream limit exceeded, reduce the number of active streams (reduce labels or reduce label values), or contact your Loki administrator to see if the limit can be increased"
	// StreamRateLimit is a reason for discarding lines when the streams own rate limit is hit
	// rather than the overall ingestion rate limit.
	StreamRateLimit = "per_stream_rate_limit"
	OutOfOrder      = "out_of_order"
	TooFarBehind    = "too_far_behind"
	// GreaterThanMaxSampleAge is a reason for discarding log lines which are older than the current time - `reject_old_samples_max_age`
	GreaterThanMaxSampleAge         = "greater_than_max_sample_age"
	GreaterThanMaxSampleAgeErrorMsg = "entry for stream '%s' has timestamp too old: %v, oldest acceptable timestamp is: %v"
	// TooFarInFuture is a reason for discarding log lines which are newer than the current time + `creation_grace_period`
	TooFarInFuture         = "too_far_in_future"
	TooFarInFutureErrorMsg = "entry for stream '%s' has timestamp too new: %v"
	// MaxLabelNamesPerSeries is a reason for discarding a log line which has too many label names
	MaxLabelNamesPerSeries         = "max_label_names_per_series"
	MaxLabelNamesPerSeriesErrorMsg = "entry for stream '%s' has %d label names; limit %d"
	// LabelNameTooLong is a reason for discarding a log line which has a label name too long
	LabelNameTooLong         = "label_name_too_long"
	LabelNameTooLongErrorMsg = "stream '%s' has label name too long: '%s'"
	// LabelValueTooLong is a reason for discarding a log line which has a lable value too long
	LabelValueTooLong         = "label_value_too_long"
	LabelValueTooLongErrorMsg = "stream '%s' has label value too long: '%s'"
	// DuplicateLabelNames is a reason for discarding a log line which has duplicate label names
	DuplicateLabelNames         = "duplicate_label_names"
	DuplicateLabelNamesErrorMsg = "stream '%s' has duplicate label name: '%s'"
)

type ErrStreamRateLimit struct {
	RateLimit flagext.ByteSize
	Labels    string
	Bytes     flagext.ByteSize
}

func (e *ErrStreamRateLimit) Error() string {
	return fmt.Sprintf("Per stream rate limit exceeded (limit: %s/sec) while attempting to ingest for stream '%s' totaling %s, consider splitting a stream via additional labels or contact your Loki administrator to see if the limit can be increased",
		e.RateLimit.String(),
		e.Labels,
		e.Bytes.String())
}

// MutatedSamples is a metric of the total number of lines mutated, by reason.
var MutatedSamples = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "mutated_samples_total",
		Help:      "The total number of samples that have been mutated.",
	},
	[]string{ReasonLabel, "truncated"},
)

// MutatedBytes is a metric of the total mutated bytes, by reason.
var MutatedBytes = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "mutated_bytes_total",
		Help:      "The total number of bytes that have been mutated.",
	},
	[]string{ReasonLabel, "truncated"},
)

// DiscardedBytes is a metric of the total discarded bytes, by reason.
var DiscardedBytes = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "discarded_bytes_total",
		Help:      "The total number of bytes that were discarded.",
	},
	[]string{ReasonLabel, "tenant"},
)

// DiscardedSamples is a metric of the number of discarded samples, by reason.
var DiscardedSamples = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "discarded_samples_total",
		Help:      "The total number of samples that were discarded.",
	},
	[]string{ReasonLabel, "tenant"},
)

func init() {
	prometheus.MustRegister(DiscardedSamples, DiscardedBytes)
}
