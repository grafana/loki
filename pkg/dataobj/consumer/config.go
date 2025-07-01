package consumer

import (
	"flag"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/uploader"
)

type Config struct {
	logsobj.BuilderConfig
	UploaderConfig   uploader.Config `yaml:"uploader"`
	IdleFlushTimeout time.Duration   `yaml:"idle_flush_timeout"`

	PartitionProcessingEnabled bool `yaml:"partition_processing_enabled"`
	IndexBuildingEnabled       bool `yaml:"index_building_enabled"`

	// Testing flags to modify behaviour during development.
	IndexBuildingEventsPerIndex int                 `yaml:"index_building_events_per_index" category:"experimental"`
	IndexStoragePrefix          string              `yaml:"index_storage_prefix" category:"experimental"`
	EnabledTenantIDs            flagext.StringSlice `yaml:"enabled_tenant_ids" category:"experimental"`
}

func (cfg *Config) Validate() error {
	if err := cfg.UploaderConfig.Validate(); err != nil {
		return err
	}

	return cfg.BuilderConfig.Validate()
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("dataobj-consumer.", f)
}

func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	cfg.BuilderConfig.RegisterFlagsWithPrefix(prefix, f)
	cfg.UploaderConfig.RegisterFlagsWithPrefix(prefix, f)

	f.DurationVar(&cfg.IdleFlushTimeout, prefix+"idle-flush-timeout", 60*60*time.Second, "The maximum amount of time to wait in seconds before flushing an object that is no longer receiving new writes")
	f.BoolVar(&cfg.PartitionProcessingEnabled, prefix+"partition-processing-enabled", true, "If true, partition processing will be enabled")
	f.BoolVar(&cfg.IndexBuildingEnabled, prefix+"index-building-enabled", false, "If true, index building will be enabled")
	f.IntVar(&cfg.IndexBuildingEventsPerIndex, prefix+"index-building-events-per-index", 32, "Experimental: The number of events to batch before building an index")
	f.StringVar(&cfg.IndexStoragePrefix, prefix+"index-storage-prefix", "indexing-v0/", "Experimental: A prefix to use for storing indexes in object storage. Used to separate the metastore & index files during initial testing.")
	f.Var(&cfg.EnabledTenantIDs, prefix+"enabled-tenant-ids", "Experimental: A list of tenant IDs to enable index building for. If empty, all tenants will be enabled.")
}
