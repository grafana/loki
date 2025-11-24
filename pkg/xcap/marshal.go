package xcap

import (
	"fmt"

	"go.opentelemetry.io/otel/attribute"

	"github.com/grafana/loki/v3/pkg/xcap/internal/proto"
)

// toProtoCapture converts a Capture to its protobuf representation.
func toProtoCapture(c *Capture) (*proto.Capture, error) {
	if c == nil {
		return nil, nil
	}

	statistics := c.getAllStatistics()
	statsIndex := make(map[StatisticKey]uint32)
	protoStats := make([]*proto.Statistic, 0, len(statistics))

	for _, stat := range statistics {
		statsIndex[stat.Key()] = uint32(len(protoStats))
		protoStats = append(protoStats, &proto.Statistic{
			Name:            stat.Name(),
			DataType:        marshalDataType(stat.DataType()),
			AggregationType: marshalAggregationType(stat.Aggregation()),
		})
	}

	// Convert regions to proto regions
	protoRegions := make([]*proto.Region, 0, len(c.regions))
	for _, region := range c.regions {
		protoRegion, err := toProtoRegion(region, statsIndex)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal region: %w", err)
		}

		if protoRegion != nil {
			protoRegions = append(protoRegions, protoRegion)
		}
	}

	return &proto.Capture{
		Regions:    protoRegions,
		Statistics: protoStats,
	}, nil
}

// toProtoRegion converts a Region to its protobuf representation.
func toProtoRegion(region *Region, statsIndex map[StatisticKey]uint32) (*proto.Region, error) {
	protoObservations := make([]*proto.Observation, 0, len(region.observations))
	for key, observation := range region.observations {
		statIndex, exists := statsIndex[key]
		if !exists {
			return nil, fmt.Errorf("statistic not found in index: %v", key)
		}

		protoValue, err := marshalObservationValue(observation.Value)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal observation: %w", err)
		}

		protoObservations = append(protoObservations, &proto.Observation{
			StatisticId: statIndex,
			Value:       protoValue,
			Count:       uint32(observation.Count),
		})
	}

	// Convert attributes to proto attributes
	protoAttributes := make([]*proto.Attribute, 0, len(region.attributes))
	for _, attr := range region.attributes {
		protoAttr, err := marshalAttribute(attr)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal attribute %s: %w", attr.Key, err)
		}
		protoAttributes = append(protoAttributes, protoAttr)
	}

	return &proto.Region{
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
func marshalObservationValue(value any) (*proto.ObservationValue, error) {
	switch v := value.(type) {
	case int64:
		return &proto.ObservationValue{
			Kind: &proto.ObservationValue_IntValue{IntValue: v},
		}, nil
	case float64:
		return &proto.ObservationValue{
			Kind: &proto.ObservationValue_FloatValue{FloatValue: v},
		}, nil
	case bool:
		return &proto.ObservationValue{
			Kind: &proto.ObservationValue_BoolValue{BoolValue: v},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported observation value type: %T", value)
	}
}

// marshalDataType converts a DataType to proto DataType.
func marshalDataType(dt DataType) proto.DataType {
	switch dt {
	case DataTypeInvalid:
		return proto.DATA_TYPE_INVALID
	case DataTypeInt64:
		return proto.DATA_TYPE_INT64
	case DataTypeFloat64:
		return proto.DATA_TYPE_FLOAT64
	case DataTypeBool:
		return proto.DATA_TYPE_BOOL
	default:
		return proto.DATA_TYPE_INVALID
	}
}

// marshalAggregationType converts an AggregationType to proto AggregationType.
func marshalAggregationType(agg AggregationType) proto.AggregationType {
	switch agg {
	case AggregationTypeInvalid:
		return proto.AGGREGATION_TYPE_INVALID
	case AggregationTypeSum:
		return proto.AGGREGATION_TYPE_SUM
	case AggregationTypeMin:
		return proto.AGGREGATION_TYPE_MIN
	case AggregationTypeMax:
		return proto.AGGREGATION_TYPE_MAX
	case AggregationTypeLast:
		return proto.AGGREGATION_TYPE_LAST
	case AggregationTypeFirst:
		return proto.AGGREGATION_TYPE_FIRST
	default:
		return proto.AGGREGATION_TYPE_INVALID
	}
}

// marshalAttribute converts an OpenTelemetry attribute to its protobuf representation.
func marshalAttribute(attr attribute.KeyValue) (*proto.Attribute, error) {
	if !attr.Valid() {
		return nil, fmt.Errorf("invalid attribute")
	}

	protoValue := &proto.AttributeValue{}
	switch attr.Value.Type() {
	case attribute.STRING:
		protoValue.Kind = &proto.AttributeValue_StringValue{StringValue: attr.Value.AsString()}
	case attribute.INT64:
		protoValue.Kind = &proto.AttributeValue_IntValue{IntValue: attr.Value.AsInt64()}
	case attribute.FLOAT64:
		protoValue.Kind = &proto.AttributeValue_FloatValue{FloatValue: attr.Value.AsFloat64()}
	case attribute.BOOL:
		protoValue.Kind = &proto.AttributeValue_BoolValue{BoolValue: attr.Value.AsBool()}
	default:
		// For unsupported types (like slices), convert to string
		protoValue.Kind = &proto.AttributeValue_StringValue{StringValue: attr.Value.Emit()}
	}

	return &proto.Attribute{
		Key:   string(attr.Key),
		Value: protoValue,
	}, nil
}
