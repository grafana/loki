package xcap

import (
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/grafana/loki/v3/pkg/xcap/internal/proto"
)

// fromProtoCapture converts a protobuf Capture to its Go representation.
func fromProtoCapture(protoCapture *proto.Capture, capture *Capture) error {
	if protoCapture == nil {
		return nil
	}

	// Build statistics map from proto statistics
	statsIndex := make(map[uint32]Statistic, len(protoCapture.Statistics))
	for i, protoStat := range protoCapture.Statistics {
		stat, err := unmarshalStatistic(protoStat)
		if err != nil {
			return fmt.Errorf("failed to unmarshal statistic: %w", err)
		}
		statsIndex[uint32(i)] = stat
	}

	// Unmarshal regions
	for _, protoRegion := range protoCapture.Regions {
		region, err := fromProtoRegion(protoRegion, statsIndex)
		if err != nil {
			return fmt.Errorf("failed to unmarshal region: %w", err)
		}

		if region != nil {
			capture.AddRegion(region)
		}
	}

	return nil
}

// fromProtoRegion converts a protobuf Region to its Go representation.
func fromProtoRegion(protoRegion *proto.Region, statIndexToStat map[uint32]Statistic) (*Region, error) {
	// Unmarshal observations
	observations := make(map[StatisticKey]*AggregatedObservation, len(protoRegion.Observations))
	for _, protoObs := range protoRegion.Observations {
		stat, exists := statIndexToStat[protoObs.StatisticId]
		if !exists {
			return nil, fmt.Errorf("invalid statistic_id %d in observation", protoObs.StatisticId)
		}

		value, err := unmarshalObservationValue(protoObs.Value)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal observation value: %w", err)
		}

		key := stat.Key()
		observations[key] = &AggregatedObservation{
			Statistic: stat,
			Value:     value,
			Count:     int(protoObs.Count),
		}
	}

	// Unmarshal region ID from proto, or generate a new one if not set
	var regionID identifier
	copy(regionID[:], protoRegion.Id)

	// Unmarshal parent ID from proto
	var parentID identifier
	copy(parentID[:], protoRegion.ParentId)

	// Unmarshal attributes from proto
	attributes := make([]attribute.KeyValue, 0)
	if protoRegion.Attributes != nil {
		attributes = make([]attribute.KeyValue, 0, len(protoRegion.Attributes))
		for _, protoAttr := range protoRegion.Attributes {
			attr, err := unmarshalAttribute(protoAttr)
			if err != nil {
				return nil, fmt.Errorf("failed to unmarshal attribute %s: %w", protoAttr.Key, err)
			}
			attributes = append(attributes, attr)
		}
	}

	// Unmarshal events from proto
	events := make([]Event, 0, len(protoRegion.Events))
	for _, protoEvent := range protoRegion.Events {
		event, err := unmarshalEvent(protoEvent)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal event %s: %w", protoEvent.Name, err)
		}
		events = append(events, event)
	}

	// Unmarshal status from proto
	status := unmarshalStatus(protoRegion.Status)

	region := &Region{
		id:           regionID,
		parentID:     parentID,
		name:         protoRegion.Name,
		startTime:    protoRegion.StartTime,
		endTime:      protoRegion.EndTime,
		observations: observations,
		attributes:   attributes,
		events:       events,
		status:       status,
		ended:        true, // Regions from proto are always ended
	}

	return region, nil
}

// unmarshalEvent converts a protobuf Event to its Go representation.
func unmarshalEvent(protoEvent *proto.Event) (Event, error) {
	attributes := make([]attribute.KeyValue, 0, len(protoEvent.Attributes))
	for _, protoAttr := range protoEvent.Attributes {
		attr, err := unmarshalAttribute(protoAttr)
		if err != nil {
			return Event{}, fmt.Errorf("failed to unmarshal event attribute %s: %w", protoAttr.Key, err)
		}
		attributes = append(attributes, attr)
	}

	return Event{
		Name:       protoEvent.Name,
		Timestamp:  protoEvent.Timestamp,
		Attributes: attributes,
	}, nil
}

