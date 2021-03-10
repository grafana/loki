package storegateway

import (
	"context"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/oklog/ulid"
	"github.com/thanos-io/thanos/pkg/block"
	"github.com/thanos-io/thanos/pkg/block/metadata"
	"github.com/thanos-io/thanos/pkg/extprom"
	"github.com/thanos-io/thanos/pkg/objstore"

	"github.com/cortexproject/cortex/pkg/storage/tsdb/bucketindex"
)

type MetadataFilterWithBucketIndex interface {
	// FilterWithBucketIndex is like Thanos MetadataFilter.Filter() but it provides in input the bucket index too.
	FilterWithBucketIndex(ctx context.Context, metas map[ulid.ULID]*metadata.Meta, idx *bucketindex.Index, synced *extprom.TxGaugeVec) error
}

// IgnoreDeletionMarkFilter is like the Thanos IgnoreDeletionMarkFilter, but it also implements
// the MetadataFilterWithBucketIndex interface.
type IgnoreDeletionMarkFilter struct {
	upstream *block.IgnoreDeletionMarkFilter

	delay           time.Duration
	deletionMarkMap map[ulid.ULID]*metadata.DeletionMark
}

// NewIgnoreDeletionMarkFilter creates IgnoreDeletionMarkFilter.
func NewIgnoreDeletionMarkFilter(logger log.Logger, bkt objstore.InstrumentedBucketReader, delay time.Duration, concurrency int) *IgnoreDeletionMarkFilter {
	return &IgnoreDeletionMarkFilter{
		upstream: block.NewIgnoreDeletionMarkFilter(logger, bkt, delay, concurrency),
		delay:    delay,
	}
}

// DeletionMarkBlocks returns blocks that were marked for deletion.
func (f *IgnoreDeletionMarkFilter) DeletionMarkBlocks() map[ulid.ULID]*metadata.DeletionMark {
	// If the cached deletion marks exist it means the filter function was called with the bucket
	// index, so it's safe to return it.
	if f.deletionMarkMap != nil {
		return f.deletionMarkMap
	}

	return f.upstream.DeletionMarkBlocks()
}

// Filter implements block.MetadataFilter.
func (f *IgnoreDeletionMarkFilter) Filter(ctx context.Context, metas map[ulid.ULID]*metadata.Meta, synced *extprom.TxGaugeVec) error {
	return f.upstream.Filter(ctx, metas, synced)
}

// FilterWithBucketIndex implements MetadataFilterWithBucketIndex.
func (f *IgnoreDeletionMarkFilter) FilterWithBucketIndex(_ context.Context, metas map[ulid.ULID]*metadata.Meta, idx *bucketindex.Index, synced *extprom.TxGaugeVec) error {
	// Build a map of block deletion marks
	marks := make(map[ulid.ULID]*metadata.DeletionMark, len(idx.BlockDeletionMarks))
	for _, mark := range idx.BlockDeletionMarks {
		marks[mark.ID] = mark.ThanosDeletionMark()
	}

	// Keep it cached.
	f.deletionMarkMap = marks

	for _, mark := range marks {
		if _, ok := metas[mark.ID]; !ok {
			continue
		}

		if time.Since(time.Unix(mark.DeletionTime, 0)).Seconds() > f.delay.Seconds() {
			synced.WithLabelValues(block.MarkedForDeletionMeta).Inc()
			delete(metas, mark.ID)
		}
	}

	return nil
}
