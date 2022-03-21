package chunk

import (
	"context"
	"fmt"
	"time"

	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/util/spanlogger"
)

// CardinalityExceededError is returned when the user reads a row that
// is too large.
type CardinalityExceededError struct {
	MetricName, LabelName string
	Size, Limit           int32
}

func (e CardinalityExceededError) Error() string {
	return fmt.Sprintf("cardinality limit exceeded for %s{%s}; %d entries, more than limit of %d",
		e.MetricName, e.LabelName, e.Size, e.Limit)
}

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
)

// seriesStore implements Store
type seriesStore struct {
	schema           SeriesStoreSchema
	writeDedupeCache cache.Cache
	chunks           Client
	limits           StoreLimits
	fetcher          *Fetcher
	schemaCfg        SchemaConfig
	cfg              StoreConfig

	index       Index
	indexClient IndexClient
}

func newSeriesStore(cfg StoreConfig, scfg SchemaConfig, schema SeriesStoreSchema, index IndexClient, chunks Client, limits StoreLimits, chunksCache, writeDedupeCache cache.Cache) (Store, error) {
	if cfg.CacheLookupsOlderThan != 0 {
		schema = &schemaCaching{
			SeriesStoreSchema: schema,
			cacheOlderThan:    time.Duration(cfg.CacheLookupsOlderThan),
		}
	}
	fetcher, err := NewChunkFetcher(chunksCache, cfg.chunkCacheStubs, scfg, chunks, cfg.ChunkCacheConfig.AsyncCacheWriteBackConcurrency, cfg.ChunkCacheConfig.AsyncCacheWriteBackBufferSize)
	if err != nil {
		return nil, err
	}
	indexStore := newSeriesIndexStore(scfg, schema, index, fetcher)
	return &seriesStore{
		cfg:              cfg,
		schema:           schema,
		chunks:           chunks,
		limits:           limits,
		writeDedupeCache: writeDedupeCache,
		fetcher:          fetcher,
		indexClient:      index,
		index:            indexStore,
		schemaCfg:        scfg,
	}, nil
}

func (c *seriesStore) GetChunkRefs(ctx context.Context, userID string, from, through model.Time, allMatchers ...*labels.Matcher) ([][]Chunk, []*Fetcher, error) {
	if ctx.Err() != nil {
		return nil, nil, ctx.Err()
	}
	log, ctx := spanlogger.New(ctx, "SeriesStore.GetChunkRefs")
	defer log.Span.Finish()

	// Validate the query is within reasonable bounds.
	metricName, matchers, shortcut, err := c.validateQuery(ctx, userID, &from, &through, allMatchers)
	if err != nil {
		return nil, nil, err
	} else if shortcut {
		return nil, nil, nil
	}

	level.Debug(log).Log("metric", metricName)
	refs, err := c.index.GetChunkRefs(ctx, userID, from, through, matchers...)

	chunks := make([]Chunk, len(refs))
	for i, ref := range refs {
		chunks[i] = Chunk{
			ChunkRef: ref,
		}
	}

	return [][]Chunk{chunks}, []*Fetcher{c.fetcher}, err
}

func (c *seriesStore) GetSeries(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]logproto.SeriesIdentifier, error) {
	return c.index.GetSeries(ctx, userID, from, through, matchers...)
}

