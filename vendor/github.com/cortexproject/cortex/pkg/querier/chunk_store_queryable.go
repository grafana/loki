package querier

import (
	"context"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/chunkenc"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/querier/chunkstore"
	seriesset "github.com/cortexproject/cortex/pkg/querier/series"
	"github.com/cortexproject/cortex/pkg/tenant"
)

type chunkIteratorFunc func(chunks []chunk.Chunk, from, through model.Time) chunkenc.Iterator

func newChunkStoreQueryable(store chunkstore.ChunkStore, chunkIteratorFunc chunkIteratorFunc) storage.Queryable {
	return storage.QueryableFunc(func(ctx context.Context, mint, maxt int64) (storage.Querier, error) {
		return &chunkStoreQuerier{
			store:             store,
			chunkIteratorFunc: chunkIteratorFunc,
			ctx:               ctx,
			mint:              mint,
			maxt:              maxt,
		}, nil
	})
}

type chunkStoreQuerier struct {
	store             chunkstore.ChunkStore
	chunkIteratorFunc chunkIteratorFunc
	ctx               context.Context
	mint, maxt        int64
}

// Select implements storage.Querier interface.
// The bool passed is ignored because the series is always sorted.
func (q *chunkStoreQuerier) Select(_ bool, sp *storage.SelectHints, matchers ...*labels.Matcher) storage.SeriesSet {
	userID, err := tenant.TenantID(q.ctx)
	if err != nil {
		return storage.ErrSeriesSet(err)
	}

	// We will hit this for /series lookup when -querier.query-store-for-labels-enabled is set.
	// If we don't skip here, it'll make /series lookups extremely slow as all the chunks will be loaded.
	// That flag is only to be set with blocks storage engine, and this is a protective measure.
	if sp == nil || sp.Func == "series" {
		return storage.EmptySeriesSet()
	}

	chunks, err := q.store.Get(q.ctx, userID, model.Time(sp.Start), model.Time(sp.End), matchers...)
	if err != nil {
		return storage.ErrSeriesSet(err)
	}

	return partitionChunks(chunks, q.mint, q.maxt, q.chunkIteratorFunc)
}

// Series in the returned set are sorted alphabetically by labels.
func partitionChunks(chunks []chunk.Chunk, mint, maxt int64, iteratorFunc chunkIteratorFunc) storage.SeriesSet {
	chunksBySeries := map[string][]chunk.Chunk{}
	for _, c := range chunks {
		key := client.LabelsToKeyString(c.Metric)
		chunksBySeries[key] = append(chunksBySeries[key], c)
	}

	series := make([]storage.Series, 0, len(chunksBySeries))
	for i := range chunksBySeries {
		series = append(series, &chunkSeries{
			labels:            chunksBySeries[i][0].Metric,
			chunks:            chunksBySeries[i],
			chunkIteratorFunc: iteratorFunc,
			mint:              mint,
			maxt:              maxt,
		})
	}

	return seriesset.NewConcreteSeriesSet(series)
}

func (q *chunkStoreQuerier) LabelValues(name string, labels ...*labels.Matcher) ([]string, storage.Warnings, error) {
	return nil, nil, nil
}

func (q *chunkStoreQuerier) LabelNames() ([]string, storage.Warnings, error) {
	return nil, nil, nil
}

func (q *chunkStoreQuerier) Close() error {
	return nil
}

// Implements SeriesWithChunks
type chunkSeries struct {
	labels            labels.Labels
	chunks            []chunk.Chunk
	chunkIteratorFunc chunkIteratorFunc
	mint, maxt        int64
}

func (s *chunkSeries) Labels() labels.Labels {
	return s.labels
}

// Iterator returns a new iterator of the data of the series.
func (s *chunkSeries) Iterator() chunkenc.Iterator {
	return s.chunkIteratorFunc(s.chunks, model.Time(s.mint), model.Time(s.maxt))
}

// Chunks implements SeriesWithChunks interface.
func (s *chunkSeries) Chunks() []chunk.Chunk {
	return s.chunks
}
