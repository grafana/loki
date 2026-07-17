package xcap

import (
	"fmt"
	"math"

	"github.com/grafana/loki/v3/pkg/xcap/internal/proto"
)

// fromProtoCapture converts a protobuf Capture to its Go representation.
func fromProtoCapture(protoCapture *proto.Capture, capture *Capture) error {
	if protoCapture == nil {
		return nil
	}

	// Build statistics map from proto statistics
	statsIndex := make(map[uint32]Statistic, len(protoCapture.Statistics))
	for i := range protoCapture.Statistics {
		stat, err := unmarshalStatistic(&protoCapture.Statistics[i])
		if err != nil {
			return fmt.Errorf("failed to unmarshal statistic: %w", err)
		}
		statsIndex[uint32(i)] = stat
	}

	capture.regionByName = make(map[string][]*Region)

	// Unmarshal regions
	for i := range protoCapture.Regions {
		region, err := fromProtoRegion(&protoCapture.Regions[i], statsIndex)
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
	// V1 was the only representation before observations_v2 was added.
	// Prefer it whenever present so that a capture containing both forms remains
	// compatible with the original encoding. New writers populate only V2.
	if len(protoRegion.Observations) > 0 {
		observations := make(map[StatisticKey]*AggregatedObservation, len(protoRegion.Observations))
		for i := range protoRegion.Observations {
			protoObs := &protoRegion.Observations[i]
			stat, exists := statIndexToStat[protoObs.StatisticId]
			if !exists {
				return nil, fmt.Errorf("invalid statistic_id %d in observation", protoObs.StatisticId)
			}

			value, err := unmarshalObservationValue(&protoObs.Value)
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

		return protoRegionWithObservations(protoRegion.Name, observations), nil
	}

	observations := make(map[StatisticKey]*AggregatedObservation, len(protoRegion.ObservationsV2))
	for i := range protoRegion.ObservationsV2 {
		protoObs := &protoRegion.ObservationsV2[i]
		stat, exists := statIndexToStat[protoObs.StatisticId]
		if !exists {
			return nil, fmt.Errorf("invalid statistic_id %d in V2 observation", protoObs.StatisticId)
		}

		value, err := unmarshalObservationV2Value(protoObs.ValueBits, stat.DataType())
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal V2 observation value: %w", err)
		}

		key := stat.Key()
		observations[key] = &AggregatedObservation{
			Statistic: stat,
			Value:     value,
			Count:     int(protoObs.Count),
		}
	}

	return protoRegionWithObservations(protoRegion.Name, observations), nil
}

func protoRegionWithObservations(name string, observations map[StatisticKey]*AggregatedObservation) *Region {
	return &Region{
		name:         name,
		observations: observations,
		ended:        true, // Regions from proto are always ended
	}
}

// unmarshalObservationV2Value converts flattened V2 value bits using the
// declared data type of the referenced statistic.
func unmarshalObservationV2Value(valueBits uint64, dataType DataType) (any, error) {
	switch dataType {
	case DataTypeInt64:
		return int64(valueBits), nil
	case DataTypeFloat64:
		return math.Float64frombits(valueBits), nil
	case DataTypeBool:
		return valueBits != 0, nil
	default:
		return nil, fmt.Errorf("unsupported observation data type: %v", dataType)
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

	// Statistics are immutable and shared across captures via the intern cache,
	// so repeated captures referencing the same statistic do not each allocate a
	// fresh instance.
	return internStatistic(protoStat.Name, dataType, aggType)
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
