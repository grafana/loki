package v1

import (
	"errors"
	"time"
)

// PrometheusDuration defines the type for Prometheus durations.
//
// +kubebuilder:validation:Pattern:="((([0-9]+)y)?(([0-9]+)w)?(([0-9]+)d)?(([0-9]+)h)?(([0-9]+)m)?(([0-9]+)s)?(([0-9]+)ms)?|0)"
type PrometheusDuration string

// StorageSchemaEffectiveDate defines the type for the Storage Schema Effect Date
//
// +kubebuilder:validation:Pattern:="^([0-9]{4,})([-]([0-9]{2})){2}$"
type StorageSchemaEffectiveDate string

// UTCTime returns the date as a time object in the UTC time zone
func (d StorageSchemaEffectiveDate) UTCTime() (time.Time, error) {
	return time.Parse(StorageSchemaEffectiveDateFormat, string(d))
}

const (
	// StorageSchemaEffectiveDateFormat is the datetime string need to format the time.
	StorageSchemaEffectiveDateFormat = "2006-01-02"
	// StorageSchemaUpdateBuffer is the amount of time used as a buffer to prevent
	// storage schemas from being added too close to midnight in UTC.
	StorageSchemaUpdateBuffer = time.Hour * 2
)

const (
	// The AnnotationDisableTenantValidation annotation can contain a boolean value that, if true, disables the tenant-ID validation.
	AnnotationDisableTenantValidation = "loki.grafana.com/disable-tenant-validation"

	// The AnnotationAvailabilityZone annotation contains the availability zone used in the Loki configuration of that pod.
	// It is automatically added to managed Pods by the operator, if needed.
	AnnotationAvailabilityZone = "loki.grafana.com/availability-zone"

	// The AnnotationAvailabilityZoneLabels annotation contains a list of node-labels that are used to construct the availability zone
	// of the annotated Pod. It is used by the zone-awareness controller and automatically added to managed Pods by the operator,
	// if needed.
	AnnotationAvailabilityZoneLabels string = "loki.grafana.com/availability-zone-labels"

	// LabelZoneAwarePod is a pod-label that is added to Pods that should be reconciled by the zone-awareness controller.
	// It is automatically added to managed Pods by the operator, if needed.
	LabelZoneAwarePod string = "loki.grafana.com/zone-aware"
)

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
	// ErrParseLogQLNotSample when the Loki rule expression does not evaluate to a sample expression.
	ErrParseLogQLNotSample = errors.New("LogQL expression is not a sample query")
	// ErrParseLogQLSelector when the Loki rule expression does not have a valid selector.
	ErrParseLogQLSelector = errors.New("Failed to get selector from LogQL expression")
	// ErrEffectiveDatesNotUnique when effective dates are not unique.
	ErrEffectiveDatesNotUnique = errors.New("Effective dates are not unique")
	// ErrParseEffectiveDates when effective dates cannot be parsed.
	ErrParseEffectiveDates = errors.New("Failed to parse effective date")
	// ErrMissingValidStartDate when a schema list is created without a valid effective date
	ErrMissingValidStartDate = errors.New("Schema does not contain a valid starting effective date")
	// ErrSchemaRetroactivelyAdded when a schema has been retroactively added
	ErrSchemaRetroactivelyAdded = errors.New("Cannot retroactively add schema")
	// ErrSchemaRetroactivelyRemoved when a schema or schemas has been retroactively removed
	ErrSchemaRetroactivelyRemoved = errors.New("Cannot retroactively remove schema(s)")
	// ErrSchemaRetroactivelyChanged when a schema has been retroactively changed
	ErrSchemaRetroactivelyChanged = errors.New("Cannot retroactively change schema")
	// ErrHeaderAuthCredentialsConflict when both Credentials and CredentialsFile are used in a header authentication client.
	ErrHeaderAuthCredentialsConflict = errors.New("credentials and credentialsFile cannot be used at the same time")
	// ErrReplicationZonesNodes when there is an error retrieving nodes with replication zones labels.
	ErrReplicationZonesNodes = errors.New("Failed to retrieve nodes for zone replication")
	// ErrReplicationFactorToZonesRatio when the replication factor defined is greater than the number of available zones.
	ErrReplicationFactorToZonesRatio = errors.New("replication factor is greater than the number of available zones")
	// ErrReplicationSpecConflict when both the ReplicationSpec and depricated ReplicationFactor are used.
	ErrReplicationSpecConflict = errors.New("replicationSpec and replicationFactor (deprecated) cannot be used at the same time")
	// ErrIPv6InstanceAddrTypeNotAllowed when the default InstanceAddrType is used with enableIPv6.
	ErrIPv6InstanceAddrTypeNotAllowed = errors.New(`instanceAddrType "default" cannot be used with enableIPv6 at the same time`)

	// ErrOTLPResourceAttributesEmptyNotAllowed when the OTLP ResourceAttributes are empty even though ignoreDefaults is enabled.
	ErrOTLPResourceAttributesEmptyNotAllowed = errors.New(`resourceAttributes cannot be empty when ignoreDefaults is true`)
	// ErrOTLPResourceAttributesIndexLabelActionMissing when OTLP ResourceAttributes does not contain at least one index label when ignoreDefaults is enabled.
	ErrOTLPResourceAttributesIndexLabelActionMissing = errors.New(`resourceAttributes does not contain at least one attributed mapped to "index_label"`)
	// ErrOTLPAttributesSpecInvalid when the OTLPAttributesSpec attibutes and regex fields are both empty.
	ErrOTLPAttributesSpecInvalid = errors.New(`attributes and regex cannot be empty at the same time`)

	// ErrRuleMustMatchNamespace indicates that an expression used in an alerting or recording rule is missing
	// matchers for a namespace.
	ErrRuleMustMatchNamespace = errors.New("rule needs to have a matcher for the namespace")
	// ErrSeverityLabelMissing indicates that an alerting rule is missing the severity label
	ErrSeverityLabelMissing = errors.New("rule requires label: severity")
	// ErrSeverityLabelInvalid indicates that an alerting rule has an invalid value for the summary label
	ErrSeverityLabelInvalid = errors.New("rule severity label value invalid, allowed values: critical, warning, info")
	// ErrSummaryAnnotationMissing indicates that an alerting rule is missing the summary annotation
	ErrSummaryAnnotationMissing = errors.New("rule requires annotation: summary")
	// ErrDescriptionAnnotationMissing indicates that an alerting rule is missing the description annotation
	ErrDescriptionAnnotationMissing = errors.New("rule requires annotation: description")
)
