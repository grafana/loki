package chunk

import (
	"context"
	"flag"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/util/extract"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/util/spanlogger"
	"github.com/grafana/loki/pkg/util/validation"
)

var (
	ErrQueryMustContainMetricName = QueryError("query must contain metric name")
	ErrMetricNameLabelMissing     = errors.New("metric name label missing")
	ErrParialDeleteChunkNoOverlap = errors.New("interval for partial deletion has not overlap with chunk interval")

	indexEntriesPerChunk = promauto.NewHistogram(prometheus.HistogramOpts{
		Namespace: "loki",
		Name:      "chunk_store_index_entries_per_chunk",
		Help:      "Number of entries written to storage per chunk.",
		Buckets:   prometheus.ExponentialBuckets(1, 2, 5),
	})
	cacheCorrupt = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "loki",
		Name:      "cache_corrupt_chunks_total",
		Help:      "Total count of corrupt chunks found in cache.",
	})
)

// Query errors are to be treated as user errors, rather than storage errors.
type QueryError string

func (e QueryError) Error() string {
	return string(e)
}

// StoreConfig specifies config for a ChunkStore
type StoreConfig struct {
	ChunkCacheConfig       cache.Config `yaml:"chunk_cache_config"`
	WriteDedupeCacheConfig cache.Config `yaml:"write_dedupe_cache_config"`

	CacheLookupsOlderThan model.Duration `yaml:"cache_lookups_older_than"`

	// Not visible in yaml because the setting shouldn't be common between ingesters and queriers.
	// This exists in case we don't want to cache all the chunks but still want to take advantage of
	// ingester chunk write deduplication. But for the queriers we need the full value. So when this option
	// is set, use different caches for ingesters and queriers.
	chunkCacheStubs bool // don't write the full chunk to cache, just a stub entry

	// When DisableIndexDeduplication is true and chunk is already there in cache, only index would be written to the store and not chunk.
	DisableIndexDeduplication bool `yaml:"-"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *StoreConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.ChunkCacheConfig.RegisterFlagsWithPrefix("store.chunks-cache.", "Cache config for chunks. ", f)
	f.BoolVar(&cfg.chunkCacheStubs, "store.chunks-cache.cache-stubs", false, "If true, don't write the full chunk to cache, just a stub entry.")
	cfg.WriteDedupeCacheConfig.RegisterFlagsWithPrefix("store.index-cache-write.", "Cache config for index entry writing.", f)

	f.Var(&cfg.CacheLookupsOlderThan, "store.cache-lookups-older-than", "Cache index entries older than this period. 0 to disable.")
}

// Validate validates the store config.
func (cfg *StoreConfig) Validate(logger log.Logger) error {
	if err := cfg.ChunkCacheConfig.Validate(); err != nil {
		return err
	}
	return cfg.WriteDedupeCacheConfig.Validate()
}

type baseStore struct {
	cfg StoreConfig
	// todo (callum) it looks like baseStore is created off a specific schema struct implementation, so perhaps we can store something else here
	// other than the entire set of schema period configs
	schemaCfg SchemaConfig

	index   IndexClient
	chunks  Client
	schema  BaseSchema
	limits  StoreLimits
	fetcher *Fetcher
}

func newBaseStore(cfg StoreConfig, scfg SchemaConfig, schema BaseSchema, index IndexClient, chunks Client, limits StoreLimits, chunksCache cache.Cache) (baseStore, error) {
	fetcher, err := NewChunkFetcher(chunksCache, cfg.chunkCacheStubs, scfg, chunks, cfg.ChunkCacheConfig.AsyncCacheWriteBackConcurrency, cfg.ChunkCacheConfig.AsyncCacheWriteBackBufferSize)
	if err != nil {
		return baseStore{}, err
	}

	return baseStore{
		cfg:       cfg,
		schemaCfg: scfg,
		index:     index,
		chunks:    chunks,
		schema:    schema,
		limits:    limits,
		fetcher:   fetcher,
	}, nil
}

// Stop any background goroutines (ie in the cache.)
func (c *baseStore) Stop() {
	c.fetcher.storage.Stop()
	c.fetcher.Stop()
	c.index.Stop()
}

// LabelValuesForMetricName retrieves all label values for a single label name and metric name.
func (c *baseStore) LabelValuesForMetricName(ctx context.Context, userID string, from, through model.Time, metricName, labelName string, matchers ...*labels.Matcher) ([]string, error) {
	log, ctx := spanlogger.New(ctx, "ChunkStore.LabelValues")
	defer log.Span.Finish()
	level.Debug(log).Log("from", from, "through", through, "metricName", metricName, "labelName", labelName)

	shortcut, err := c.validateQueryTimeRange(ctx, userID, &from, &through)
	if err != nil {
		return nil, err
	} else if shortcut {
		return nil, nil
	}

	if len(matchers) == 0 {
		queries, err := c.schema.GetReadQueriesForMetricLabel(from, through, userID, metricName, labelName)
		if err != nil {
			return nil, err
		}

		entries, err := c.lookupEntriesByQueries(ctx, queries)
		if err != nil {
			return nil, err
		}

		var result UniqueStrings
		for _, entry := range entries {
			_, labelValue, err := parseChunkTimeRangeValue(entry.RangeValue, entry.Value)
			if err != nil {
				return nil, err
			}
			result.Add(string(labelValue))
		}
		return result.Strings(), nil
	}

	return nil, errors.New("unimplemented: Matchers are not supported by chunk store")
}

func (c *baseStore) validateQueryTimeRange(ctx context.Context, userID string, from *model.Time, through *model.Time) (bool, error) {
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

func (c *baseStore) validateQuery(ctx context.Context, userID string, from *model.Time, through *model.Time, matchers []*labels.Matcher) (string, []*labels.Matcher, bool, error) {
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

func (c *baseStore) lookupIdsByMetricNameMatcher(ctx context.Context, from, through model.Time, userID, metricName string, matcher *labels.Matcher, filter func([]IndexQuery) []IndexQuery) ([]string, error) {
	var err error
	var queries []IndexQuery
	var labelName string
	if matcher == nil {
		queries, err = c.schema.GetReadQueriesForMetric(from, through, userID, metricName)
	} else if matcher.Type == labels.MatchEqual {
		labelName = matcher.Name
		queries, err = c.schema.GetReadQueriesForMetricLabelValue(from, through, userID, metricName, matcher.Name, matcher.Value)
	} else {
		labelName = matcher.Name
		queries, err = c.schema.GetReadQueriesForMetricLabel(from, through, userID, metricName, matcher.Name)
	}
	if err != nil {
		return nil, err
	}
	unfilteredQueries := len(queries)

	if filter != nil {
		queries = filter(queries)
	}

	entries, err := c.lookupEntriesByQueries(ctx, queries)
	if e, ok := err.(CardinalityExceededError); ok {
		e.MetricName = metricName
		e.LabelName = labelName
		return nil, e
	} else if err != nil {
		return nil, err
	}

	ids, err := c.parseIndexEntries(ctx, entries, matcher)
	if err != nil {
		return nil, err
	}
	level.Debug(util_log.WithContext(ctx, util_log.Logger)).
		Log(
			"msg", "Store.lookupIdsByMetricNameMatcher",
			"matcher", formatMatcher(matcher),
			"queries", unfilteredQueries,
			"filteredQueries", len(queries),
			"entries", len(entries),
			"ids", len(ids),
		)

	return ids, nil
}

// Using this function avoids logging of nil matcher, which works, but indirectly via panic and recover.
// That confuses attached debugger, which wants to breakpoint on each panic.
// Using simple check is also faster.
func formatMatcher(matcher *labels.Matcher) string {
	if matcher == nil {
		return "nil"
	}
	return matcher.String()
}

func (c *baseStore) lookupEntriesByQueries(ctx context.Context, queries []IndexQuery) ([]IndexEntry, error) {
	// Nothing to do if there are no queries.
	if len(queries) == 0 {
		return nil, nil
	}

	var lock sync.Mutex
	var entries []IndexEntry
	err := c.index.QueryPages(ctx, queries, func(query IndexQuery, resp ReadBatch) bool {
		iter := resp.Iterator()
		lock.Lock()
		for iter.Next() {
			entries = append(entries, IndexEntry{
				TableName:  query.TableName,
				HashValue:  query.HashValue,
				RangeValue: iter.RangeValue(),
				Value:      iter.Value(),
			})
		}
		lock.Unlock()
		return true
	})
	if err != nil {
		level.Error(util_log.WithContext(ctx, util_log.Logger)).Log("msg", "error querying storage", "err", err)
	}
	return entries, err
}

func (c *baseStore) parseIndexEntries(_ context.Context, entries []IndexEntry, matcher *labels.Matcher) ([]string, error) {
	// Nothing to do if there are no entries.
	if len(entries) == 0 {
		return nil, nil
	}

	matchSet := map[string]struct{}{}
	if matcher != nil && matcher.Type == labels.MatchRegexp {
		set := FindSetMatches(matcher.Value)
		for _, v := range set {
			matchSet[v] = struct{}{}
		}
	}

	result := make([]string, 0, len(entries))
	for _, entry := range entries {
		chunkKey, labelValue, err := parseChunkTimeRangeValue(entry.RangeValue, entry.Value)
		if err != nil {
			return nil, err
		}

		// If the matcher is like a set (=~"a|b|c|d|...") and
		// the label value is not in that set move on.
		if len(matchSet) > 0 {
			if _, ok := matchSet[string(labelValue)]; !ok {
				continue
			}

			// If its in the set, then add it to set, we don't need to run
			// matcher on it again.
			result = append(result, chunkKey)
			continue
		}

		if matcher != nil && !matcher.Matches(string(labelValue)) {
			continue
		}
		result = append(result, chunkKey)
	}
	// Return ids sorted and deduped because they will be merged with other sets.
	sort.Strings(result)
	result = uniqueStrings(result)
	return result, nil
}

func (c *baseStore) convertChunkIDsToChunks(_ context.Context, userID string, chunkIDs []string) ([]Chunk, error) {
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

func (c *baseStore) GetChunkFetcher(_ model.Time) *Fetcher {
	return c.fetcher
}
