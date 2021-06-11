package chunk

import (
	"context"
	"fmt"
	"time"

	"github.com/go-kit/kit/log/level"
	jsoniter "github.com/json-iterator/go"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"

	"github.com/cortexproject/cortex/pkg/chunk/cache"
	"github.com/cortexproject/cortex/pkg/querier/astmapper"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/spanlogger"
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
		Namespace: "cortex",
		Name:      "chunk_store_index_lookups_per_query",
		Help:      "Distribution of #index lookups per query.",
		Buckets:   prometheus.ExponentialBuckets(1, 2, 5),
	})
	preIntersectionPerQuery = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "cortex",
		Name:      "chunk_store_series_pre_intersection_per_query",
		Help:      "Distribution of #series (pre intersection) per query.",
		// A reasonable upper bound is around 100k - 10*(8^(6-1)) = 327k.
		Buckets: prometheus.ExponentialBuckets(10, 8, 6),
	})
	postIntersectionPerQuery = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "cortex",
		Name:      "chunk_store_series_post_intersection_per_query",
		Help:      "Distribution of #series (post intersection) per query.",
		// A reasonable upper bound is around 100k - 10*(8^(6-1)) = 327k.
		Buckets: prometheus.ExponentialBuckets(10, 8, 6),
	})
	chunksPerQuery = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "cortex",
		Name:      "chunk_store_chunks_per_query",
		Help:      "Distribution of #chunks per query.",
		// For 100k series for 7 week, could be 1.2m - 10*(8^(7-1)) = 2.6m.
		Buckets: prometheus.ExponentialBuckets(10, 8, 7),
	})
	dedupedChunksTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "cortex",
		Name:      "chunk_store_deduped_chunks_total",
		Help:      "Count of chunks which were not stored because they have already been stored by another replica.",
	})
)

// seriesStore implements Store
type seriesStore struct {
	baseStore
	schema           SeriesStoreSchema
	writeDedupeCache cache.Cache
}

func newSeriesStore(cfg StoreConfig, schema SeriesStoreSchema, index IndexClient, chunks Client, limits StoreLimits, chunksCache, writeDedupeCache cache.Cache) (Store, error) {
	rs, err := newBaseStore(cfg, schema, index, chunks, limits, chunksCache)
	if err != nil {
		return nil, err
	}

	if cfg.CacheLookupsOlderThan != 0 {
		schema = &schemaCaching{
			SeriesStoreSchema: schema,
			cacheOlderThan:    time.Duration(cfg.CacheLookupsOlderThan),
		}
	}

	return &seriesStore{
		baseStore:        rs,
		schema:           schema,
		writeDedupeCache: writeDedupeCache,
	}, nil
}

// Get implements Store
func (c *seriesStore) Get(ctx context.Context, userID string, from, through model.Time, allMatchers ...*labels.Matcher) ([]Chunk, error) {
	log, ctx := spanlogger.New(ctx, "SeriesStore.Get")
	defer log.Span.Finish()
	level.Debug(log).Log("from", from, "through", through, "matchers", len(allMatchers))

	chks, fetchers, err := c.GetChunkRefs(ctx, userID, from, through, allMatchers...)
	if err != nil {
		return nil, err
	}

	if len(chks) == 0 {
		// Shortcut
		return nil, nil
	}

	chunks := chks[0]
	fetcher := fetchers[0]
	// Protect ourselves against OOMing.
	maxChunksPerQuery := c.limits.MaxChunksPerQueryFromStore(userID)
	if maxChunksPerQuery > 0 && len(chunks) > maxChunksPerQuery {
		err := QueryError(fmt.Sprintf("Query %v fetched too many chunks (%d > %d)", allMatchers, len(chunks), maxChunksPerQuery))
		level.Error(log).Log("err", err)
		return nil, err
	}

	// Now fetch the actual chunk data from Memcache / S3
	keys := keysFromChunks(chunks)
	allChunks, err := fetcher.FetchChunks(ctx, chunks, keys)
	if err != nil {
		level.Error(log).Log("msg", "FetchChunks", "err", err)
		return nil, err
	}

	// inject artificial __cortex_shard__ labels if present in the query. GetChunkRefs guarantees any chunk refs match the shard.
	shard, _, err := astmapper.ShardFromMatchers(allMatchers)
	if err != nil {
		return nil, err
	}
	if shard != nil {
		injectShardLabels(allChunks, *shard)
	}

	// Filter out chunks based on the empty matchers in the query.
	filteredChunks := filterChunksByMatchers(allChunks, allMatchers)
	return filteredChunks, nil
}

