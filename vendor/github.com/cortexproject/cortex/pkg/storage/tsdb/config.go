package tsdb

import (
	"flag"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/alecthomas/units"
	"github.com/pkg/errors"
	"github.com/thanos-io/thanos/pkg/store"

	"github.com/cortexproject/cortex/pkg/storage/backend/azure"
	"github.com/cortexproject/cortex/pkg/storage/backend/filesystem"
	"github.com/cortexproject/cortex/pkg/storage/backend/gcs"
	"github.com/cortexproject/cortex/pkg/storage/backend/s3"
	"github.com/cortexproject/cortex/pkg/util"
)

const (
	// BackendS3 is the value for the S3 storage backend
	BackendS3 = "s3"

	// BackendGCS is the value for the GCS storage backend
	BackendGCS = "gcs"

	// BackendAzure is the value for the Azure storage backend
	BackendAzure = "azure"

	// BackendFilesystem is the value for the filesystem storge backend
	BackendFilesystem = "filesystem"

	// TenantIDExternalLabel is the external label containing the tenant ID,
	// set when shipping blocks to the storage.
	TenantIDExternalLabel = "__org_id__"

	// IngesterIDExternalLabel is the external label containing the ingester ID,
	// set when shipping blocks to the storage.
	IngesterIDExternalLabel = "__ingester_id__"

	// ShardIDExternalLabel is the external label containing the shard ID
	// and can be used to shard blocks.
	ShardIDExternalLabel = "__shard_id__"
)

// Validation errors
var (
	supportedBackends = []string{BackendS3, BackendGCS, BackendAzure, BackendFilesystem}

	errUnsupportedStorageBackend    = errors.New("unsupported TSDB storage backend")
	errInvalidShipConcurrency       = errors.New("invalid TSDB ship concurrency")
	errInvalidCompactionInterval    = errors.New("invalid TSDB compaction interval")
	errInvalidCompactionConcurrency = errors.New("invalid TSDB compaction concurrency")
	errInvalidStripeSize            = errors.New("invalid TSDB stripe size")
	errEmptyBlockranges             = errors.New("empty block ranges for TSDB")
)

// BucketConfig holds configuration for accessing long-term storage.
type BucketConfig struct {
	Backend string `yaml:"backend"`
	// Backends
	S3         s3.Config         `yaml:"s3"`
	GCS        gcs.Config        `yaml:"gcs"`
	Azure      azure.Config      `yaml:"azure"`
	Filesystem filesystem.Config `yaml:"filesystem"`
}

