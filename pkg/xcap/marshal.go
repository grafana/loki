package xcap

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/xcap/internal/proto"
	"github.com/grafana/loki/v3/pkg/xcap/statid"
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

	// Aggregate wire observations by region name so local-only statistics are
	// not serialized.
	regions := aggregateRegionsByName(c.regions)

	// Convert regions to proto regions.
	protoRegions := make([]proto.Region, 0, len(regions))
	for _, region := range regions {
		protoRegion, err := toProtoRegion(region)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal region: %w", err)
		}

		protoRegions = append(protoRegions, protoRegion)
	}

	return &proto.Capture{
		Regions: protoRegions,
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
					observations: make(map[StatisticKey]AggregatedObservation),
				}
				byName[src.name] = dst
				aggregated = append(aggregated, dst)
			}

			if dstObs, ok := dst.observations[key]; ok {
				dstObs.Merge(&srcObs)
				dst.observations[key] = dstObs
				continue
			}
			dst.observations[key] = AggregatedObservation{
				Statistic: srcObs.Statistic,
				value:     srcObs.value,
				Count:     srcObs.Count,
			}
		}
		src.mu.RUnlock()
	}

	return aggregated
}

// toProtoRegion converts a Region to its protobuf representation.
func toProtoRegion(region *Region) (proto.Region, error) {
	protoObservations := make([]proto.ObservationV2, 0, len(region.observations))
	for _, observation := range region.observations {
		if observation.Statistic.ID() == statid.Invalid {
			return proto.Region{}, fmt.Errorf("wire statistic %q has no ID", observation.Statistic.Name())
		}

		protoObservations = append(protoObservations, proto.ObservationV2{
			StatId:    uint32(observation.Statistic.ID()),
			Count:     uint32(observation.Count),
			ValueBits: observation.value.bits,
		})
	}

	return proto.Region{
		Name:           region.name,
		ObservationsV2: protoObservations,
	}, nil
}
