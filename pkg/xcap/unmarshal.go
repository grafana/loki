package xcap

import (
	"github.com/grafana/loki/v3/pkg/xcap/internal/proto"
	"github.com/grafana/loki/v3/pkg/xcap/statid"
)

// fromProtoCapture converts a protobuf Capture to its Go representation.
func fromProtoCapture(protoCapture *proto.Capture, capture *Capture) error {
	if protoCapture == nil {
		return nil
	}

	capture.regionByName = make(map[string][]*Region)

	// Unmarshal regions
	for i := range protoCapture.Regions {
		capture.AddRegion(fromProtoRegion(&protoCapture.Regions[i]))
	}

	return nil
}

// fromProtoRegion converts a protobuf Region to its Go representation.
func fromProtoRegion(protoRegion *proto.Region) *Region {
	observations := make(map[StatisticKey]*AggregatedObservation, len(protoRegion.ObservationsV2))
	for i := range protoRegion.ObservationsV2 {
		protoObs := &protoRegion.ObservationsV2[i]
		if statid.IsReserved(statid.ID(protoObs.StatId)) {
			continue
		}
		stat, exists := statisticByID(protoObs.StatId)
		if !exists {
			recordUnknownStatisticObservation()
			continue
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
	}
}
