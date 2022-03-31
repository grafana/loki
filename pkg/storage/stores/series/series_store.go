package series

import (
	"context"
	"time"

	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/chunk/fetcher"
	"github.com/grafana/loki/pkg/storage/chunk/index"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/util/spanlogger"
)

var (
	indexLookupsPerQuery = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "loki",
		Name:      "chunk_store_index_lookups_per_query",
		Help:      "Distribution of #index lookups per query.",
		Buckets:   prometheus.ExponentialBuckets(1, 2, 5),
	})
	preIntersectionPerQuery = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "loki",
		Name:      "chunk_store_series_pre_intersection_per_query",
		Help:      "Distribution of #series (pre intersection) per query.",
		// A reasonable upper bound is around 100k - 10*(8^(6-1)) = 327k.
		Buckets: prometheus.ExponentialBuckets(10, 8, 6),
	})
	postIntersectionPerQuery = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "loki",
		Name:      "chunk_store_series_post_intersection_per_query",
		Help:      "Distribution of #series (post intersection) per query.",
		// A reasonable upper bound is around 100k - 10*(8^(6-1)) = 327k.
		Buckets: prometheus.ExponentialBuckets(10, 8, 6),
	})
	chunksPerQuery = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "loki",
		Name:      "chunk_store_chunks_per_query",
		Help:      "Distribution of #chunks per query.",
		// For 100k series for 7 week, could be 1.2m - 10*(8^(7-1)) = 2.6m.
		Buckets: prometheus.ExponentialBuckets(10, 8, 7),
	})
	dedupedChunksTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "chunk_store_deduped_chunks_total",
		Help:      "Count of chunks which were not stored because they have already been stored by another replica.",
	})

	indexEntriesPerChunk = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "loki",
		Name:      "chunk_store_index_entries_per_chunk",
		Help:      "Number of entries written to storage per chunk.",
		Buckets:   prometheus.ExponentialBuckets(1, 2, 5),
	})
)

// StoreLimits helps get Limits specific to Queries for Stores
type StoreLimits interface {
	MaxChunksPerQueryFromStore(userID string) int
	MaxQueryLength(userID string) time.Duration
}

// seriesStore implements Store
type Store struct {
	schema           index.SeriesStoreSchema
	writeDedupeCache cache.Cache
	limits           StoreLimits
	fetcher          *fetcher.Fetcher
	schemaCfg        config.SchemaConfig
	cfg              config.ChunkStoreConfig

	index       Index
	indexClient index.IndexClient
}

func NewStore(cfg config.ChunkStoreConfig, scfg config.SchemaConfig, schema index.SeriesStoreSchema, idx index.IndexClient, limits StoreLimits, chunksCache, writeDedupeCache cache.Cache) (*Store, error) {
	fetcher, err := fetcher.New(chunksCache, cfg.ChunkCacheStubs(), scfg, chunks, cfg.ChunkCacheConfig.AsyncCacheWriteBackConcurrency, cfg.ChunkCacheConfig.AsyncCacheWriteBackBufferSize)
	if err != nil {
		return nil, err
	}
	indexStore := newSeriesIndexStore(scfg, schema, idx, fetcher)
	return &Store{
		cfg:              cfg,
		schema:           schema,
		limits:           limits,
		writeDedupeCache: writeDedupeCache,
		fetcher:          fetcher,
		indexClient:      idx,
		index:            indexStore,
		schemaCfg:        scfg,
	}, nil
}

func (c *Store) GetSeries(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]logproto.SeriesIdentifier, error) {
	return c.index.GetSeries(ctx, userID, from, through, matchers...)
}

// LabelNamesForMetricName retrieves all label names for a metric name.
func (c *Store) LabelNamesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string) ([]string, error) {
	log, ctx := spanlogger.New(ctx, "SeriesStore.LabelNamesForMetricName")
	defer log.Span.Finish()

	shortcut, err := c.validateQueryTimeRange(ctx, userID, &from, &through)
	if err != nil {
		return nil, err
	} else if shortcut {
		return nil, nil
	}
	level.Debug(log).Log("metric", metricName)

	return c.index.LabelNamesForMetricName(ctx, userID, from, through, metricName)
}

func (c *Store) LabelValuesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string, labelName string, matchers ...*labels.Matcher) ([]string, error) {
	log, ctx := spanlogger.New(ctx, "SeriesStore.LabelValuesForMetricName")
	defer log.Span.Finish()

	shortcut, err := c.validateQueryTimeRange(ctx, userID, &from, &through)
	if err != nil {
		return nil, err
	} else if shortcut {
		return nil, nil
	}

	return c.index.LabelValuesForMetricName(ctx, userID, from, through, metricName, labelName, matchers...)
}

// Stop any background goroutines (ie in the cache.)
func (c *Store) Stop() {
	c.fetcher.storage.Stop()
	c.fetcher.Stop()
	c.indexClient.Stop()
}

func (c *Store) GetChunkFetcher(_ model.Time) *fetcher.Fetcher {
	return c.fetcher
}
