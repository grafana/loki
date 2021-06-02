package validation

import (
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/weaveworks/common/httpgrpc"

	"github.com/cortexproject/cortex/pkg/cortexpb"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/extract"
)

const (
	discardReasonLabel = "reason"

	errMetadataMissingMetricName = "metadata missing metric name"
	errMetadataTooLong           = "metadata '%s' value too long: %.200q metric %.200q"

	typeMetricName = "METRIC_NAME"
	typeHelp       = "HELP"
	typeUnit       = "UNIT"

	metricNameTooLong = "metric_name_too_long"
	helpTooLong       = "help_too_long"
	unitTooLong       = "unit_too_long"

	// ErrQueryTooLong is used in chunk store, querier and query frontend.
	ErrQueryTooLong = "the query time range exceeds the limit (query length: %s, limit: %s)"

	missingMetricName       = "missing_metric_name"
	invalidMetricName       = "metric_name_invalid"
	greaterThanMaxSampleAge = "greater_than_max_sample_age"
	maxLabelNamesPerSeries  = "max_label_names_per_series"
	tooFarInFuture          = "too_far_in_future"
	invalidLabel            = "label_invalid"
	labelNameTooLong        = "label_name_too_long"
	duplicateLabelNames     = "duplicate_label_names"
	labelsNotSorted         = "labels_not_sorted"
	labelValueTooLong       = "label_value_too_long"

	// Exemplar-specific validation reasons
	exemplarLabelsMissing    = "exemplar_labels_missing"
	exemplarLabelsTooLong    = "exemplar_labels_too_long"
	exemplarTimestampInvalid = "exemplar_timestamp_invalid"

	// RateLimited is one of the values for the reason to discard samples.
	// Declared here to avoid duplication in ingester and distributor.
	RateLimited = "rate_limited"

	// Too many HA clusters is one of the reasons for discarding samples.
	TooManyHAClusters = "too_many_ha_clusters"

	// The combined length of the label names and values of an Exemplar's LabelSet MUST NOT exceed 128 UTF-8 characters
	// https://github.com/OpenObservability/OpenMetrics/blob/main/specification/OpenMetrics.md#exemplars
	ExemplarMaxLabelSetLength = 128
)

// DiscardedSamples is a metric of the number of discarded samples, by reason.
var DiscardedSamples = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "cortex_discarded_samples_total",
		Help: "The total number of samples that were discarded.",
	},
	[]string{discardReasonLabel, "user"},
)

// DiscardedExemplars is a metric of the number of discarded exemplars, by reason.
var DiscardedExemplars = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "cortex_discarded_exemplars_total",
		Help: "The total number of exemplars that were discarded.",
	},
	[]string{discardReasonLabel, "user"},
)

// DiscardedMetadata is a metric of the number of discarded metadata, by reason.
var DiscardedMetadata = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "cortex_discarded_metadata_total",
		Help: "The total number of metadata that were discarded.",
	},
	[]string{discardReasonLabel, "user"},
)

func init() {
	prometheus.MustRegister(DiscardedSamples)
	prometheus.MustRegister(DiscardedExemplars)
	prometheus.MustRegister(DiscardedMetadata)
}

// SampleValidationConfig helps with getting required config to validate sample.
type SampleValidationConfig interface {
	RejectOldSamples(userID string) bool
	RejectOldSamplesMaxAge(userID string) time.Duration
	CreationGracePeriod(userID string) time.Duration
}

// ValidateSample returns an err if the sample is invalid.
// The returned error may retain the provided series labels.
func ValidateSample(cfg SampleValidationConfig, userID string, ls []cortexpb.LabelAdapter, s cortexpb.Sample) ValidationError {
	unsafeMetricName, _ := extract.UnsafeMetricNameFromLabelAdapters(ls)

	if cfg.RejectOldSamples(userID) && model.Time(s.TimestampMs) < model.Now().Add(-cfg.RejectOldSamplesMaxAge(userID)) {
		DiscardedSamples.WithLabelValues(greaterThanMaxSampleAge, userID).Inc()
		return newSampleTimestampTooOldError(unsafeMetricName, s.TimestampMs)
	}

	if model.Time(s.TimestampMs) > model.Now().Add(cfg.CreationGracePeriod(userID)) {
		DiscardedSamples.WithLabelValues(tooFarInFuture, userID).Inc()
		return newSampleTimestampTooNewError(unsafeMetricName, s.TimestampMs)
	}

	return nil
}

// ValidateExemplar returns an error if the exemplar is invalid.
// The returned error may retain the provided series labels.
func ValidateExemplar(userID string, ls []cortexpb.LabelAdapter, e cortexpb.Exemplar) ValidationError {
	if len(e.Labels) <= 0 {
		DiscardedExemplars.WithLabelValues(exemplarLabelsMissing, userID).Inc()
		return newExemplarEmtpyLabelsError(ls, []cortexpb.LabelAdapter{}, e.TimestampMs)
	}

	if e.TimestampMs == 0 {
		DiscardedExemplars.WithLabelValues(exemplarTimestampInvalid, userID).Inc()
		return newExemplarMissingTimestampError(
			ls,
			e.Labels,
			e.TimestampMs,
		)
	}

	// Exemplar label length does not include chars involved in text
	// rendering such as quotes, commas, etc.  See spec and const definition.
	labelSetLen := 0
	for _, l := range e.Labels {
		labelSetLen += utf8.RuneCountInString(l.Name)
		labelSetLen += utf8.RuneCountInString(l.Value)
	}

	if labelSetLen > ExemplarMaxLabelSetLength {
		DiscardedExemplars.WithLabelValues(exemplarLabelsTooLong, userID).Inc()
		return newExemplarLabelLengthError(
			ls,
			e.Labels,
			e.TimestampMs,
		)
	}

	return nil
}

