package xcap

import (
	"fmt"

	"go.opentelemetry.io/otel/attribute"
)

// ToProtoCapture converts a Capture to its protobuf representation.
func ToProtoCapture(c *Capture) (*ProtoCapture, error) {
	if c == nil {
		return nil, nil
	}

	statistics := c.getAllStatistics()
	statsIndex := make(map[StatisticKey]uint32)
	protoStats := make([]*ProtoStatistic, 0, len(statistics))

	for _, stat := range statistics {
		statsIndex[stat.Key()] = uint32(len(protoStats))
		protoStats = append(protoStats, &ProtoStatistic{
			Name:            stat.Name(),
			DataType:        marshalDataType(stat.DataType()),
			AggregationType: marshalAggregationType(stat.Aggregation()),
		})
	}

	// Convert regions to proto regions
	protoRegions := make([]*ProtoRegion, 0, len(c.regions))
	for _, region := range c.regions {
		protoRegion, err := toProtoRegion(region, statsIndex)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal region: %w", err)
		}

		if protoRegion != nil {
			protoRegions = append(protoRegions, protoRegion)
		}
	}

	return &ProtoCapture{
		Regions:    protoRegions,
		Statistics: protoStats,
	}, nil
}

// toProtoRegion converts a Region to its protobuf representation.
func toProtoRegion(region *Region, statsIndex map[StatisticKey]uint32) (*ProtoRegion, error) {
	protoObservations := make([]*ProtoObservation, 0, len(region.observations))
	for key, observation := range region.observations {
		statIndex, exists := statsIndex[key]
		if !exists {
			return nil, fmt.Errorf("statistic not found in index: %v", key)
		}

		protoValue, err := marshalObservationValue(observation.Value)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal observation: %w", err)
		}

		protoObservations = append(protoObservations, &ProtoObservation{
			StatisticId: statIndex,
			Value:       protoValue,
			Count:       uint32(observation.Count),
		})
	}

	// Convert attributes to proto attributes
	protoAttributes := make([]*ProtoAttribute, 0, len(region.attributes))
	for _, attr := range region.attributes {
		protoAttr, err := marshalAttribute(attr)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal attribute %s: %w", attr.Key, err)
		}
		protoAttributes = append(protoAttributes, protoAttr)
	}

	return &ProtoRegion{
		Name:         region.name,
		StartTime:    region.startTime,
		EndTime:      region.endTime,
		Observations: protoObservations,
		Id:           region.id[:],
		ParentId:     region.parentID[:],
		Attributes:   protoAttributes,
	}, nil
}

// marshalObservationValue converts an observation value to proto ObservationValue.
func marshalObservationValue(value any) (*ProtoObservationValue, error) {
	switch v := value.(type) {
	case int64:
		return &ProtoObservationValue{
			Kind: &ProtoObservationValue_IntValue{IntValue: v},
		}, nil
	case float64:
		return &ProtoObservationValue{
			Kind: &ProtoObservationValue_FloatValue{FloatValue: v},
		}, nil
	case bool:
		return &ProtoObservationValue{
			Kind: &ProtoObservationValue_BoolValue{BoolValue: v},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported observation value type: %T", value)
	}
}

// marshalDataType converts a DataType to proto DataType.
func marshalDataType(dt DataType) ProtoDataType {
	switch dt {
	case DataTypeInvalid:
		return PROTO_DATA_TYPE_INVALID
	case DataTypeInt64:
		return PROTO_DATA_TYPE_INT64
	case DataTypeFloat64:
		return PROTO_DATA_TYPE_FLOAT64
	case DataTypeBool:
		return PROTO_DATA_TYPE_BOOL
	default:
		return PROTO_DATA_TYPE_INVALID
	}
}

// marshalAggregationType converts an AggregationType to proto AggregationType.
func marshalAggregationType(agg AggregationType) ProtoAggregationType {
	switch agg {
	case AggregationTypeInvalid:
		return PROTO_AGGREGATION_TYPE_INVALID
	case AggregationTypeSum:
		return PROTO_AGGREGATION_TYPE_SUM
	case AggregationTypeMin:
		return PROTO_AGGREGATION_TYPE_MIN
	case AggregationTypeMax:
		return PROTO_AGGREGATION_TYPE_MAX
	case AggregationTypeLast:
		return PROTO_AGGREGATION_TYPE_LAST
	case AggregationTypeFirst:
		return PROTO_AGGREGATION_TYPE_FIRST
	default:
		return PROTO_AGGREGATION_TYPE_INVALID
	}
}

// marshalAttribute converts an OpenTelemetry attribute to its protobuf representation.
func marshalAttribute(attr attribute.KeyValue) (*ProtoAttribute, error) {
	if !attr.Valid() {
		return nil, fmt.Errorf("invalid attribute")
	}

	protoValue := &ProtoAttributeValue{}
	switch attr.Value.Type() {
	case attribute.STRING:
		protoValue.Kind = &ProtoAttributeValue_StringValue{StringValue: attr.Value.AsString()}
	case attribute.INT64:
		protoValue.Kind = &ProtoAttributeValue_IntValue{IntValue: attr.Value.AsInt64()}
	case attribute.FLOAT64:
		protoValue.Kind = &ProtoAttributeValue_FloatValue{FloatValue: attr.Value.AsFloat64()}
	case attribute.BOOL:
		protoValue.Kind = &ProtoAttributeValue_BoolValue{BoolValue: attr.Value.AsBool()}
	default:
		// For unsupported types (like slices), convert to string
		protoValue.Kind = &ProtoAttributeValue_StringValue{StringValue: attr.Value.Emit()}
	}

	return &ProtoAttribute{
		Key:   string(attr.Key),
		Value: protoValue,
	}, nil
}
