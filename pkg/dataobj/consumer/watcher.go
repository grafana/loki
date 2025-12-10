package consumer

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/grafana/dskit/services"

	"github.com/grafana/loki/v3/pkg/kafka/partition"
)

// OffsetWatcher keeps track of offsets in almost real time.
type OffsetWatcher struct {
	services.Service
	offsetManager      partition.OffsetManager
	partitionID        int32        // The partition to watch.
	lastProducedOffset atomic.Int64 // The offset of the last produced record.
}

func NewOffsetWatcher(offsetManager partition.OffsetManager, partitionID int32) *OffsetWatcher {
	w := OffsetWatcher{
		offsetManager: offsetManager,
		partitionID:   partitionID,
	}
	w.Service = services.NewTimerService(time.Minute, w.Refresh, w.Refresh, nil)
	return &w
}

// LastProducedOffset returns the offset of the last produced record.
func (w *OffsetWatcher) LastProducedOffset() int64 {
	return w.lastProducedOffset.Load()
}

// Refresh the offsets.
func (w *OffsetWatcher) Refresh(ctx context.Context) error {
	endOffset, err := w.offsetManager.PartitionOffset(ctx, w.partitionID, partition.KafkaEndOffset)
	if err != nil {
		return fmt.Errorf("failed to get end offset: %w", err)
	}
	w.lastProducedOffset.Store(endOffset)
	return nil
}
