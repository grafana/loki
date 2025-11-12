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
	statIndex := make(map[xcap.StatisticKey]uint32)
	protoStats := make([]*Statistic, 0, len(allStats))

	for _, stat := range allStats {
		key := stat.Key()
		if _, exists := statIndex[key]; !exists {
			statIndex[key] = uint32(len(protoStats))
			protoStats = append(protoStats, &Statistic{
				Name:            stat.Name(),
				DataType:        marshalDataType(stat.DataType()),
				AggregationType: marshalAggregationType(stat.Aggregation()),
			})
		}
	}

	// Convert scopes to proto scopes
	scopes := c.Scopes()
	protoScopes := make([]*Scope, 0, len(scopes))
	for _, scope := range scopes {
		protoScope, err := toPbScope(scope, statIndex)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal scope: %w", err)
		}

		if protoScope != nil {
			protoScopes = append(protoScopes, protoScope)
		}
	}

	return &Capture{
		Scopes:     protoScopes,
		Statistics: protoStats,
	}, nil
}

// toPbScope converts a Scope to its protobuf representation.
func toPbScope(scope *xcap.Scope, statKeyToIndex map[xcap.StatisticKey]uint32) (*Scope, error) {
	if scope == nil {
		return nil, nil
	}

	startTime := scope.StartTime()
	endTime := scope.EndTime()
	name := scope.Name()

	// Marshal observations
	observations := scope.GetObservations()
	protoObservations := make([]*Observation, 0, len(observations))
	for _, observation := range observations {
		key := observation.Statistic.Key()
		statIndex, exists := statKeyToIndex[key]
		if !exists {
			return nil, fmt.Errorf("statistic not found in index: %v", key)
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

	// Build proto scope
	protoScope := &Scope{
		Name:         name,
		StartTime:    startTime,
		EndTime:      endTime,
		Observations: protoObservations,
	}

	return protoScope, nil
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
