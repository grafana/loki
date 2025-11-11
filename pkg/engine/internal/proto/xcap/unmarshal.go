package xcap

import (
	"fmt"
	"reflect"

	"go.opentelemetry.io/otel/attribute"

	"github.com/grafana/loki/v3/pkg/engine/internal/executor/xcap"
)

// UnmarshalCapture converts a protobuf Capture to its Go representation.
func UnmarshalCapture(proto *Capture) (*xcap.Capture, error) {
	if proto == nil {
		return nil, nil
	}

	// Build statistics map from proto statistics
	statIndexToStat := make(map[uint32]xcap.Statistic, len(proto.Statistics))
	for i, protoStat := range proto.Statistics {
		stat, err := unmarshalStatistic(protoStat)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal statistic %d: %w", i, err)
		}
		statIndexToStat[uint32(i)] = stat
	}

	// Unmarshal regions
	regions := make([]*xcap.Region, 0, len(proto.Regions))
	for i, protoRegion := range proto.Regions {
		region, err := unmarshalRegion(protoRegion, statIndexToStat)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal region %d: %w", i, err)
		}
		if region != nil {
			regions = append(regions, region)
		}
	}

	// Create capture using reflection to set private fields
	capture := &xcap.Capture{}
	captureValue := reflect.ValueOf(capture).Elem()

	// Set regions field
	regionsField := captureValue.FieldByName("regions")
	if regionsField.IsValid() && regionsField.CanSet() {
		regionsField.Set(reflect.ValueOf(regions))
	}

	// Set ended field
	endedField := captureValue.FieldByName("ended")
	if endedField.IsValid() && endedField.CanSet() {
		endedField.SetBool(false) // Capture is not ended when unmarshaling
	}

	// Note: We don't set attributes and startTime as they're not in the proto
	// You may want to add these fields to the proto or use default values

	return capture, nil
}

// unmarshalRegion converts a protobuf Region to its Go representation.
func unmarshalRegion(proto *Region, statIndexToStat map[uint32]xcap.Statistic) (*xcap.Region, error) {
	if proto == nil {
		return nil, nil
	}

	// Unmarshal attributes
	attrs := make([]attribute.KeyValue, 0, len(proto.Attributes))
	for _, protoAttr := range proto.Attributes {
		attr, err := unmarshalAttribute(protoAttr)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal attribute: %w", err)
		}
		if attr.Valid() {
			attrs = append(attrs, attr)
		}
	}

	// Unmarshal observations
	observations := make(map[string]xcap.ObservationDetails, len(proto.Observations))
	for _, protoObs := range proto.Observations {
		stat, exists := statIndexToStat[protoObs.StatisticId]
		if !exists {
			return nil, fmt.Errorf("invalid statistic_id %d in observation", protoObs.StatisticId)
		}

		value, err := unmarshalObservationValue(protoObs.Value)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal observation value: %w", err)
		}

		statName := stat.Name()
		observations[statName] = xcap.ObservationDetails{
			Statistic: stat,
			Value:     value,
			Count:     int(protoObs.Count),
		}
	}

	// Create region using reflection to set private fields
	region := &xcap.Region{}
	regionValue := reflect.ValueOf(region).Elem()

	// Set name field
	nameField := regionValue.FieldByName("name")
	if nameField.IsValid() && nameField.CanSet() {
		nameField.SetString(proto.Name)
	}

	// Set attributes field
	attrsField := regionValue.FieldByName("attributes")
	if attrsField.IsValid() && attrsField.CanSet() {
		attrsField.Set(reflect.ValueOf(attrs))
	}

	// Set startTime field
	startTimeField := regionValue.FieldByName("startTime")
	if startTimeField.IsValid() && startTimeField.CanSet() {
		startTimeField.Set(reflect.ValueOf(proto.StartTime))
	}

	// Set endTime field
	endTimeField := regionValue.FieldByName("endTime")
	if endTimeField.IsValid() && endTimeField.CanSet() {
		endTimeField.Set(reflect.ValueOf(proto.EndTime))
	}

	// Set ended field
	endedField := regionValue.FieldByName("ended")
	if endedField.IsValid() && endedField.CanSet() {
		endedField.SetBool(proto.Ended)
	}

	// Set observations field - need to convert ObservationDetails to observationValue
	// Since observationValue is private, we'll use reflection to construct the map
	obsField := regionValue.FieldByName("observations")
	if obsField.IsValid() && obsField.CanSet() {
		// Get the type of observationValue from the field
		obsMapType := obsField.Type()
		obsMap := reflect.MakeMap(obsMapType)

		// Get the observationValue type (the value type of the map)
		obsValueType := obsMapType.Elem()

		for statName, obsDetails := range observations {
			// Create a new observationValue using reflection
			obsValue := reflect.New(obsValueType).Elem()

			// Set statistic field
			statField := obsValue.FieldByName("statistic")
			if statField.IsValid() && statField.CanSet() {
				statField.Set(reflect.ValueOf(obsDetails.Statistic))
			}

			// Set value field
			valueField := obsValue.FieldByName("value")
			if valueField.IsValid() && valueField.CanSet() {
				valueField.Set(reflect.ValueOf(obsDetails.Value))
			}

			// Set count field
			countField := obsValue.FieldByName("count")
			if countField.IsValid() && countField.CanSet() {
				countField.SetInt(int64(obsDetails.Count))
			}

			// Add to map
			obsMap.SetMapIndex(reflect.ValueOf(statName), obsValue)
		}

		obsField.Set(obsMap)
	}

	return region, nil
}

