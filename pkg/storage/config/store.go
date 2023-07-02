package config

import (
	"flag"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/storage/chunk/cache"
)

type ChunkStoreConfig struct {
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

	// Limits query start time to be greater than now() - MaxLookBackPeriod, if set.
	// Will be deprecated in the next major release.
	MaxLookBackPeriod model.Duration `yaml:"max_look_back_period"`
}

func (cfg *ChunkStoreConfig) ChunkCacheStubs() bool {
	return cfg.chunkCacheStubs
}

// RegisterFlags adds the flags required to configure this flag set.
func (cfg *ChunkStoreConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.ChunkCacheConfig.RegisterFlagsWithPrefix("store.chunks-cache.", "", f)
	f.BoolVar(&cfg.chunkCacheStubs, "store.chunks-cache.cache-stubs", false, "If true, don't write the full chunk to cache, just a stub entry.")
	cfg.WriteDedupeCacheConfig.RegisterFlagsWithPrefix("store.index-cache-write.", "", f)

	f.Var(&cfg.CacheLookupsOlderThan, "store.cache-lookups-older-than", "Cache index entries older than this period. 0 to disable.")
	f.Var(&cfg.MaxLookBackPeriod, "store.max-look-back-period", "This flag is deprecated. Use -querier.max-query-lookback instead.")
}

func (cfg *ChunkStoreConfig) Validate(logger log.Logger) error {
	if cfg.MaxLookBackPeriod > 0 {
		flagext.DeprecatedFlagsUsed.Inc()
		level.Warn(logger).Log("msg", "running with DEPRECATED flag -store.max-look-back-period, use -querier.max-query-lookback instead.")
	}
	if err := cfg.ChunkCacheConfig.Validate(); err != nil {
		return err
	}
	return cfg.WriteDedupeCacheConfig.Validate()
}
