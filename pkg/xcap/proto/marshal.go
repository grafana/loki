package proto

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/xcap"
)

// ToPbCapture converts a Capture to its protobuf representation.
func ToPbCapture(c *xcap.Capture) (*Capture, error) {
	if c == nil {
		return nil, nil
	}

	allStats := c.GetAllStatistics()
	statIndex := make(map[string]uint32)
	protoStats := make([]*Statistic, 0, len(allStats))

	for _, stat := range allStats {
		statID := stat.UniqueIdentifier()
		if _, exists := statIndex[statID]; !exists {
			statIndex[statID] = uint32(len(protoStats))
			protoStats = append(protoStats, &Statistic{
				Name:            stat.Name(),
				DataType:        marshalDataType(stat.DataType()),
				AggregationType: marshalAggregationType(stat.Aggregation()),
			})
		}
	}

	// Convert regions to proto regions
	regions := c.Regions()
	protoRegions := make([]*Region, 0, len(regions))
	for _, region := range regions {
		protoRegion, err := toPbRegion(region, statIndex)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal region: %w", err)
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

// toPbRegion converts a Region to its protobuf representation.
func toPbRegion(region *xcap.Region, statNameToIndex map[string]uint32) (*Region, error) {
	if region == nil {
		return nil, nil
	}

	startTime := region.StartTime()
	endTime := region.EndTime()
	name := region.Name()

	// Marshal observations
	observations := region.GetObservations()
	protoObservations := make([]*Observation, 0, len(observations))
	for _, observation := range observations {
		statIndex, exists := statNameToIndex[observation.Statistic.UniqueIdentifier()]
		if !exists {
			// this shouldn't happen
			continue
		}

		protoValue, err := marshalObservationValue(observation.Value)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal observation value for statistic %s: %w", observation.Statistic.UniqueIdentifier(), err)
		}

		protoObservations = append(protoObservations, &Observation{
			StatisticId: statIndex,
			Value:       protoValue,
			Count:       uint32(observation.Count),
		})
	}

	// Build proto region
	protoRegion := &Region{
		Name:         name,
		StartTime:    startTime,
		EndTime:      endTime,
		Observations: protoObservations,
	}

	return protoRegion, nil
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

// marshalDataType converts a DataType to proto DataType.
func marshalDataType(dt xcap.DataType) DataType {
	switch dt {
	case xcap.DataTypeInvalid:
		return DATA_TYPE_INVALID
	case xcap.DataTypeInt64:
		return DATA_TYPE_INT64
	case xcap.DataTypeFloat64:
		return DATA_TYPE_FLOAT64
	case xcap.DataTypeBool:
		return DATA_TYPE_BOOL
	default:
		return DATA_TYPE_INVALID
	}
}

// marshalAggregationType converts an AggregationType to proto AggregationType.
func marshalAggregationType(agg xcap.AggregationType) AggregationType {
	switch agg {
	case xcap.AggregationTypeInvalid:
		return AGGREGATION_TYPE_INVALID
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

