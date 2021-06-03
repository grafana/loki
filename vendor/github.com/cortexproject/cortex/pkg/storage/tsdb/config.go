package tsdb

import (
	"flag"
	"path/filepath"
	"strings"
	"time"

	"github.com/alecthomas/units"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/tsdb/chunks"
	"github.com/prometheus/prometheus/tsdb/wal"
	"github.com/thanos-io/thanos/pkg/store"

	"github.com/cortexproject/cortex/pkg/storage/bucket"
)

const (
	// TenantIDExternalLabel is the external label containing the tenant ID,
	// set when shipping blocks to the storage.
	TenantIDExternalLabel = "__org_id__"

	// IngesterIDExternalLabel is the external label containing the ingester ID,
	// set when shipping blocks to the storage.
	IngesterIDExternalLabel = "__ingester_id__"

	// ShardIDExternalLabel is the external label containing the shard ID
	// and can be used to shard blocks.
	ShardIDExternalLabel = "__shard_id__"

	// How often are open TSDBs checked for being idle and closed.
	DefaultCloseIdleTSDBInterval = 5 * time.Minute

	// How often to check for tenant deletion mark.
	DeletionMarkCheckInterval = 1 * time.Hour

	// Default minimum bucket size (bytes) of the chunk pool.
	ChunkPoolDefaultMinBucketSize = store.EstimatedMaxChunkSize

	// Default maximum bucket size (bytes) of the chunk pool.
	ChunkPoolDefaultMaxBucketSize = 50e6
)

// Validation errors
var (
	errInvalidShipConcurrency       = errors.New("invalid TSDB ship concurrency")
	errInvalidOpeningConcurrency    = errors.New("invalid TSDB opening concurrency")
	errInvalidCompactionInterval    = errors.New("invalid TSDB compaction interval")
	errInvalidCompactionConcurrency = errors.New("invalid TSDB compaction concurrency")
	errInvalidWALSegmentSizeBytes   = errors.New("invalid TSDB WAL segment size bytes")
	errInvalidStripeSize            = errors.New("invalid TSDB stripe size")
	errEmptyBlockranges             = errors.New("empty block ranges for TSDB")
)

// BlocksStorageConfig holds the config information for the blocks storage.
//nolint:golint
type BlocksStorageConfig struct {
	Bucket      bucket.Config     `yaml:",inline"`
	BucketStore BucketStoreConfig `yaml:"bucket_store" doc:"description=This configures how the querier and store-gateway discover and synchronize blocks stored in the bucket."`
	TSDB        TSDBConfig        `yaml:"tsdb"`
}

// DurationList is the block ranges for a tsdb
type DurationList []time.Duration

// String implements the flag.Value interface
func (d *DurationList) String() string {
	values := make([]string, 0, len(*d))
	for _, v := range *d {
		values = append(values, v.String())
	}

	return strings.Join(values, ",")
}

// Set implements the flag.Value interface
func (d *DurationList) Set(s string) error {
	values := strings.Split(s, ",")
	*d = make([]time.Duration, 0, len(values)) // flag.Parse may be called twice, so overwrite instead of append
	for _, v := range values {
		t, err := time.ParseDuration(v)
		if err != nil {
			return err
		}
		*d = append(*d, t)
	}
	return nil
}

// ToMilliseconds returns the duration list in milliseconds
func (d *DurationList) ToMilliseconds() []int64 {
	values := make([]int64, 0, len(*d))
	for _, t := range *d {
		values = append(values, t.Milliseconds())
	}

	return values
}

// RegisterFlags registers the TSDB flags
func (cfg *BlocksStorageConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.Bucket.RegisterFlagsWithPrefix("blocks-storage.", f)
	cfg.BucketStore.RegisterFlags(f)
	cfg.TSDB.RegisterFlags(f)
}

// Validate the config.
func (cfg *BlocksStorageConfig) Validate() error {
	if err := cfg.Bucket.Validate(); err != nil {
		return err
	}

	if err := cfg.TSDB.Validate(); err != nil {
		return err
	}

	return cfg.BucketStore.Validate()
}

