package tsdb

import (
	"context"
	"fmt"
	"math"
	"path"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/config"
	shipperindex "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/index"
	tsdbindex "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

type indexShipperIterator interface {
	ForEachConcurrent(ctx context.Context, tableName, userID string, callback shipperindex.ForEachIndexCallback) error
}

// indexShipperQuerier is used for querying index from the shipper.
type indexShipperQuerier struct {
	shipper       indexShipperIterator
	chunkFilterMu sync.Mutex
	chunkFilter   chunk.RequestChunkFilterer
	tableRange    config.TableRange
	// skipSuperseded makes Stats and Volume skip the files a tenant's newest
	// compacted file already contains. See supersededFilter.
	skipSuperseded bool
}

func newIndexShipperQuerier(shipper indexShipperIterator, tableRange config.TableRange, skipSuperseded bool) Index {
	return &indexShipperQuerier{shipper: shipper, tableRange: tableRange, skipSuperseded: skipSuperseded}
}

// supersededMultiTenantMargin is how much older than a tenant's newest
// compacted file a multi-tenant file has to be named for Stats and Volume to
// treat it as already compacted into it. A compacted file is named after the
// time it was built, which can be tens of minutes after the compactor listed
// its sources on big tables. A multi-tenant file is named after the start of
// its head, about one head rotation plus an upload interval before it can be
// listed. Within the margin, files the compactor read are still counted
// twice. Beyond it, a file uploaded too late to be read, e.g. after an
// ingester restart, is skipped until the next compaction contains it.
const supersededMultiTenantMargin = time.Hour

// supersededFilter reports which files of a table Stats and Volume can skip
// for userID because its newest compacted file already contains them. Every
// compactor pass over a table, including retention and deletes, first
// compacts all the multi-tenant files it lists into a new file for each tenant
// in them, merged with that tenant's compacted files. It uploads those before
// deleting the sources, so until the deletion is synced a querier sees the
// same chunks more than once. Unlike GetChunkRefs, Stats and Volume don't dedupe chunks across
// files, so they would count those chunks again.
//
// Multi-tenant files not compacted yet are still summed: replicated
// ingesters upload copies of the same streams, so the most recent data,
// since the last compaction, is over-counted by up to the replication factor.
// This is accepted since Stats and Volume are estimates.
//
// It returns nil if userID has no compacted file in the table.
func (i *indexShipperQuerier) supersededFilter(ctx context.Context, tableName, userID string) (func(multitenant bool, name string) bool, error) {
	var (
		mtx    sync.Mutex
		newest SingleTenantTSDBIdentifier
		found  bool
	)
	if err := i.shipper.ForEachConcurrent(ctx, tableName, userID, func(multitenant bool, idx shipperindex.Index) error {
		if multitenant {
			return nil
		}
		id, ok := ParseSingleTenantTSDBPath(path.Base(idx.Name()))
		if !ok {
			return nil
		}

		mtx.Lock()
		defer mtx.Unlock()
		if !found || id.TS.After(newest.TS) {
			newest, found = id, true
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}

	multiTenantCutoff := newest.TS.Add(-supersededMultiTenantMargin)
	return func(multitenant bool, name string) bool {
		if multitenant {
			id, ok := parseMultitenantTSDBNameFromBase(path.Base(name))
			return ok && id.Ts.Before(multiTenantCutoff)
		}
		id, ok := ParseSingleTenantTSDBPath(path.Base(name))
		return ok && id.TS.Before(newest.TS)
	}, nil
}

type indexIterFunc func(func(context.Context, Index) error) error

func (i indexIterFunc) For(_ context.Context, _ int, f func(context.Context, Index) error) error {
	return i(f)
}

// indices returns the index files for the query. With skipSuperseded, files a
// tenant's newest compacted file already contains are left out, which only
// suits estimates: a file skipped by mistake loses its chunks.
func (i *indexShipperQuerier) indices(ctx context.Context, from, through model.Time, user string, skipSuperseded bool) (Index, error) {
	itr := indexIterFunc(func(f func(context.Context, Index) error) error {
		// Ensure we query both per tenant and multitenant TSDBs
		idxBuckets := IndexBuckets(from, through, []config.TableRange{i.tableRange})
		for _, bkt := range idxBuckets {
			var superseded func(multitenant bool, name string) bool
			if skipSuperseded {
				var err error
				if superseded, err = i.supersededFilter(ctx, bkt.Prefix, user); err != nil {
					return err
				}
			}

			if err := i.shipper.ForEachConcurrent(ctx, bkt.Prefix, user, func(multitenant bool, idx shipperindex.Index) error {
				if superseded != nil && superseded(multitenant, idx.Name()) {
					return nil
				}

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

	if f := i.getChunkFilter(); f != nil {
		idx.SetChunkFilterer(f)
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
	i.chunkFilterMu.Lock()
	i.chunkFilter = chunkFilter
	i.chunkFilterMu.Unlock()
}

func (i *indexShipperQuerier) getChunkFilter() chunk.RequestChunkFilterer {
	i.chunkFilterMu.Lock()
	f := i.chunkFilter
	i.chunkFilterMu.Unlock()
	return f
}

// Close implements Index.Close, but we offload this responsibility
// to the index shipper
func (i *indexShipperQuerier) Close() error {
	return nil
}

func (i *indexShipperQuerier) GetChunkRefs(ctx context.Context, userID string, from, through model.Time, res []logproto.ChunkRefWithSizingInfo, fpFilter tsdbindex.FingerprintFilter, matchers ...*labels.Matcher) ([]logproto.ChunkRefWithSizingInfo, error) {
	idx, err := i.indices(ctx, from, through, userID, false)
	if err != nil {
		return nil, err
	}
	return idx.GetChunkRefs(ctx, userID, from, through, res, fpFilter, matchers...)
}

func (i *indexShipperQuerier) Series(ctx context.Context, userID string, from, through model.Time, res []Series, fpFilter tsdbindex.FingerprintFilter, matchers ...*labels.Matcher) ([]Series, error) {
	idx, err := i.indices(ctx, from, through, userID, false)
	if err != nil {
		return nil, err
	}
	return idx.Series(ctx, userID, from, through, res, fpFilter, matchers...)
}

func (i *indexShipperQuerier) LabelNames(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]string, error) {
	idx, err := i.indices(ctx, from, through, userID, false)
	if err != nil {
		return nil, err
	}
	return idx.LabelNames(ctx, userID, from, through, matchers...)
}

func (i *indexShipperQuerier) LabelValues(ctx context.Context, userID string, from, through model.Time, name string, matchers ...*labels.Matcher) ([]string, error) {
	idx, err := i.indices(ctx, from, through, userID, false)
	if err != nil {
		return nil, err
	}
	return idx.LabelValues(ctx, userID, from, through, name, matchers...)
}

func (i *indexShipperQuerier) Stats(ctx context.Context, userID string, from, through model.Time, acc IndexStatsAccumulator, fpFilter tsdbindex.FingerprintFilter, shouldIncludeChunk shouldIncludeChunk, matchers ...*labels.Matcher) error {
	idx, err := i.indices(ctx, from, through, userID, i.skipSuperseded)
	if err != nil {
		return err
	}

	return idx.Stats(ctx, userID, from, through, acc, fpFilter, shouldIncludeChunk, matchers...)
}

func (i *indexShipperQuerier) Volume(ctx context.Context, userID string, from, through model.Time, acc VolumeAccumulator, fpFilter tsdbindex.FingerprintFilter, shouldIncludeChunk shouldIncludeChunk, targetLabels []string, aggregateBy string, matchers ...*labels.Matcher) error {
	idx, err := i.indices(ctx, from, through, userID, i.skipSuperseded)
	if err != nil {
		return err
	}

	return idx.Volume(ctx, userID, from, through, acc, fpFilter, shouldIncludeChunk, targetLabels, aggregateBy, matchers...)
}

func (i *indexShipperQuerier) ForSeries(ctx context.Context, userID string, fpFilter tsdbindex.FingerprintFilter, from, through model.Time, fn func(labels.Labels, model.Fingerprint, []tsdbindex.ChunkMeta) (stop bool), matchers ...*labels.Matcher) error {
	idx, err := i.indices(ctx, from, through, userID, false)
	if err != nil {
		return err
	}

	return idx.ForSeries(ctx, userID, fpFilter, from, through, fn, matchers...)
}

type resultAccumulator[T any] struct {
	mtx   sync.Mutex
	items []T
	merge func(xs []T) (T, error)
}

// TODO(owen-d): make generic to avoid casting at runtime.
func newResultAccumulator[T any](merge func(xs []T) (T, error)) *resultAccumulator[T] {
	return &resultAccumulator[T]{
		merge: merge,
	}
}

func (acc *resultAccumulator[T]) Add(item T) {
	acc.mtx.Lock()
	defer acc.mtx.Unlock()
	acc.items = append(acc.items, item)

}

func (acc *resultAccumulator[T]) Merge() (res T, err error) {
	acc.mtx.Lock()
	defer acc.mtx.Unlock()

	ln := len(acc.items)
	if ln == 0 {
		return res, ErrEmptyAccumulator
	}

	if ln == 1 {
		return acc.items[0], nil
	}

	return acc.merge(acc.items)
}

var ErrEmptyAccumulator = errors.New("no items in result accumulator")
