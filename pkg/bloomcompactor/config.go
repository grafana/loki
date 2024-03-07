package bloomcompactor

import (
	"flag"
	"fmt"
	"time"

	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/downloads"
	"github.com/grafana/loki/pkg/util/ring"
)

// Config configures the bloom-compactor component.
type Config struct {
	// Ring configures the ring store used to save and retrieve the different Bloom-Compactor instances.
	// In case it isn't explicitly set, it follows the same behavior of the other rings (ex: using the common configuration
	// section and the ingester configuration by default).
	Ring RingConfig `yaml:"ring,omitempty" doc:"description=Defines the ring to be used by the bloom-compactor servers. In case this isn't configured, this block supports inheriting configuration from the common ring section."`
	// Enabled configures whether bloom-compactors should be used to compact index values into bloomfilters
	Enabled                  bool          `yaml:"enabled"`
	CompactionInterval       time.Duration `yaml:"compaction_interval"`
	MinTableCompactionPeriod int           `yaml:"min_table_compaction_period"`
	MaxTableCompactionPeriod int           `yaml:"max_table_compaction_period"`
	WorkerParallelism        int           `yaml:"worker_parallelism"`
	RetryMinBackoff          time.Duration `yaml:"compaction_retries_min_backoff"`
	RetryMaxBackoff          time.Duration `yaml:"compaction_retries_max_backoff"`
	CompactionRetries        int           `yaml:"compaction_retries"`

	MaxCompactionParallelism int `yaml:"max_compaction_parallelism"`
}

// RegisterFlags registers flags for the Bloom-Compactor configuration.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.Ring.RegisterFlagsWithPrefix("bloom-compactor.", "collectors/", f)
	f.BoolVar(&cfg.Enabled, "bloom-compactor.enabled", false, "Flag to enable or disable the usage of the bloom-compactor component.")
	f.DurationVar(&cfg.CompactionInterval, "bloom-compactor.compaction-interval", 10*time.Minute, "Interval at which to re-run the compaction operation.")
	f.IntVar(&cfg.WorkerParallelism, "bloom-compactor.worker-parallelism", 1, "Number of workers to run in parallel for compaction.")
	// TODO(owen-d): This is a confusing name. Rename it to `min_table_offset`
	f.IntVar(&cfg.MinTableCompactionPeriod, "bloom-compactor.min-table-compaction-period", 1, "How many index periods (days) to wait before building bloom filters for a table. This can be used to lower cost by not re-writing data to object storage too frequently since recent data changes more often.")
	// TODO(owen-d): ideally we'd set this per tenant based on their `reject_old_samples_max_age` setting,
	// but due to how we need to discover tenants, we can't do that yet. Tenant+Period discovery is done by
	// iterating the table periods in object storage and looking for tenants within that period.
	// In order to have this done dynamically, we'd need to account for tenant specific overrides, which are also
	// dynamically reloaded.
	// I'm doing it the simple way for now.
	// TODO(owen-d): This is a confusing name. Rename it to `max_table_offset`
	f.IntVar(&cfg.MaxTableCompactionPeriod, "bloom-compactor.max-table-compaction-period", 7, "The maximum number of index periods (days) to build bloom filters for a table. This can be used to lower cost by not trying to compact older data which doesn't change. This can be optimized by aligning it with the maximum `reject_old_samples_max_age` setting of any tenant.")
	f.DurationVar(&cfg.RetryMinBackoff, "bloom-compactor.compaction-retries-min-backoff", 10*time.Second, "Minimum backoff time between retries.")
	f.DurationVar(&cfg.RetryMaxBackoff, "bloom-compactor.compaction-retries-max-backoff", time.Minute, "Maximum backoff time between retries.")
	f.IntVar(&cfg.CompactionRetries, "bloom-compactor.compaction-retries", 3, "Number of retries to perform when compaction fails.")
	f.IntVar(&cfg.MaxCompactionParallelism, "bloom-compactor.max-compaction-parallelism", 1, "Maximum number of tables to compact in parallel. While increasing this value, please make sure compactor has enough disk space allocated to be able to store and compact as many tables.")
}

func (cfg *Config) Validate() error {
	if cfg.MinTableCompactionPeriod > cfg.MaxTableCompactionPeriod {
		return fmt.Errorf("min_compaction_age must be less than or equal to max_compaction_age")
	}
	return nil
}

type RingConfig struct {
	ring.RingConfig `yaml:",inline"`

	Tokens int `yaml:"tokens"`
}

func (cfg *RingConfig) RegisterFlagsWithPrefix(flagsPrefix, storePrefix string, f *flag.FlagSet) {
	cfg.RingConfig.RegisterFlagsWithPrefix(flagsPrefix, storePrefix, f)
	f.IntVar(&cfg.Tokens, flagsPrefix+"ring.tokens", 10, "Number of tokens to use in the ring. The bigger the number of tokens, the more fingerprint ranges the compactor will own, but the smaller these ranges will be. Bigger number of tokens will result in more and smaller metas and blocks.")
}

type Limits interface {
	downloads.Limits
	BloomCompactorShardSize(tenantID string) int
	BloomCompactorChunksBatchSize(userID string) int
	BloomCompactorMaxTableAge(tenantID string) time.Duration
	BloomCompactorEnabled(tenantID string) bool
	BloomNGramLength(tenantID string) int
	BloomNGramSkip(tenantID string) int
	BloomFalsePositiveRate(tenantID string) float64
	BloomCompactorMaxBlockSize(tenantID string) int
}
