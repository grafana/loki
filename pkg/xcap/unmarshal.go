package xcap

import (
	"fmt"

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
	observations := make(map[StatisticKey]*AggregatedObservation, len(protoRegion.ObservationsV2))
	for i := range protoRegion.ObservationsV2 {
		protoObs := &protoRegion.ObservationsV2[i]
		stat, exists := statIndexToStat[protoObs.StatisticId]
		if !exists {
			return nil, fmt.Errorf("invalid statistic_id %d in V2 observation", protoObs.StatisticId)
		}

		observations[stat.Key()] = &AggregatedObservation{
			Statistic: stat,
			value:     valueFromBits(protoObs.ValueBits),
			Count:     int(protoObs.Count),
		}
	}

	return &Region{
		name:         protoRegion.Name,
		observations: observations,
		ended:        true, // Regions from proto are always ended
	}, nil
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