// TSDBConfig holds the config for TSDB opened in the ingesters.
//nolint:golint
type TSDBConfig struct {
	Dir                       string        `yaml:"dir"`
	BlockRanges               DurationList  `yaml:"block_ranges_period"`
	Retention                 time.Duration `yaml:"retention_period"`
	ShipInterval              time.Duration `yaml:"ship_interval"`
	ShipConcurrency           int           `yaml:"ship_concurrency"`
	HeadCompactionInterval    time.Duration `yaml:"head_compaction_interval"`
	HeadCompactionConcurrency int           `yaml:"head_compaction_concurrency"`
	HeadCompactionIdleTimeout time.Duration `yaml:"head_compaction_idle_timeout"`
	HeadChunksWriteBufferSize int           `yaml:"head_chunks_write_buffer_size_bytes"`
	StripeSize                int           `yaml:"stripe_size"`
	WALCompressionEnabled     bool          `yaml:"wal_compression_enabled"`
	WALSegmentSizeBytes       int           `yaml:"wal_segment_size_bytes"`
	FlushBlocksOnShutdown     bool          `yaml:"flush_blocks_on_shutdown"`
	CloseIdleTSDBTimeout      time.Duration `yaml:"close_idle_tsdb_timeout"`

	// MaxTSDBOpeningConcurrencyOnStartup limits the number of concurrently opening TSDB's during startup.
	MaxTSDBOpeningConcurrencyOnStartup int `yaml:"max_tsdb_opening_concurrency_on_startup"`

	// If true, user TSDBs are not closed on shutdown. Only for testing.
	// If false (default), user TSDBs are closed to make sure all resources are released and closed properly.
	KeepUserTSDBOpenOnShutdown bool `yaml:"-"`

	// How often to check for idle TSDBs for closing. DefaultCloseIdleTSDBInterval is not suitable for testing, so tests can override.
	CloseIdleTSDBInterval time.Duration `yaml:"-"`

	// Positive value enables experiemental support for exemplars. 0 or less to disable.
	MaxExemplars int `yaml:"max_exemplars"`
}

// RegisterFlags registers the TSDBConfig flags.
func (cfg *TSDBConfig) RegisterFlags(f *flag.FlagSet) {
	if len(cfg.BlockRanges) == 0 {
		cfg.BlockRanges = []time.Duration{2 * time.Hour} // Default 2h block
	}

	f.StringVar(&cfg.Dir, "blocks-storage.tsdb.dir", "tsdb", "Local directory to store TSDBs in the ingesters.")
	f.Var(&cfg.BlockRanges, "blocks-storage.tsdb.block-ranges-period", "TSDB blocks range period.")
	f.DurationVar(&cfg.Retention, "blocks-storage.tsdb.retention-period", 6*time.Hour, "TSDB blocks retention in the ingester before a block is removed. This should be larger than the block_ranges_period and large enough to give store-gateways and queriers enough time to discover newly uploaded blocks.")
	f.DurationVar(&cfg.ShipInterval, "blocks-storage.tsdb.ship-interval", 1*time.Minute, "How frequently the TSDB blocks are scanned and new ones are shipped to the storage. 0 means shipping is disabled.")
	f.IntVar(&cfg.ShipConcurrency, "blocks-storage.tsdb.ship-concurrency", 10, "Maximum number of tenants concurrently shipping blocks to the storage.")
	f.IntVar(&cfg.MaxTSDBOpeningConcurrencyOnStartup, "blocks-storage.tsdb.max-tsdb-opening-concurrency-on-startup", 10, "limit the number of concurrently opening TSDB's on startup")
	f.DurationVar(&cfg.HeadCompactionInterval, "blocks-storage.tsdb.head-compaction-interval", 1*time.Minute, "How frequently does Cortex try to compact TSDB head. Block is only created if data covers smallest block range. Must be greater than 0 and max 5 minutes.")
	f.IntVar(&cfg.HeadCompactionConcurrency, "blocks-storage.tsdb.head-compaction-concurrency", 5, "Maximum number of tenants concurrently compacting TSDB head into a new block")
	f.DurationVar(&cfg.HeadCompactionIdleTimeout, "blocks-storage.tsdb.head-compaction-idle-timeout", 1*time.Hour, "If TSDB head is idle for this duration, it is compacted. Note that up to 25% jitter is added to the value to avoid ingesters compacting concurrently. 0 means disabled.")
	f.IntVar(&cfg.HeadChunksWriteBufferSize, "blocks-storage.tsdb.head-chunks-write-buffer-size-bytes", chunks.DefaultWriteBufferSize, "The write buffer size used by the head chunks mapper. Lower values reduce memory utilisation on clusters with a large number of tenants at the cost of increased disk I/O operations.")
	f.IntVar(&cfg.StripeSize, "blocks-storage.tsdb.stripe-size", 16384, "The number of shards of series to use in TSDB (must be a power of 2). Reducing this will decrease memory footprint, but can negatively impact performance.")
	f.BoolVar(&cfg.WALCompressionEnabled, "blocks-storage.tsdb.wal-compression-enabled", false, "True to enable TSDB WAL compression.")
	f.IntVar(&cfg.WALSegmentSizeBytes, "blocks-storage.tsdb.wal-segment-size-bytes", wal.DefaultSegmentSize, "TSDB WAL segments files max size (bytes).")
	f.BoolVar(&cfg.FlushBlocksOnShutdown, "blocks-storage.tsdb.flush-blocks-on-shutdown", false, "True to flush blocks to storage on shutdown. If false, incomplete blocks will be reused after restart.")
	f.DurationVar(&cfg.CloseIdleTSDBTimeout, "blocks-storage.tsdb.close-idle-tsdb-timeout", 0, "If TSDB has not received any data for this duration, and all blocks from TSDB have been shipped, TSDB is closed and deleted from local disk. If set to positive value, this value should be equal or higher than -querier.query-ingesters-within flag to make sure that TSDB is not closed prematurely, which could cause partial query results. 0 or negative value disables closing of idle TSDB.")
	f.IntVar(&cfg.MaxExemplars, "blocks-storage.tsdb.max-exemplars", 0, "Enables support for exemplars in TSDB and sets the maximum number that will be stored. 0 or less means disabled.")
}

