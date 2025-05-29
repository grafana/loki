package limits

import (
	"context"
	"sync"

	"github.com/coder/quartz"
)

type partitionState int

const (
	partitionPending partitionState = iota
	partitionReplaying
	partitionReady
)

// String implements the [fmt.Stringer] interface.
func (s partitionState) String() string {
	switch s {
	case partitionPending:
		return "pending"
	case partitionReplaying:
		return "replaying"
	case partitionReady:
		return "ready"
	default:
		return "unknown"
	}
}

// partitionManager keeps track of the partitions assigned and for
// each partition a timestamp of when it was assigned.
type partitionManager struct {
	partitions map[int32]partitionEntry
	mtx        sync.Mutex

	// Used for tests.
	clock quartz.Clock
}

// partitionEntry contains metadata about an assigned partition.
type partitionEntry struct {
	assignedAt   int64
	targetOffset int64
	state        partitionState
}

// newPartitionManager returns a new [PartitionManager].
func newPartitionManager() *partitionManager {
	return &partitionManager{
		partitions: make(map[int32]partitionEntry),
		clock:      quartz.NewReal(),
	}
}

// Assign assigns the partitions.
func (m *partitionManager) Assign(_ context.Context, partitions []int32) {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	for _, partition := range partitions {
		m.partitions[partition] = partitionEntry{
			assignedAt: m.clock.Now().UnixNano(),
			state:      partitionPending,
		}
	}
}

// GetState returns the current state of the partition. It returns false
// if the partition does not exist.
func (m *partitionManager) GetState(partition int32) (partitionState, bool) {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	entry, ok := m.partitions[partition]
	return entry.state, ok
}

// TargetOffsetReached returns true if the partition is replaying and the
// target offset has been reached.
func (m *partitionManager) TargetOffsetReached(partition int32, offset int64) bool {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	entry, ok := m.partitions[partition]
	if ok {
		return entry.state == partitionReplaying && entry.targetOffset <= offset
	}
	return false
}

// Has returns true if the partition is assigned, otherwise false.
func (m *partitionManager) Has(partition int32) bool {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	_, ok := m.partitions[partition]
	return ok
}

// List returns a map of all assigned partitions and the timestamp of when
// each partition was assigned.
func (m *partitionManager) List() map[int32]int64 {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	result := make(map[int32]int64)
	for partition, entry := range m.partitions {
		result[partition] = entry.assignedAt
	}
	return result
}

// ListByState returns all partitions with the specified state and their last
// updated timestamps.
func (m *partitionManager) ListByState(state partitionState) map[int32]int64 {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	result := make(map[int32]int64)
	for partition, entry := range m.partitions {
		if entry.state == state {
			result[partition] = entry.assignedAt
		}
	}
	return result
}

// SetReplaying sets the partition as replaying and the offset that must
// be consumed for it to become ready. It returns false if the partition
// does not exist.
func (m *partitionManager) SetReplaying(partition int32, offset int64) bool {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	entry, ok := m.partitions[partition]
	if ok {
		entry.state = partitionReplaying
		entry.targetOffset = offset
		m.partitions[partition] = entry
	}
	return ok
}

// SetReady sets the partition as ready. It returns false if the partition
// does not exist.
func (m *partitionManager) SetReady(partition int32) bool {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	entry, ok := m.partitions[partition]
	if ok {
		entry.state = partitionReady
		entry.targetOffset = 0
		m.partitions[partition] = entry
	}
	return ok
}

// Revoke deletes the partitions.
func (m *partitionManager) Revoke(_ context.Context, partitions []int32) {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	for _, partition := range partitions {
		delete(m.partitions, partition)
	}
}
