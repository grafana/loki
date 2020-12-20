package validation

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/weaveworks/common/httpgrpc"

	"github.com/cortexproject/cortex/pkg/ingester/client"
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

	errMissingMetricName  = "sample missing metric name"
	errInvalidMetricName  = "sample invalid metric name: %.200q"
	errInvalidLabel       = "sample invalid label: %.200q metric %.200q"
	errLabelNameTooLong   = "label name too long: %.200q metric %.200q"
	errLabelValueTooLong  = "label value too long: %.200q metric %.200q"
	errTooManyLabels      = "sample for '%s' has %d label names; limit %d"
	errTooOld             = "sample for '%s' has timestamp too old: %d"
	errTooNew             = "sample for '%s' has timestamp too new: %d"
	errDuplicateLabelName = "duplicate label name: %.200q metric %.200q"
	errLabelsNotSorted    = "labels not sorted: %.200q metric %.200q"

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

	// RateLimited is one of the values for the reason to discard samples.
	// Declared here to avoid duplication in ingester and distributor.
	RateLimited = "rate_limited"
)

// DiscardedSamples is a metric of the number of discarded samples, by reason.
var DiscardedSamples = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "cortex_discarded_samples_total",
		Help: "The total number of samples that were discarded.",
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
	prometheus.MustRegister(DiscardedMetadata)
}

// SampleValidationConfig helps with getting required config to validate sample.
type SampleValidationConfig interface {
	RejectOldSamples(userID string) bool
	RejectOldSamplesMaxAge(userID string) time.Duration
	CreationGracePeriod(userID string) time.Duration
}

// ValidateSample returns an err if the sample is invalid.
func ValidateSample(cfg SampleValidationConfig, userID string, metricName string, s client.Sample) error {
	if cfg.RejectOldSamples(userID) && model.Time(s.TimestampMs) < model.Now().Add(-cfg.RejectOldSamplesMaxAge(userID)) {
		DiscardedSamples.WithLabelValues(greaterThanMaxSampleAge, userID).Inc()
		return httpgrpc.Errorf(http.StatusBadRequest, errTooOld, metricName, model.Time(s.TimestampMs))
	}

	if model.Time(s.TimestampMs) > model.Now().Add(cfg.CreationGracePeriod(userID)) {
		DiscardedSamples.WithLabelValues(tooFarInFuture, userID).Inc()
		return httpgrpc.Errorf(http.StatusBadRequest, errTooNew, metricName, model.Time(s.TimestampMs))
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
func ValidateLabels(cfg LabelValidationConfig, userID string, ls []client.LabelAdapter, skipLabelNameValidation bool) error {
	if cfg.EnforceMetricName(userID) {
		metricName, err := extract.MetricNameFromLabelAdapters(ls)
		if err != nil {
			DiscardedSamples.WithLabelValues(missingMetricName, userID).Inc()
			return httpgrpc.Errorf(http.StatusBadRequest, errMissingMetricName)
		}

		if !model.IsValidMetricName(model.LabelValue(metricName)) {
			DiscardedSamples.WithLabelValues(invalidMetricName, userID).Inc()
			return httpgrpc.Errorf(http.StatusBadRequest, errInvalidMetricName, metricName)
		}
	}

	numLabelNames := len(ls)
	if numLabelNames > cfg.MaxLabelNamesPerSeries(userID) {
		DiscardedSamples.WithLabelValues(maxLabelNamesPerSeries, userID).Inc()
		return httpgrpc.Errorf(http.StatusBadRequest, errTooManyLabels, client.FromLabelAdaptersToMetric(ls).String(), numLabelNames, cfg.MaxLabelNamesPerSeries(userID))
	}

	maxLabelNameLength := cfg.MaxLabelNameLength(userID)
	maxLabelValueLength := cfg.MaxLabelValueLength(userID)
	lastLabelName := ""
	for _, l := range ls {
		var errTemplate string
		var reason string
		var cause interface{}
		if !skipLabelNameValidation && !model.LabelName(l.Name).IsValid() {
			reason = invalidLabel
			errTemplate = errInvalidLabel
			cause = l.Name
		} else if len(l.Name) > maxLabelNameLength {
			reason = labelNameTooLong
			errTemplate = errLabelNameTooLong
			cause = l.Name
		} else if len(l.Value) > maxLabelValueLength {
			reason = labelValueTooLong
			errTemplate = errLabelValueTooLong
			cause = l.Value
		} else if cmp := strings.Compare(lastLabelName, l.Name); cmp >= 0 {
			if cmp == 0 {
				reason = duplicateLabelNames
				errTemplate = errDuplicateLabelName
				cause = l.Name
			} else {
				reason = labelsNotSorted
				errTemplate = errLabelsNotSorted
				cause = l.Name
			}
		}
		if errTemplate != "" {
			DiscardedSamples.WithLabelValues(reason, userID).Inc()
			return httpgrpc.Errorf(http.StatusBadRequest, errTemplate, cause, formatLabelSet(ls))
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
func ValidateMetadata(cfg MetadataValidationConfig, userID string, metadata *client.MetricMetadata) error {
	if cfg.EnforceMetadataMetricName(userID) && metadata.MetricName == "" {
		DiscardedMetadata.WithLabelValues(missingMetricName, userID).Inc()
		return httpgrpc.Errorf(http.StatusBadRequest, errMetadataMissingMetricName)
	}

	maxMetadataValueLength := cfg.MaxMetadataLength(userID)
	var reason string
	var cause string
	var metadataType string
	if len(metadata.MetricName) > maxMetadataValueLength {
		metadataType = typeMetricName
		reason = metricNameTooLong
		cause = metadata.MetricName
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
		return httpgrpc.Errorf(http.StatusBadRequest, errMetadataTooLong, metadataType, cause, metadata.MetricName)
	}

	return nil
}

// this function formats label adapters as a metric name with labels, while preserving
// label order, and keeping duplicates. If there are multiple "__name__" labels, only
// first one is used as metric name, other ones will be included as regular labels.
func formatLabelSet(ls []client.LabelAdapter) string {
	metricName, hasMetricName := "", false

	labelStrings := make([]string, 0, len(ls))
	for _, l := range ls {
		if l.Name == model.MetricNameLabel && !hasMetricName && l.Value != "" {
			metricName = l.Value
			hasMetricName = true
		} else {
			labelStrings = append(labelStrings, fmt.Sprintf("%s=%q", l.Name, l.Value))
		}
	}

	if len(labelStrings) == 0 {
		if hasMetricName {
			return metricName
		}
		return "{}"
	}

	return fmt.Sprintf("%s{%s}", metricName, strings.Join(labelStrings, ", "))
}
