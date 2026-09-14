package ring

import (
	"time"
)

// PartitionInstanceRings holds one PartitionInstanceRing per watcher in a PartitionRingWatchers
// group, each pairing that watcher's partition ring with a shared instance ring. Rings are accessed
// by the same index used by the underlying PartitionRingWatchers group.
type PartitionInstanceRings struct {
	// rings is indexed by position in the group.
	rings []*PartitionInstanceRing
}

// NewPartitionInstanceRings builds one PartitionInstanceRing per watcher in the group, pairing each
// watcher's partition ring with the shared instance ring.
func NewPartitionInstanceRings(watchers *PartitionRingWatchers, instanceRing InstanceRingReader, heartbeatTimeout time.Duration) *PartitionInstanceRings {
	rings := make([]*PartitionInstanceRing, watchers.Count())
	for i := range rings {
		rings[i] = NewPartitionInstanceRing(watchers.Watcher(i), instanceRing, heartbeatTimeout)
	}
	return &PartitionInstanceRings{rings: rings}
}

// Count returns the number of rings in the group.
func (r *PartitionInstanceRings) Count() int {
	return len(r.rings)
}

// Get returns the partition-instance ring at the given index.
func (r *PartitionInstanceRings) Get(idx int) *PartitionInstanceRing {
	return r.rings[idx]
}
