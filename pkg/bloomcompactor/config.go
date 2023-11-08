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

	// No need to add options to customize the retry backoff,
	// given the defaults should be fine, but allow to override
	// it in tests.
	retryMinBackoff          time.Duration `yaml:"-"`
	retryMaxBackoff          time.Duration `yaml:"-"`
	CompactionRetries        int           `yaml:"compaction_retries" category:"advanced"`
	TablesToCompact          int           `yaml:"tables_to_compact"`
	SkipLatestNTables        int           `yaml:"skip_latest_n_tables"`
	MaxCompactionParallelism int           `yaml:"max_compaction_parallelism"`
}

// RegisterFlags registers flags for the Bloom-Compactor configuration.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.Ring.RegisterFlagsWithPrefix("bloom-compactor.", "collectors/", f)
	f.BoolVar(&cfg.Enabled, "bloom-compactor.enabled", false, "Flag to enable or disable the usage of the bloom-compactor component.")
}

type Limits interface {
	downloads.Limits
	BloomCompactorShardSize(tenantID string) int
}