// LabelValidationConfig helps with getting required config to validate labels.
type LabelValidationConfig interface {
	EnforceMetricName(userID string) bool
	MaxLabelNamesPerSeries(userID string) int
	MaxLabelNameLength(userID string) int
	MaxLabelValueLength(userID string) int
}

// ValidateLabels returns an err if the labels are invalid.
// The returned error may retain the provided series labels.
func ValidateLabels(cfg LabelValidationConfig, userID string, ls []cortexpb.LabelAdapter, skipLabelNameValidation bool) ValidationError {
	if cfg.EnforceMetricName(userID) {
		unsafeMetricName, err := extract.UnsafeMetricNameFromLabelAdapters(ls)
		if err != nil {
			DiscardedSamples.WithLabelValues(missingMetricName, userID).Inc()
			return newNoMetricNameError()
		}

		if !model.IsValidMetricName(model.LabelValue(unsafeMetricName)) {
			DiscardedSamples.WithLabelValues(invalidMetricName, userID).Inc()
			return newInvalidMetricNameError(unsafeMetricName)
		}
	}

	numLabelNames := len(ls)
	if numLabelNames > cfg.MaxLabelNamesPerSeries(userID) {
		DiscardedSamples.WithLabelValues(maxLabelNamesPerSeries, userID).Inc()
		return newTooManyLabelsError(ls, cfg.MaxLabelNamesPerSeries(userID))
	}

	maxLabelNameLength := cfg.MaxLabelNameLength(userID)
	maxLabelValueLength := cfg.MaxLabelValueLength(userID)
	lastLabelName := ""
	for _, l := range ls {
		if !skipLabelNameValidation && !model.LabelName(l.Name).IsValid() {
			DiscardedSamples.WithLabelValues(invalidLabel, userID).Inc()
			return newInvalidLabelError(ls, l.Name)
		} else if len(l.Name) > maxLabelNameLength {
			DiscardedSamples.WithLabelValues(labelNameTooLong, userID).Inc()
			return newLabelNameTooLongError(ls, l.Name)
		} else if len(l.Value) > maxLabelValueLength {
			DiscardedSamples.WithLabelValues(labelValueTooLong, userID).Inc()
			return newLabelValueTooLongError(ls, l.Value)
		} else if cmp := strings.Compare(lastLabelName, l.Name); cmp >= 0 {
			if cmp == 0 {
				DiscardedSamples.WithLabelValues(duplicateLabelNames, userID).Inc()
				return newDuplicatedLabelError(ls, l.Name)
			}

			DiscardedSamples.WithLabelValues(labelsNotSorted, userID).Inc()
			return newLabelsNotSortedError(ls, l.Name)
		}

		lastLabelName = l.Name
	}
	return nil
}

// MetadataValidationConfig helps with getting required config to validate metadata.
type MetadataValidationConfig interface {
	EnforceMetadataMetricName(userID string) bool
	MaxMetadataLength(userID string) int
}

// ValidateMetadata returns an err if a metric metadata is invalid.
func ValidateMetadata(cfg MetadataValidationConfig, userID string, metadata *cortexpb.MetricMetadata) error {
	if cfg.EnforceMetadataMetricName(userID) && metadata.GetMetricFamilyName() == "" {
		DiscardedMetadata.WithLabelValues(missingMetricName, userID).Inc()
		return httpgrpc.Errorf(http.StatusBadRequest, errMetadataMissingMetricName)
	}

	maxMetadataValueLength := cfg.MaxMetadataLength(userID)
	var reason string
	var cause string
	var metadataType string
	if len(metadata.GetMetricFamilyName()) > maxMetadataValueLength {
		metadataType = typeMetricName
		reason = metricNameTooLong
		cause = metadata.GetMetricFamilyName()
	} else if len(metadata.Help) > maxMetadataValueLength {
		metadataType = typeHelp
		reason = helpTooLong
		cause = metadata.Help
	} else if len(metadata.Unit) > maxMetadataValueLength {
		metadataType = typeUnit
		reason = unitTooLong
		cause = metadata.Unit
	}

	if reason != "" {
		DiscardedMetadata.WithLabelValues(reason, userID).Inc()
		return httpgrpc.Errorf(http.StatusBadRequest, errMetadataTooLong, metadataType, cause, metadata.GetMetricFamilyName())
	}

	return nil
}

func DeletePerUserValidationMetrics(userID string, log log.Logger) {
	filter := map[string]string{"user": userID}

	if err := util.DeleteMatchingLabels(DiscardedSamples, filter); err != nil {
		level.Warn(log).Log("msg", "failed to remove cortex_discarded_samples_total metric for user", "user", userID, "err", err)
	}
	if err := util.DeleteMatchingLabels(DiscardedExemplars, filter); err != nil {
		level.Warn(log).Log("msg", "failed to remove cortex_discarded_exemplars_total metric for user", "user", userID, "err", err)
	}
	if err := util.DeleteMatchingLabels(DiscardedMetadata, filter); err != nil {
		level.Warn(log).Log("msg", "failed to remove cortex_discarded_metadata_total metric for user", "user", userID, "err", err)
	}
}
