package chunk

import (
	"context"
	"flag"
	"fmt"
	"sort"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	ot "github.com/opentracing/opentracing-go"
	otlog "github.com/opentracing/opentracing-go/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"

	"github.com/weaveworks/common/user"
	"github.com/weaveworks/cortex/pkg/chunk/cache"
	"github.com/weaveworks/cortex/pkg/util"
	"github.com/weaveworks/cortex/pkg/util/extract"
)

var (
	indexEntriesPerChunk = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "cortex",
		Name:      "chunk_store_index_entries_per_chunk",
		Help:      "Number of entries written to storage per chunk.",
		Buckets:   prometheus.ExponentialBuckets(1, 2, 5),
	})
	rowWrites = util.NewHashBucketHistogram(util.HashBucketHistogramOpts{
		HistogramOpts: prometheus.HistogramOpts{
			Namespace: "cortex",
			Name:      "chunk_store_row_writes_distribution",
			Help:      "Distribution of writes to individual storage rows",
			Buckets:   prometheus.DefBuckets,
		},
		HashBuckets: 1024,
	})
	cacheCorrupt = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "cortex",
		Name:      "cache_corrupt_chunks_total",
		Help:      "Total count of corrupt chunks found in cache.",
	})
)

func init() {
	prometheus.MustRegister(indexEntriesPerChunk)
	prometheus.MustRegister(rowWrites)
	prometheus.MustRegister(cacheCorrupt)
}

// StoreConfig specifies config for a ChunkStore
type StoreConfig struct {
	CacheConfig cache.Config

	MinChunkAge     time.Duration
	QueryChunkLimit int

	// For injecting different schemas in tests.
	schemaFactory func(cfg SchemaConfig) Schema
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *StoreConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.CacheConfig.RegisterFlags(f)
	f.DurationVar(&cfg.MinChunkAge, "store.min-chunk-age", 0, "Minimum time between chunk update and being saved to the store.")
	f.IntVar(&cfg.QueryChunkLimit, "store.query-chunk-limit", 2e6, "Maximum number of chunks that can be fetched in a single query.")
}

// Store implements Store
type Store struct {
	cfg StoreConfig

	storage StorageClient
	cache   cache.Cache
	schema  Schema
}

// NewStore makes a new ChunkStore
func NewStore(cfg StoreConfig, schemaCfg SchemaConfig, storage StorageClient) (*Store, error) {
	var schema Schema
	var err error
	if cfg.schemaFactory == nil {
		schema, err = newCompositeSchema(schemaCfg)
	} else {
		schema = cfg.schemaFactory(schemaCfg)
	}
	if err != nil {
		return nil, err
	}

	cache, err := cache.New(cfg.CacheConfig)
	if err != nil {
		return nil, err
	}

	return &Store{
		cfg:     cfg,
		storage: storage,
		schema:  schema,
		cache:   cache,
	}, nil
}

// Stop any background goroutines (ie in the cache.)
func (c *Store) Stop() {
	c.cache.Stop()
}

// Put implements ChunkStore
func (c *Store) Put(ctx context.Context, chunks []Chunk) error {
	userID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return err
	}

	err = c.storage.PutChunks(ctx, chunks)
	if err != nil {
		return err
	}

	c.writeBackCache(ctx, chunks)
	return c.updateIndex(ctx, userID, chunks)
}

func (c *Store) updateIndex(ctx context.Context, userID string, chunks []Chunk) error {
	writeReqs, err := c.calculateDynamoWrites(userID, chunks)
	if err != nil {
		return err
	}

	return c.storage.BatchWrite(ctx, writeReqs)
}

