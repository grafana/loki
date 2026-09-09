package xcap

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/xcap/internal/proto"
)

// toProtoCapture converts a Capture to its protobuf representation.
//
// Marshalling is intended to happen after the capture lifecycle is
// complete. toProtoCapture marks the capture and all its regions
// as ended to prevent further recording while marshalling is in progress.
func toProtoCapture(c *Capture) (*proto.Capture, error) {
	if c == nil {
		return nil, nil
	}

	// end the capture and its regions. This operation is idempotent.
	c.End()
	for _, region := range c.regions {
		region.End()
	}

	// Aggregate wire observations by region name before creating the statistics
	// index so local-only statistics are not serialized.
	regions := aggregateRegionsByName(c.regions)

	// Build the statistics index from deduplicated wire statistics.
	statistics := statisticsFromRegions(regions)
	statsIndex := make(map[StatisticKey]uint32, len(statistics))
	protoStats := make([]proto.Statistic, 0, len(statistics))

	for _, stat := range statistics {
		statsIndex[stat.Key()] = uint32(len(protoStats))
		protoStats = append(protoStats, proto.Statistic{
			Name:            stat.Name(),
			DataType:        marshalDataType(stat.DataType()),
			AggregationType: marshalAggregationType(stat.Aggregation()),
		})
	}

	// Convert regions to proto regions.
	protoRegions := make([]proto.Region, 0, len(regions))
	for _, region := range regions {
		protoRegion, err := toProtoRegion(region, statsIndex)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal region: %w", err)
		}

		protoRegions = append(protoRegions, protoRegion)
	}

	return &proto.Capture{
		Regions:    protoRegions,
		Statistics: protoStats,
	}, nil
}

// aggregateRegionsByName folds all wire observations from regions that share a
// name into a single region, merging their observations with
// [AggregatedObservation] semantics.
//
// Aggregated regions have fresh IDs and no parent linkage because wire
// consumers only use region names and observations.
func aggregateRegionsByName(regions []*Region) []*Region {
	byName := make(map[string]*Region, len(regions))
	aggregated := make([]*Region, 0, len(regions))

	for _, src := range regions {
		src.mu.RLock()
		for key, srcObs := range src.observations {
			if srcObs.Statistic.Scope() != ExportWire {
				continue
			}

			dst, ok := byName[src.name]
			if !ok {
				dst = &Region{
					id:           newID(),
					name:         src.name,
					observations: make(map[StatisticKey]*AggregatedObservation),
				}
				byName[src.name] = dst
				aggregated = append(aggregated, dst)
			}

			if dstObs, ok := dst.observations[key]; ok {
				dstObs.Merge(srcObs)
				continue
			}
			dst.observations[key] = &AggregatedObservation{
				Statistic: srcObs.Statistic,
				value:     srcObs.value,
				Count:     srcObs.Count,
			}
		}
		src.mu.RUnlock()
	}

	return aggregated
}

func statisticsFromRegions(regions []*Region) map[StatisticKey]Statistic {
	statistics := make(map[StatisticKey]Statistic)
	for _, region := range regions {
		for key, obs := range region.observations {
			statistics[key] = obs.Statistic
		}
	}
	return statistics
}

// toProtoRegion converts a Region to its protobuf representation.
func toProtoRegion(region *Region, statsIndex map[StatisticKey]uint32) (proto.Region, error) {
	protoObservations := make([]proto.ObservationV2, 0, len(region.observations))
	for key, observation := range region.observations {
		statIndex, exists := statsIndex[key]
		if !exists {
			return proto.Region{}, fmt.Errorf("statistic not found in index: %v", key)
		}

		protoObservations = append(protoObservations, proto.ObservationV2{
			StatisticId: statIndex,
			Count:       uint32(observation.Count),
			ValueBits:   observation.value.bits,
		})
	}

	return proto.Region{
		Name:           region.name,
		ObservationsV2: protoObservations,
	}, nil
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
