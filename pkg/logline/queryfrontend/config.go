package queryfrontend

import (
	"flag"
	"fmt"
	"time"
)

const (
	defaultNgramLength                    = 6
	defaultMaxHintParallel                = 64
	defaultHintTimeout                    = 15 * time.Second
	defaultMinQueryBytes                  = int64(500 * 1024 * 1024 * 1024) // 500 GB
	defaultShardPlanningEnabled           = true
	defaultShardPlanningMinReductionRatio = 0.75

	shardPlanningStrategyPowerOfTwo = "power_of_two"
)

// ShardPlanningConfig controls cancel-then-rerun behavior: when logline hints
// finish before the provisional query and show sufficient narrowing, the
// in-flight query is cancelled and restarted with power_of_two TSDB sharding.
type ShardPlanningConfig struct {
	Enabled               bool    `yaml:"enabled"`
	MinTimeReductionRatio float64 `yaml:"min_time_reduction_ratio"`
}

func (c *ShardPlanningConfig) applyDefaults() {
	if c.MinTimeReductionRatio == 0 {
		c.MinTimeReductionRatio = defaultShardPlanningMinReductionRatio
	}
}

func (c *ShardPlanningConfig) UnmarshalYAML(unmarshal func(any) error) error {
	type plain ShardPlanningConfig
	defaults := ShardPlanningConfig{Enabled: defaultShardPlanningEnabled}
	defaults.applyDefaults()
	*c = defaults
	return unmarshal((*plain)(c))
}

// MiddlewareConfig controls logline query narrowing behavior.
type MiddlewareConfig struct {
	// DryRun performs passive hint lookups for verification without modifying
	// query execution.
	DryRun bool `yaml:"dry_run"`
	// RequireOptInHeader requires X-Logline-Index header to activate logline
	// behavior. Applies to both active narrowing and dry-run mode.
	RequireOptInHeader bool `yaml:"require_opt_in_header"`
	// NgramLength is the n-gram size used for hint lookups against the logline index.
	// Defaults to 6 when zero.
	NgramLength int `yaml:"ngram_length"`
	// MaxHintParallel is the maximum number of concurrent hint index workers
	// dispatched per query. Defaults to 64 when zero.
	MaxHintParallel int `yaml:"max_hint_parallel"`
	// QueryIngestersWithin is the window of recent data that lives only in
	// ingesters (not yet flushed to object storage). The logline index cannot
	// cover this window, so it is always passed through to the Loki pipeline.
	// Populated from Loki's querier.query_ingesters_within at assembly time.
	QueryIngestersWithin time.Duration `yaml:"query_ingesters_within"`
	// HintTimeout is the maximum time to wait for the logline index hint
	// lookup before falling back to passthrough. Defaults to 15s when zero.
	HintTimeout time.Duration `yaml:"hint_timeout"`
	// HintCacheTTL is the TTL for cached hint results. When no external cache
	// backend (memcached/redis) is available from Loki's results cache config,
	// an in-process embedded cache is used automatically.
	// Set to 0 to disable hint caching.
	HintCacheTTL time.Duration `yaml:"hint_cache_ttl"`
	// HintCacheMaxSizeMB is the maximum size in megabytes for the embedded
	// hint cache. Only used when no external cache backend is configured.
	// Defaults to 100 when zero.
	HintCacheMaxSizeMB int64 `yaml:"hint_cache_max_size_mb"`
	// MinQueryBytesForIndex is the minimum query size (from index stats bytes)
	// required before running logline index hint lookup.
	// Set to 0 to disable stats-based gating.
	MinQueryBytesForIndex int64 `yaml:"min_query_bytes_for_index"`
	// ShardPlanning controls optional live-query reruns that force Loki's TSDB
	// shard planner to use a configured strategy when hints prove the request is narrow.
	ShardPlanning ShardPlanningConfig `yaml:"shard_planning"`
}

// RegisterFlags registers configuration flags with the given FlagSet.
func (c *MiddlewareConfig) RegisterFlags(f *flag.FlagSet) {
	c.RegisterFlagsWithPrefix("query-frontend", f)
}