// Validate the config.
func (cfg *TSDBConfig) Validate() error {
	if cfg.ShipInterval > 0 && cfg.ShipConcurrency <= 0 {
		return errInvalidShipConcurrency
	}

	if cfg.MaxTSDBOpeningConcurrencyOnStartup <= 0 {
		return errInvalidOpeningConcurrency
	}

	if cfg.HeadCompactionInterval <= 0 || cfg.HeadCompactionInterval > 5*time.Minute {
		return errInvalidCompactionInterval
	}

	if cfg.HeadCompactionConcurrency <= 0 {
		return errInvalidCompactionConcurrency
	}

	if cfg.HeadChunksWriteBufferSize < chunks.MinWriteBufferSize || cfg.HeadChunksWriteBufferSize > chunks.MaxWriteBufferSize || cfg.HeadChunksWriteBufferSize%1024 != 0 {
		return errors.Errorf("head chunks write buffer size must be a multiple of 1024 between %d and %d", chunks.MinWriteBufferSize, chunks.MaxWriteBufferSize)
	}

	if cfg.StripeSize <= 1 || (cfg.StripeSize&(cfg.StripeSize-1)) != 0 { // ensure stripe size is a positive power of 2
		return errInvalidStripeSize
	}

	if len(cfg.BlockRanges) == 0 {
		return errEmptyBlockranges
	}

	if cfg.WALSegmentSizeBytes <= 0 {
		return errInvalidWALSegmentSizeBytes
	}

	return nil
}

// BlocksDir returns the directory path where TSDB blocks and wal should be
// stored by the ingester
func (cfg *TSDBConfig) BlocksDir(userID string) string {
	return filepath.Join(cfg.Dir, userID)
}

// IsShippingEnabled returns whether blocks shipping is enabled.
func (cfg *TSDBConfig) IsBlocksShippingEnabled() bool {
	return cfg.ShipInterval > 0
}