// BlocksStorageConfig holds the config information for the blocks storage.
//nolint:golint
type BlocksStorageConfig struct {
	Bucket      BucketConfig      `yaml:",inline"`
	BucketStore BucketStoreConfig `yaml:"bucket_store" doc:"description=This configures how the store-gateway synchronizes blocks stored in the bucket."`
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

// RegisterFlags registers the TSDB Backend
func (cfg *BucketConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.S3.RegisterFlags(f)
	cfg.GCS.RegisterFlags(f)
	cfg.Azure.RegisterFlags(f)
	cfg.Filesystem.RegisterFlags(f)

	f.StringVar(&cfg.Backend, "experimental.blocks-storage.backend", "s3", fmt.Sprintf("Backend storage to use. Supported backends are: %s.", strings.Join(supportedBackends, ", ")))
}

// RegisterFlags registers the TSDB flags
func (cfg *BlocksStorageConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.Bucket.RegisterFlags(f)
	cfg.BucketStore.RegisterFlags(f)
	cfg.TSDB.RegisterFlags(f)
}

func (cfg *BucketConfig) Validate() error {
	if !util.StringsContain(supportedBackends, cfg.Backend) {
		return errUnsupportedStorageBackend
	}

	return nil
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
	StripeSize                int           `yaml:"stripe_size"`
	WALCompressionEnabled     bool          `yaml:"wal_compression_enabled"`
	FlushBlocksOnShutdown     bool          `yaml:"flush_blocks_on_shutdown"`

	// MaxTSDBOpeningConcurrencyOnStartup limits the number of concurrently opening TSDB's during startup.
	MaxTSDBOpeningConcurrencyOnStartup int `yaml:"max_tsdb_opening_concurrency_on_startup"`

	// If true, user TSDBs are not closed on shutdown. Only for testing.
	// If false (default), user TSDBs are closed to make sure all resources are released and closed properly.
	KeepUserTSDBOpenOnShutdown bool `yaml:"-"`
}

// RegisterFlags registers the TSDBConfig flags.
func (cfg *TSDBConfig) RegisterFlags(f *flag.FlagSet) {
	if len(cfg.BlockRanges) == 0 {
		cfg.BlockRanges = []time.Duration{2 * time.Hour} // Default 2h block
	}

	f.StringVar(&cfg.Dir, "experimental.blocks-storage.tsdb.dir", "tsdb", "Local directory to store TSDBs in the ingesters.")
	f.Var(&cfg.BlockRanges, "experimental.blocks-storage.tsdb.block-ranges-period", "TSDB blocks range period.")
	f.DurationVar(&cfg.Retention, "experimental.blocks-storage.tsdb.retention-period", 6*time.Hour, "TSDB blocks retention in the ingester before a block is removed. This should be larger than the block_ranges_period and large enough to give store-gateways and queriers enough time to discover newly uploaded blocks.")
	f.DurationVar(&cfg.ShipInterval, "experimental.blocks-storage.tsdb.ship-interval", 1*time.Minute, "How frequently the TSDB blocks are scanned and new ones are shipped to the storage. 0 means shipping is disabled.")
	f.IntVar(&cfg.ShipConcurrency, "experimental.blocks-storage.tsdb.ship-concurrency", 10, "Maximum number of tenants concurrently shipping blocks to the storage.")
	f.IntVar(&cfg.MaxTSDBOpeningConcurrencyOnStartup, "experimental.blocks-storage.tsdb.max-tsdb-opening-concurrency-on-startup", 10, "limit the number of concurrently opening TSDB's on startup")
	f.DurationVar(&cfg.HeadCompactionInterval, "experimental.blocks-storage.tsdb.head-compaction-interval", 1*time.Minute, "How frequently does Cortex try to compact TSDB head. Block is only created if data covers smallest block range. Must be greater than 0 and max 5 minutes.")
	f.IntVar(&cfg.HeadCompactionConcurrency, "experimental.blocks-storage.tsdb.head-compaction-concurrency", 5, "Maximum number of tenants concurrently compacting TSDB head into a new block")
	f.DurationVar(&cfg.HeadCompactionIdleTimeout, "experimental.blocks-storage.tsdb.head-compaction-idle-timeout", 1*time.Hour, "If TSDB head is idle for this duration, it is compacted. 0 means disabled.")
	f.IntVar(&cfg.StripeSize, "experimental.blocks-storage.tsdb.stripe-size", 16384, "The number of shards of series to use in TSDB (must be a power of 2). Reducing this will decrease memory footprint, but can negatively impact performance.")
	f.BoolVar(&cfg.WALCompressionEnabled, "experimental.blocks-storage.tsdb.wal-compression-enabled", false, "True to enable TSDB WAL compression.")
	f.BoolVar(&cfg.FlushBlocksOnShutdown, "experimental.blocks-storage.tsdb.flush-blocks-on-shutdown", false, "True to flush blocks to storage on shutdown. If false, incomplete blocks will be reused after restart.")
}

// Validate the config.
func (cfg *TSDBConfig) Validate() error {
	if cfg.ShipInterval > 0 && cfg.ShipConcurrency <= 0 {
		return errInvalidShipConcurrency
	}

	if cfg.HeadCompactionInterval <= 0 || cfg.HeadCompactionInterval > 5*time.Minute {
		return errInvalidCompactionInterval
	}

	if cfg.HeadCompactionConcurrency <= 0 {
		return errInvalidCompactionConcurrency
	}

	if cfg.StripeSize <= 1 || (cfg.StripeSize&(cfg.StripeSize-1)) != 0 { // ensure stripe size is a positive power of 2
		return errInvalidStripeSize
	}

	if len(cfg.BlockRanges) == 0 {
		return errEmptyBlockranges
	}

	return nil
}

// BlocksDir returns the directory path where TSDB blocks and wal should be
// stored by the ingester
func (cfg *TSDBConfig) BlocksDir(userID string) string {
	return filepath.Join(cfg.Dir, userID)
}

// BucketStoreConfig holds the config information for Bucket Stores used by the querier
type BucketStoreConfig struct {
	SyncDir                  string              `yaml:"sync_dir"`
	SyncInterval             time.Duration       `yaml:"sync_interval"`
	MaxChunkPoolBytes        uint64              `yaml:"max_chunk_pool_bytes"`
	MaxConcurrent            int                 `yaml:"max_concurrent"`
	TenantSyncConcurrency    int                 `yaml:"tenant_sync_concurrency"`
	BlockSyncConcurrency     int                 `yaml:"block_sync_concurrency"`
	MetaSyncConcurrency      int                 `yaml:"meta_sync_concurrency"`
	ConsistencyDelay         time.Duration       `yaml:"consistency_delay"`
	IndexCache               IndexCacheConfig    `yaml:"index_cache"`
	ChunksCache              ChunksCacheConfig   `yaml:"chunks_cache"`
	MetadataCache            MetadataCacheConfig `yaml:"metadata_cache"`
	IgnoreDeletionMarksDelay time.Duration       `yaml:"ignore_deletion_mark_delay"`

	// Controls what is the ratio of postings offsets store will hold in memory.
	// Larger value will keep less offsets, which will increase CPU cycles needed for query touching those postings.
	// It's meant for setups that want low baseline memory pressure and where less traffic is expected.
	// On the contrary, smaller value will increase baseline memory usage, but improve latency slightly.
	// 1 will keep all in memory. Default value is the same as in Prometheus which gives a good balance.
	PostingOffsetsInMemSampling int `yaml:"postings_offsets_in_mem_sampling" doc:"hidden"`
}

// RegisterFlags registers the BucketStore flags
func (cfg *BucketStoreConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.IndexCache.RegisterFlagsWithPrefix(f, "experimental.blocks-storage.bucket-store.index-cache.")
	cfg.ChunksCache.RegisterFlagsWithPrefix(f, "experimental.blocks-storage.bucket-store.chunks-cache.")
	cfg.MetadataCache.RegisterFlagsWithPrefix(f, "experimental.blocks-storage.bucket-store.metadata-cache.")

	f.StringVar(&cfg.SyncDir, "experimental.blocks-storage.bucket-store.sync-dir", "tsdb-sync", "Directory to store synchronized TSDB index headers.")
	f.DurationVar(&cfg.SyncInterval, "experimental.blocks-storage.bucket-store.sync-interval", 5*time.Minute, "How frequently scan the bucket to look for changes (new blocks shipped by ingesters and blocks removed by retention or compaction). 0 disables it.")
	f.Uint64Var(&cfg.MaxChunkPoolBytes, "experimental.blocks-storage.bucket-store.max-chunk-pool-bytes", uint64(2*units.Gibibyte), "Max size - in bytes - of a per-tenant chunk pool, used to reduce memory allocations.")
	f.IntVar(&cfg.MaxConcurrent, "experimental.blocks-storage.bucket-store.max-concurrent", 100, "Max number of concurrent queries to execute against the long-term storage. The limit is shared across all tenants.")
	f.IntVar(&cfg.TenantSyncConcurrency, "experimental.blocks-storage.bucket-store.tenant-sync-concurrency", 10, "Maximum number of concurrent tenants synching blocks.")
	f.IntVar(&cfg.BlockSyncConcurrency, "experimental.blocks-storage.bucket-store.block-sync-concurrency", 20, "Maximum number of concurrent blocks synching per tenant.")
	f.IntVar(&cfg.MetaSyncConcurrency, "experimental.blocks-storage.bucket-store.meta-sync-concurrency", 20, "Number of Go routines to use when syncing block meta files from object storage per tenant.")
	f.DurationVar(&cfg.ConsistencyDelay, "experimental.blocks-storage.bucket-store.consistency-delay", 0, "Minimum age of a block before it's being read. Set it to safe value (e.g 30m) if your object storage is eventually consistent. GCS and S3 are (roughly) strongly consistent.")
	f.DurationVar(&cfg.IgnoreDeletionMarksDelay, "experimental.blocks-storage.bucket-store.ignore-deletion-marks-delay", time.Hour*6, "Duration after which the blocks marked for deletion will be filtered out while fetching blocks. "+
		"The idea of ignore-deletion-marks-delay is to ignore blocks that are marked for deletion with some delay. This ensures store can still serve blocks that are meant to be deleted but do not have a replacement yet. "+
		"Default is 6h, half of the default value for -compactor.deletion-delay.")
	f.IntVar(&cfg.PostingOffsetsInMemSampling, "experimental.blocks-storage.bucket-store.posting-offsets-in-mem-sampling", store.DefaultPostingOffsetInMemorySampling, "Controls what is the ratio of postings offsets that the store will hold in memory.")
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