// calculateDynamoWrites creates a set of batched WriteRequests to dynamo for all
// the chunks it is given.
func (c *Store) calculateDynamoWrites(userID string, chunks []Chunk) (WriteBatch, error) {
	seenIndexEntries := map[string]struct{}{}

	writeReqs := c.storage.NewWriteBatch()
	for _, chunk := range chunks {
		metricName, err := extract.MetricNameFromMetric(chunk.Metric)
		if err != nil {
			return nil, err
		}

		entries, err := c.schema.GetWriteEntries(chunk.From, chunk.Through, userID, metricName, chunk.Metric, chunk.ExternalKey())
		if err != nil {
			return nil, err
		}
		indexEntriesPerChunk.Observe(float64(len(entries)))

		// Remove duplicate entries based on tableName:hashValue:rangeValue
		unseenEntries := []IndexEntry{}
		for _, entry := range entries {
			key := fmt.Sprintf("%s:%s:%x", entry.TableName, entry.HashValue, entry.RangeValue)
			if _, ok := seenIndexEntries[key]; !ok {
				seenIndexEntries[key] = struct{}{}
				unseenEntries = append(unseenEntries, entry)
			}
		}

		for _, entry := range unseenEntries {
			rowWrites.Observe(entry.HashValue, 1)
			writeReqs.Add(entry.TableName, entry.HashValue, entry.RangeValue, entry.Value)
		}
	}
	return writeReqs, nil
}

// spanLogger unifies tracing and logging, to reduce repetition.
type spanLogger struct {
	log.Logger
	ot.Span
}

func newSpanLogger(ctx context.Context, method string) (*spanLogger, context.Context) {
	span, ctx := ot.StartSpanFromContext(ctx, "ChunkStore.Get")
	return &spanLogger{
		Logger: log.With(util.WithContext(ctx, util.Logger), "method", method),
		Span:   span,
	}, ctx
}

func (s *spanLogger) Log(kvps ...interface{}) error {
	s.Logger.Log(kvps...)
	fields, err := otlog.InterleavedKVToFields(kvps...)
	if err != nil {
		return err
	}
	s.Span.LogFields(fields...)
	return nil
}

// Get implements ChunkStore
func (c *Store) Get(ctx context.Context, from, through model.Time, allMatchers ...*labels.Matcher) (model.Matrix, error) {
	log, ctx := newSpanLogger(ctx, "ChunkStore.Get")
	defer log.Span.Finish()

	now := model.Now()
	level.Debug(log).Log("from", from, "through", through, "now", now, "matchers", len(allMatchers))

	if through < from {
		return nil, fmt.Errorf("invalid query, through < from (%d < %d)", through, from)
	}

	if from.After(now) {
		// time-span start is in future ... regard as legal
		level.Error(log).Log("msg", "whole timerange in future, yield empty resultset", "through", through, "from", from, "now", now)
		return nil, nil
	}

	if from.After(now.Add(-c.cfg.MinChunkAge)) {
		// no data relevant to this query will have arrived at the store yet
		return nil, nil
	}

	if through.After(now.Add(5 * time.Minute)) {
		// time-span end is in future ... regard as legal
		level.Error(log).Log("msg", "adjusting end timerange from future to now", "old_through", through, "new_through", now)
		through = now // Avoid processing future part - otherwise some schemas could fail with eg non-existent table gripes
	}

	// Fetch metric name chunks if the matcher is of type equal,
	metricNameMatcher, matchers, ok := extract.MetricNameMatcherFromMatchers(allMatchers)
	if ok && metricNameMatcher.Type == labels.MatchEqual {
		log.Span.SetTag("metric", metricNameMatcher.Value)
		return c.getMetricNameMatrix(ctx, from, through, matchers, metricNameMatcher.Value)
	}

	// Otherwise we consult the metric name index first and then create queries for each matching metric name.
	return c.getSeriesMatrix(ctx, from, through, matchers, metricNameMatcher)
}

func (c *Store) getMetricNameMatrix(ctx context.Context, from, through model.Time, allMatchers []*labels.Matcher, metricName string) (model.Matrix, error) {
	chunks, err := c.getMetricNameChunks(ctx, from, through, allMatchers, metricName)
	if err != nil {
		return nil, err
	}
	return chunksToMatrix(ctx, chunks, from, through)
}

