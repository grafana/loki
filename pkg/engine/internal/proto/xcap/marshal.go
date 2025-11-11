package xcap

import (
	"fmt"

	"go.opentelemetry.io/otel/attribute"

	"github.com/grafana/loki/v3/pkg/engine/internal/executor/xcap"
)

// MarshalCapture converts a Capture to its protobuf representation.
func MarshalCapture(c *xcap.Capture) (*Capture, error) {
	if c == nil {
		return nil, nil
	}

	regions := c.Regions()

	// Build statistics list (deduplicated by name) first, as observations reference them
	allStats := c.GetAllStatistics()
	statNameToIndex := make(map[string]uint32)
	protoStats := make([]*Statistic, 0, len(allStats))

	for _, stat := range allStats {
		statName := stat.Name()
		if _, exists := statNameToIndex[statName]; !exists {
			statNameToIndex[statName] = uint32(len(protoStats))
			protoStats = append(protoStats, &Statistic{
				Name:            statName,
				AggregationType: marshalAggregationType(stat.Aggregation()),
			})
		}
	}

	// Convert regions to proto regions
	protoRegions := make([]*Region, 0, len(regions))
	for i, region := range regions {
		protoRegion, err := marshalRegion(region, statNameToIndex)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal region %d: %w", i, err)
		}
		if protoRegion != nil {
			protoRegions = append(protoRegions, protoRegion)
		}
	}

	return &Capture{
		Regions:    protoRegions,
		Statistics: protoStats,
	}, nil
}

// marshalRegion converts a Region to its protobuf representation.
func marshalRegion(region *xcap.Region, statNameToIndex map[string]uint32) (*Region, error) {
	if region == nil {
		return nil, nil
	}

	// Get basic information from Region
	startTime := region.StartTime()
	endTime := region.EndTime()
	name := region.Name()
	attrs := region.Attributes()
	ended := region.IsEnded()

	// Marshal attributes
	protoAttrs := make([]*Attribute, 0, len(attrs))
	for _, attr := range attrs {
		protoAttr, err := marshalAttribute(attr)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal attribute: %w", err)
		}
		if protoAttr != nil {
			protoAttrs = append(protoAttrs, protoAttr)
		}
	}

	// Marshal observations
	obsDetails := region.GetObservationDetails()
	protoObservations := make([]*Observation, 0, len(obsDetails))
	for statName, details := range obsDetails {
		statIndex, exists := statNameToIndex[statName]
		if !exists {
			// This shouldn't happen if GetAllStatistics is correct, but handle it gracefully
			continue
		}

		protoValue, err := marshalObservationValue(details.Value)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal observation value for statistic %s: %w", statName, err)
		}

		protoObservations = append(protoObservations, &Observation{
			StatisticId: statIndex,
			Value:       protoValue,
			Count:       uint32(details.Count),
		})
	}

	// Build proto region
	protoRegion := &Region{
		Name:         name,
		Attributes:   protoAttrs,
		StartTime:    startTime,
		EndTime:      endTime,
		Ended:        ended,
		Observations: protoObservations,
	}

	return protoRegion, nil
}

// marshalAttribute converts an OpenTelemetry KeyValue to proto Attribute.
func marshalAttribute(kv attribute.KeyValue) (*Attribute, error) {
	if !kv.Valid() {
		return nil, nil
	}

	protoAttr := &Attribute{
		Key: string(kv.Key),
	}

	value := kv.Value
	protoValue, err := marshalAttributeValue(value)
	if err != nil {
		return nil, err
	}

	protoAttr.Value = protoValue
	return protoAttr, nil
}

// marshalAttributeValue converts an OpenTelemetry attribute Value to proto AttributeValue.
func marshalAttributeValue(value attribute.Value) (*AttributeValue, error) {
	protoValue := &AttributeValue{}

	switch value.Type() {
	case attribute.BOOL:
		protoValue.Kind = &AttributeValue_BoolValue{BoolValue: value.AsBool()}
	case attribute.INT64:
		protoValue.Kind = &AttributeValue_IntValue{IntValue: value.AsInt64()}
	case attribute.FLOAT64:
		protoValue.Kind = &AttributeValue_FloatValue{FloatValue: value.AsFloat64()}
	case attribute.STRING:
		protoValue.Kind = &AttributeValue_StringValue{StringValue: value.AsString()}
	default:
		// Fallback to string representation
		protoValue.Kind = &AttributeValue_StringValue{StringValue: value.AsString()}
	}

	return protoValue, nil
}

// marshalObservationValue converts an observation value to proto ObservationValue.
func marshalObservationValue(value interface{}) (*ObservationValue, error) {
	switch v := value.(type) {
	case int64:
		return &ObservationValue{
			Kind: &ObservationValue_IntValue{IntValue: v},
		}, nil
	case float64:
		return &ObservationValue{
			Kind: &ObservationValue_FloatValue{FloatValue: v},
		}, nil
	case bool:
		return &ObservationValue{
			Kind: &ObservationValue_BoolValue{BoolValue: v},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported observation value type: %T", value)
	}
}

// marshalAggregationType converts an AggregationType to proto AggregationType.
func marshalAggregationType(agg xcap.AggregationType) AggregationType {
	switch agg {
	case xcap.AggregationTypeSum:
		return AGGREGATION_TYPE_SUM
	case xcap.AggregationTypeMin:
		return AGGREGATION_TYPE_MIN
	case xcap.AggregationTypeMax:
		return AGGREGATION_TYPE_MAX
	case xcap.AggregationTypeLast:
		return AGGREGATION_TYPE_LAST
	case xcap.AggregationTypeFirst:
		return AGGREGATION_TYPE_FIRST
	default:
		return AGGREGATION_TYPE_INVALID
	}
}
