// package bloomshipperconfig resides in its own package to prevent circular imports with storage package
package config

import (
	"errors"
	"flag"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/grafana/dskit/flagext"

	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	lokiflagext "github.com/grafana/loki/v3/pkg/util/flagext"
	"github.com/grafana/loki/v3/pkg/util/mempool"
)

type Config struct {
	WorkingDirectory    flagext.StringSliceCSV    `yaml:"working_directory"`
	MaxQueryPageSize    flagext.Bytes             `yaml:"max_query_page_size"`
	DownloadParallelism int                       `yaml:"download_parallelism"`
	BlocksCache         BlocksCacheConfig         `yaml:"blocks_cache"`
	MetasCache          cache.Config              `yaml:"metas_cache"`
	MetasLRUCache       cache.EmbeddedCacheConfig `yaml:"metas_lru_cache"`
	MemoryManagement    MemoryManagementConfig    `yaml:"memory_management" doc:"hidden"`

	// This will always be set to true when flags are registered.
	// In unit tests, you can set this to false as a literal.
	// In integration tests, you can override this via the flag.
	CacheListOps bool `yaml:"-"`
}

func (c *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	//FIXME E.Welch, the helm chart does not use the /data dir rather /var so we need to probably consider not defaulting this? not sure what's best to do here.
	c.WorkingDirectory = []string{"/data/blooms"}
	f.Var(&c.WorkingDirectory, prefix+"shipper.working-directory", "Working directory to store downloaded bloom blocks. Supports multiple directories, separated by comma.")
	_ = c.MaxQueryPageSize.Set("64MiB") // default should match the one set in pkg/storage/bloom/v1/bloom.go
	f.Var(&c.MaxQueryPageSize, prefix+"max-query-page-size", "Maximum size of bloom pages that should be queried. Larger pages than this limit are skipped when querying blooms to limit memory usage.")
	f.IntVar(&c.DownloadParallelism, prefix+"download-parallelism", 8, "The amount of maximum concurrent bloom blocks downloads. Usually set to 2x number of CPU cores.")
	c.BlocksCache.RegisterFlagsWithPrefixAndDefaults(prefix+"blocks-cache.", "Cache for bloom blocks. ", f, 24*time.Hour)
	c.MetasCache.RegisterFlagsWithPrefix(prefix+"metas-cache.", "Cache for bloom metas. ", f)
	c.MetasLRUCache.RegisterFlagsWithPrefix(prefix+"metas-lru-cache.", "In-memory LRU cache for bloom metas. ", f)
	c.MemoryManagement.RegisterFlagsWithPrefix(prefix+"memory-management.", f)
	f.BoolVar(&c.CacheListOps, prefix+"cache-list-ops", true, "Cache LIST operations. This is a hidden flag.")
}

func (c *Config) Validate() error {
	if len(c.WorkingDirectory) == 0 {
		return errors.New("at least one working directory must be specified")
	}
	if err := c.MemoryManagement.Validate(); err != nil {
		return err
	}
	return nil
}

// BlocksCacheConfig represents in-process embedded cache config.
type BlocksCacheConfig struct {
	SoftLimit flagext.Bytes `yaml:"soft_limit"`
	HardLimit flagext.Bytes `yaml:"hard_limit"`
	TTL       time.Duration `yaml:"ttl"`

	// PurgeInterval tell how often should we remove keys that are expired.
	// by default it takes `defaultPurgeInterval`
	PurgeInterval time.Duration `yaml:"-"`
}

func (cfg *BlocksCacheConfig) RegisterFlagsWithPrefix(prefix, description string, f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefixAndDefaults(prefix, description, f, time.Hour)
}

func (cfg *BlocksCacheConfig) RegisterFlagsWithPrefixAndDefaults(prefix, description string, f *flag.FlagSet, defaultTTL time.Duration) {
	_ = cfg.SoftLimit.Set("32GiB")
	f.Var(&cfg.SoftLimit, prefix+"soft-limit", description+"Soft limit of the cache in bytes. Exceeding this limit will trigger evictions of least recently used items in the background.")
	_ = cfg.HardLimit.Set("64GiB")
	f.Var(&cfg.HardLimit, prefix+"hard-limit", description+"Hard limit of the cache in bytes. Exceeding this limit will block execution until soft limit is deceeded.")
	f.DurationVar(&cfg.TTL, prefix+"ttl", defaultTTL, description+"The time to live for items in the cache before they get purged.")
}

func (cfg *BlocksCacheConfig) Validate() error {
	if cfg.TTL == 0 {
		return errors.New("blocks cache ttl must not be 0")
	}
	if cfg.SoftLimit == 0 {
		return errors.New("blocks cache soft_limit must not be 0")
	}
	if cfg.SoftLimit > cfg.HardLimit {
		return errors.New("blocks cache soft_limit must not be greater than hard_limit")
	}
	return nil
}

var (
	// the default that describes a 4GiB memory pool
	defaultMemPoolBuckets = mempool.Buckets{
		{Size: 128, Capacity: 64 << 10}, // 8MiB -- for tests
		{Size: 512, Capacity: 2 << 20},  // 1024MiB
		{Size: 128, Capacity: 8 << 20},  // 1024MiB
		{Size: 32, Capacity: 32 << 20},  // 1024MiB
		{Size: 8, Capacity: 128 << 20},  // 1024MiB
	}
	types = supportedAllocationTypes{
		"simple", "simple heap allocations using Go's make([]byte, n) and no re-cycling of buffers",
		"dynamic", "a buffer pool with variable sized buckets and best effort re-cycling of buffers using Go's sync.Pool",
		"fixed", "a fixed size memory pool with configurable slab sizes, see mem-pool-buckets",
	}
)

type MemoryManagementConfig struct {
	BloomPageAllocationType string                          `yaml:"bloom_page_alloc_type"`
	BloomPageMemPoolBuckets lokiflagext.CSV[mempool.Bucket] `yaml:"bloom_page_mem_pool_buckets"`
}

func (cfg *MemoryManagementConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&cfg.BloomPageAllocationType, prefix+"alloc-type", "dynamic", fmt.Sprintf("One of: %s", strings.Join(types.descriptions(), ", ")))

	_ = cfg.BloomPageMemPoolBuckets.Set(defaultMemPoolBuckets.String())
	f.Var(&cfg.BloomPageMemPoolBuckets, prefix+"mem-pool-buckets", "Comma separated list of buckets in the format {size}x{bytes}")
}

func (cfg *MemoryManagementConfig) Validate() error {
	if !slices.Contains(types.names(), cfg.BloomPageAllocationType) {
		msg := fmt.Sprintf("bloom_page_alloc_type must be one of: %s", strings.Join(types.descriptions(), ", "))
		return errors.New(msg)
	}
	if cfg.BloomPageAllocationType == "fixed" && len(cfg.BloomPageMemPoolBuckets) == 0 {
		return errors.New("fixed memory pool requires at least one bucket")
	}
	return nil
}

type supportedAllocationTypes []string

func (t supportedAllocationTypes) names() []string {
	names := make([]string, 0, len(t)/2)
	for i := 0; i < len(t); i += 2 {
		names = append(names, t[i])
	}
	return names
}

func (t supportedAllocationTypes) descriptions() []string {
	names := make([]string, 0, len(t)/2)
	for i := 0; i < len(t); i += 2 {
		names = append(names, fmt.Sprintf("%s (%s)", t[i], t[i+1]))
	}
	return names
}
