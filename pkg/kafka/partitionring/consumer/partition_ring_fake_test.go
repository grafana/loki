package consumer

import (
	"time"

	"github.com/grafana/dskit/ring"
)

// fakePartitionRing satisfies ring.PartitionRingReader with an in-memory
// snapshot. Tests can mutate it between rebalances via markActive,
// markInactive, and removePartition. It exists so unit tests can
// exercise ring-aware code without spinning up memberlist (which would
// bind real ports, gossip, and break in CI).
type fakePartitionRing struct {
	desc *ring.PartitionRingDesc
	snap *ring.PartitionRing
}

// newFakePartitionRing returns a fake ring with the given IDs in
// PartitionActive state. Pass at least one ID — an empty ring would
// make the partition-ring-aware balancer refuse to assign partitions,
// matching the production "fail loudly on empty ring" behavior.
func newFakePartitionRing(activeIDs ...int32) *fakePartitionRing {
	f := &fakePartitionRing{desc: ring.NewPartitionRingDesc()}
	for _, id := range activeIDs {
		f.desc.AddPartition(id, ring.PartitionActive, time.Now())
	}
	f.rebuild()
	return f
}

// newDefaultFakePartitionRing returns a fake ring with 16 active
// partitions (0..15). Tests that don't care about exact partition
// membership can use this. Tests asserting on partition counts (e.g.
// multi-partition consumption) should call newFakePartitionRing with
// the exact IDs to match the kfake topic.
func newDefaultFakePartitionRing() *fakePartitionRing {
	ids := make([]int32, 16)
	for i := range ids {
		ids[i] = int32(i)
	}
	return newFakePartitionRing(ids...)
}

func (f *fakePartitionRing) PartitionRing() *ring.PartitionRing { return f.snap }

// markActive adds the partition (or updates its state if already
// present) with state PartitionActive, then rebuilds the snapshot.
func (f *fakePartitionRing) markActive(id int32) {
	f.desc.AddPartition(id, ring.PartitionActive, time.Now())
	f.rebuild()
}

// markInactive adds the partition (or updates its state if already
// present) with state PartitionInactive, then rebuilds the snapshot.
func (f *fakePartitionRing) markInactive(id int32) {
	f.desc.AddPartition(id, ring.PartitionInactive, time.Now())
	f.rebuild()
}

// removePartition deletes the partition from the ring entirely.
func (f *fakePartitionRing) removePartition(id int32) {
	f.desc.RemovePartition(id)
	f.rebuild()
}

func (f *fakePartitionRing) rebuild() {
	r, err := ring.NewPartitionRing(*f.desc)
	if err != nil {
		panic(err)
	}
	f.snap = r
}