// unmarshalStatus converts a protobuf Status to its Go representation.
func unmarshalStatus(protoStatus *proto.Status) Status {
	if protoStatus == nil {
		return Status{}
	}
	return Status{
		Code:    codes.Code(protoStatus.Code),
		Message: protoStatus.Message,
	}
}

// unmarshalObservationValue converts a protobuf ObservationValue to a Go value.
func unmarshalObservationValue(protoValue *proto.ObservationValue) (any, error) {
	if protoValue == nil || protoValue.Kind == nil {
		return nil, fmt.Errorf("invalid observation value")
	}

	switch v := protoValue.Kind.(type) {
	case *proto.ObservationValue_IntValue:
		return v.IntValue, nil
	case *proto.ObservationValue_FloatValue:
		return v.FloatValue, nil
	case *proto.ObservationValue_BoolValue:
		return v.BoolValue, nil
	default:
		return nil, fmt.Errorf("unsupported observation value type: %T", protoValue.Kind)
	}
}

// unmarshalStatistic converts a protobuf Statistic to a Go Statistic.
func unmarshalStatistic(protoStat *proto.Statistic) (Statistic, error) {
	dataType, err := unmarshalDataType(protoStat.DataType)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal data type: %w", err)
	}

	aggType, err := unmarshalAggregationType(protoStat.AggregationType)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal aggregation type: %w", err)
	}

	switch dataType {
	case DataTypeInt64:
		return NewStatisticInt64(protoStat.Name, aggType), nil
	case DataTypeFloat64:
		return NewStatisticFloat64(protoStat.Name, aggType), nil
	case DataTypeBool:
		return NewStatisticFlag(protoStat.Name), nil
	default:
		return nil, fmt.Errorf("unsupported data type: %v", protoStat.DataType)
	}
}

// unmarshalDataType converts a proto DataType to Go DataType.
func unmarshalDataType(protoType proto.DataType) (DataType, error) {
	switch protoType {
	case proto.DATA_TYPE_INVALID:
		return DataTypeInvalid, nil
	case proto.DATA_TYPE_INT64:
		return DataTypeInt64, nil
	case proto.DATA_TYPE_FLOAT64:
		return DataTypeFloat64, nil
	case proto.DATA_TYPE_BOOL:
		return DataTypeBool, nil
	default:
		return DataTypeInvalid, fmt.Errorf("unknown data type: %v", protoType)
	}
}

// unmarshalAggregationType converts a proto AggregationType to Go AggregationType.
func unmarshalAggregationType(protoType proto.AggregationType) (AggregationType, error) {
	switch protoType {
	case proto.AGGREGATION_TYPE_INVALID:
		return AggregationTypeInvalid, nil
	case proto.AGGREGATION_TYPE_SUM:
		return AggregationTypeSum, nil
	case proto.AGGREGATION_TYPE_MIN:
		return AggregationTypeMin, nil
	case proto.AGGREGATION_TYPE_MAX:
		return AggregationTypeMax, nil
	case proto.AGGREGATION_TYPE_LAST:
		return AggregationTypeLast, nil
	case proto.AGGREGATION_TYPE_FIRST:
		return AggregationTypeFirst, nil
	default:
		return AggregationTypeInvalid, fmt.Errorf("unknown aggregation type: %v", protoType)
	}
}

// unmarshalAttribute converts a protobuf attribute to an OpenTelemetry attribute.
func unmarshalAttribute(protoAttr *proto.Attribute) (attribute.KeyValue, error) {
	if protoAttr == nil || protoAttr.Value == nil {
		return attribute.KeyValue{}, fmt.Errorf("invalid attribute")
	}

	key := attribute.Key(protoAttr.Key)
	switch v := protoAttr.Value.Kind.(type) {
	case *proto.AttributeValue_StringValue:
		return key.String(v.StringValue), nil
	case *proto.AttributeValue_IntValue:
		return key.Int64(v.IntValue), nil
	case *proto.AttributeValue_FloatValue:
		return key.Float64(v.FloatValue), nil
	case *proto.AttributeValue_BoolValue:
		return key.Bool(v.BoolValue), nil
	default:
		return attribute.KeyValue{}, fmt.Errorf("unsupported attribute value type: %T", protoAttr.Value.Kind)
	}
}
