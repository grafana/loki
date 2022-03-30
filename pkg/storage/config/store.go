package config

import (
	"flag"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/flagext"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/chunk/client/aws"
	"github.com/grafana/loki/pkg/storage/chunk/client/azure"
	"github.com/grafana/loki/pkg/storage/chunk/client/cassandra"
	"github.com/grafana/loki/pkg/storage/chunk/client/gcp"
	"github.com/grafana/loki/pkg/storage/chunk/client/grpc"
	"github.com/grafana/loki/pkg/storage/chunk/client/hedging"
	"github.com/grafana/loki/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/pkg/storage/chunk/client/openstack"
	"github.com/grafana/loki/pkg/storage/stores/shipper"
	util_log "github.com/grafana/loki/pkg/util/log"
)

type ChunkStoreConfig struct {
	ChunkCacheConfig       cache.Config `yaml:"chunk_cache_config"`
	WriteDedupeCacheConfig cache.Config `yaml:"write_dedupe_cache_config"`

	CacheLookupsOlderThan model.Duration `yaml:"cache_lookups_older_than"`

	// Not visible in yaml because the setting shouldn't be common between ingesters and queriers.
	// This exists in case we don't want to cache all the chunks but still want to take advantage of
	// ingester chunk write deduplication. But for the queriers we need the full value. So when this option
	// is set, use different caches for ingesters and queriers.
	chunkCacheStubs bool // don't write the full chunk to cache, just a stub entry

	// When DisableIndexDeduplication is true and chunk is already there in cache, only index would be written to the store and not chunk.
	DisableIndexDeduplication bool `yaml:"-"`

	// Limits query start time to be greater than now() - MaxLookBackPeriod, if set.
	// Will be deprecated in the next major release.
	MaxLookBackPeriod model.Duration `yaml:"max_look_back_period"`
}

// RegisterFlags adds the flags required to configure this flag set.
func (cfg *ChunkStoreConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.ChunkCacheConfig.RegisterFlagsWithPrefix("store.chunks-cache.", "Cache config for chunks. ", f)
	f.BoolVar(&cfg.chunkCacheStubs, "store.chunks-cache.cache-stubs", false, "If true, don't write the full chunk to cache, just a stub entry.")
	cfg.WriteDedupeCacheConfig.RegisterFlagsWithPrefix("store.index-cache-write.", "Cache config for index entry writing.", f)

	f.Var(&cfg.CacheLookupsOlderThan, "store.cache-lookups-older-than", "Cache index entries older than this period. 0 to disable.")
	f.Var(&cfg.MaxLookBackPeriod, "store.max-look-back-period", "This flag is deprecated. Use -querier.max-query-lookback instead.")
}

func (cfg *ChunkStoreConfig) Validate(logger log.Logger) error {
	if cfg.MaxLookBackPeriod > 0 {
		flagext.DeprecatedFlagsUsed.Inc()
		level.Warn(logger).Log("msg", "running with DEPRECATED flag -store.max-look-back-period, use -querier.max-query-lookback instead.")
	}
	if err := cfg.ChunkCacheConfig.Validate(); err != nil {
		return err
	}
	return cfg.WriteDedupeCacheConfig.Validate()
}

// Config chooses which storage client to use.
type Config struct {
	AWSStorageConfig       aws.StorageConfig       `yaml:"aws"`
	AzureStorageConfig     azure.BlobStorageConfig `yaml:"azure"`
	GCPStorageConfig       gcp.Config              `yaml:"bigtable"`
	GCSConfig              gcp.GCSConfig           `yaml:"gcs"`
	CassandraStorageConfig cassandra.Config        `yaml:"cassandra"`
	BoltDBConfig           local.BoltDBConfig      `yaml:"boltdb"`
	FSConfig               local.FSConfig          `yaml:"filesystem"`
	Swift                  openstack.SwiftConfig   `yaml:"swift"`

	IndexCacheValidity time.Duration `yaml:"index_cache_validity"`

	IndexQueriesCacheConfig  cache.Config `yaml:"index_queries_cache_config"`
	DisableBroadIndexQueries bool         `yaml:"disable_broad_index_queries"`
	MaxParallelGetChunk      int          `yaml:"max_parallel_get_chunk"`

	GrpcConfig grpc.Config `yaml:"grpc_store"`

	Hedging hedging.Config `yaml:"hedging"`

	MaxChunkBatchSize   int            `yaml:"max_chunk_batch_size"`
	BoltDBShipperConfig shipper.Config `yaml:"boltdb_shipper"`
}

// RegisterFlags adds the flags required to configure this flag set.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.AWSStorageConfig.RegisterFlags(f)
	cfg.AzureStorageConfig.RegisterFlags(f)
	cfg.GCPStorageConfig.RegisterFlags(f)
	cfg.GCSConfig.RegisterFlags(f)
	cfg.CassandraStorageConfig.RegisterFlags(f)
	cfg.BoltDBConfig.RegisterFlags(f)
	cfg.FSConfig.RegisterFlags(f)
	cfg.Swift.RegisterFlags(f)
	cfg.GrpcConfig.RegisterFlags(f)
	cfg.Hedging.RegisterFlagsWithPrefix("store.", f)

	cfg.IndexQueriesCacheConfig.RegisterFlagsWithPrefix("store.index-cache-read.", "Cache config for index entry reading.", f)
	f.DurationVar(&cfg.IndexCacheValidity, "store.index-cache-validity", 5*time.Minute, "Cache validity for active index entries. Should be no higher than -ingester.max-chunk-idle.")
	f.BoolVar(&cfg.DisableBroadIndexQueries, "store.disable-broad-index-queries", false, "Disable broad index queries which results in reduced cache usage and faster query performance at the expense of somewhat higher QPS on the index store.")
	f.IntVar(&cfg.MaxParallelGetChunk, "store.max-parallel-get-chunk", 150, "Maximum number of parallel chunk reads.")
	cfg.BoltDBShipperConfig.RegisterFlags(f)
	f.IntVar(&cfg.MaxChunkBatchSize, "store.max-chunk-batch-size", 50, "The maximum number of chunks to fetch per batch.")
}

// Validate config and returns error on failure
func (cfg *Config) Validate() error {
	if err := cfg.CassandraStorageConfig.Validate(); err != nil {
		return errors.Wrap(err, "invalid Cassandra Storage config")
	}
	if err := cfg.GCPStorageConfig.Validate(util_log.Logger); err != nil {
		return errors.Wrap(err, "invalid GCP Storage Storage config")
	}
	if err := cfg.Swift.Validate(); err != nil {
		return errors.Wrap(err, "invalid Swift Storage config")
	}
	if err := cfg.IndexQueriesCacheConfig.Validate(); err != nil {
		return errors.Wrap(err, "invalid Index Queries Cache config")
	}
	if err := cfg.AzureStorageConfig.Validate(); err != nil {
		return errors.Wrap(err, "invalid Azure Storage config")
	}
	if err := cfg.AWSStorageConfig.Validate(); err != nil {
		return errors.Wrap(err, "invalid AWS Storage config")
	}
	return nil
}
