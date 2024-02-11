package bloomcompactor

import (
	"flag"
	"fmt"
	"time"

	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper"
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
	MinCompactionAge   time.Duration `yaml:"min_compaction_age"`
	MaxCompactionAge   time.Duration `yaml:"max_compaction_age"`
	WorkerParallelism  int           `yaml:"worker_parallelism"`
	RetryMinBackoff    time.Duration `yaml:"compaction_retries_min_backoff"`
	RetryMaxBackoff    time.Duration `yaml:"compaction_retries_max_backoff"`
	CompactionRetries  int           `yaml:"compaction_retries"`

	MaxCompactionParallelism int `yaml:"max_compaction_parallelism"`
}

// RegisterFlags registers flags for the Bloom-Compactor configuration.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.Ring.RegisterFlagsWithPrefix("bloom-compactor.", "collectors/", f)
	f.BoolVar(&cfg.Enabled, "bloom-compactor.enabled", false, "Flag to enable or disable the usage of the bloom-compactor component.")
	f.StringVar(&cfg.WorkingDirectory, "bloom-compactor.working-directory", "", "Directory where files can be downloaded for compaction.")
	f.DurationVar(&cfg.CompactionInterval, "bloom-compactor.compaction-interval", 10*time.Minute, "Interval at which to re-run the compaction operation.")
	f.IntVar(&cfg.WorkerParallelism, "bloom-compactor.worker-parallelism", 1, "Number of workers to run in parallel for compaction.")
	f.DurationVar(&cfg.MinCompactionAge, "bloom-compactor.min-compaction-age", 24*time.Hour, "Minimum age of a table before it is considered for compaction.")
	// TODO(owen-d): ideally we'd set this per tenant based on their `reject_old_samples_max_age` setting,
	// but due to how we need to discover tenants, we can't do that yet. Tenant+Period discovery is done by
	// iterating the table periods in object storage and looking for tenants within that period.
	// In order to have this done dynamically, we'd need to account for tenant specific overrides, which are also
	// dynamically reloaded.
	// I'm doing it the simple way for now.
	f.DurationVar(&cfg.MaxCompactionAge, "bloom-compactor.max-compaction-age", 7*24*time.Hour, "Maximum age of a table before it is considered for compaction.")
	f.DurationVar(&cfg.RetryMinBackoff, "bloom-compactor.compaction-retries-min-backoff", 10*time.Second, "Minimum backoff time between retries.")
	f.DurationVar(&cfg.RetryMaxBackoff, "bloom-compactor.compaction-retries-max-backoff", time.Minute, "Maximum backoff time between retries.")
	f.IntVar(&cfg.CompactionRetries, "bloom-compactor.compaction-retries", 3, "Number of retries to perform when compaction fails.")
	f.IntVar(&cfg.MaxCompactionParallelism, "bloom-compactor.max-compaction-parallelism", 1, "Maximum number of tables to compact in parallel. While increasing this value, please make sure compactor has enough disk space allocated to be able to store and compact as many tables.")
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
}

// TODO(owen-d): Remove this type in favor of config.DayTime
type DayTable model.Time

func (d DayTable) String() string {
	return fmt.Sprintf("%d", d.ModelTime().Time().UnixNano()/int64(config.ObjectStorageIndexRequiredPeriod))
}

func (d DayTable) Inc() DayTable {
	return DayTable(d.ModelTime().Add(config.ObjectStorageIndexRequiredPeriod))
}

func (d DayTable) Dec() DayTable {
	return DayTable(d.ModelTime().Add(-config.ObjectStorageIndexRequiredPeriod))
}

func (d DayTable) Before(other DayTable) bool {
	return d.ModelTime().Before(model.Time(other))
}

func (d DayTable) After(other DayTable) bool {
	return d.ModelTime().After(model.Time(other))
}

func (d DayTable) ModelTime() model.Time {
	return model.Time(d)
}

func (d DayTable) Bounds() bloomshipper.Interval {
	return bloomshipper.Interval{
		Start: model.Time(d),
		End:   model.Time(d.Inc()),
	}
}
