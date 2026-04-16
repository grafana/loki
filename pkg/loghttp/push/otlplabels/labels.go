package otlplabels

import (
	"encoding/hex"
	"fmt"

	"github.com/prometheus/common/model"
	"github.com/prometheus/otlptranslator"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"

	"github.com/grafana/loki/pkg/push"
)

const (
	AttrServiceName  = "service.name"
	LabelServiceName = "service_name"
	ServiceUnknown   = "unknown_service"

	OTLPSeverityNumber = "severity_number"
	OTLPSeverityText   = "severity_text"
	OTLPEventName      = "event_name"
)

// AttributeToLabels converts a single OTLP attribute key/value into one or
// more LabelAdapters. Map-valued attributes are expanded recursively with the
// parent key as prefix. The key is normalized via otlptranslator.LabelNamer.
func AttributeToLabels(k string, v pcommon.Value, prefix string) (push.LabelsAdapter, error) {
	var labelsAdapter push.LabelsAdapter

	keyWithPrefix := k
	if prefix != "" {
		keyWithPrefix = prefix + "_" + k
	}

	labelNamer := otlptranslator.LabelNamer{}
	keyWithPrefix, err := labelNamer.Build(keyWithPrefix)
	if err != nil {
		return nil, fmt.Errorf("symbolizer lookup: %w", err)
	}

	typ := v.Type()
	if typ == pcommon.ValueTypeMap {
		mv := v.Map()
		labelsAdapter = make(push.LabelsAdapter, 0, mv.Len())
		var rangeErr error
		mv.Range(func(k string, v pcommon.Value) bool {
			lbls, err := AttributeToLabels(k, v, keyWithPrefix)
			if err != nil {
				rangeErr = fmt.Errorf("symbolizer lookup: %w", err)
				return false
			}
			labelsAdapter = append(labelsAdapter, lbls...)
			return true
		})
		if rangeErr != nil {
			return nil, rangeErr
		}
	} else {
		labelsAdapter = push.LabelsAdapter{
			push.LabelAdapter{Name: keyWithPrefix, Value: v.AsString()},
		}
	}

	return labelsAdapter, nil
}

func attributesToLabels(attrs pcommon.Map, prefix string) (push.LabelsAdapter, error) {
	labelsAdapter := make(push.LabelsAdapter, 0, attrs.Len())
	if attrs.Len() == 0 {
		return labelsAdapter, nil
	}

	var rangeErr error
	attrs.Range(func(k string, v pcommon.Value) bool {
		lbls, err := AttributeToLabels(k, v, prefix)
		if err != nil {
			rangeErr = err
			return false
		}
		labelsAdapter = append(labelsAdapter, lbls...)
		return true
	})

	return labelsAdapter, rangeErr
}

// ResourceAttrsResult holds the output of ResourceAttrsToStreamLabels.
type ResourceAttrsResult struct {
	// StreamLabels are attributes configured as IndexLabel.
	StreamLabels model.LabelSet
	// StructuredMetadata are attributes configured as StructuredMetadata.
	StructuredMetadata push.LabelsAdapter
}

