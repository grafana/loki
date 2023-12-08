// package bloomshipperconfig resides in its own package to prevent circular imports with storage package
package config

import (
	"errors"
	"flag"
	"strings"
	"time"

	"github.com/grafana/loki/pkg/storage/chunk/cache"
)

type Config struct {
	WorkingDirectory       string                 `yaml:"working_directory"`
	BlocksDownloadingQueue DownloadingQueueConfig `yaml:"blocks_downloading_queue"`
	BlocksCache            BlocksCacheConfig      `yaml:"blocks_cache"`
}

type BlocksCacheConfig struct {
	EmbeddedCacheConfig           cache.EmbeddedCacheConfig `yaml:",inline"`
	RemoveDirectoryGracefulPeriod time.Duration             `yaml:"remove_directory_graceful_period"`
}

func (c *BlocksCacheConfig) RegisterFlagsWithPrefixAndDefaults(prefix string, f *flag.FlagSet) {
	c.EmbeddedCacheConfig.RegisterFlagsWithPrefixAndDefaults(prefix, "", f, 0)
	f.DurationVar(&c.RemoveDirectoryGracefulPeriod, prefix+"remove-directory-graceful-period", 5*time.Minute,
		"During this period the process waits until the directory becomes not used and only after this it will be deleted. If the timeout is reached, the directory is force deleted.")
}

type DownloadingQueueConfig struct {
	WorkersCount              int `yaml:"workers_count"`
	MaxTasksEnqueuedPerTenant int `yaml:"max_tasks_enqueued_per_tenant"`
}

func (cfg *DownloadingQueueConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.IntVar(&cfg.WorkersCount, prefix+"workers-count", 100, "The count of parallel workers that download Bloom Blocks.")
	f.IntVar(&cfg.MaxTasksEnqueuedPerTenant, prefix+"max_tasks_enqueued_per_tenant", 10_000, "Maximum number of task in queue per tenant per bloom-gateway. Enqueuing the tasks above this limit will fail an error.")
}

func (c *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&c.WorkingDirectory, prefix+"shipper.working-directory", "bloom-shipper", "Working directory to store downloaded Bloom Blocks.")
	c.BlocksDownloadingQueue.RegisterFlagsWithPrefix(prefix+"shipper.blocks-downloading-queue.", f)
	c.BlocksCache.RegisterFlagsWithPrefixAndDefaults("blocks-cache.", f)
}

func (c *Config) Validate() error {
	if strings.TrimSpace(c.WorkingDirectory) == "" {
		return errors.New("working directory must be specified")
	}
	return nil
}
