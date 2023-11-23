package bloomcompactor

import (
	"flag"
	"time"

	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/downloads"
	"github.com/grafana/loki/pkg/util/ring"
)

// Config configures the bloom-compactor component.
type Config struct {
	// Ring configures the ring store used to save and retrieve the different Bloom-Compactor instances.
	// In case it isn't explicitly set, it follows the same behavior of the other rings (ex: using the common configuration
	// section and the ingester configuration by default).
	Ring ring.RingConfig `yaml:"ring,omitempty" doc:"description=Defines the ring to be used by the bloom-compactor servers. In case this isn't configured, this block supports inheriting configuration from the common ring section."`
	// Enabled configures whether bloom-compactors should be used to compact index values into bloomfilters
	Enabled            bool          `yaml:"enabled"`
	WorkingDirectory   string        `yaml:"working_directory"`
	CompactionInterval time.Duration `yaml:"compaction_interval"`

	RetryMinBackoff   time.Duration `yaml:"compaction_retries_min_backoff"`
	RetryMaxBackoff   time.Duration `yaml:"compaction_retries_max_backoff"`
	CompactionRetries int           `yaml:"compaction_retries"`

	MaxCompactionParallelism int `yaml:"max_compaction_parallelism"`
}

// RegisterFlags registers flags for the Bloom-Compactor configuration.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.Ring.RegisterFlagsWithPrefix("bloom-compactor.", "collectors/", f)
	f.BoolVar(&cfg.Enabled, "bloom-compactor.enabled", false, "Flag to enable or disable the usage of the bloom-compactor component.")
	f.StringVar(&cfg.WorkingDirectory, "bloom-compactor.working-directory", "", "Directory where files can be downloaded for compaction.")
	f.DurationVar(&cfg.CompactionInterval, "bloom-compactor.compaction-interval", 10*time.Minute, "Interval at which to re-run the compaction operation.")
	f.DurationVar(&cfg.RetryMinBackoff, "bloom-compactor.compaction-retries-min-backoff", 10*time.Second, "Minimum backoff time between retries.")
	f.DurationVar(&cfg.RetryMaxBackoff, "bloom-compactor.compaction-retries-max-backoff", time.Minute, "Maximum backoff time between retries.")
	f.IntVar(&cfg.CompactionRetries, "bloom-compactor.compaction-retries", 3, "Number of retries to perform when compaction fails.")
	f.IntVar(&cfg.MaxCompactionParallelism, "bloom-compactor.max-compaction-parallelism", 1, "Maximum number of tables to compact in parallel. While increasing this value, please make sure compactor has enough disk space allocated to be able to store and compact as many tables.")
}

type Limits interface {
	downloads.Limits
	BloomCompactorShardSize(tenantID string) int
	BloomCompactorMaxTableAge(tenantID string) time.Duration
	BloomCompactorMinTableAge(tenantID string) time.Duration
	BloomCompactorEnabled(tenantID string) bool
	BloomNGramLength(tenantID string) int
	BloomNGramSkip(tenantID string) int
	BloomFalsePositiveRate(tenantID string) float64
}