// ResourceAttrsToStreamLabels converts OTLP resource attributes into stream
// labels and structured metadata according to the OTLPConfig.
//
// When discoverServiceName is non-empty and no explicit service.name attribute
// is present, the first indexed label whose name appears in discoverServiceName
// is promoted to service_name. If no candidate is found, service_name defaults
// to "unknown_service".
func ResourceAttrsToStreamLabels(attrs pcommon.Map, otlpConfig OTLPConfig, discoverServiceName []string) (*ResourceAttrsResult, error) {
	result := &ResourceAttrsResult{
		StreamLabels:       make(model.LabelSet, 30),
		StructuredMetadata: make(push.LabelsAdapter, 0, attrs.Len()),
	}

	shouldDiscoverServiceName := len(discoverServiceName) > 0
	hasServiceName := false
	if v, ok := attrs.Get(AttrServiceName); ok && v.AsString() != "" {
		hasServiceName = true
	}

	var rangeErr error
	attrs.Range(func(k string, v pcommon.Value) bool {
		action := otlpConfig.ActionForResourceAttribute(k)
		if action == Drop {
			return true
		}

		attributeAsLabels, err := AttributeToLabels(k, v, "")
		if err != nil {
			rangeErr = err
			return false
		}

		if action == IndexLabel {
			for _, lbl := range attributeAsLabels {
				result.StreamLabels[model.LabelName(lbl.Name)] = model.LabelValue(lbl.Value)

				if !hasServiceName && shouldDiscoverServiceName {
					for _, labelName := range discoverServiceName {
						if lbl.Name == labelName {
							result.StreamLabels[model.LabelName(LabelServiceName)] = model.LabelValue(lbl.Value)
							hasServiceName = true
							break
						}
					}
				}
			}
		} else if action == StructuredMetadata {
			result.StructuredMetadata = append(result.StructuredMetadata, attributeAsLabels...)
		}

		return true
	})
	if rangeErr != nil {
		return nil, rangeErr
	}

	if !hasServiceName && shouldDiscoverServiceName {
		result.StreamLabels[model.LabelName(LabelServiceName)] = model.LabelValue(ServiceUnknown)
	}

	return result, nil
}

// ScopeAttrsResult holds the output of ScopeAttrsToStructuredMetadata.
type ScopeAttrsResult struct {
	StructuredMetadata push.LabelsAdapter
}

// ScopeAttrsToStructuredMetadata converts OTLP scope attributes plus scope
// name, version, and dropped attributes count into structured metadata labels.
func ScopeAttrsToStructuredMetadata(sls plog.ScopeLogsSlice, idx int, otlpConfig OTLPConfig) (*ScopeAttrsResult, error) {
	scope := sls.At(idx).Scope()
	scopeAttrs := scope.Attributes()
	result := &ScopeAttrsResult{
		StructuredMetadata: make(push.LabelsAdapter, 0, scopeAttrs.Len()+3),
	}

	var rangeErr error
	scopeAttrs.Range(func(k string, v pcommon.Value) bool {
		action := otlpConfig.ActionForScopeAttribute(k)
		if action == Drop {
			return true
		}

		attributeAsLabels, err := AttributeToLabels(k, v, "")
		if err != nil {
			rangeErr = err
			return false
		}
		if action == StructuredMetadata {
			result.StructuredMetadata = append(result.StructuredMetadata, attributeAsLabels...)
		}

		return true
	})
	if rangeErr != nil {
		return nil, rangeErr
	}

	if scopeName := scope.Name(); scopeName != "" {
		result.StructuredMetadata = append(result.StructuredMetadata, push.LabelAdapter{
			Name:  "scope_name",
			Value: scopeName,
		})
	}
	if scopeVersion := scope.Version(); scopeVersion != "" {
		result.StructuredMetadata = append(result.StructuredMetadata, push.LabelAdapter{
			Name:  "scope_version",
			Value: scopeVersion,
		})
	}
	if scopeDroppedAttributesCount := scope.DroppedAttributesCount(); scopeDroppedAttributesCount != 0 {
		result.StructuredMetadata = append(result.StructuredMetadata, push.LabelAdapter{
			Name:  "scope_dropped_attributes_count",
			Value: fmt.Sprintf("%d", scopeDroppedAttributesCount),
		})
	}

	return result, nil
}

// LogAttrsResult holds the output of LogAttrsToLabels.
type LogAttrsResult struct {
	// IndexLabels are log attributes configured as IndexLabel.
	IndexLabels model.LabelSet
	// StructuredMetadata are log attributes plus log record fields
	// (severity, trace_id, span_id, etc.) stored as structured metadata.
	StructuredMetadata push.LabelsAdapter
}

