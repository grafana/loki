package queryfrontend

import (
	"flag"
	"fmt"
	"time"
)

const (
	defaultNgramLength                    = 6
	defaultMaxHintParallel                = 64
	defaultMaxHintDaysParallel            = 7
	defaultHintTimeout                    = 15 * time.Second
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

// Config is logline's read path configuration.
type Config struct {
	// Enabled turns on logline filtering in the query path.
	Enabled bool `yaml:"enabled"`
	// DryRun performs passive hint lookups for verification without modifying
	// query execution.
	DryRun bool `yaml:"dry_run"`
	// RequireOptInHeader requires X-Logline-Index header to activate logline
	// behavior. Applies to both active narrowing and dry-run mode.
	RequireOptInHeader bool `yaml:"require_opt_in_header"`
	// NgramLength is the n-gram size used for hint lookups against the logline index.
	// Defaults to 6 when zero.
	NgramLength int `yaml:"-"`
	// MaxHintParallel is the maximum number of concurrent hint index workers
	// dispatched per query. Defaults to 64 when zero.
	MaxHintParallel int `yaml:"max_hint_parallel"`
	// MaxHintDaysParallel is the max concurrent cache-miss day fetches
	// in CachingHintProvider. Defaults to 7 when zero.
	MaxHintDaysParallel int `yaml:"max_hint_days_parallel"`
	// QueryIngestersWithin is the window of recent data that lives only in
	// ingesters (not yet flushed to object storage). The logline index cannot
	// cover this window, so it is always passed through to the Loki pipeline.
	// Populated from Loki's querier.query_ingesters_within at assembly time.
	QueryIngestersWithin time.Duration `yaml:"query_ingesters_within"`
	// QuerySplitDuration is Loki's split_queries_by_interval. Prefetch sits
	// above SplitByInterval and uses this to apply the filter's k-budget per
	// slice. Populated from limits_config.split_queries_by_interval at assembly
	// time. Zero disables splitting, matching Loki.
	QuerySplitDuration time.Duration `yaml:"-"`
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
	// ShardPlanning controls optional live-query reruns that force Loki's TSDB
	// shard planner to use a configured strategy when hints prove the request is narrow.
	ShardPlanning ShardPlanningConfig `yaml:"shard_planning"`
}

// RegisterFlagsWithPrefix registers the section's flags under prefix.
func (c *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.BoolVar(&c.Enabled, prefix+".enabled", false,
		"Enable logline filtering in the query path.")
	f.BoolVar(&c.DryRun, prefix+".dry-run", false,
		"Run logline hint lookups passively for verification without modifying query execution")
	f.BoolVar(&c.RequireOptInHeader, prefix+".require-opt-in-header", true,
		"Require X-Logline-Index header to activate hint narrowing")
	f.IntVar(&c.MaxHintParallel, prefix+".max-hint-parallel", 0,
		"Maximum concurrent hint index workers per query (default 64)")
	f.IntVar(&c.MaxHintDaysParallel, prefix+".max-hint-days-parallel", 0,
		"Maximum concurrent hint-cache day fetches (default 7)")
	f.DurationVar(&c.HintTimeout, prefix+".hint-timeout", 0,
		"Maximum time to wait for logline index hint lookup (default 15s)")
	f.DurationVar(&c.HintCacheTTL, prefix+".hint-cache-ttl", 2*time.Minute,
		"TTL for cached hint results. Uses Loki results cache backend; set to 0 to disable.")
	f.Int64Var(&c.HintCacheMaxSizeMB, prefix+".hint-cache-max-size-mb", 0,
		"Max size in MB for embedded hint cache when no external backend is configured (default 100)")
	f.BoolVar(&c.ShardPlanning.Enabled, prefix+".shard-planning.enabled", defaultShardPlanningEnabled,
		"Enable logline shard-planning reruns for narrow live queries")
	f.Float64Var(&c.ShardPlanning.MinTimeReductionRatio, prefix+".shard-planning.min-time-reduction-ratio", defaultShardPlanningMinReductionRatio,
		"Minimum hinted time reduction ratio required for a shard-planning rerun (default 0.75)")
}

// Validate checks constraints and applies defaults for zero-valued fields.
func (c *Config) Validate() error {
	if c.NgramLength <= 0 {
		c.NgramLength = defaultNgramLength
	}
	if c.MaxHintParallel <= 0 {
		c.MaxHintParallel = defaultMaxHintParallel
	}
	if c.MaxHintDaysParallel <= 0 {
		c.MaxHintDaysParallel = defaultMaxHintDaysParallel
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
