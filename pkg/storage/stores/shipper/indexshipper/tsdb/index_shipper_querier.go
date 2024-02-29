package tsdb

import (
	"context"
	"fmt"
	"math"
	"sync"

	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/config"
	shipperindex "github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/index"
	tsdbindex "github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

type indexShipperIterator interface {
	ForEachConcurrent(ctx context.Context, tableName, userID string, callback shipperindex.ForEachIndexCallback) error
}

// indexShipperQuerier is used for querying index from the shipper.
type indexShipperQuerier struct {
	shipper     indexShipperIterator
	chunkFilter chunk.RequestChunkFilterer
	tableRange  config.TableRange
}

func newIndexShipperQuerier(shipper indexShipperIterator, tableRange config.TableRange) Index {
	return &indexShipperQuerier{shipper: shipper, tableRange: tableRange}
}

type indexIterFunc func(func(context.Context, Index) error) error

func (i indexIterFunc) For(_ context.Context, _ int, f func(context.Context, Index) error) error {
	return i(f)
}

func (i *indexShipperQuerier) indices(ctx context.Context, from, through model.Time, user string) (Index, error) {
	itr := indexIterFunc(func(f func(context.Context, Index) error) error {
		// Ensure we query both per tenant and multitenant TSDBs
		idxBuckets := indexBuckets(from, through, []config.TableRange{i.tableRange})
		for _, bkt := range idxBuckets {
			if err := i.shipper.ForEachConcurrent(ctx, bkt.prefix, user, func(multitenant bool, idx shipperindex.Index) error {
				impl, ok := idx.(Index)
				if !ok {
					return fmt.Errorf("unexpected shipper index type: %T", idx)
				}
				if multitenant {
					impl = NewMultiTenantIndex(impl)
				}

				return f(ctx, impl)
			}); err != nil {
				return err
			}
		}
		return nil
	})

	idx := NewMultiIndex(itr)

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

func (i *indexShipperQuerier) GetChunkRefs(ctx context.Context, userID string, from, through model.Time, res []ChunkRef, fpFilter tsdbindex.FingerprintFilter, matchers ...*labels.Matcher) ([]ChunkRef, error) {
	idx, err := i.indices(ctx, from, through, userID)
	if err != nil {
		return nil, err
	}
	return idx.GetChunkRefs(ctx, userID, from, through, res, fpFilter, matchers...)
}

func (i *indexShipperQuerier) Series(ctx context.Context, userID string, from, through model.Time, res []Series, fpFilter tsdbindex.FingerprintFilter, matchers ...*labels.Matcher) ([]Series, error) {
	idx, err := i.indices(ctx, from, through, userID)
	if err != nil {
		return nil, err
	}
	return idx.Series(ctx, userID, from, through, res, fpFilter, matchers...)
}

func (i *indexShipperQuerier) LabelNames(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]string, error) {
	idx, err := i.indices(ctx, from, through, userID)
	if err != nil {
		return nil, err
	}
	return idx.LabelNames(ctx, userID, from, through, matchers...)
}

func (i *indexShipperQuerier) LabelValues(ctx context.Context, userID string, from, through model.Time, name string, matchers ...*labels.Matcher) ([]string, error) {
	idx, err := i.indices(ctx, from, through, userID)
	if err != nil {
		return nil, err
	}
	return idx.LabelValues(ctx, userID, from, through, name, matchers...)
}

func (i *indexShipperQuerier) Stats(ctx context.Context, userID string, from, through model.Time, acc IndexStatsAccumulator, fpFilter tsdbindex.FingerprintFilter, shouldIncludeChunk shouldIncludeChunk, matchers ...*labels.Matcher) error {
	idx, err := i.indices(ctx, from, through, userID)
	if err != nil {
		return err
	}

	return idx.Stats(ctx, userID, from, through, acc, fpFilter, shouldIncludeChunk, matchers...)
}

func (i *indexShipperQuerier) Volume(ctx context.Context, userID string, from, through model.Time, acc VolumeAccumulator, fpFilter tsdbindex.FingerprintFilter, shouldIncludeChunk shouldIncludeChunk, targetLabels []string, aggregateBy string, matchers ...*labels.Matcher) error {
	idx, err := i.indices(ctx, from, through, userID)
	if err != nil {
		return err
	}

	return idx.Volume(ctx, userID, from, through, acc, fpFilter, shouldIncludeChunk, targetLabels, aggregateBy, matchers...)
}

type resultAccumulator struct {
	mtx   sync.Mutex
	items []interface{}
	merge func(xs []interface{}) (interface{}, error)
}

func newResultAccumulator(merge func(xs []interface{}) (interface{}, error)) *resultAccumulator {
	return &resultAccumulator{
		merge: merge,
	}
}

func (acc *resultAccumulator) Add(item interface{}) {
	acc.mtx.Lock()
	defer acc.mtx.Unlock()
	acc.items = append(acc.items, item)

}

func (acc *resultAccumulator) Merge() (interface{}, error) {
	acc.mtx.Lock()
	defer acc.mtx.Unlock()

	if len(acc.items) == 0 {
		return nil, ErrEmptyAccumulator
	}

	return acc.merge(acc.items)
}

var ErrEmptyAccumulator = errors.New("no items in result accumulator")