func (c *seriesStore) GetChunkRefs(ctx context.Context, userID string, from, through model.Time, allMatchers ...*labels.Matcher) ([][]Chunk, []*Fetcher, error) {
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

	// Fetch the series IDs from the index, based on non-empty matchers from
	// the query.
	_, matchers = util.SplitFiltersAndMatchers(matchers)
	seriesIDs, err := c.lookupSeriesByMetricNameMatchers(ctx, from, through, userID, metricName, matchers)
	if err != nil {
		return nil, nil, err
	}
	level.Debug(log).Log("series-ids", len(seriesIDs))

	// Lookup the series in the index to get the chunks.
	chunkIDs, err := c.lookupChunksBySeries(ctx, from, through, userID, seriesIDs)
	if err != nil {
		level.Error(log).Log("msg", "lookupChunksBySeries", "err", err)
		return nil, nil, err
	}
	level.Debug(log).Log("chunk-ids", len(chunkIDs))

	chunks, err := c.convertChunkIDsToChunks(ctx, userID, chunkIDs)
	if err != nil {
		level.Error(log).Log("op", "convertChunkIDsToChunks", "err", err)
		return nil, nil, err
	}

	chunks = filterChunksByTime(from, through, chunks)
	level.Debug(log).Log("chunks-post-filtering", len(chunks))
	chunksPerQuery.Observe(float64(len(chunks)))

	// We should return an empty chunks slice if there are no chunks.
	if len(chunks) == 0 {
		return [][]Chunk{}, []*Fetcher{}, nil
	}

	return [][]Chunk{chunks}, []*Fetcher{c.baseStore.fetcher}, nil
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

	// Fetch the series IDs from the index
	seriesIDs, err := c.lookupSeriesByMetricNameMatchers(ctx, from, through, userID, metricName, nil)
	if err != nil {
		return nil, err
	}
	level.Debug(log).Log("series-ids", len(seriesIDs))

	// Lookup the series in the index to get label names.
	labelNames, err := c.lookupLabelNamesBySeries(ctx, from, through, userID, seriesIDs)
	if err != nil {
		// looking up metrics by series is not supported falling back on chunks
		if err == ErrNotSupported {
			return c.lookupLabelNamesByChunks(ctx, from, through, userID, seriesIDs)
		}
		level.Error(log).Log("msg", "lookupLabelNamesBySeries", "err", err)
		return nil, err
	}
	level.Debug(log).Log("labelNames", len(labelNames))

	return labelNames, nil
}