// unmarshalAttribute converts a protobuf Attribute to OpenTelemetry KeyValue.
func unmarshalAttribute(proto *Attribute) (attribute.KeyValue, error) {
	if proto == nil {
		return attribute.KeyValue{}, nil
	}

	value, err := unmarshalAttributeValue(proto.Value)
	if err != nil {
		return attribute.KeyValue{}, err
	}

	return attribute.KeyValue{
		Key:   attribute.Key(proto.Key),
		Value: value,
	}, nil
}

// unmarshalAttributeValue converts a protobuf AttributeValue to OpenTelemetry attribute Value.
func unmarshalAttributeValue(proto *AttributeValue) (attribute.Value, error) {
	if proto == nil || proto.Kind == nil {
		return attribute.Value{}, fmt.Errorf("invalid attribute value")
	}

	switch v := proto.Kind.(type) {
	case *AttributeValue_BoolValue:
		return attribute.BoolValue(v.BoolValue), nil
	case *AttributeValue_IntValue:
		return attribute.Int64Value(v.IntValue), nil
	case *AttributeValue_FloatValue:
		return attribute.Float64Value(v.FloatValue), nil
	case *AttributeValue_StringValue:
		return attribute.StringValue(v.StringValue), nil
	default:
		return attribute.Value{}, fmt.Errorf("unsupported attribute value type: %T", proto.Kind)
	}
}

// unmarshalObservationValue converts a protobuf ObservationValue to a Go value.
func unmarshalObservationValue(proto *ObservationValue) (interface{}, error) {
	if proto == nil || proto.Kind == nil {
		return nil, fmt.Errorf("invalid observation value")
	}

	switch v := proto.Kind.(type) {
	case *ObservationValue_IntValue:
		return v.IntValue, nil
	case *ObservationValue_FloatValue:
		return v.FloatValue, nil
	case *ObservationValue_BoolValue:
		return v.BoolValue, nil
	default:
		return nil, fmt.Errorf("unsupported observation value type: %T", proto.Kind)
	}
}

// unmarshalStatistic converts a protobuf Statistic to a Go Statistic.
func unmarshalStatistic(proto *Statistic) (xcap.Statistic, error) {
	if proto == nil {
		return nil, fmt.Errorf("invalid statistic")
	}

	aggType := unmarshalAggregationType(proto.AggregationType)

	// Create the appropriate statistic type based on the aggregation type
	// For now, we'll create a StatisticInt64 as a default
	// You may need to adjust this based on how statistics are actually used
	switch aggType {
	case xcap.AggregationTypeSum, xcap.AggregationTypeMin, xcap.AggregationTypeMax:
		return xcap.NewStatisticInt64(proto.Name, aggType), nil
	case xcap.AggregationTypeLast, xcap.AggregationTypeFirst:
		// These might be used with different types, but we'll default to int64
		return xcap.NewStatisticInt64(proto.Name, aggType), nil
	default:
		return nil, fmt.Errorf("unsupported aggregation type: %v", proto.AggregationType)
	}
}

// unmarshalAggregationType converts a proto AggregationType to Go AggregationType.
func unmarshalAggregationType(proto AggregationType) xcap.AggregationType {
	switch proto {
	case AGGREGATION_TYPE_SUM:
		return xcap.AggregationTypeSum
	case AGGREGATION_TYPE_MIN:
		return xcap.AggregationTypeMin
	case AGGREGATION_TYPE_MAX:
		return xcap.AggregationTypeMax
	case AGGREGATION_TYPE_LAST:
		return xcap.AggregationTypeLast
	case AGGREGATION_TYPE_FIRST:
		return xcap.AggregationTypeFirst
	default:
		return xcap.AggregationTypeSum // Default fallback
	}
}
