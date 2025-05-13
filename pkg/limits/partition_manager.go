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
func (m *PartitionManager) Assign(_ context.Context, _ *kgo.Client, topicPartitions map[string][]int32) {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	for _, partitions := range topicPartitions {
		for _, partition := range partitions {
			m.partitions[partition] = m.clock.Now().UnixNano()
		}
	}
}

// Has returns true if the partition is assigned, otherwise false.
func (m *PartitionManager) Has(partition int32) bool {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	_, ok := m.partitions[partition]
	return ok
}

// List returns a map of all assigned partitions and their last updated timestamps.
func (m *PartitionManager) List() map[int32]int64 {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	v := make(map[int32]int64)
	for partition, lastUpdated := range m.partitions {
		v[partition] = lastUpdated
	}
	return v
}

func (m *PartitionManager) Remove(_ context.Context, _ *kgo.Client, topicPartitions map[string][]int32) {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	for _, partitions := range topicPartitions {
		for _, partition := range partitions {
			delete(m.partitions, partition)
		}
	}
}
