package stores

import (
	"context"
	"sort"

	"github.com/grafana/loki/pkg/storage/stores/index/seriesvolume"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/fetcher"
	"github.com/grafana/loki/pkg/storage/stores/index"
	"github.com/grafana/loki/pkg/storage/stores/index/stats"
	"github.com/grafana/loki/pkg/util"
)

type ChunkWriter interface {
	Put(ctx context.Context, chunks []chunk.Chunk) error
	PutOne(ctx context.Context, from, through model.Time, chunk chunk.Chunk) error
}

type ChunkFetcherProvider interface {
	GetChunkFetcher(tm model.Time) *fetcher.Fetcher
}

type ChunkFetcher interface {
	GetChunks(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([][]chunk.Chunk, []*fetcher.Fetcher, error)
}

type Store interface {
	index.BaseReader
	index.StatsReader
	index.Filterable
	ChunkWriter
	ChunkFetcher
	ChunkFetcherProvider
	Stop()
}

// CompositeStore is a Store which delegates to various stores depending
// on when they were activated.
type CompositeStore struct {
	limits StoreLimits
	stores []compositeStoreEntry
}

// Ensure interface implementation of CompositeStore
var _ Store = &CompositeStore{}

// NewCompositeStore creates a new Store which delegates to different stores depending
// on time.
func NewCompositeStore(limits StoreLimits) *CompositeStore {
	return &CompositeStore{
		stores: make([]compositeStoreEntry, 0),
		limits: limits,
	}
}

func (c *CompositeStore) AddStore(start model.Time, fetcher *fetcher.Fetcher, index index.Reader, writer ChunkWriter, stop func()) {
	c.stores = append(c.stores, compositeStoreEntry{
		start: start,
		Store: &storeEntry{
			fetcher:     fetcher,
			indexReader: index,
			ChunkWriter: writer,
			limits:      c.limits,
			stop:        stop,
		},
	})
}

func (c *CompositeStore) Stores() []Store {
	var stores []Store
	for _, store := range c.stores {
		stores = append(stores, store.Store)
	}
	return stores
}

func (c CompositeStore) Put(ctx context.Context, chunks []chunk.Chunk) error {
	for _, chunk := range chunks {
		err := c.forStores(ctx, chunk.From, chunk.Through, func(innerCtx context.Context, from, through model.Time, store Store) error {
			return store.PutOne(innerCtx, from, through, chunk)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (c CompositeStore) PutOne(ctx context.Context, from, through model.Time, chunk chunk.Chunk) error {
	return c.forStores(ctx, from, through, func(innerCtx context.Context, from, through model.Time, store Store) error {
		return store.PutOne(innerCtx, from, through, chunk)
	})
}

func (c CompositeStore) SetChunkFilterer(chunkFilter chunk.RequestChunkFilterer) {
	for _, store := range c.stores {
		store.Store.SetChunkFilterer(chunkFilter)
	}
}

func (c CompositeStore) GetSeries(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]labels.Labels, error) {
	var results []labels.Labels
	found := map[uint64]struct{}{}
	err := c.forStores(ctx, from, through, func(innerCtx context.Context, from, through model.Time, store Store) error {
		series, err := store.GetSeries(innerCtx, userID, from, through, matchers...)
		if err != nil {
			return err
		}
		for _, s := range series {
			if _, ok := found[s.Hash()]; !ok {
				results = append(results, s)
				found[s.Hash()] = struct{}{}
			}
		}
		return nil
	})
	sort.Slice(results, func(i, j int) bool {
		return labels.Compare(results[i], results[j]) < 0
	})
	return results, err
}

// LabelValuesForMetricName retrieves all label values for a single label name and metric name.
func (c CompositeStore) LabelValuesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string, labelName string, matchers ...*labels.Matcher) ([]string, error) {
	var result util.UniqueStrings
	err := c.forStores(ctx, from, through, func(innerCtx context.Context, from, through model.Time, store Store) error {
		labelValues, err := store.LabelValuesForMetricName(innerCtx, userID, from, through, metricName, labelName, matchers...)
		if err != nil {
			return err
		}
		result.Add(labelValues...)
		return nil
	})
	return result.Strings(), err
}

// LabelNamesForMetricName retrieves all label names for a metric name.
func (c CompositeStore) LabelNamesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string) ([]string, error) {
	var result util.UniqueStrings
	err := c.forStores(ctx, from, through, func(innerCtx context.Context, from, through model.Time, store Store) error {
		labelNames, err := store.LabelNamesForMetricName(innerCtx, userID, from, through, metricName)
		if err != nil {
			return err
		}
		result.Add(labelNames...)
		return nil
	})
	return result.Strings(), err
}

func (c CompositeStore) GetChunks(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([][]chunk.Chunk, []*fetcher.Fetcher, error) {
	chunkIDs := [][]chunk.Chunk{}
	fetchers := []*fetcher.Fetcher{}
	err := c.forStores(ctx, from, through, func(innerCtx context.Context, from, through model.Time, store Store) error {
		ids, fetcher, err := store.GetChunks(innerCtx, userID, from, through, matchers...)
		if err != nil {
			return err
		}

		// Skip it if there are no chunks.
		if len(ids) == 0 {
			return nil
		}

		chunkIDs = append(chunkIDs, ids...)
		fetchers = append(fetchers, fetcher...)
		return nil
	})
	return chunkIDs, fetchers, err
}

func (c CompositeStore) Stats(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) (*stats.Stats, error) {
	xs := make([]*stats.Stats, 0, len(c.stores))
	err := c.forStores(ctx, from, through, func(innerCtx context.Context, from, through model.Time, store Store) error {
		x, err := store.Stats(innerCtx, userID, from, through, matchers...)
		xs = append(xs, x)
		return err
	})

	if err != nil {
		return nil, err

	}
	res := stats.MergeStats(xs...)
	return &res, err
}

func (c CompositeStore) Volume(ctx context.Context, userID string, from, through model.Time, limit int32, targetLabels []string, aggregateBy string, matchers ...*labels.Matcher) (*logproto.VolumeResponse, error) {
	volumes := make([]*logproto.VolumeResponse, 0, len(c.stores))
	err := c.forStores(ctx, from, through, func(innerCtx context.Context, from, through model.Time, store Store) error {
		volume, err := store.Volume(innerCtx, userID, from, through, limit, targetLabels, aggregateBy, matchers...)
		volumes = append(volumes, volume)
		return err
	})

	if err != nil {
		return nil, err
	}

	res := seriesvolume.Merge(volumes, limit)
	return res, err
}

func (c CompositeStore) GetChunkFetcher(tm model.Time) *fetcher.Fetcher {
	// find the schema with the lowest start _after_ tm
	j := sort.Search(len(c.stores), func(j int) bool {
		return c.stores[j].start > tm
	})

	// reduce it by 1 because we want a schema with start <= tm
	j--

	if 0 <= j && j < len(c.stores) {
		return c.stores[j].GetChunkFetcher(tm)
	}

	return nil
}

func (c CompositeStore) Stop() {
	for _, store := range c.stores {
		store.Stop()
	}
}

func (c CompositeStore) forStores(ctx context.Context, from, through model.Time, callback func(innerCtx context.Context, from, through model.Time, store Store) error) error {
	if len(c.stores) == 0 {
		return nil
	}

	// first, find the schema with the highest start _before or at_ from
	i := sort.Search(len(c.stores), func(i int) bool {
		return c.stores[i].start > from
	})
	if i > 0 {
		i--
	} else {
		// This could happen if we get passed a sample from before 1970.
		i = 0
		from = c.stores[0].start
	}

	// next, find the schema with the lowest start _after_ through
	j := sort.Search(len(c.stores), func(j int) bool {
		return c.stores[j].start > through
	})

	min := func(a, b model.Time) model.Time {
		if a < b {
			return a
		}
		return b
	}

	start := from
	for ; i < j; i++ {
		nextSchemaStarts := model.Latest
		if i+1 < len(c.stores) {
			nextSchemaStarts = c.stores[i+1].start
		}

		end := min(through, nextSchemaStarts-1)
		err := callback(ctx, start, end, c.stores[i])
		if err != nil {
			return err
		}

		start = nextSchemaStarts
	}

	return nil
}
