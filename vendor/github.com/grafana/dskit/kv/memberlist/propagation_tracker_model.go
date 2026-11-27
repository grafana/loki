package memberlist

import (
	"fmt"
	"time"

	"github.com/gogo/protobuf/proto"

	"github.com/grafana/dskit/kv/codec"
)

const (
	// PropagationDelayTrackerCodecID is the codec ID used for the propagation delay tracker.
	PropagationDelayTrackerCodecID = "propagationDelayTracker"
)

// PropagationDelayTrackerDescFactory creates a new PropagationDelayTrackerDesc.
func PropagationDelayTrackerDescFactory() proto.Message {
	return NewPropagationDelayTrackerDesc()
}

// NewPropagationDelayTrackerDesc creates a new, empty PropagationDelayTrackerDesc.
func NewPropagationDelayTrackerDesc() *PropagationDelayTrackerDesc {
	return &PropagationDelayTrackerDesc{
		Beacons: make(map[uint64]BeaconDesc),
	}
}

// GetPropagationDelayTrackerCodec returns the codec for PropagationDelayTrackerDesc.
func GetPropagationDelayTrackerCodec() codec.Codec {
	return codec.NewProtoCodec(PropagationDelayTrackerCodecID, PropagationDelayTrackerDescFactory)
}

// GetOrCreatePropagationDelayTrackerDesc returns the given value as a *PropagationDelayTrackerDesc,
// or creates a new one if the value is nil.
func GetOrCreatePropagationDelayTrackerDesc(in any) *PropagationDelayTrackerDesc {
	if in == nil {
		return NewPropagationDelayTrackerDesc()
	}

	desc := in.(*PropagationDelayTrackerDesc)
	if desc == nil {
		return NewPropagationDelayTrackerDesc()
	}

	return desc
}

// Merge implements Mergeable.
func (d *PropagationDelayTrackerDesc) Merge(other Mergeable, localCAS bool) (Mergeable, error) {
	return d.mergeWithTime(other, localCAS, time.Now())
}

func (d *PropagationDelayTrackerDesc) mergeWithTime(mergeable Mergeable, localCAS bool, now time.Time) (Mergeable, error) {
	if mergeable == nil {
		return nil, nil
	}

	other, ok := mergeable.(*PropagationDelayTrackerDesc)
	if !ok {
		return nil, fmt.Errorf("expected *PropagationDelayTrackerDesc, got %T", mergeable)
	}

	if other == nil {
		return nil, nil
	}

	change := NewPropagationDelayTrackerDesc()

	for beaconID, otherBeacon := range other.Beacons {
		changed := false

		thisBeacon, exists := d.Beacons[beaconID]
		if !exists {
			changed = true
			thisBeacon = otherBeacon
		} else {
			// In case the timestamp is equal we give priority to the higher DeletedAt (later deletion wins).
			if otherBeacon.PublishedAt > thisBeacon.PublishedAt || (otherBeacon.PublishedAt == thisBeacon.PublishedAt && otherBeacon.DeletedAt > thisBeacon.DeletedAt) {
				changed = true
				thisBeacon = otherBeacon
			}
		}

		if changed {
			d.Beacons[beaconID] = thisBeacon
			change.Beacons[beaconID] = thisBeacon
		}
	}

	if localCAS {
		// Let's mark all missing beacons as deleted.
		// This breaks commutativity! But we only do it locally, not when gossiping with others.
		for beaconID, thisBeacon := range d.Beacons {
			if _, exists := other.Beacons[beaconID]; !exists && thisBeacon.DeletedAt == 0 {
				// Beacon was removed. We need to preserve it locally, but mark as deleted.
				thisBeacon.DeletedAt = now.UnixMilli()
				d.Beacons[beaconID] = thisBeacon
				change.Beacons[beaconID] = thisBeacon
			}
		}
	}

	// If nothing changed, report nothing.
	if len(change.Beacons) == 0 {
		return nil, nil
	}

	return change, nil
}

// MergeContent implements Mergeable.
func (d *PropagationDelayTrackerDesc) MergeContent() []string {
	result := make([]string, 0, len(d.Beacons))
	for beaconID := range d.Beacons {
		result = append(result, fmt.Sprintf("%d", beaconID))
	}
	return result
}

// RemoveTombstones implements Mergeable.
func (d *PropagationDelayTrackerDesc) RemoveTombstones(limit time.Time) (total, removed int) {
	for beaconID, beacon := range d.Beacons {
		if beacon.DeletedAt != 0 {
			if limit.IsZero() || time.UnixMilli(beacon.DeletedAt).Before(limit) {
				delete(d.Beacons, beaconID)
				removed++
			} else {
				total++
			}
		}
	}
	return
}

// Clone implements Mergeable.
func (d *PropagationDelayTrackerDesc) Clone() Mergeable {
	clone := proto.Clone(d).(*PropagationDelayTrackerDesc)

	// Ensure empty maps are preserved (easier to compare with a deep equal in tests).
	if d.Beacons != nil && clone.Beacons == nil {
		clone.Beacons = map[uint64]BeaconDesc{}
	}

	return clone
}

func (b BeaconDesc) GetPublishedAtTime() time.Time {
	return time.UnixMilli(b.PublishedAt)
}
