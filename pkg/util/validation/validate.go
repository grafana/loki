package validation

import (
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	discardReasonLabel = "reason"

	// RateLimited is one of the values for the reason to discard samples.
	// Declared here to avoid duplication in ingester and distributor.
	RateLimited       = "rate_limited"
	rateLimitErrorMsg = "Ingestion rate limit exceeded (limit: %d bytes/sec) while attempting to ingest '%d' lines totaling '%d' bytes, reduce log volume or contact your Loki administrator to see if the limit can be increased"
	// LineTooLong is a reason for discarding too long log lines.
	LineTooLong         = "line_too_long"
	lineTooLongErrorMsg = "Max entry size '%d' bytes exceeded for stream '%s' while adding an entry with length '%d' bytes"
	// StreamLimit is a reason for discarding lines when we can't create a new stream
	// because the limit of active streams has been reached.
	StreamLimit         = "stream_limit"
	streamLimitErrorMsg = "Maximum active stream limit exceeded, reduce the number of active streams (reduce labels or reduce label values), or contact your Loki administrator to see if the limit can be increased"
	// GreaterThanMaxSampleAge is a reason for discarding log lines which are older than the current time - `reject_old_samples_max_age`
	GreaterThanMaxSampleAge         = "greater_than_max_sample_age"
	greaterThanMaxSampleAgeErrorMsg = "entry for stream '%s' has timestamp too old: %v"
	// TooFarInFuture is a reason for discarding log lines which are newer than the current time + `creation_grace_period`
	TooFarInFuture         = "too_far_in_future"
	tooFarInFutureErrorMsg = "entry for stream '%s' has timestamp too new: %v"
	// MaxLabelNamesPerSeries is a reason for discarding a log line which has too many label names
	MaxLabelNamesPerSeries         = "max_label_names_per_series"
	maxLabelNamesPerSeriesErrorMsg = "entry for stream '%s' has %d label names; limit %d"
	// LabelNameTooLong is a reason for discarding a log line which has a label name too long
	LabelNameTooLong         = "label_name_too_long"
	labelNameTooLongErrorMsg = "stream '%s' has label name too long: '%s'"
	// LabelValueTooLong is a reason for discarding a log line which has a lable value too long
	LabelValueTooLong         = "label_value_too_long"
	labelValueTooLongErrorMsg = "stream '%s' has label value too long: '%s'"
	// DuplicateLabelNames is a reason for discarding a log line which has duplicate label names
	DuplicateLabelNames         = "duplicate_label_names"
	duplicateLabelNamesErrorMsg = "stream '%s' has duplicate label name: '%s'"
)

// DiscardedBytes is a metric of the total discarded bytes, by reason.
var DiscardedBytes = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "discarded_bytes_total",
		Help:      "The total number of bytes that were discarded.",
	},
	[]string{discardReasonLabel, "tenant"},
)

// DiscardedSamples is a metric of the number of discarded samples, by reason.
var DiscardedSamples = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "discarded_samples_total",
		Help:      "The total number of samples that were discarded.",
	},
	[]string{discardReasonLabel, "tenant"},
)

func init() {
	prometheus.MustRegister(DiscardedSamples, DiscardedBytes)
}

// RateLimitedErrorMsg returns an error string for rate limited requests
func RateLimitedErrorMsg(limit, lines, bytes int) string {
	return fmt.Sprintf(rateLimitErrorMsg, limit, lines, bytes)
}

// LineTooLongErrorMsg returns an error string for a line which is too long
func LineTooLongErrorMsg(maxLength, entryLength int, stream string) string {
	return fmt.Sprintf(lineTooLongErrorMsg, maxLength, stream, entryLength)
}

// StreamLimitErrorMsg returns an error string for requests refused for exceeding active stream limits
func StreamLimitErrorMsg() string {
	return fmt.Sprint(streamLimitErrorMsg)
}

// GreaterThanMaxSampleAgeErrorMsg returns an error string for a line with a timestamp too old
func GreaterThanMaxSampleAgeErrorMsg(stream string, timestamp time.Time) string {
	return fmt.Sprintf(greaterThanMaxSampleAgeErrorMsg, stream, timestamp)
}

// TooFarInFutureErrorMsg returns an error string for a line with a timestamp too far in the future
func TooFarInFutureErrorMsg(stream string, timestamp time.Time) string {
	return fmt.Sprintf(tooFarInFutureErrorMsg, stream, timestamp)
}

// MaxLabelNamesPerSeriesErrorMsg returns an error string for a stream with too many labels
func MaxLabelNamesPerSeriesErrorMsg(stream string, labelCount, labelLimit int) string {
	return fmt.Sprintf(maxLabelNamesPerSeriesErrorMsg, stream, labelCount, labelLimit)
}

// LabelNameTooLongErrorMsg returns an error string for a stream with a label name too long
func LabelNameTooLongErrorMsg(stream, label string) string {
	return fmt.Sprintf(labelNameTooLongErrorMsg, stream, label)
}

// LabelValueTooLongErrorMsg returns an error string for a stream with a label value too long
func LabelValueTooLongErrorMsg(stream, labelValue string) string {
	return fmt.Sprintf(labelValueTooLongErrorMsg, stream, labelValue)
}

// DuplicateLabelNamesErrorMsg returns an error string for a stream which has duplicate labels
func DuplicateLabelNamesErrorMsg(stream, label string) string {
	return fmt.Sprintf(duplicateLabelNamesErrorMsg, stream, label)
}
