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
	statistics   []Statistic
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

	stats, err := decodeProtoStatistics(protoCapture.Statistics)
	if err != nil {
		return nil, err
	}
	if err := validateProtoRegions(protoCapture.Regions, stats); err != nil {
		return nil, err
	}

	return &DecodedCapture{
		protoCapture: protoCapture,
		statistics:   stats,
	}, nil
}

// MergeBinary decodes a serialized Capture and merges it into c without first
// constructing an intermediate Capture. Regions are merged using the same
// semantics as [Capture.Merge].
func (c *Capture) MergeBinary(parent *Region, data []byte) error {
	if c == nil || len(data) == 0 {
		return nil
	}

	decoded, err := DecodeBinary(data)
	if err != nil {
		return err
	}
	return c.MergeDecoded(parent, decoded)
}

// MergeDecoded merges a validated decoded Capture into c without constructing
// an intermediate Capture.
func (c *Capture) MergeDecoded(parent *Region, decoded *DecodedCapture) error {
	if c == nil || decoded == nil {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.ended {
		return nil
	}

	for regionIndex := range decoded.protoCapture.Regions {
		protoRegion := &decoded.protoCapture.Regions[regionIndex]
		dst := c.mergeDestinationRegion(parent, protoRegion.Name)

		dst.mu.Lock()
		dst.mergeProtoObservations(protoRegion.ObservationsV2, decoded.statistics)
		dst.mu.Unlock()
	}

	return nil
}

func decodeProtoStatistics(protoStats []internal.Statistic) ([]Statistic, error) {
	stats := make([]Statistic, len(protoStats))
	for i := range protoStats {
		stat, err := unmarshalStatistic(&protoStats[i])
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal statistic: %w", err)
		}
		stats[i] = stat
	}
	return stats, nil
}

func validateProtoRegions(regions []internal.Region, stats []Statistic) error {
	for regionIndex := range regions {
		region := &regions[regionIndex]
		for observationIndex := range region.ObservationsV2 {
			obs := &region.ObservationsV2[observationIndex]
			if _, err := statisticForID(stats, obs.StatisticId); err != nil {
				return fmt.Errorf("region %q: %w", region.Name, err)
			}
		}
	}
	return nil
}

func statisticForID(stats []Statistic, statisticID uint32) (Statistic, error) {
	if uint64(statisticID) >= uint64(len(stats)) {
		return nil, fmt.Errorf("invalid statistic_id %d in observation", statisticID)
	}
	return stats[statisticID], nil
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
func (r *Region) mergeProtoObservations(observations []internal.ObservationV2, stats []Statistic) {
	for i := range observations {
		obs := &observations[i]
		stat := stats[obs.StatisticId]
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