func (c *Store) getMetricNameChunks(ctx context.Context, from, through model.Time, allMatchers []*labels.Matcher, metricName string) ([]Chunk, error) {
	log, ctx := newSpanLogger(ctx, "ChunkStore.getMetricNameChunks")
	level.Debug(log).Log("from", from, "through", through, "metricName", metricName, "matchers", len(allMatchers))

	filters, matchers := util.SplitFiltersAndMatchers(allMatchers)
	chunks, err := c.lookupChunksByMetricName(ctx, from, through, matchers, metricName)
	if err != nil {
		return nil, err
	}
	level.Debug(log).Log("Chunks in index", len(chunks))

	// Filter out chunks that are not in the selected time range.
	filtered := make([]Chunk, 0, len(chunks))
	keys := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		if chunk.Through < from || through < chunk.From {
			continue
		}
		filtered = append(filtered, chunk)
		keys = append(keys, chunk.ExternalKey())
	}
	level.Debug(log).Log("Chunks post filtering", len(chunks))

	if len(filtered) > c.cfg.QueryChunkLimit {
		err := fmt.Errorf("Query %v fetched too many chunks (%d > %d)", allMatchers, len(filtered), c.cfg.QueryChunkLimit)
		level.Error(log).Log("err", err)
		return nil, err
	}

	// Now fetch the actual chunk data from Memcache / S3
	cacheHits, cacheBufs, _, err := c.cache.FetchChunkData(ctx, keys)
	if err != nil {
		level.Warn(log).Log("msg", "error fetching from cache", "err", err)
	}

	fromCache, missing, err := ProcessCacheResponse(filtered, cacheHits, cacheBufs)
	if err != nil {
		level.Warn(log).Log("msg", "error fetching from cache", "err", err)
	}

	fromStorage, err := c.storage.GetChunks(ctx, missing)

	// Always cache any chunks we did get
	if cacheErr := c.writeBackCache(ctx, fromStorage); cacheErr != nil {
		level.Warn(log).Log("msg", "could not store chunks in chunk cache", "err", cacheErr)
	}

	if err != nil {
		return nil, promql.ErrStorage(err)
	}

	allChunks := append(fromCache, fromStorage...)

	// Filter out chunks
	filteredChunks := make([]Chunk, 0, len(allChunks))
outer:
	for _, chunk := range allChunks {
		for _, filter := range filters {
			if !filter.Matches(string(chunk.Metric[model.LabelName(filter.Name)])) {
				continue outer
			}
		}
		filteredChunks = append(filteredChunks, chunk)
	}

	return filteredChunks, nil
}

// ProcessCacheResponse decodes the chunks coming back from the cache, separating
// hits and misses.
func ProcessCacheResponse(chunks []Chunk, keys []string, bufs [][]byte) (found []Chunk, missing []Chunk, err error) {
	decodeContext := NewDecodeContext()

	i, j := 0, 0
	for i < len(chunks) && j < len(keys) {
		chunkKey := chunks[i].ExternalKey()

		if chunkKey < keys[j] {
			missing = append(missing, chunks[i])
			i++
		} else if chunkKey > keys[j] {
			level.Debug(util.Logger).Log("msg", "got chunk from cache we didn't ask for")
			j++
		} else {
			chunk := chunks[i]
			err = chunk.Decode(decodeContext, bufs[j])
			if err != nil {
				cacheCorrupt.Inc()
				return
			}
			found = append(found, chunk)
			i++
			j++
		}
	}

	for ; i < len(chunks); i++ {
		missing = append(missing, chunks[i])
	}

	return
}

func (c *Store) getSeriesMatrix(ctx context.Context, from, through model.Time, allMatchers []*labels.Matcher, metricNameMatcher *labels.Matcher) (model.Matrix, error) {
	// Get all series from the index
	userID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return nil, err
	}
	seriesQueries, err := c.schema.GetReadQueries(from, through, userID)
	if err != nil {
		return nil, err
	}
	seriesEntries, err := c.lookupEntriesByQueries(ctx, seriesQueries)
	if err != nil {
		return nil, err
	}

	chunks := make([]Chunk, 0, len(seriesEntries))
