// package bloomshipperconfig resides in its own package to prevent circular imports with storage package
package config

import (
	"errors"
	"flag"
	"time"

	"github.com/grafana/dskit/flagext"

	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
)

type Config struct {
	WorkingDirectory    flagext.StringSliceCSV `yaml:"working_directory"`
	MaxQueryPageSize    flagext.Bytes          `yaml:"max_query_page_size"`
	DownloadParallelism int                    `yaml:"download_parallelism"`
	BlocksCache         BlocksCacheConfig      `yaml:"blocks_cache"`
	MetasCache          cache.Config           `yaml:"metas_cache"`
}

func (c *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	c.WorkingDirectory = []string{"/data/blooms"}
	f.Var(&c.WorkingDirectory, prefix+"shipper.working-directory", "Working directory to store downloaded bloom blocks. Supports multiple directories, separated by comma.")
	_ = c.MaxQueryPageSize.Set("64MiB") // default should match the one set in pkg/storage/bloom/v1/bloom.go
	f.Var(&c.MaxQueryPageSize, prefix+"max-query-page-size", "Maximum size of bloom pages that should be queried. Larger pages than this limit are skipped when querying blooms to limit memory usage.")
	f.IntVar(&c.DownloadParallelism, prefix+"download-parallelism", 16, "The amount of maximum concurrent bloom blocks downloads.")
	c.BlocksCache.RegisterFlagsWithPrefixAndDefaults(prefix+"blocks-cache.", "Cache for bloom blocks. ", f, 24*time.Hour)
	c.MetasCache.RegisterFlagsWithPrefix(prefix+"metas-cache.", "Cache for bloom metas. ", f)
}

func (c *Config) Validate() error {
	if len(c.WorkingDirectory) == 0 {
		return errors.New("at least one working directory must be specified")
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
