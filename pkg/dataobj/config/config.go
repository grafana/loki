package config

import (
	"flag"
	"time"

	"github.com/grafana/loki/v3/pkg/dataobj/builder"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/uploader"
	"github.com/grafana/loki/v3/pkg/engine/compactor"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
)

// DefaultStorageLag is the default of -dataobj.storage-lag.
const DefaultStorageLag = 3 * time.Hour

type Config struct {
	Builder builder.Config `yaml:"builder"`
	// Uploader is shared by every target that uploads data objects.
	Uploader  uploader.Config  `yaml:"uploader"`
	Metastore metastore.Config `yaml:"metastore"`
	// MetadataCache caches the metadata region of log objects and index objects, or the entire object
	// if it is tiny. When the ToC resolver is enabled (-dataobj-metastore.toc-resolver.enabled), it also
	// holds the tables of contents (ToCs) that the resolver refreshes. It is enabled when a backend is
	// configured. When -dataobj.logs-metadata-memory-cache-enabled is set, log objects use the in-memory
	// cache instead.
	MetadataCache cache.Config `yaml:"metadata_cache" doc:"description=Caches the metadata region of the log objects and index objects the querier reads, or the entire object if it is tiny. When the ToC resolver is enabled (-dataobj-metastore.toc-resolver.enabled), the cache also holds the tables of contents (ToCs) that the resolver refreshes. The cache is enabled when a backend is configured. Entries are keyed by object path, so the cache cannot differentiate entries between different buckets and clusters. Multiple installations must not share the same cache backend. When -dataobj.logs-metadata-memory-cache-enabled is set, log objects use the in-memory cache instead."`
	// Compaction is the dataobj-compaction-planner target's configuration.
	// Disabled by default; setting Compaction.Enabled = true in addition
	// to the top-level Enabled flag opts the deployment in.
	Compaction compactor.Config `yaml:"compaction"`
	// StorageBucketPrefix is the prefix to use for the storage bucket.
	StorageBucketPrefix string        `yaml:"storage_bucket_prefix"`
	Enabled             bool          `yaml:"enabled"`
	StorageLag          time.Duration `yaml:"storage_lag" category:"experimental"`

	// LogsMetadataMemoryCache enables an in-memory cache of the whole metadata block of every log object
	// the querier reads, or of the whole object if it is tiny. The cache never evicts.
	LogsMetadataMemoryCache bool `yaml:"logs_metadata_memory_cache_enabled" category:"experimental"`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.Builder.RegisterFlags(f)
	cfg.Uploader.RegisterFlagsWithPrefix("dataobj.uploader.", f)
	cfg.Metastore.RegisterFlags(f)
	cfg.MetadataCache.RegisterFlagsWithPrefix("dataobj.metadata-cache.", "", f)
	cfg.Compaction.RegisterFlags(f)
	f.StringVar(
		&cfg.StorageBucketPrefix,
		"dataobj-storage-bucket-prefix",
		"dataobj/",
		"The prefix to use for the storage bucket.",
	)
	f.BoolVar(
		&cfg.Enabled,
		"dataobj.enabled",
		false,
		"Enable data objects.",
	)
	f.DurationVar(
		&cfg.StorageLag,
		"dataobj.storage-lag",
		DefaultStorageLag,
		"Delay after which data objects hold all the data.",
	)
	f.BoolVar(
		&cfg.LogsMetadataMemoryCache,
		"dataobj.logs-metadata-memory-cache-enabled",
		false,
		"Keep the whole metadata block of every log object the querier reads in an in-memory cache, or the whole object if it is tiny. The cache never evicts and has no limit on its total size, so memory use grows with the number of distinct objects read. Log objects use this cache instead of the backend cache set in -dataobj.metadata-cache.*. Index objects and ToCs still use the backend cache. It has no effect unless data objects are enabled.",
	)
}

func (cfg *Config) Validate() error {
	if !cfg.Enabled {
		// Do not validate configuration if disabled.
		return nil
	}
	if err := cfg.Builder.Validate(); err != nil {
		return err
	}
	if err := cfg.Uploader.Validate(); err != nil {
		return err
	}
	if err := cfg.Compaction.Validate(); err != nil {
		return err
	}
	return nil
}
