package chunk

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"sort"

	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"

	"github.com/weaveworks/common/user"
	"github.com/weaveworks/cortex/pkg/util"
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
)

func init() {
	prometheus.MustRegister(indexEntriesPerChunk)
	prometheus.MustRegister(rowWrites)
}

// StoreConfig specifies config for a ChunkStore
type StoreConfig struct {
	CacheConfig

	// For injecting different schemas in tests.
	schemaFactory func(cfg SchemaConfig) Schema
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *StoreConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.CacheConfig.RegisterFlags(f)
}

// Store implements Store
type Store struct {
	cfg StoreConfig

	storage StorageClient
	cache   *Cache
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

	return &Store{
		cfg:     cfg,
		storage: storage,
		schema:  schema,
		cache:   NewCache(cfg.CacheConfig),
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
		metricName, err := util.ExtractMetricNameFromMetric(chunk.Metric)
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

// Get implements ChunkStore
func (c *Store) Get(ctx context.Context, from, through model.Time, allMatchers ...*labels.Matcher) (model.Matrix, error) {
	if through < from {
		return nil, fmt.Errorf("invalid query, through < from (%d < %d)", through, from)
	}

	now := model.Now()
	if from.After(now) {
		// time-span start is in future ... regard as legal
		level.Error(util.WithContext(ctx, util.Logger)).Log("msg", "whole timerange in future, yield empty resultset", "through", through, "from", from, "now", now)
		return nil, nil
	}

	if through.After(now) {
		// time-span end is in future ... regard as legal
		level.Error(util.WithContext(ctx, util.Logger)).Log("msg", "adjusting end timerange from future to now", "old_through", through, "new_through", now)
		through = now // Avoid processing future part - otherwise some schemas could fail with eg non-existent table gripes
	}

	// Fetch metric name chunks if the matcher is of type equal,
	metricNameMatcher, matchers, ok := util.ExtractMetricNameMatcherFromMatchers(allMatchers)
	if ok && metricNameMatcher.Type == labels.MatchEqual {
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
	return chunksToMatrix(chunks)
}

func (c *Store) getMetricNameChunks(ctx context.Context, from, through model.Time, allMatchers []*labels.Matcher, metricName string) ([]Chunk, error) {
	logger := util.WithContext(ctx, util.Logger)
	filters, matchers := util.SplitFiltersAndMatchers(allMatchers)
	chunks, err := c.lookupChunksByMetricName(ctx, from, through, matchers, metricName)
	if err != nil {
		return nil, err
	}

	// Filter out chunks that are not in the selected time range.
	filtered := make([]Chunk, 0, len(chunks))
	for _, chunk := range chunks {
		if chunk.Through < from || through < chunk.From {
			continue
		}
		filtered = append(filtered, chunk)
	}

	// Now fetch the actual chunk data from Memcache / S3
	fromCache, missing, err := c.cache.FetchChunkData(ctx, filtered)
	if err != nil {
		level.Warn(logger).Log("msg", "error fetching from cache", "err", err)
	}

	fromStorage, err := c.storage.GetChunks(ctx, missing)

	// Always cache any chunks we did get
	if cacheErr := c.writeBackCache(ctx, fromStorage); cacheErr != nil {
		level.Warn(logger).Log("msg", "could not store chunks in chunk cache", "err", cacheErr)
	}

	if err != nil {
		return nil, promql.ErrStorage(err)
	}

	// TODO instead of doing this sort, propagate an index and assign chunks
	// into the result based on that index.
	allChunks := append(fromCache, fromStorage...)
	sort.Sort(ByKey(allChunks))

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
	return chunksToMatrix(chunks)
}

func (c *Store) lookupChunksByMetricName(ctx context.Context, from, through model.Time, matchers []*labels.Matcher, metricName string) ([]Chunk, error) {
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

		entries, err := c.lookupEntriesByQueries(ctx, queries)
		if err != nil {
			return nil, err
		}

		return c.convertIndexEntriesToChunks(ctx, entries, nil)
	}

	// Otherwise get chunks which include other matchers
	incomingChunkSets := make(chan ByKey)
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

			// Lookup IndexEntry's
			entries, err := c.lookupEntriesByQueries(ctx, queries)
			if err != nil {
				incomingErrors <- err
				return
			}

			// Convert IndexEntry's into chunks
			chunks, err := c.convertIndexEntriesToChunks(ctx, entries, matcher)
			if err != nil {
				incomingErrors <- err
			} else {
				incomingChunkSets <- chunks
			}
		}(matcher)
	}

	// Receive chunkSets from all matchers
	var chunkSets []ByKey
	var lastErr error
	for i := 0; i < len(matchers); i++ {
		select {
		case incoming := <-incomingChunkSets:
			chunkSets = append(chunkSets, incoming)
		case err := <-incomingErrors:
			lastErr = err
		}
	}

	// Merge chunkSets in order because we wish to keep label series together consecutively
	return nWayIntersect(chunkSets), lastErr
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

	if err := c.storage.QueryPages(ctx, query, func(resp ReadBatch, lastPage bool) (shouldContinue bool) {
		for i := 0; i < resp.Len(); i++ {
			entries = append(entries, IndexEntry{
				TableName:  query.TableName,
				HashValue:  query.HashValue,
				RangeValue: resp.RangeValue(i),
				Value:      resp.Value(i),
			})
		}
		return !lastPage
	}); err != nil {
		level.Error(util.WithContext(ctx, util.Logger)).Log("msg", "error querying storage", "err", err)
		return nil, err
	}

	return entries, nil
}

func (c *Store) convertIndexEntriesToChunks(ctx context.Context, entries []IndexEntry, matcher *labels.Matcher) (ByKey, error) {
	userID, err := user.ExtractOrgID(ctx)
	if err != nil {
		return nil, err
	}

	var chunkSet ByKey

	for _, entry := range entries {
		chunkKey, labelValue, metadataInIndex, err := parseChunkTimeRangeValue(entry.RangeValue, entry.Value)
		if err != nil {
			return nil, err
		}

		chunk, err := parseExternalKey(userID, chunkKey)
		if err != nil {
			return nil, err
		}

		// This can be removed in Dev 2017, 13 months after the last chunks
		// was written with metadata in the index.
		if metadataInIndex && entry.Value != nil {
			if err := json.Unmarshal(entry.Value, &chunk); err != nil {
				return nil, err
			}
			chunk.metadataInIndex = true
		}

		if matcher != nil && !matcher.Matches(string(labelValue)) {
			level.Debug(util.WithContext(ctx, util.Logger)).Log("msg", "dropping chunk for non-matching metric", "metric", chunk.Metric)
			continue
		}
		chunkSet = append(chunkSet, chunk)
	}

	// Return chunks sorted and deduped because they will be merged with other sets
	sort.Sort(chunkSet)
	return unique(chunkSet), nil
}

func (c *Store) writeBackCache(_ context.Context, chunks []Chunk) error {
	for i := range chunks {
		encoded, err := chunks[i].Encode()
		if err != nil {
			return err
		}
		c.cache.BackgroundWrite(chunks[i].ExternalKey(), encoded)
	}
	return nil
}