outer:
	for _, seriesEntry := range seriesEntries {
		metric, err := parseSeriesRangeValue(seriesEntry.RangeValue, seriesEntry.Value)
		if err != nil {
			return nil, err
		}

		// Apply metric name matcher
		if metricNameMatcher != nil && !metricNameMatcher.Matches(string(metric[model.LabelName(metricNameMatcher.Name)])) {
			continue outer
		}

		// Apply matchers
		for _, matcher := range allMatchers {
			if !matcher.Matches(string(metric[model.LabelName(matcher.Name)])) {
				continue outer
			}
		}

		var matchers []*labels.Matcher
		for labelName, labelValue := range metric {
			if labelName == "__name__" {
				continue
			}

			matcher, err := labels.NewMatcher(labels.MatchEqual, string(labelName), string(labelValue))
			if err != nil {
				return nil, err
			}
			matchers = append(matchers, matcher)
		}

		cs, err := c.getMetricNameChunks(ctx, from, through, matchers, string(metric[model.MetricNameLabel]))
		if err != nil {
			return nil, err
		}

		for _, chunk := range cs {
			// getMetricNameChunks() may have selected too many metrics - metrics that match all matchers,
			// but also have additional labels. We don't want to return those.
			if chunk.Metric.Equal(metric) {
				chunks = append(chunks, chunk)
			}
		}
	}
	return chunksToMatrix(ctx, chunks, from, through)
}

func (c *Store) lookupChunksByMetricName(ctx context.Context, from, through model.Time, matchers []*labels.Matcher, metricName string) ([]Chunk, error) {
	log, ctx := newSpanLogger(ctx, "ChunkStore.lookupChunksByMetricName")

	userID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return nil, err
	}

	// Just get chunks for metric if there are no matchers
	if len(matchers) == 0 {
		queries, err := c.schema.GetReadQueriesForMetric(from, through, userID, model.LabelValue(metricName))
		if err != nil {
			return nil, err
		}
		level.Debug(log).Log("queries", len(queries))

		entries, err := c.lookupEntriesByQueries(ctx, queries)
		if err != nil {
			return nil, err
		}
		level.Debug(log).Log("entries", len(entries))

		chunkIDs, err := c.parseIndexEntries(ctx, entries, nil)
		if err != nil {
			return nil, err
		}
		level.Debug(log).Log("chunkIDs", len(chunkIDs))

		return c.convertChunkIDsToChunks(ctx, chunkIDs)
	}

	// Otherwise get chunks which include other matchers
	incomingChunkIDs := make(chan []string)
	incomingErrors := make(chan error)
	for _, matcher := range matchers {
		go func(matcher *labels.Matcher) {
			// Lookup IndexQuery's
			var queries []IndexQuery
			var err error
			if matcher.Type != labels.MatchEqual {
				queries, err = c.schema.GetReadQueriesForMetricLabel(from, through, userID, model.LabelValue(metricName), model.LabelName(matcher.Name))
			} else {
				queries, err = c.schema.GetReadQueriesForMetricLabelValue(from, through, userID, model.LabelValue(metricName), model.LabelName(matcher.Name), model.LabelValue(matcher.Value))
			}
			if err != nil {
				incomingErrors <- err
				return
			}
			level.Debug(log).Log("matcher", matcher, "queries", len(queries))

			// Lookup IndexEntry's
			entries, err := c.lookupEntriesByQueries(ctx, queries)
			if err != nil {
				incomingErrors <- err
				return
			}
			level.Debug(log).Log("matcher", matcher, "entries", len(entries))

			// Convert IndexEntry's to chunk IDs, filter out non-matchers at the same time.
			chunkIDs, err := c.parseIndexEntries(ctx, entries, matcher)
			if err != nil {
				incomingErrors <- err
				return
			}
			level.Debug(log).Log("matcher", matcher, "chunkIDs", len(chunkIDs))
			incomingChunkIDs <- chunkIDs
		}(matcher)
	}

	// Receive chunkSets from all matchers
	var chunkIDs []string
	var lastErr error
	for i := 0; i < len(matchers); i++ {
		select {
		case incoming := <-incomingChunkIDs:
			if chunkIDs == nil {
				chunkIDs = incoming
			} else {
				chunkIDs = intersectStrings(chunkIDs, incoming)
			}
		case err := <-incomingErrors:
			lastErr = err
		}
	}
	if lastErr != nil {
		return nil, lastErr
	}

	level.Debug(log).Log("msg", "post intersection", "entries", len(chunkIDs))

	// Convert IndexEntry's into chunks
	return c.convertChunkIDsToChunks(ctx, chunkIDs)
}

