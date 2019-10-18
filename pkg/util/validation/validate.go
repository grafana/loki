package validation

import (
	"net/http"
	"time"

	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/util/extract"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/util"
)

var (
	errMissingMetricName = util.NewCodedError(http.StatusBadRequest, "sample missing metric name")
)

const (
	discardReasonLabel = "reason"

	errInvalidMetricName = "sample invalid metric name: %.200q"
	errInvalidLabel      = "sample invalid label: %.200q metric %.200q"
	errLabelNameTooLong  = "label name too long: %.200q metric %.200q"
	errLabelValueTooLong = "label value too long: %.200q metric %.200q"
	errTooManyLabels     = "stream '%s' has %d label names; limit %d"
	errTooOld            = "log entry for stream '%s' has timestamp too old: %d"
	errTooNew            = "log entry for stream '%s' has timestamp too new: %d"

	greaterThanMaxSampleAge = "greater_than_max_sample_age"
	maxLabelNamesPerSeries  = "max_label_names_per_series"
	tooFarInFuture          = "too_far_in_future"
	invalidLabel            = "label_invalid"
	labelNameTooLong        = "label_name_too_long"
	labelValueTooLong       = "label_value_too_long"

	// RateLimited is one of the values for the reason to discard samples.
	// Declared here to avoid duplication in ingester and distributor.
	RateLimited = "rate_limited"
)

// DiscardedSamples is a metric of the number of discarded samples, by reason.
var DiscardedSamples = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "loki_discarded_samples_total",
		Help: "The total number of samples that were discarded.",
	},
	[]string{discardReasonLabel, "user"},
)

func init() {
	prometheus.MustRegister(DiscardedSamples)
}

// SampleValidationConfig helps with getting required config to validate sample.
type SampleValidationConfig interface {
	RejectOldSamples(userID string) bool
	RejectOldSamplesMaxAge(userID string) time.Duration
	CreationGracePeriod(userID string) time.Duration
}

// ValidateSample returns an err if the sample is invalid.
func ValidateSample(cfg SampleValidationConfig, userID string, streamLabels string, s client.Sample) error {
	if cfg.RejectOldSamples(userID) && model.Time(s.TimestampMs) < model.Now().Add(-cfg.RejectOldSamplesMaxAge(userID)) {
		DiscardedSamples.WithLabelValues(greaterThanMaxSampleAge, userID).Inc()
		return util.NewCodedErrorf(http.StatusBadRequest, errTooOld, streamLabels, model.Time(s.TimestampMs))
	}

	if model.Time(s.TimestampMs) > model.Now().Add(cfg.CreationGracePeriod(userID)) {
		DiscardedSamples.WithLabelValues(tooFarInFuture, userID).Inc()
		return util.NewCodedErrorf(http.StatusBadRequest, errTooNew, streamLabels, model.Time(s.TimestampMs))
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
func ValidateLabels(cfg LabelValidationConfig, userID string, streamLabels string, ls []client.LabelAdapter) error {
	metricName, err := extract.MetricNameFromLabelAdapters(ls)
	if cfg.EnforceMetricName(userID) {
		if err != nil {
			return errMissingMetricName
		}
		//TODO should this be simplifed to validate that the metric name always == logs??
		if !model.IsValidMetricName(model.LabelValue(metricName)) {
			return util.NewCodedErrorf(http.StatusBadRequest, errInvalidMetricName, metricName)
		}
	}

	numLabelNames := len(ls)
	if numLabelNames > cfg.MaxLabelNamesPerSeries(userID) {
		DiscardedSamples.WithLabelValues(maxLabelNamesPerSeries, userID).Inc()
		return util.NewCodedErrorf(http.StatusBadRequest, errTooManyLabels, streamLabels, numLabelNames, cfg.MaxLabelNamesPerSeries(userID))
	}

	maxLabelNameLength := cfg.MaxLabelNameLength(userID)
	maxLabelValueLength := cfg.MaxLabelValueLength(userID)
	for _, l := range ls {
		var errTemplate string
		var reason string
		var cause interface{}
		if !model.LabelName(l.Name).IsValid() {
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
		}
		if errTemplate != "" {
			DiscardedSamples.WithLabelValues(reason, userID).Inc()
			return util.NewCodedErrorf(http.StatusBadRequest, errTemplate, cause, client.FromLabelAdaptersToMetric(ls).String())
		}
	}
	return nil
}