// BucketStoreConfig holds the config information for Bucket Stores used by the querier and store-gateway.
type BucketStoreConfig struct {
	SyncDir                  string              `yaml:"sync_dir"`
	SyncInterval             time.Duration       `yaml:"sync_interval"`
	MaxConcurrent            int                 `yaml:"max_concurrent"`
	TenantSyncConcurrency    int                 `yaml:"tenant_sync_concurrency"`
	BlockSyncConcurrency     int                 `yaml:"block_sync_concurrency"`
	MetaSyncConcurrency      int                 `yaml:"meta_sync_concurrency"`
	ConsistencyDelay         time.Duration       `yaml:"consistency_delay"`
	IndexCache               IndexCacheConfig    `yaml:"index_cache"`
	ChunksCache              ChunksCacheConfig   `yaml:"chunks_cache"`
	MetadataCache            MetadataCacheConfig `yaml:"metadata_cache"`
	IgnoreDeletionMarksDelay time.Duration       `yaml:"ignore_deletion_mark_delay"`
	BucketIndex              BucketIndexConfig   `yaml:"bucket_index"`

	// Chunk pool.
	MaxChunkPoolBytes           uint64 `yaml:"max_chunk_pool_bytes"`
	ChunkPoolMinBucketSizeBytes int    `yaml:"chunk_pool_min_bucket_size_bytes" doc:"hidden"`
	ChunkPoolMaxBucketSizeBytes int    `yaml:"chunk_pool_max_bucket_size_bytes" doc:"hidden"`

	// Controls whether index-header lazy loading is enabled.
	IndexHeaderLazyLoadingEnabled     bool          `yaml:"index_header_lazy_loading_enabled"`
	IndexHeaderLazyLoadingIdleTimeout time.Duration `yaml:"index_header_lazy_loading_idle_timeout"`

	// Controls the partitioner, used to aggregate multiple GET object API requests.
	// The config option is hidden until experimental.
	PartitionerMaxGapBytes uint64 `yaml:"partitioner_max_gap_bytes" doc:"hidden"`

	// Controls what is the ratio of postings offsets store will hold in memory.
	// Larger value will keep less offsets, which will increase CPU cycles needed for query touching those postings.
	// It's meant for setups that want low baseline memory pressure and where less traffic is expected.
	// On the contrary, smaller value will increase baseline memory usage, but improve latency slightly.
	// 1 will keep all in memory. Default value is the same as in Prometheus which gives a good balance.
	PostingOffsetsInMemSampling int `yaml:"postings_offsets_in_mem_sampling" doc:"hidden"`
}

