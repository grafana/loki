package v1beta1

import "errors"

// PrometheusDuration defines the type for Prometheus durations.
//
// +kubebuilder:validation:Pattern:="((([0-9]+)y)?(([0-9]+)w)?(([0-9]+)d)?(([0-9]+)h)?(([0-9]+)m)?(([0-9]+)s)?(([0-9]+)ms)?|0)"
type PrometheusDuration string

var (
	// ErrGroupNamesNotUnique is the error type when loki groups have not unique names.
	ErrGroupNamesNotUnique = errors.New("Group names are not unique")
	// ErrInvalidRecordMetricName when any loki recording rule has a invalid PromQL metric name.
	ErrInvalidRecordMetricName = errors.New("Failed to parse record metric name")
	// ErrParseAlertForPeriod when any loki alerting rule for period is not a valid PromQL duration.
	ErrParseAlertForPeriod = errors.New("Failed to parse alert firing period")
	// ErrParseEvaluationInterval when any loki group evaluation internal is not a valid PromQL duration.
	ErrParseEvaluationInterval = errors.New("Failed to parse evaluation")
	// ErrParseLogQLExpression when any loki rule expression is not a valid LogQL expression.
	ErrParseLogQLExpression = errors.New("Failed to parse LogQL expression")
)
