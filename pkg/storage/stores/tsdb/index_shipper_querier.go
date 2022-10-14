package tsdb

import (
	"context"
	"fmt"
	"math"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/config"
	shipper_index "github.com/grafana/loki/pkg/storage/stores/indexshipper/index"
	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
)

type indexShipperIterator interface {
	ForEach(ctx context.Context, tableName, userID string, doneChan <-chan struct{}, callback shipper_index.ForEachIndexCallback) error
}

// indexShipperQuerier is used for querying index from the shipper.
type indexShipperQuerier struct {
	shipper     indexShipperIterator
	chunkFilter chunk.RequestChunkFilterer
	tableRanges config.TableRanges
}

func newIndexShipperQuerier(shipper indexShipperIterator, tableRanges config.TableRanges) Index {
	return &indexShipperQuerier{shipper: shipper, tableRanges: tableRanges}
}

func (i *indexShipperQuerier) indices(ctx context.Context, from, through model.Time, user string, doneChan <-chan struct{}) (Index, error) {
	var indices []Index

	// Ensure we query both per tenant and multitenant TSDBs
	idxBuckets := indexBuckets(from, through, i.tableRanges)
	for _, bkt := range idxBuckets {
		if err := i.shipper.ForEach(ctx, bkt, user, doneChan, func(multitenant bool, idx shipper_index.Index) error {
			impl, ok := idx.(Index)
			if !ok {
				return fmt.Errorf("unexpected shipper index type: %T", idx)
			}
			if multitenant {
				indices = append(indices, NewMultiTenantIndex(impl))
			} else {
				indices = append(indices, impl)
			}
			return nil
		}); err != nil {
			return nil, err
		}

	}

	if len(indices) == 0 {
		return NoopIndex{}, nil
	}
	idx, err := NewMultiIndex(indices...)
	if err != nil {
		return nil, err
	}

	if i.chunkFilter != nil {
		idx.SetChunkFilterer(i.chunkFilter)
	}
	return idx, nil
}

// TODO(owen-d): how to better implement this?
// setting 0->maxint will force the tsdbmanager to always query
// underlying tsdbs, which is safe, but can we optimize this?
func (i *indexShipperQuerier) Bounds() (model.Time, model.Time) {
	return 0, math.MaxInt64
}

func (i *indexShipperQuerier) SetChunkFilterer(chunkFilter chunk.RequestChunkFilterer) {
	i.chunkFilter = chunkFilter
}

// Close implements Index.Close, but we offload this responsibility
// to the index shipper
func (i *indexShipperQuerier) Close() error {
	return nil
}

func (i *indexShipperQuerier) GetChunkRefs(ctx context.Context, userID string, from, through model.Time, res []ChunkRef, shard *index.ShardAnnotation, matchers ...*labels.Matcher) ([]ChunkRef, error) {
	doneChan := make(chan struct{})
	defer close(doneChan)

	idx, err := i.indices(ctx, from, through, userID, doneChan)
	if err != nil {
		return nil, err
	}
	return idx.GetChunkRefs(ctx, userID, from, through, res, shard, matchers...)
}

func (i *indexShipperQuerier) Series(ctx context.Context, userID string, from, through model.Time, res []Series, shard *index.ShardAnnotation, matchers ...*labels.Matcher) ([]Series, error) {
	doneChan := make(chan struct{})
	defer close(doneChan)

	idx, err := i.indices(ctx, from, through, userID, doneChan)
	if err != nil {
		return nil, err
	}
	return idx.Series(ctx, userID, from, through, res, shard, matchers...)
}

func (i *indexShipperQuerier) LabelNames(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]string, error) {
	doneChan := make(chan struct{})
	defer close(doneChan)

	idx, err := i.indices(ctx, from, through, userID, doneChan)
	if err != nil {
		return nil, err
	}
	return idx.LabelNames(ctx, userID, from, through, matchers...)
}

func (i *indexShipperQuerier) LabelValues(ctx context.Context, userID string, from, through model.Time, name string, matchers ...*labels.Matcher) ([]string, error) {
	doneChan := make(chan struct{})
	defer close(doneChan)

	idx, err := i.indices(ctx, from, through, userID, doneChan)
	if err != nil {
		return nil, err
	}
	return idx.LabelValues(ctx, userID, from, through, name, matchers...)
}

func (i *indexShipperQuerier) Stats(ctx context.Context, userID string, from, through model.Time, acc IndexStatsAccumulator, shard *index.ShardAnnotation, shouldIncludeChunk shouldIncludeChunk, matchers ...*labels.Matcher) error {
	doneChan := make(chan struct{})
	defer close(doneChan)

	idx, err := i.indices(ctx, from, through, userID, doneChan)
	if err != nil {
		return err
	}

	return idx.Stats(ctx, userID, from, through, acc, shard, shouldIncludeChunk, matchers...)
}