func (c *seriesStore) lookupLabelNamesByChunks(ctx context.Context, from, through model.Time, userID string, seriesIDs []string) ([]string, error) {
	log, ctx := spanlogger.New(ctx, "SeriesStore.lookupLabelNamesByChunks")
	defer log.Span.Finish()

	// Lookup the series in the index to get the chunks.
	chunkIDs, err := c.lookupChunksBySeries(ctx, from, through, userID, seriesIDs)
	if err != nil {
		level.Error(log).Log("msg", "lookupChunksBySeries", "err", err)
		return nil, err
	}
	level.Debug(log).Log("chunk-ids", len(chunkIDs))

	chunks, err := c.convertChunkIDsToChunks(ctx, userID, chunkIDs)
	if err != nil {
		level.Error(log).Log("err", "convertChunkIDsToChunks", "err", err)
		return nil, err
	}

	// Filter out chunks that are not in the selected time range and keep a single chunk per fingerprint
	filtered := filterChunksByTime(from, through, chunks)
	filtered, keys := filterChunksByUniqueFingerprint(filtered)
	level.Debug(log).Log("Chunks post filtering", len(chunks))

	chunksPerQuery.Observe(float64(len(filtered)))

	// Now fetch the actual chunk data from Memcache / S3
	allChunks, err := c.fetcher.FetchChunks(ctx, filtered, keys)
	if err != nil {
		level.Error(log).Log("msg", "FetchChunks", "err", err)
		return nil, err
	}
	return labelNamesFromChunks(allChunks), nil
}
func (c *seriesStore) lookupSeriesByMetricNameMatchers(ctx context.Context, from, through model.Time, userID, metricName string, matchers []*labels.Matcher) ([]string, error) {
	log, ctx := spanlogger.New(ctx, "SeriesStore.lookupSeriesByMetricNameMatchers", "metricName", metricName, "matchers", len(matchers))
	defer log.Span.Finish()

	// Check if one of the labels is a shard annotation, pass that information to lookupSeriesByMetricNameMatcher,
	// and remove the label.
	shard, shardLabelIndex, err := astmapper.ShardFromMatchers(matchers)
	if err != nil {
		return nil, err
	}

	if shard != nil {
		matchers = append(matchers[:shardLabelIndex], matchers[shardLabelIndex+1:]...)
	}

	// Just get series for metric if there are no matchers
	if len(matchers) == 0 {
		indexLookupsPerQuery.Observe(1)
		series, err := c.lookupSeriesByMetricNameMatcher(ctx, from, through, userID, metricName, nil, shard)
		if err != nil {
			preIntersectionPerQuery.Observe(float64(len(series)))
			postIntersectionPerQuery.Observe(float64(len(series)))
		}
		return series, err
	}

	// Otherwise get series which include other matchers
	incomingIDs := make(chan []string)
	incomingErrors := make(chan error)
	indexLookupsPerQuery.Observe(float64(len(matchers)))
	for _, matcher := range matchers {
		go func(matcher *labels.Matcher) {
			ids, err := c.lookupSeriesByMetricNameMatcher(ctx, from, through, userID, metricName, matcher, shard)
			if err != nil {
				incomingErrors <- err
				return
			}
			incomingIDs <- ids
		}(matcher)
	}

	// Receive series IDs from all matchers, intersect as we go.
	var ids []string
	var preIntersectionCount int
	var lastErr error
	var cardinalityExceededErrors int
	var cardinalityExceededError CardinalityExceededError
	var initialized bool
	for i := 0; i < len(matchers); i++ {
		select {
		case incoming := <-incomingIDs:
			preIntersectionCount += len(incoming)
			if !initialized {
				ids = incoming
				initialized = true
			} else {
				ids = intersectStrings(ids, incoming)
			}
		case err := <-incomingErrors:
			// The idea is that if we have 2 matchers, and if one returns a lot of
			// series and the other returns only 10 (a few), we don't lookup the first one at all.
			// We just manually filter through the 10 series again using "filterChunksByMatchers",
			// saving us from looking up and intersecting a lot of series.
			if e, ok := err.(CardinalityExceededError); ok {
				cardinalityExceededErrors++
				cardinalityExceededError = e
			} else {
				lastErr = err
			}
		}
	}

	// But if every single matcher returns a lot of series, then it makes sense to abort the query.
	if cardinalityExceededErrors == len(matchers) {
		return nil, cardinalityExceededError
	} else if lastErr != nil {
		return nil, lastErr
	}
	preIntersectionPerQuery.Observe(float64(preIntersectionCount))
	postIntersectionPerQuery.Observe(float64(len(ids)))

	level.Debug(log).Log("msg", "post intersection", "ids", len(ids))
	return ids, nil
}

func (c *seriesStore) lookupSeriesByMetricNameMatcher(ctx context.Context, from, through model.Time, userID, metricName string, matcher *labels.Matcher, shard *astmapper.ShardAnnotation) ([]string, error) {
	return c.lookupIdsByMetricNameMatcher(ctx, from, through, userID, metricName, matcher, func(queries []IndexQuery) []IndexQuery {
		return c.schema.FilterReadQueries(queries, shard)
	})
}

func (c *seriesStore) lookupChunksBySeries(ctx context.Context, from, through model.Time, userID string, seriesIDs []string) ([]string, error) {
	log, ctx := spanlogger.New(ctx, "SeriesStore.lookupChunksBySeries")
	defer log.Span.Finish()

	level.Debug(log).Log("seriesIDs", len(seriesIDs))

	queries := make([]IndexQuery, 0, len(seriesIDs))
	for _, seriesID := range seriesIDs {
		qs, err := c.schema.GetChunksForSeries(from, through, userID, []byte(seriesID))
		if err != nil {
			return nil, err
		}
		queries = append(queries, qs...)
	}
	level.Debug(log).Log("queries", len(queries))

	entries, err := c.lookupEntriesByQueries(ctx, queries)
	if err != nil {
		return nil, err
	}
	level.Debug(log).Log("entries", len(entries))

	result, err := c.parseIndexEntries(ctx, entries, nil)
	return result, err
}

