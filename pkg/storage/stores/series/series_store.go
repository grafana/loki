package series

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
	"github.com/grafana/loki/pkg/storage/chunk/config"
	"github.com/grafana/loki/pkg/storage/chunk/encoding"
	"github.com/grafana/loki/pkg/storage/chunk/index"
	"github.com/grafana/loki/pkg/util/extract"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/util/spanlogger"
	"github.com/grafana/loki/pkg/util/validation"
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

	ErrQueryMustContainMetricName = QueryError("query must contain metric name")

	indexEntriesPerChunk = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "loki",
		Name:      "chunk_store_index_entries_per_chunk",
		Help:      "Number of entries written to storage per chunk.",
		Buckets:   prometheus.ExponentialBuckets(1, 2, 5),
	})
)

// Query errors are to be treated as user errors, rather than storage errors.
type QueryError string

func (e QueryError) Error() string {
	return string(e)
}

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
	fetcher          *Fetcher
	schemaCfg        config.SchemaConfig
	cfg              StoreConfig

	index       Index
	indexClient index.IndexClient
}

func NewStore(cfg StoreConfig, scfg config.SchemaConfig, schema index.SeriesStoreSchema, idx index.IndexClient, limits StoreLimits, chunksCache, writeDedupeCache cache.Cache) (*Store, error) {
	if cfg.CacheLookupsOlderThan != 0 {
		schema = index.NewSchemaCaching(schema, time.Duration(cfg.CacheLookupsOlderThan))
	}
	fetcher, err := NewChunkFetcher(chunksCache, cfg.chunkCacheStubs, scfg, chunks, cfg.ChunkCacheConfig.AsyncCacheWriteBackConcurrency, cfg.ChunkCacheConfig.AsyncCacheWriteBackBufferSize)
	if err != nil {
		return nil, err
	}
	indexStore := newSeriesIndexStore(scfg, schema, idx, fetcher)
	return &Store{
		cfg:              cfg,
		schema:           schema,
		chunks:           chunks,
		limits:           limits,
		writeDedupeCache: writeDedupeCache,
		fetcher:          fetcher,
		indexClient:      idx,
		index:            indexStore,
		schemaCfg:        scfg,
	}, nil
}

func (c *Store) GetChunkRefs(ctx context.Context, userID string, from, through model.Time, allMatchers ...*labels.Matcher) ([][]encoding.Chunk, []*Fetcher, error) {
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

	chunks := make([]encoding.Chunk, len(refs))
	for i, ref := range refs {
		chunks[i] = encoding.Chunk{
			ChunkRef: ref,
		}
	}

	return [][]encoding.Chunk{chunks}, []*Fetcher{c.fetcher}, err
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

// Put implements Store
func (c *Store) Put(ctx context.Context, chunks []encoding.Chunk) error {
	for _, chunk := range chunks {
		if err := c.PutOne(ctx, chunk.From, chunk.Through, chunk); err != nil {
			return err
		}
	}
	return nil
}

// PutOne implements Store
func (c *Store) PutOne(ctx context.Context, from, through model.Time, chunk encoding.Chunk) error {
	log, ctx := spanlogger.New(ctx, "SeriesStore.PutOne")
	defer log.Finish()
	writeChunk := true

	// If this chunk is in cache it must already be in the database so we don't need to write it again
	found, _, _, _ := c.fetcher.cache.Fetch(ctx, []string{c.schemaCfg.ExternalKey(chunk.ChunkRef)})

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

	chunks := []encoding.Chunk{chunk}

	writeReqs, keysToCache, err := c.calculateIndexEntries(ctx, from, through, chunk)
	if err != nil {
		return err
	}

	if oic, ok := c.fetcher.storage.(ObjectAndIndexClient); ok {
		chunks := chunks
		if !writeChunk {
			chunks = []encoding.Chunk{}
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
func (c *Store) calculateIndexEntries(ctx context.Context, from, through model.Time, chunk encoding.Chunk) (index.WriteBatch, []string, error) {
	seenIndexEntries := map[string]struct{}{}
	entries := []index.IndexEntry{}

	metricName := chunk.Metric.Get(labels.MetricName)
	if metricName == "" {
		return nil, nil, fmt.Errorf("no MetricNameLabel for chunk")
	}

	keys, labelEntries, err := c.schema.GetCacheKeysAndLabelWriteEntries(from, through, chunk.UserID, metricName, chunk.Metric, c.schemaCfg.ExternalKey(chunk.ChunkRef))
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

	chunkEntries, err := c.schema.GetChunkWriteEntries(from, through, chunk.UserID, metricName, chunk.Metric, c.schemaCfg.ExternalKey(chunk.ChunkRef))
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

// Stop any background goroutines (ie in the cache.)
func (c *Store) Stop() {
	c.fetcher.storage.Stop()
	c.fetcher.Stop()
	c.indexClient.Stop()
}

func (c *Store) validateQueryTimeRange(ctx context.Context, userID string, from *model.Time, through *model.Time) (bool, error) {
	//nolint:ineffassign,staticcheck //Leaving ctx even though we don't currently use it, we want to make it available for when we might need it and hopefully will ensure us using the correct context at that time

	if *through < *from {
		return false, QueryError(fmt.Sprintf("invalid query, through < from (%s < %s)", through, from))
	}

	maxQueryLength := c.limits.MaxQueryLength(userID)
	if maxQueryLength > 0 && (*through).Sub(*from) > maxQueryLength {
		return false, QueryError(fmt.Sprintf(validation.ErrQueryTooLong, (*through).Sub(*from), maxQueryLength))
	}

	now := model.Now()

	if from.After(now) {
		// time-span start is in future ... regard as legal
		level.Info(util_log.WithContext(ctx, util_log.Logger)).Log("msg", "whole timerange in future, yield empty resultset", "through", through, "from", from, "now", now)
		return true, nil
	}

	if through.After(now.Add(5 * time.Minute)) {
		// time-span end is in future ... regard as legal
		level.Info(util_log.WithContext(ctx, util_log.Logger)).Log("msg", "adjusting end timerange from future to now", "old_through", through, "new_through", now)
		*through = now // Avoid processing future part - otherwise some schemas could fail with eg non-existent table gripes
	}

	return false, nil
}

func (c *Store) validateQuery(ctx context.Context, userID string, from *model.Time, through *model.Time, matchers []*labels.Matcher) (string, []*labels.Matcher, bool, error) {
	shortcut, err := c.validateQueryTimeRange(ctx, userID, from, through)
	if err != nil {
		return "", nil, false, err
	}
	if shortcut {
		return "", nil, true, nil
	}

	// Check there is a metric name matcher of type equal,
	metricNameMatcher, matchers, ok := extract.MetricNameMatcherFromMatchers(matchers)
	if !ok || metricNameMatcher.Type != labels.MatchEqual {
		return "", nil, false, ErrQueryMustContainMetricName
	}

	return metricNameMatcher.Value, matchers, false, nil
}

func (c *Store) GetChunkFetcher(_ model.Time) *Fetcher {
	return c.fetcher
}