// RegisterFlagsWithPrefix registers middleware flags under a custom prefix.
func (c *MiddlewareConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	if f == nil {
		f = flag.CommandLine
	}
	if prefix == "" {
		prefix = "query-frontend"
	}

	f.IntVar(&c.NgramLength, prefix+".ngram-length", 0,
		"N-gram size for hint lookups against the logline index (default 6)")
	f.BoolVar(&c.DryRun, prefix+".dry-run", false,
		"Run logline hint lookups passively for verification without modifying query execution")
	f.BoolVar(&c.RequireOptInHeader, prefix+".require-opt-in-header", true,
		"Require X-Logline-Index header to activate hint narrowing")
	f.IntVar(&c.MaxHintParallel, prefix+".max-hint-parallel", 0,
		"Maximum concurrent hint index workers per query (default 64)")
	f.DurationVar(&c.HintTimeout, prefix+".hint-timeout", 0,
		"Maximum time to wait for logline index hint lookup (default 15s)")
	f.DurationVar(&c.HintCacheTTL, prefix+".hint-cache-ttl", 2*time.Minute,
		"TTL for cached hint results. Uses Loki results cache backend; set to 0 to disable.")
	f.Int64Var(&c.HintCacheMaxSizeMB, prefix+".hint-cache-max-size-mb", 0,
		"Max size in MB for embedded hint cache when no external backend is configured (default 100)")
	f.Int64Var(&c.MinQueryBytesForIndex, prefix+".min-query-bytes-for-index", defaultMinQueryBytes,
		"Minimum index-stats bytes required before performing logline hint lookup; set to 0 to disable (default 500 GB)")
	f.BoolVar(&c.ShardPlanning.Enabled, prefix+".shard-planning.enabled", defaultShardPlanningEnabled,
		"Enable logline shard-planning reruns for narrow live queries")
	f.Float64Var(&c.ShardPlanning.MinTimeReductionRatio, prefix+".shard-planning.min-time-reduction-ratio", defaultShardPlanningMinReductionRatio,
		"Minimum hinted time reduction ratio required for a shard-planning rerun (default 0.75)")
}

// Validate checks constraints and applies defaults for zero-valued fields.
func (c *MiddlewareConfig) Validate() error {
	if c.NgramLength <= 0 {
		c.NgramLength = defaultNgramLength
	}
	if c.MaxHintParallel <= 0 {
		c.MaxHintParallel = defaultMaxHintParallel
	}
	if c.HintTimeout <= 0 {
		c.HintTimeout = defaultHintTimeout
	}
	if c.HintCacheTTL < 0 {
		return fmt.Errorf("hint_cache_ttl must be >= 0")
	}
	if c.HintCacheMaxSizeMB < 0 {
		return fmt.Errorf("hint_cache_max_size_mb must be >= 0")
	}
	if c.HintCacheMaxSizeMB == 0 {
		c.HintCacheMaxSizeMB = 100
	}
	if c.MinQueryBytesForIndex < 0 {
		return fmt.Errorf("min_query_bytes_for_index must be >= 0")
	}
	if err := c.ShardPlanning.Validate(); err != nil {
		return fmt.Errorf("invalid shard_planning config: %w", err)
	}
	return nil
}

func (c *ShardPlanningConfig) Validate() error {
	zero := *c == ShardPlanningConfig{}
	if c.MinTimeReductionRatio < 0 {
		return fmt.Errorf("min_time_reduction_ratio must be >= 0")
	}
	if zero {
		c.Enabled = defaultShardPlanningEnabled
	}
	if !c.Enabled {
		return nil
	}
	c.applyDefaults()
	return nil
}

// Config controls logline query frontend middleware injection.
type Config struct {
	Enabled       bool             `yaml:"enabled"`
	QueryFrontend MiddlewareConfig `yaml:"query_frontend"`
}

// RegisterFlags registers the integration flags for the logline middleware.
func (c *Config) RegisterFlags(f *flag.FlagSet) {
	if f == nil {
		f = flag.CommandLine
	}

	f.BoolVar(&c.Enabled, "logline.enabled", false, "Enable logline query frontend middleware injection")
	c.QueryFrontend.RegisterFlagsWithPrefix("logline-query-frontend", f)
}

// Validate checks constraints and applies defaults.
func (c *Config) Validate() error {
	if err := c.QueryFrontend.Validate(); err != nil {
		return fmt.Errorf("invalid query frontend config: %w", err)
	}
	return nil
}
