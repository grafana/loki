package limits

import (
	"context"
	"sync"

	"github.com/coder/quartz"
	"github.com/go-kit/log"
)

// PartitionManager keeps track of the partitions assigned and the timestamp
// of when it was assigned.
type PartitionManager struct {
	partitions map[int32]int64
	mtx        sync.Mutex
	logger     log.Logger

	// Used for tests.
	clock quartz.Clock
}

// NewPartitionManager returns a new [PartitionManager].
func NewPartitionManager(logger log.Logger) *PartitionManager {
	return &PartitionManager{
		partitions: make(map[int32]int64),
		logger:     log.With(logger, "component", "limits.PartitionManager"),
		clock:      quartz.NewReal(),
	}
}

// Assign assigns the partitions.
func (m *PartitionManager) Assign(_ context.Context, partitions []int32) {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	for _, partition := range partitions {
		m.partitions[partition] = m.clock.Now().UnixNano()
	}
}

// Has returns true if the partition is assigned, otherwise false.
func (m *PartitionManager) Has(partition int32) bool {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	_, ok := m.partitions[partition]
	return ok
}

// List returns a map of all assigned partitions and the timestamp of when
// each partition was assigned.
func (m *PartitionManager) List() map[int32]int64 {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	result := make(map[int32]int64)
	for partition, lastUpdated := range m.partitions {
		result[partition] = lastUpdated
	}
	return result
}

// Revoke revokes the partitions.
func (m *PartitionManager) Revoke(_ context.Context, partitions []int32) {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	for _, partition := range partitions {
		delete(m.partitions, partition)
	}
}
