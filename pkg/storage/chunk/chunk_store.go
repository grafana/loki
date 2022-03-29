package chunk

import (
	"context"
	"flag"
	"fmt"
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
	"github.com/grafana/loki/pkg/util/validation"
)

var (
	ErrQueryMustContainMetricName = QueryError("query must contain metric name")
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

// Stop any background goroutines (ie in the cache.)
func (c *seriesStore) Stop() {
	c.fetcher.storage.Stop()
	c.fetcher.Stop()
	c.indexClient.Stop()
}

func (c *seriesStore) validateQueryTimeRange(ctx context.Context, userID string, from *model.Time, through *model.Time) (bool, error) {
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

func (c *seriesStore) validateQuery(ctx context.Context, userID string, from *model.Time, through *model.Time, matchers []*labels.Matcher) (string, []*labels.Matcher, bool, error) {
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

// Using this function avoids logging of nil matcher, which works, but indirectly via panic and recover.
// That confuses attached debugger, which wants to breakpoint on each panic.
// Using simple check is also faster.
func formatMatcher(matcher *labels.Matcher) string {
	if matcher == nil {
		return "nil"
	}
	return matcher.String()
}

func (c *seriesStore) GetChunkFetcher(_ model.Time) *Fetcher {
	return c.fetcher
}
