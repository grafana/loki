package queryfrontend

import (
	"flag"
	"fmt"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/flagext"

	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

const (
	defaultNgramLength     = 6
	defaultMaxHintParallel = 64
	defaultHintTimeout     = 15 * time.Second
)

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
	// AlignQueriesWithStep floors both ends of a range metric query onto the step
	// before the splitter rounds the end up. Populated from
	// query_range.align_queries_with_step at assembly time.
	AlignQueriesWithStep bool `yaml:"-"`
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
	f.DurationVar(&c.HintTimeout, prefix+".hint-timeout", 0,
		"Maximum time to wait for logline index hint lookup (default 15s)")
	f.DurationVar(&c.HintCacheTTL, prefix+".hint-cache-ttl", 2*time.Minute,
		"TTL for cached hint results. Uses Loki results cache backend; set to 0 to disable.")
	f.Int64Var(&c.HintCacheMaxSizeMB, prefix+".hint-cache-max-size-mb", 0,
		"Max size in MB for embedded hint cache when no external backend is configured (default 100)")

	// Cancel-and-rerun no longer reads these. Keep them so existing
	// deployments still start, and remove them in a follow-up.
	const shardPlanningDeprecated = "Deprecated: cancel-and-rerun has been removed. This flag is ignored and will be removed in a future release."
	f.Var(deprecatedBoolFlag{name: prefix + ".shard-planning.enabled", logger: util_log.Logger}, prefix+".shard-planning.enabled", shardPlanningDeprecated)
	flagext.DeprecatedFlag(f, prefix+".shard-planning.min-time-reduction-ratio", shardPlanningDeprecated, util_log.Logger)
}

// deprecatedBoolFlag accepts a boolean flag and ignores the value.
// IsBoolFlag preserves the bare -flag form, which a plain DeprecatedFlag rejects.
type deprecatedBoolFlag struct {
	name   string
	logger log.Logger
}

func (deprecatedBoolFlag) String() string { return "deprecated" }

func (deprecatedBoolFlag) IsBoolFlag() bool { return true }

func (d deprecatedBoolFlag) Set(string) error {
	level.Warn(d.logger).Log("msg", "flag disabled", "flag", d.name)
	flagext.DeprecatedFlagsUsed.Inc()
	return nil
}

// Validate checks constraints and applies defaults for zero-valued fields.
func (c *Config) Validate() error {
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
	return nil
}
