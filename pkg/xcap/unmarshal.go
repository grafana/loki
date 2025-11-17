package xcap

import (
	"context"
	"fmt"
)

// FromProtoCapture converts a protobuf Capture to its Go representation.
func FromProtoCapture(proto *ProtoCapture) (*Capture, error) {
	if proto == nil {
		return nil, nil
	}

	// Build statistics map from proto statistics
	statsIndex := make(map[uint32]Statistic, len(proto.Statistics))
	for i, protoStat := range proto.Statistics {
		stat, err := unmarshalStatistic(protoStat)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal statistic: %w", err)
		}
		statsIndex[uint32(i)] = stat
	}

	_, capture := NewCapture(context.Background(), nil)

	// Unmarshal regions
	for _, protoRegion := range proto.Regions {
		region, err := fromProtoRegion(protoRegion, statsIndex)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal region: %w", err)
		}

		if region != nil {
			capture.AddRegion(region)
		}
	}

	return capture, nil
}

// fromProtoRegion converts a protobuf Region to its Go representation.
func fromProtoRegion(proto *ProtoRegion, statIndexToStat map[uint32]Statistic) (*Region, error) {
	// Unmarshal observations
	observations := make(map[StatisticKey]AggregatedObservation, len(proto.Observations))
	for _, protoObs := range proto.Observations {
		stat, exists := statIndexToStat[protoObs.StatisticId]
		if !exists {
			return nil, fmt.Errorf("invalid statistic_id %d in observation", protoObs.StatisticId)
		}

		value, err := unmarshalObservationValue(protoObs.Value)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal observation value: %w", err)
		}

		key := stat.Key()
		observations[key] = AggregatedObservation{
			Statistic: stat,
			Value:     value,
			Count:     int(protoObs.Count),
		}
	}

	// Unmarshal region ID from proto, or generate a new one if not set
	var regionID identifier
	copy(regionID[:], proto.Id)

	// Unmarshal parent ID from proto
	var parentID identifier
	copy(parentID[:], proto.ParentId)

	region := &Region{
		id:           regionID,
		parentID:     parentID,
		name:         proto.Name,
		startTime:    proto.StartTime,
		endTime:      proto.EndTime,
		observations: observations,
		ended:        true, // Regions from proto are always ended
	}

	return region, nil
}

// unmarshalObservationValue converts a protobuf ObservationValue to a Go value.
func unmarshalObservationValue(proto *ProtoObservationValue) (any, error) {
	if proto == nil || proto.Kind == nil {
		return nil, fmt.Errorf("invalid observation value")
	}

	switch v := proto.Kind.(type) {
	case *ProtoObservationValue_IntValue:
		return v.IntValue, nil
	case *ProtoObservationValue_FloatValue:
		return v.FloatValue, nil
	case *ProtoObservationValue_BoolValue:
		return v.BoolValue, nil
	default:
		return nil, fmt.Errorf("unsupported observation value type: %T", proto.Kind)
	}
}

// unmarshalStatistic converts a protobuf Statistic to a Go Statistic.
func unmarshalStatistic(proto *ProtoStatistic) (Statistic, error) {
	dataType, err := unmarshalDataType(proto.DataType)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal data type: %w", err)
	}

	aggType, err := unmarshalAggregationType(proto.AggregationType)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal aggregation type: %w", err)
	}

	switch dataType {
	case DataTypeInt64:
		return NewStatisticInt64(proto.Name, aggType), nil
	case DataTypeFloat64:
		return NewStatisticFloat64(proto.Name, aggType), nil
	case DataTypeBool:
		return NewStatisticFlag(proto.Name), nil
	default:
		return nil, fmt.Errorf("unsupported data type: %v", proto.DataType)
	}
}

// unmarshalDataType converts a proto DataType to Go DataType.
func unmarshalDataType(proto ProtoDataType) (DataType, error) {
	switch proto {
	case PROTO_DATA_TYPE_INVALID:
		return DataTypeInvalid, nil
	case PROTO_DATA_TYPE_INT64:
		return DataTypeInt64, nil
	case PROTO_DATA_TYPE_FLOAT64:
		return DataTypeFloat64, nil
	case PROTO_DATA_TYPE_BOOL:
		return DataTypeBool, nil
	default:
		return DataTypeInvalid, fmt.Errorf("unknown data type: %v", proto)
	}
}

// unmarshalAggregationType converts a proto AggregationType to Go AggregationType.
func unmarshalAggregationType(proto ProtoAggregationType) (AggregationType, error) {
	switch proto {
	case PROTO_AGGREGATION_TYPE_INVALID:
		return AggregationTypeInvalid, nil
	case PROTO_AGGREGATION_TYPE_SUM:
		return AggregationTypeSum, nil
	case PROTO_AGGREGATION_TYPE_MIN:
		return AggregationTypeMin, nil
	case PROTO_AGGREGATION_TYPE_MAX:
		return AggregationTypeMax, nil
	case PROTO_AGGREGATION_TYPE_LAST:
		return AggregationTypeLast, nil
	case PROTO_AGGREGATION_TYPE_FIRST:
		return AggregationTypeFirst, nil
	default:
		return AggregationTypeInvalid, fmt.Errorf("unknown aggregation type: %v", proto)
	}
}
