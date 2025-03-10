package limits

import (
	"context"
	"sync"

	"github.com/coder/quartz"
	"github.com/go-kit/log"
	"github.com/twmb/franz-go/pkg/kgo"
)

// PartitionManager keeps track of the partitions assigned and for
// each partition a timestamp of when it was last updated.
type PartitionManager struct {
	// partitions maps partitionID to last updated (unix nanoseconds).
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

// Assign assigns the partitions and sets the last updated timestamp for each
// partition to the current time.
func (m *PartitionManager) Assign(_ context.Context, _ *kgo.Client, partitions map[string][]int32) {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	for _, partitionIDs := range partitions {
		for _, partitionID := range partitionIDs {
			m.partitions[partitionID] = m.clock.Now().UnixNano()
		}
	}
}

// Has returns true if the partition is assigned, otherwise false.
func (m *PartitionManager) Has(partitionID int32) bool {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	_, ok := m.partitions[partitionID]
	return ok
}

// List returns a map of all assigned partitions and their last updated timestamps.
func (m *PartitionManager) List() map[int32]int64 {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	v := make(map[int32]int64)
	for partitionID, lastUpdated := range m.partitions {
		v[partitionID] = lastUpdated
	}
	return v
}

func (m *PartitionManager) Remove(_ context.Context, _ *kgo.Client, partitions map[string][]int32) {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	for _, partitionIDs := range partitions {
		for _, partitionID := range partitionIDs {
			delete(m.partitions, partitionID)
		}
	}
}
