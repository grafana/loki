package config

import (
	"flag"
	"fmt"
	"time"

	"github.com/grafana/loki/v3/pkg/dataobj/bucketcache"
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
	// holds the tables of contents (ToCs) that the resolver refreshes. When a bucket cache is enabled,
	// it also holds the attributes of the objects that the bucket cache serves. It is enabled when a
	// backend is configured.
	MetadataCache cache.Config `yaml:"metadata_cache" doc:"description=Caches the metadata region of the log objects and index objects the querier reads, or the entire object if it is tiny. When the ToC resolver is enabled (-dataobj-metastore.toc-resolver.enabled), the cache also holds the tables of contents (ToCs) that the resolver refreshes. When a bucket cache is enabled, the cache also holds the attributes of the objects that the bucket cache serves. The cache is enabled when a backend is configured. Entries are keyed by object path, so the cache cannot differentiate entries between different buckets and clusters. Multiple installations must not share the same cache backend."`
	// LogsBucketCache and IndexBucketCache cache the byte ranges that the querier reads from log objects
	// and from index objects. Each is enabled when a backend is configured, and has its own backend. When
	// the two backends are separate servers, the evictions of one do not affect the other.
	LogsBucketCache  bucketcache.Config `yaml:"logs_bucket_cache" doc:"description=Caches the byte ranges that the querier reads from log objects, in aligned blocks. The cache is enabled when a backend is configured."`
	IndexBucketCache bucketcache.Config `yaml:"index_bucket_cache" doc:"description=Caches the byte ranges that the querier reads from index objects, in aligned blocks. The cache is enabled when a backend is configured."`
	// Compaction is the dataobj-compaction-planner target's configuration.
	// Disabled by default; setting Compaction.Enabled = true in addition
	// to the top-level Enabled flag opts the deployment in.
	Compaction compactor.Config `yaml:"compaction"`
	// StorageBucketPrefix is the prefix to use for the storage bucket.
	StorageBucketPrefix string        `yaml:"storage_bucket_prefix"`
	Enabled             bool          `yaml:"enabled"`
	StorageLag          time.Duration `yaml:"storage_lag" category:"experimental"`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.Builder.RegisterFlags(f)
	cfg.Uploader.RegisterFlagsWithPrefix("dataobj.uploader.", f)
	cfg.Metastore.RegisterFlags(f)
	cfg.MetadataCache.RegisterFlagsWithPrefix("dataobj.metadata-cache.", "", f)
	cfg.LogsBucketCache.RegisterFlagsWithPrefix("dataobj.logs-bucket-cache.", f)
	cfg.IndexBucketCache.RegisterFlagsWithPrefix("dataobj.index-bucket-cache.", f)
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
	if err := cfg.LogsBucketCache.Validate(); err != nil {
		return fmt.Errorf("invalid logs bucket cache: %w", err)
	}
	if err := cfg.IndexBucketCache.Validate(); err != nil {
		return fmt.Errorf("invalid index bucket cache: %w", err)
	}
	return nil
}