// LabelNamesForMetricName retrieves all label names for a metric name.
func (c *seriesStore) LabelNamesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string) ([]string, error) {
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

func (c *seriesStore) LabelValuesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName string, labelName string, matchers ...*labels.Matcher) ([]string, error) {
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

// Put implements Store
func (c *seriesStore) Put(ctx context.Context, chunks []Chunk) error {
	for _, chunk := range chunks {
		if err := c.PutOne(ctx, chunk.From, chunk.Through, chunk); err != nil {
			return err
		}
	}
	return nil
}

// PutOne implements Store
func (c *seriesStore) PutOne(ctx context.Context, from, through model.Time, chunk Chunk) error {
	log, ctx := spanlogger.New(ctx, "SeriesStore.PutOne")
	defer log.Finish()
	writeChunk := true

	// If this chunk is in cache it must already be in the database so we don't need to write it again
	found, _, _, _ := c.fetcher.cache.Fetch(ctx, []string{c.schemaCfg.ExternalKey(chunk)})

	if len(found) > 0 {
		writeChunk = false
		dedupedChunksTotal.Inc()
	}

	// If we dont have to write the chunk and DisableIndexDeduplication is false, we do not have to do anything.
	// If we dont have to write the chunk and DisableIndexDeduplication is true, we have to write index and not chunk.
	// Otherwise write both index and chunk.
	if !writeChunk && !c.cfg.DisableIndexDeduplication {
		return nil
	}

	chunks := []Chunk{chunk}

	writeReqs, keysToCache, err := c.calculateIndexEntries(ctx, from, through, chunk)
	if err != nil {
		return err
	}

	if oic, ok := c.fetcher.storage.(ObjectAndIndexClient); ok {
		chunks := chunks
		if !writeChunk {
			chunks = []Chunk{}
		}
		if err = oic.PutChunksAndIndex(ctx, chunks, writeReqs); err != nil {
			return err
		}
	} else {
		// chunk not found, write it.
		if writeChunk {
			err := c.fetcher.storage.PutChunks(ctx, chunks)
			if err != nil {
				return err
			}
		}
		if err := c.indexClient.BatchWrite(ctx, writeReqs); err != nil {
			return err
		}
	}

	// we already have the chunk in the cache so don't write it back to the cache.
	if writeChunk {
		if cacheErr := c.fetcher.writeBackCache(ctx, chunks); cacheErr != nil {
			level.Warn(log).Log("msg", "could not store chunks in chunk cache", "err", cacheErr)
		}
	}

	bufs := make([][]byte, len(keysToCache))
	err = c.writeDedupeCache.Store(ctx, keysToCache, bufs)
	if err != nil {
		level.Warn(log).Log("msg", "could not Store store in write dedupe cache", "err", err)
	}
	return nil
}

// calculateIndexEntries creates a set of batched WriteRequests for all the chunks it is given.
func (c *seriesStore) calculateIndexEntries(ctx context.Context, from, through model.Time, chunk Chunk) (WriteBatch, []string, error) {
	seenIndexEntries := map[string]struct{}{}
	entries := []IndexEntry{}

	metricName := chunk.Metric.Get(labels.MetricName)
	if metricName == "" {
		return nil, nil, fmt.Errorf("no MetricNameLabel for chunk")
	}

	keys, labelEntries, err := c.schema.GetCacheKeysAndLabelWriteEntries(from, through, chunk.UserID, metricName, chunk.Metric, c.schemaCfg.ExternalKey(chunk))
	if err != nil {
		return nil, nil, err
	}
	_, _, missing, _ := c.writeDedupeCache.Fetch(ctx, keys)
	// keys and labelEntries are matched in order, but Fetch() may
	// return missing keys in any order so check against all of them.
	for _, missingKey := range missing {
		for i, key := range keys {
			if key == missingKey {
				entries = append(entries, labelEntries[i]...)
			}
		}
	}

	chunkEntries, err := c.schema.GetChunkWriteEntries(from, through, chunk.UserID, metricName, chunk.Metric, c.schemaCfg.ExternalKey(chunk))
	if err != nil {
		return nil, nil, err
	}
	entries = append(entries, chunkEntries...)

	indexEntriesPerChunk.Observe(float64(len(entries)))

	// Remove duplicate entries based on tableName:hashValue:rangeValue
	result := c.indexClient.NewWriteBatch()
	for _, entry := range entries {
		key := fmt.Sprintf("%s:%s:%x", entry.TableName, entry.HashValue, entry.RangeValue)
		if _, ok := seenIndexEntries[key]; !ok {
			seenIndexEntries[key] = struct{}{}
			result.Add(entry.TableName, entry.HashValue, entry.RangeValue, entry.Value)
		}
	}

	return result, missing, nil
}