// RegisterFlags registers the BucketStore flags
func (cfg *BucketStoreConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.IndexCache.RegisterFlagsWithPrefix(f, "blocks-storage.bucket-store.index-cache.")
	cfg.ChunksCache.RegisterFlagsWithPrefix(f, "blocks-storage.bucket-store.chunks-cache.")
	cfg.MetadataCache.RegisterFlagsWithPrefix(f, "blocks-storage.bucket-store.metadata-cache.")
	cfg.BucketIndex.RegisterFlagsWithPrefix(f, "blocks-storage.bucket-store.bucket-index.")

	f.StringVar(&cfg.SyncDir, "blocks-storage.bucket-store.sync-dir", "tsdb-sync", "Directory to store synchronized TSDB index headers.")
	f.DurationVar(&cfg.SyncInterval, "blocks-storage.bucket-store.sync-interval", 15*time.Minute, "How frequently to scan the bucket, or to refresh the bucket index (if enabled), in order to look for changes (new blocks shipped by ingesters and blocks deleted by retention or compaction).")
	f.Uint64Var(&cfg.MaxChunkPoolBytes, "blocks-storage.bucket-store.max-chunk-pool-bytes", uint64(2*units.Gibibyte), "Max size - in bytes - of a chunks pool, used to reduce memory allocations. The pool is shared across all tenants. 0 to disable the limit.")
	f.IntVar(&cfg.ChunkPoolMinBucketSizeBytes, "blocks-storage.bucket-store.chunk-pool-min-bucket-size-bytes", ChunkPoolDefaultMinBucketSize, "Size - in bytes - of the smallest chunks pool bucket.")
	f.IntVar(&cfg.ChunkPoolMaxBucketSizeBytes, "blocks-storage.bucket-store.chunk-pool-max-bucket-size-bytes", ChunkPoolDefaultMaxBucketSize, "Size - in bytes - of the largest chunks pool bucket.")
	f.IntVar(&cfg.MaxConcurrent, "blocks-storage.bucket-store.max-concurrent", 100, "Max number of concurrent queries to execute against the long-term storage. The limit is shared across all tenants.")
	f.IntVar(&cfg.TenantSyncConcurrency, "blocks-storage.bucket-store.tenant-sync-concurrency", 10, "Maximum number of concurrent tenants synching blocks.")
	f.IntVar(&cfg.BlockSyncConcurrency, "blocks-storage.bucket-store.block-sync-concurrency", 20, "Maximum number of concurrent blocks synching per tenant.")
	f.IntVar(&cfg.MetaSyncConcurrency, "blocks-storage.bucket-store.meta-sync-concurrency", 20, "Number of Go routines to use when syncing block meta files from object storage per tenant.")
	f.DurationVar(&cfg.ConsistencyDelay, "blocks-storage.bucket-store.consistency-delay", 0, "Minimum age of a block before it's being read. Set it to safe value (e.g 30m) if your object storage is eventually consistent. GCS and S3 are (roughly) strongly consistent.")
	f.DurationVar(&cfg.IgnoreDeletionMarksDelay, "blocks-storage.bucket-store.ignore-deletion-marks-delay", time.Hour*6, "Duration after which the blocks marked for deletion will be filtered out while fetching blocks. "+
		"The idea of ignore-deletion-marks-delay is to ignore blocks that are marked for deletion with some delay. This ensures store can still serve blocks that are meant to be deleted but do not have a replacement yet. "+
		"Default is 6h, half of the default value for -compactor.deletion-delay.")
	f.IntVar(&cfg.PostingOffsetsInMemSampling, "blocks-storage.bucket-store.posting-offsets-in-mem-sampling", store.DefaultPostingOffsetInMemorySampling, "Controls what is the ratio of postings offsets that the store will hold in memory.")
	f.BoolVar(&cfg.IndexHeaderLazyLoadingEnabled, "blocks-storage.bucket-store.index-header-lazy-loading-enabled", false, "If enabled, store-gateway will lazy load an index-header only once required by a query.")
	f.DurationVar(&cfg.IndexHeaderLazyLoadingIdleTimeout, "blocks-storage.bucket-store.index-header-lazy-loading-idle-timeout", 20*time.Minute, "If index-header lazy loading is enabled and this setting is > 0, the store-gateway will offload unused index-headers after 'idle timeout' inactivity.")
	f.Uint64Var(&cfg.PartitionerMaxGapBytes, "blocks-storage.bucket-store.partitioner-max-gap-bytes", store.PartitionerMaxGapSize, "Max size - in bytes - of a gap for which the partitioner aggregates together two bucket GET object requests.")
}

// Validate the config.
func (cfg *BucketStoreConfig) Validate() error {
	err := cfg.IndexCache.Validate()
	if err != nil {
		return errors.Wrap(err, "index-cache configuration")
	}
	err = cfg.ChunksCache.Validate()
	if err != nil {
		return errors.Wrap(err, "chunks-cache configuration")
	}
	err = cfg.MetadataCache.Validate()
	if err != nil {
		return errors.Wrap(err, "metadata-cache configuration")
	}
	return nil
}

type BucketIndexConfig struct {
	Enabled               bool          `yaml:"enabled"`
	UpdateOnErrorInterval time.Duration `yaml:"update_on_error_interval"`
	IdleTimeout           time.Duration `yaml:"idle_timeout"`
	MaxStalePeriod        time.Duration `yaml:"max_stale_period"`
}

func (cfg *BucketIndexConfig) RegisterFlagsWithPrefix(f *flag.FlagSet, prefix string) {
	f.BoolVar(&cfg.Enabled, prefix+"enabled", false, "True to enable querier and store-gateway to discover blocks in the storage via bucket index instead of bucket scanning.")
	f.DurationVar(&cfg.UpdateOnErrorInterval, prefix+"update-on-error-interval", time.Minute, "How frequently a bucket index, which previously failed to load, should be tried to load again. This option is used only by querier.")
	f.DurationVar(&cfg.IdleTimeout, prefix+"idle-timeout", time.Hour, "How long a unused bucket index should be cached. Once this timeout expires, the unused bucket index is removed from the in-memory cache. This option is used only by querier.")
	f.DurationVar(&cfg.MaxStalePeriod, prefix+"max-stale-period", time.Hour, "The maximum allowed age of a bucket index (last updated) before queries start failing because the bucket index is too old. The bucket index is periodically updated by the compactor, while this check is enforced in the querier (at query time).")
}
