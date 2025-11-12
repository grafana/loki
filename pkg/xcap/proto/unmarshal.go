package proto

import (
	"context"
	"fmt"

	"github.com/grafana/loki/v3/pkg/xcap"
)

// ToCapture converts a protobuf Capture to its Go representation.
func ToCapture(proto *Capture) (*xcap.Capture, error) {
	if proto == nil {
		return nil, nil
	}

	// Build statistics map from proto statistics
	statIndexToStat := make(map[uint32]xcap.Statistic, len(proto.Statistics))
	for i, protoStat := range proto.Statistics {
		stat, err := unmarshalStatistic(protoStat)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal statistic: %w", err)
		}
		statIndexToStat[uint32(i)] = stat
	}

	_, capture := xcap.NewCapture(context.Background(), nil)

	// Unmarshal regions
	for _, protoRegion := range proto.Regions {
		region, err := toRegion(protoRegion, statIndexToStat, capture)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal region: %w", err)
		}

		if region != nil {
			capture.AddRegion(region)
		}
	}

	return capture, nil
}

// toRegion converts a protobuf Region to its Go representation.
func toRegion(proto *Region, statIndexToStat map[uint32]xcap.Statistic, capture *xcap.Capture) (*xcap.Region, error) {
	if proto == nil {
		return nil, nil
	}

	// Unmarshal observations
	// We need to convert from proto observations (keyed by statistic_id)
	// to map[string]AggregatedObservation (keyed by unique identifier)
	observations := make(map[string]xcap.AggregatedObservation, len(proto.Observations))
	for _, protoObs := range proto.Observations {
		stat, exists := statIndexToStat[protoObs.StatisticId]
		if !exists {
			return nil, fmt.Errorf("invalid statistic_id %d in observation", protoObs.StatisticId)
		}

		value, err := unmarshalObservationValue(protoObs.Value)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal observation value: %w", err)
		}

		key := stat.UniqueIdentifier()
		observations[key] = xcap.AggregatedObservation{
			Statistic: stat,
			Value:     value,
			Count:     int(protoObs.Count),
		}
	}

	// Use constructor to create region
	region := xcap.NewRegion(
		proto.Name,
		proto.StartTime,
		proto.EndTime,
		observations,
		true,
		capture,
	)

	return region, nil
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

	dataType, err := unmarshalDataType(proto.DataType)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal data type: %w", err)
	}

	aggType, err := unmarshalAggregationType(proto.AggregationType)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal aggregation type: %w", err)
	}

	switch dataType {
	case xcap.DataTypeInt64:
		return xcap.NewStatisticInt64(proto.Name, aggType), nil
	case xcap.DataTypeFloat64:
		return xcap.NewStatisticFloat64(proto.Name, aggType), nil
	case xcap.DataTypeBool:
		return xcap.NewStatisticFlag(proto.Name), nil
	default:
		return nil, fmt.Errorf("unsupported data type: %v", proto.DataType)
	}
}

// unmarshalDataType converts a proto DataType to Go DataType.
func unmarshalDataType(proto DataType) (xcap.DataType, error) {
	switch proto {
	case DATA_TYPE_INVALID:
		return xcap.DataTypeInvalid, nil
	case DATA_TYPE_INT64:
		return xcap.DataTypeInt64, nil
	case DATA_TYPE_FLOAT64:
		return xcap.DataTypeFloat64, nil
	case DATA_TYPE_BOOL:
		return xcap.DataTypeBool, nil
	default:
		return xcap.DataTypeInvalid, fmt.Errorf("unknown data type: %v", proto)
	}
}

// unmarshalAggregationType converts a proto AggregationType to Go AggregationType.
func unmarshalAggregationType(proto AggregationType) (xcap.AggregationType, error) {
	switch proto {
	case AGGREGATION_TYPE_INVALID:
		return xcap.AggregationTypeInvalid, nil
	case AGGREGATION_TYPE_SUM:
		return xcap.AggregationTypeSum, nil
	case AGGREGATION_TYPE_MIN:
		return xcap.AggregationTypeMin, nil
	case AGGREGATION_TYPE_MAX:
		return xcap.AggregationTypeMax, nil
	case AGGREGATION_TYPE_LAST:
		return xcap.AggregationTypeLast, nil
	case AGGREGATION_TYPE_FIRST:
		return xcap.AggregationTypeFirst, nil
	default:
		return xcap.AggregationTypeInvalid, fmt.Errorf("unknown aggregation type: %v", proto)
	}
}
