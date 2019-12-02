package chunk

import (
	"context"
	"fmt"
	"net/http"

	"github.com/go-kit/kit/log/level"
	jsoniter "github.com/json-iterator/go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/weaveworks/common/httpgrpc"

	"github.com/cortexproject/cortex/pkg/chunk/cache"
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
		Buckets:   prometheus.DefBuckets,
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
)

// seriesStore implements Store
type seriesStore struct {
	store
	writeDedupeCache cache.Cache
}

func newSeriesStore(cfg StoreConfig, schema Schema, index IndexClient, chunks ObjectClient, limits StoreLimits) (Store, error) {
	fetcher, err := NewChunkFetcher(cfg.ChunkCacheConfig, cfg.chunkCacheStubs, chunks)
	if err != nil {
		return nil, err
	}

	writeDedupeCache, err := cache.New(cfg.WriteDedupeCacheConfig)
	if err != nil {
		return nil, err
	}

	if cfg.CacheLookupsOlderThan != 0 {
		schema = &schemaCaching{
			Schema:         schema,
			cacheOlderThan: cfg.CacheLookupsOlderThan,
		}
	}

	return &seriesStore{
		store: store{
			cfg:     cfg,
			index:   index,
			chunks:  chunks,
			schema:  schema,
			limits:  limits,
			Fetcher: fetcher,
		},
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
	maxChunksPerQuery := c.limits.MaxChunksPerQuery(userID)
	if maxChunksPerQuery > 0 && len(chunks) > maxChunksPerQuery {
		err := httpgrpc.Errorf(http.StatusBadRequest, "Query %v fetched too many chunks (%d > %d)", allMatchers, len(chunks), maxChunksPerQuery)
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

	return [][]Chunk{chunks}, []*Fetcher{c.store.Fetcher}, nil
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
	allChunks, err := c.FetchChunks(ctx, filtered, keys)
	if err != nil {
		level.Error(log).Log("msg", "FetchChunks", "err", err)
		return nil, err
	}
	return labelNamesFromChunks(allChunks), nil
}
func (c *seriesStore) lookupSeriesByMetricNameMatchers(ctx context.Context, from, through model.Time, userID, metricName string, matchers []*labels.Matcher) ([]string, error) {
	log, ctx := spanlogger.New(ctx, "SeriesStore.lookupSeriesByMetricNameMatchers", "metricName", metricName, "matchers", len(matchers))
	defer log.Span.Finish()

	// Just get series for metric if there are no matchers
	if len(matchers) == 0 {
		indexLookupsPerQuery.Observe(1)
		series, err := c.lookupSeriesByMetricNameMatcher(ctx, from, through, userID, metricName, nil)
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
			ids, err := c.lookupSeriesByMetricNameMatcher(ctx, from, through, userID, metricName, matcher)
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
	for i := 0; i < len(matchers); i++ {
		select {
		case incoming := <-incomingIDs:
			preIntersectionCount += len(incoming)
			if ids == nil {
				ids = incoming
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

func (c *seriesStore) lookupSeriesByMetricNameMatcher(ctx context.Context, from, through model.Time, userID, metricName string, matcher *labels.Matcher) ([]string, error) {
	log, ctx := spanlogger.New(ctx, "SeriesStore.lookupSeriesByMetricNameMatcher", "metricName", metricName, "matcher", matcher)
	defer log.Span.Finish()

	var err error
	var queries []IndexQuery
	var labelName string
	if matcher == nil {
		queries, err = c.schema.GetReadQueriesForMetric(from, through, userID, metricName)
	} else if matcher.Type != labels.MatchEqual {
		labelName = matcher.Name
		queries, err = c.schema.GetReadQueriesForMetricLabel(from, through, userID, metricName, matcher.Name)
	} else {
		labelName = matcher.Name
		queries, err = c.schema.GetReadQueriesForMetricLabelValue(from, through, userID, metricName, matcher.Name, matcher.Value)
	}
	if err != nil {
		return nil, err
	}
	level.Debug(log).Log("queries", len(queries))

	entries, err := c.lookupEntriesByQueries(ctx, queries)
	if e, ok := err.(CardinalityExceededError); ok {
		e.MetricName = metricName
		e.LabelName = labelName
		return nil, e
	} else if err != nil {
		return nil, err
	}
	level.Debug(log).Log("entries", len(entries))

	ids, err := c.parseIndexEntries(ctx, entries, matcher)
	if err != nil {
		return nil, err
	}
	level.Debug(log).Log("ids", len(ids))

	return ids, nil
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

// Put implements ChunkStore
func (c *seriesStore) Put(ctx context.Context, chunks []Chunk) error {
	for _, chunk := range chunks {
		if err := c.PutOne(ctx, chunk.From, chunk.Through, chunk); err != nil {
			return err
		}
	}
	return nil
}

// PutOne implements ChunkStore
func (c *seriesStore) PutOne(ctx context.Context, from, through model.Time, chunk Chunk) error {
	// If this chunk is in cache it must already be in the database so we don't need to write it again
	found, _, _ := c.cache.Fetch(ctx, []string{chunk.ExternalKey()})
	if len(found) > 0 {
		return nil
	}

	chunks := []Chunk{chunk}

	writeReqs, keysToCache, err := c.calculateIndexEntries(ctx, from, through, chunk)
	if err != nil {
		return err
	}

	if oic, ok := c.storage.(ObjectAndIndexClient); ok {
		if err = oic.PutChunkAndIndex(ctx, chunk, writeReqs); err != nil {
			return err
		}
	} else {
		err := c.storage.PutChunks(ctx, chunks)
		if err != nil {
			return err
		}
		if err := c.index.BatchWrite(ctx, writeReqs); err != nil {
			return err
		}
	}
	c.writeBackCache(ctx, chunks)

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