func (c *Store) lookupEntriesByQueries(ctx context.Context, queries []IndexQuery) ([]IndexEntry, error) {
	incomingEntries := make(chan []IndexEntry)
	incomingErrors := make(chan error)
	for _, query := range queries {
		go func(query IndexQuery) {
			entries, err := c.lookupEntriesByQuery(ctx, query)
			if err != nil {
				incomingErrors <- err
			} else {
				incomingEntries <- entries
			}
		}(query)
	}

	// Combine the results into one slice
	var entries []IndexEntry
	var lastErr error
	for i := 0; i < len(queries); i++ {
		select {
		case incoming := <-incomingEntries:
			entries = append(entries, incoming...)
		case err := <-incomingErrors:
			lastErr = err
		}
	}

	return entries, lastErr
}

func (c *Store) lookupEntriesByQuery(ctx context.Context, query IndexQuery) ([]IndexEntry, error) {
	var entries []IndexEntry

	if err := c.storage.QueryPages(ctx, query, func(resp ReadBatch) (shouldContinue bool) {
		for i := 0; i < resp.Len(); i++ {
			entries = append(entries, IndexEntry{
				TableName:  query.TableName,
				HashValue:  query.HashValue,
				RangeValue: resp.RangeValue(i),
				Value:      resp.Value(i),
			})
		}
		return true
	}); err != nil {
		level.Error(util.WithContext(ctx, util.Logger)).Log("msg", "error querying storage", "err", err)
		return nil, err
	}

	return entries, nil
}

func (c *Store) parseIndexEntries(ctx context.Context, entries []IndexEntry, matcher *labels.Matcher) ([]string, error) {
	result := make([]string, 0, len(entries))

	for _, entry := range entries {
		chunkKey, labelValue, _, err := parseChunkTimeRangeValue(entry.RangeValue, entry.Value)
		if err != nil {
			return nil, err
		}

		if matcher != nil && !matcher.Matches(string(labelValue)) {
			level.Debug(util.WithContext(ctx, util.Logger)).Log("msg", "dropping chunk for non-matching label", "label", labelValue)
			continue
		}
		result = append(result, chunkKey)
	}

	// Return ids sorted and deduped because they will be merged with other sets.
	sort.Strings(result)
	result = uniqueStrings(result)
	return result, nil
}

func (c *Store) convertChunkIDsToChunks(ctx context.Context, chunkIDs []string) ([]Chunk, error) {
	userID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return nil, err
	}

	chunkSet := make([]Chunk, 0, len(chunkIDs))
	for _, chunkID := range chunkIDs {
		chunk, err := ParseExternalKey(userID, chunkID)
		if err != nil {
			return nil, err
		}
		chunkSet = append(chunkSet, chunk)
	}

	return chunkSet, nil
}

func (c *Store) writeBackCache(ctx context.Context, chunks []Chunk) error {
	for i := range chunks {
		encoded, err := chunks[i].Encode()
		if err != nil {
			return err
		}
		if err := c.cache.StoreChunk(ctx, chunks[i].ExternalKey(), encoded); err != nil {
			return err
		}
	}
	return nil
}