// LogAttrsToLabels converts OTLP log record attributes and built-in fields
// (severity, trace/span IDs, event name, flags, dropped attributes count,
// observed timestamp) into index labels and structured metadata.
func LogAttrsToLabels(log plog.LogRecord, otlpConfig OTLPConfig) (*LogAttrsResult, error) {
	logAttrs := log.Attributes()
	result := &LogAttrsResult{
		IndexLabels:        make(model.LabelSet),
		StructuredMetadata: make(push.LabelsAdapter, 0, logAttrs.Len()+8),
	}

	var rangeErr error
	logAttrs.Range(func(k string, v pcommon.Value) bool {
		action := otlpConfig.ActionForLogAttribute(k)
		if action == Drop {
			return true
		}

		// If the dedicated OTLP EventName field is set, skip any log attribute
		// also named event_name to avoid duplicate entries. The first-class field
		// takes precedence over the attribute.
		if k == OTLPEventName && log.EventName() != "" {
			return true
		}

		attributeAsLabels, err := AttributeToLabels(k, v, "")
		if err != nil {
			rangeErr = err
			return false
		}
		if action == StructuredMetadata {
			result.StructuredMetadata = append(result.StructuredMetadata, attributeAsLabels...)
		}

		if action == IndexLabel {
			for _, lbl := range attributeAsLabels {
				result.IndexLabels[model.LabelName(lbl.Name)] = model.LabelValue(lbl.Value)
			}
		}

		return true
	})
	if rangeErr != nil {
		return nil, rangeErr
	}

	// if log.Timestamp() is 0, we would have already stored log.ObservedTimestamp as log timestamp so no need to store again in structured metadata
	if log.Timestamp() != 0 && log.ObservedTimestamp() != 0 {
		result.StructuredMetadata = append(result.StructuredMetadata, push.LabelAdapter{
			Name:  "observed_timestamp",
			Value: fmt.Sprintf("%d", log.ObservedTimestamp().AsTime().UnixNano()),
		})
	}

	if severityNum := log.SeverityNumber(); severityNum != plog.SeverityNumberUnspecified {
		result.StructuredMetadata = append(result.StructuredMetadata, push.LabelAdapter{
			Name:  OTLPSeverityNumber,
			Value: fmt.Sprintf("%d", severityNum),
		})
	}
	if severityText := log.SeverityText(); severityText != "" {
		// Add severity_text as an index label if configured
		if otlpConfig.SeverityTextAsLabel {
			result.IndexLabels[model.LabelName(OTLPSeverityText)] = model.LabelValue(severityText)
		}

		// Always add severity_text as structured metadata
		result.StructuredMetadata = append(result.StructuredMetadata, push.LabelAdapter{
			Name:  OTLPSeverityText,
			Value: severityText,
		})
	}

	if droppedAttributesCount := log.DroppedAttributesCount(); droppedAttributesCount != 0 {
		result.StructuredMetadata = append(result.StructuredMetadata, push.LabelAdapter{
			Name:  "dropped_attributes_count",
			Value: fmt.Sprintf("%d", droppedAttributesCount),
		})
	}
	if logRecordFlags := log.Flags(); logRecordFlags != 0 {
		result.StructuredMetadata = append(result.StructuredMetadata, push.LabelAdapter{
			Name:  "flags",
			Value: fmt.Sprintf("%d", logRecordFlags),
		})
	}

	if traceID := log.TraceID(); !traceID.IsEmpty() {
		result.StructuredMetadata = append(result.StructuredMetadata, push.LabelAdapter{
			Name:  "trace_id",
			Value: hex.EncodeToString(traceID[:]),
		})
	}
	if spanID := log.SpanID(); !spanID.IsEmpty() {
		result.StructuredMetadata = append(result.StructuredMetadata, push.LabelAdapter{
			Name:  "span_id",
			Value: hex.EncodeToString(spanID[:]),
		})
	}
	if eventName := log.EventName(); eventName != "" {
		result.StructuredMetadata = append(result.StructuredMetadata, push.LabelAdapter{
			Name:  OTLPEventName,
			Value: eventName,
		})
	}

	return result, nil
}
