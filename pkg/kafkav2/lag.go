package kafkav2

import (
	"context"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
)

// partitionLag observes the consumer lag for a partition.
type partitionLag struct {
	maxLag, currentLag             time.Duration
	maxLagOffset, currentLagOffset int64
}

// PartitionLagTracker tracks the consumer lag of all consumed partitions.
// Partitions that did not consume any records since the last iteration of the
// lag tracker are removed.
type PartitionLagTracker struct {
	*services.BasicService
	mtx        sync.Mutex
	partitions map[int32]partitionLag
	logger     log.Logger
}

// NewPartitionLagTracker returns a PartitionLagTracker that logs the observed
// lag of each partition once per minute.
func NewPartitionLagTracker(logger log.Logger) *PartitionLagTracker {
	t := &PartitionLagTracker{
		partitions: make(map[int32]partitionLag),
		logger:     logger,
	}
	t.BasicService = services.NewTimerService(time.Minute, nil, t.runOnce, nil)
	return t
}

// Observe records the lag for the partition.
func (t *PartitionLagTracker) Observe(partition int32, lag time.Duration, offset int64) {
	t.mtx.Lock()
	defer t.mtx.Unlock()
	tmp := t.partitions[partition]
	if lag > tmp.maxLag {
		tmp.maxLag = lag
		tmp.maxLagOffset = offset
	}
	tmp.currentLag = lag
	tmp.currentLagOffset = offset
	t.partitions[partition] = tmp
}

// runOnce logs the lag of each partition observed since the last call.
func (t *PartitionLagTracker) runOnce(_ context.Context) error {
	t.mtx.Lock()
	partitions := t.partitions
	t.partitions = make(map[int32]partitionLag)
	t.mtx.Unlock()
	for partition, tmp := range partitions {
		level.Info(t.logger).Log(
			"msg", "consumer lag",
			"partition", partition,
			"max_lag", tmp.maxLag,
			"max_lag_offset", tmp.maxLagOffset,
			"current_lag", tmp.currentLag,
			"current_lag_offset", tmp.currentLagOffset,
		)
	}
	return nil
}
