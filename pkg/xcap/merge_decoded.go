package xcap

import (
	"fmt"

	"github.com/gogo/protobuf/proto"

	internal "github.com/grafana/loki/v3/pkg/xcap/internal/proto"
)

// DecodedCapture is a validated protobuf representation of a Capture. It can
// be merged into a destination Capture with [Capture.MergeDecoded].
type DecodedCapture struct {
	protoCapture *internal.Capture
}

// DecodeBinary validates serialized Capture data and returns a representation
// that can be merged without constructing an intermediate Capture.
func DecodeBinary(data []byte) (*DecodedCapture, error) {
	if len(data) == 0 {
		return nil, nil
	}

	protoCapture := &internal.Capture{}
	if err := proto.Unmarshal(data, protoCapture); err != nil {
		return nil, fmt.Errorf("failed to unmarshal proto capture: %w", err)
	}

	return &DecodedCapture{
		protoCapture: protoCapture,
	}, nil
}

// MergeDecoded merges a validated decoded Capture into c without constructing
// an intermediate Capture.
func (c *Capture) MergeDecoded(parent *Region, decoded *DecodedCapture) {
	if c == nil || decoded == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ended {
		return
	}

	for regionIndex := range decoded.protoCapture.Regions {
		protoRegion := &decoded.protoCapture.Regions[regionIndex]
		dst := c.mergeDestinationRegion(parent, protoRegion.Name)

		dst.mu.Lock()
		dst.mergeProtoObservations(protoRegion.ObservationsV2)
		dst.mu.Unlock()
	}

}

func (c *Capture) mergeDestinationRegion(parent *Region, name string) *Region {
	if dsts := c.regionByName[name]; len(dsts) > 0 {
		return dsts[len(dsts)-1]
	}

	dst := &Region{
		id:           newID(),
		name:         name,
		observations: make(map[StatisticKey]*AggregatedObservation),
	}
	if parent != nil {
		dst.parentID = parent.id
	}

	c.regions = append(c.regions, dst)
	c.regionByName[dst.name] = append(c.regionByName[dst.name], dst)
	return dst
}

// mergeProtoObservations folds decoded V2 observations into r, reusing the
// standard aggregation path. The caller must hold r.mu.
func (r *Region) mergeProtoObservations(observations []internal.ObservationV2) {
	for i := range observations {
		obs := &observations[i]
		stat, exists := statisticByID(obs.StatId)
		if !exists {
			recordUnknownStatisticObservation()
			continue
		}
		val := valueFromBits(obs.ValueBits)
		key := stat.Key()

		if existing, ok := r.observations[key]; ok {
			existing.aggregate(stat.Aggregation(), val)
			existing.Count += int(obs.Count)
			continue
		}

		r.observations[key] = &AggregatedObservation{
			Statistic: stat,
			value:     val,
			Count:     int(obs.Count),
		}
	}
}
