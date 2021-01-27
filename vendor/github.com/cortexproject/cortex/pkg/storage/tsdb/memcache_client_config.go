package tsdb

import (
	"flag"
	"strings"
	"time"

	"github.com/thanos-io/thanos/pkg/cacheutil"
	"github.com/thanos-io/thanos/pkg/model"
)

type MemcachedClientConfig struct {
	Addresses              string        `yaml:"addresses"`
	Timeout                time.Duration `yaml:"timeout"`
	MaxIdleConnections     int           `yaml:"max_idle_connections"`
	MaxAsyncConcurrency    int           `yaml:"max_async_concurrency"`
	MaxAsyncBufferSize     int           `yaml:"max_async_buffer_size"`
	MaxGetMultiConcurrency int           `yaml:"max_get_multi_concurrency"`
	MaxGetMultiBatchSize   int           `yaml:"max_get_multi_batch_size"`
	MaxItemSize            int           `yaml:"max_item_size"`
}

func (cfg *MemcachedClientConfig) RegisterFlagsWithPrefix(f *flag.FlagSet, prefix string) {
	f.StringVar(&cfg.Addresses, prefix+"addresses", "", "Comma separated list of memcached addresses. Supported prefixes are: dns+ (looked up as an A/AAAA query), dnssrv+ (looked up as a SRV query, dnssrvnoa+ (looked up as a SRV query, with no A/AAAA lookup made after that).")
	f.DurationVar(&cfg.Timeout, prefix+"timeout", 100*time.Millisecond, "The socket read/write timeout.")
	f.IntVar(&cfg.MaxIdleConnections, prefix+"max-idle-connections", 16, "The maximum number of idle connections that will be maintained per address.")
	f.IntVar(&cfg.MaxAsyncConcurrency, prefix+"max-async-concurrency", 50, "The maximum number of concurrent asynchronous operations can occur.")
	f.IntVar(&cfg.MaxAsyncBufferSize, prefix+"max-async-buffer-size", 10000, "The maximum number of enqueued asynchronous operations allowed.")
	f.IntVar(&cfg.MaxGetMultiConcurrency, prefix+"max-get-multi-concurrency", 100, "The maximum number of concurrent connections running get operations. If set to 0, concurrency is unlimited.")
	f.IntVar(&cfg.MaxGetMultiBatchSize, prefix+"max-get-multi-batch-size", 0, "The maximum number of keys a single underlying get operation should run. If more keys are specified, internally keys are split into multiple batches and fetched concurrently, honoring the max concurrency. If set to 0, the max batch size is unlimited.")
	f.IntVar(&cfg.MaxItemSize, prefix+"max-item-size", 1024*1024, "The maximum size of an item stored in memcached. Bigger items are not stored. If set to 0, no maximum size is enforced.")
}

func (cfg *MemcachedClientConfig) GetAddresses() []string {
	if cfg.Addresses == "" {
		return []string{}
	}

	return strings.Split(cfg.Addresses, ",")
}

// Validate the config.
func (cfg *MemcachedClientConfig) Validate() error {
	if len(cfg.GetAddresses()) == 0 {
		return errNoIndexCacheAddresses
	}

	return nil
}

func (cfg MemcachedClientConfig) ToMemcachedClientConfig() cacheutil.MemcachedClientConfig {
	return cacheutil.MemcachedClientConfig{
		Addresses:                 cfg.GetAddresses(),
		Timeout:                   cfg.Timeout,
		MaxIdleConnections:        cfg.MaxIdleConnections,
		MaxAsyncConcurrency:       cfg.MaxAsyncConcurrency,
		MaxAsyncBufferSize:        cfg.MaxAsyncBufferSize,
		MaxGetMultiConcurrency:    cfg.MaxGetMultiConcurrency,
		MaxGetMultiBatchSize:      cfg.MaxGetMultiBatchSize,
		MaxItemSize:               model.Bytes(cfg.MaxItemSize),
		DNSProviderUpdateInterval: 30 * time.Second,
	}
}