func (c *seriesStore) lookupLabelNamesBySeries(ctx context.Context, from, through model.Time, userID string, seriesIDs []string) ([]string, error) {
	log, ctx := spanlogger.New(ctx, "SeriesStore.lookupLabelNamesBySeries")
	defer log.Span.Finish()

	level.Debug(log).Log("seriesIDs", len(seriesIDs))
	queries := make([]IndexQuery, 0, len(seriesIDs))
	for _, seriesID := range seriesIDs {
		qs, err := c.schema.GetLabelNamesForSeries(from, through, userID, []byte(seriesID))
		if err != nil {
			return nil, err
		}
		queries = append(queries, qs...)
	}
	level.Debug(log).Log("queries", len(queries))
	entries, err := c.lookupEntriesByQueries(ctx, queries)
	if err != nil {
		return nil, err
	}
	level.Debug(log).Log("entries", len(entries))

	var result UniqueStrings
	result.Add(model.MetricNameLabel)
	for _, entry := range entries {
		lbs := []string{}
		err := jsoniter.ConfigFastest.Unmarshal(entry.Value, &lbs)
		if err != nil {
			return nil, err
		}
		result.Add(lbs...)
	}
	return result.Strings(), nil
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
	found, _, _ := c.fetcher.cache.Fetch(ctx, []string{chunk.ExternalKey()})
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
		if err := c.index.BatchWrite(ctx, writeReqs); err != nil {
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
	c.writeDedupeCache.Store(ctx, keysToCache, bufs)
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

	keys, labelEntries, err := c.schema.GetCacheKeysAndLabelWriteEntries(from, through, chunk.UserID, metricName, chunk.Metric, chunk.ExternalKey())
	if err != nil {
		return nil, nil, err
	}
	_, _, missing := c.writeDedupeCache.Fetch(ctx, keys)
	// keys and labelEntries are matched in order, but Fetch() may
	// return missing keys in any order so check against all of them.
	for _, missingKey := range missing {
		for i, key := range keys {
			if key == missingKey {
				entries = append(entries, labelEntries[i]...)
			}
		}
	}

	chunkEntries, err := c.schema.GetChunkWriteEntries(from, through, chunk.UserID, metricName, chunk.Metric, chunk.ExternalKey())
	if err != nil {
		return nil, nil, err
	}
	entries = append(entries, chunkEntries...)

	indexEntriesPerChunk.Observe(float64(len(entries)))

	// Remove duplicate entries based on tableName:hashValue:rangeValue
	result := c.index.NewWriteBatch()
	for _, entry := range entries {
		key := fmt.Sprintf("%s:%s:%x", entry.TableName, entry.HashValue, entry.RangeValue)
		if _, ok := seenIndexEntries[key]; !ok {
			seenIndexEntries[key] = struct{}{}
			result.Add(entry.TableName, entry.HashValue, entry.RangeValue, entry.Value)
		}
	}

	return result, missing, nil
}

func injectShardLabels(chunks []Chunk, shard astmapper.ShardAnnotation) {
	for i, chunk := range chunks {
		b := labels.NewBuilder(chunk.Metric)
		l := shard.Label()
		b.Set(l.Name, l.Value)
		chunk.Metric = b.Labels()
		chunks[i] = chunk
	}
}

func (c *seriesStore) DeleteChunk(ctx context.Context, from, through model.Time, userID, chunkID string, metric labels.Labels, partiallyDeletedInterval *model.Interval) error {
	metricName := metric.Get(model.MetricNameLabel)
	if metricName == "" {
		return ErrMetricNameLabelMissing
	}

	chunkWriteEntries, err := c.schema.GetChunkWriteEntries(from, through, userID, string(metricName), metric, chunkID)
	if err != nil {
		return errors.Wrapf(err, "when getting chunk index entries to delete for chunkID=%s", chunkID)
	}

	return c.deleteChunk(ctx, userID, chunkID, metric, chunkWriteEntries, partiallyDeletedInterval, func(chunk Chunk) error {
		return c.PutOne(ctx, chunk.From, chunk.Through, chunk)
	})
}

func (c *seriesStore) DeleteSeriesIDs(ctx context.Context, from, through model.Time, userID string, metric labels.Labels) error {

	entries, err := c.schema.GetSeriesDeleteEntries(from, through, userID, metric, func(userID, seriesID string, from, through model.Time) (b bool, e error) {
		return c.hasChunksForInterval(ctx, userID, seriesID, from, through)
	})
	if err != nil {
		return err
	}

	batch := c.index.NewWriteBatch()
	for i := range entries {
		batch.Delete(entries[i].TableName, entries[i].HashValue, entries[i].RangeValue)
	}

	return c.index.BatchWrite(ctx, batch)
}

func (c *seriesStore) hasChunksForInterval(ctx context.Context, userID, seriesID string, from, through model.Time) (bool, error) {
	chunkIDs, err := c.lookupChunksBySeries(ctx, from, through, userID, []string{seriesID})
	if err != nil {
		return false, err
	}

	chunks, err := c.convertChunkIDsToChunks(ctx, userID, chunkIDs)
	if err != nil {
		return false, err
	}

	for _, chunk := range chunks {
		if intervalsOverlap(model.Interval{Start: from, End: through}, model.Interval{Start: chunk.From, End: chunk.Through}) {
			return true, nil
		}
	}

	return false, nil
}
