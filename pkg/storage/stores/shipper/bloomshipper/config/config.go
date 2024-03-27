// package bloomshipperconfig resides in its own package to prevent circular imports with storage package
package config

import (
	"errors"
	"flag"
	"strings"
	"time"

	"github.com/grafana/dskit/flagext"

	"github.com/grafana/loki/pkg/storage/chunk/cache"
)

type Config struct {
	WorkingDirectory       string                 `yaml:"working_directory"`
	MaxQueryPageSize       flagext.Bytes          `yaml:"max_query_page_size"`
	BlocksDownloadingQueue DownloadingQueueConfig `yaml:"blocks_downloading_queue"`
	BlocksCache            BlocksCacheConfig      `yaml:"blocks_cache"`
	MetasCache             cache.Config           `yaml:"metas_cache"`
}

type DownloadingQueueConfig struct {
	WorkersCount              int `yaml:"workers_count"`
	MaxTasksEnqueuedPerTenant int `yaml:"max_tasks_enqueued_per_tenant"`
}

func (cfg *DownloadingQueueConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.IntVar(&cfg.WorkersCount, prefix+"workers-count", 16, "The count of parallel workers that download Bloom Blocks.")
	f.IntVar(&cfg.MaxTasksEnqueuedPerTenant, prefix+"max_tasks_enqueued_per_tenant", 10_000, "Maximum number of task in queue per tenant per bloom-gateway. Enqueuing the tasks above this limit will fail an error.")
}

func (c *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&c.WorkingDirectory, prefix+"shipper.working-directory", "bloom-shipper", "Working directory to store downloaded Bloom Blocks.")
	_ = c.MaxQueryPageSize.Set("64MiB") // default should match the one set in pkg/storage/bloom/v1/bloom.go
	f.Var(&c.MaxQueryPageSize, prefix+"max-query-page-size", "Maximum size of bloom pages that should be queried. Larger pages than this limit are skipped when querying blooms to limit memory usage.")
	c.BlocksDownloadingQueue.RegisterFlagsWithPrefix(prefix+"shipper.blocks-downloading-queue.", f)
	c.BlocksCache.RegisterFlagsWithPrefixAndDefaults(prefix+"blocks-cache.", "Cache for bloom blocks. ", f, 24*time.Hour)
	c.MetasCache.RegisterFlagsWithPrefix(prefix+"metas-cache.", "Cache for bloom metas. ", f)
}

func (c *Config) Validate() error {
	if strings.TrimSpace(c.WorkingDirectory) == "" {
		return errors.New("working directory must be specified")
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
