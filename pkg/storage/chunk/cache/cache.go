package cache

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/pkg/errors"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/pkg/logqlmodel/stats"
)

// Cache byte arrays by key.
type Cache interface {
	Store(ctx context.Context, key []string, buf [][]byte) error
	Fetch(ctx context.Context, keys []string) (found []string, bufs [][]byte, missing []string, err error)
	Stop()
	// GetCacheType returns a string indicating the cache "type" for the purpose of grouping cache usage statistics
	GetCacheType() stats.CacheType
}

// Config for building Caches.
type Config struct {
	EnableFifoCache bool `yaml:"enable_fifocache"`

	DefaultValidity time.Duration `yaml:"default_validity"`

	Background     BackgroundConfig      `yaml:"background"`
	Memcache       MemcachedConfig       `yaml:"memcached"`
	MemcacheClient MemcachedClientConfig `yaml:"memcached_client"`
	Redis          RedisConfig           `yaml:"redis"`
	EmbeddedCache  EmbeddedCacheConfig   `yaml:"embedded_cache"`
	Fifocache      FifoCacheConfig       `yaml:"fifocache"` // deprecated

	// This is to name the cache metrics properly.
	Prefix string `yaml:"prefix" doc:"hidden"`

	// For tests to inject specific implementations.
	Cache Cache `yaml:"-"`

	// AsyncCacheWriteBackConcurrency specifies the number of goroutines to use when asynchronously writing chunks fetched from the store to the chunk cache.
	AsyncCacheWriteBackConcurrency int `yaml:"async_cache_write_back_concurrency"`
	// AsyncCacheWriteBackBufferSize specifies the maximum number of fetched chunks to buffer for writing back to the chunk cache.
	AsyncCacheWriteBackBufferSize int `yaml:"async_cache_write_back_buffer_size"`
}

// RegisterFlagsWithPrefix adds the flags required to config this to the given FlagSet
func (cfg *Config) RegisterFlagsWithPrefix(prefix string, description string, f *flag.FlagSet) {
	cfg.Background.RegisterFlagsWithPrefix(prefix, description, f)
	cfg.Memcache.RegisterFlagsWithPrefix(prefix, description, f)
	cfg.MemcacheClient.RegisterFlagsWithPrefix(prefix, description, f)
	cfg.Redis.RegisterFlagsWithPrefix(prefix, description, f)
	cfg.Fifocache.RegisterFlagsWithPrefix(prefix, description, f)
	cfg.EmbeddedCache.RegisterFlagsWithPrefix(prefix, description, f)
	f.IntVar(&cfg.AsyncCacheWriteBackConcurrency, prefix+"max-async-cache-write-back-concurrency", 16, "The maximum number of concurrent asynchronous writeback cache can occur.")
	f.IntVar(&cfg.AsyncCacheWriteBackBufferSize, prefix+"max-async-cache-write-back-buffer-size", 500, "The maximum number of enqueued asynchronous writeback cache allowed.")
	f.DurationVar(&cfg.DefaultValidity, prefix+"default-validity", time.Hour, description+"The default validity of entries for caches unless overridden.")
	f.BoolVar(&cfg.EnableFifoCache, prefix+"cache.enable-fifocache", false, description+"(deprecated: use embedded-cache instead) Enable in-memory cache (auto-enabled for the chunks & query results cache if no other cache is configured).")

	cfg.Prefix = prefix
}

func (cfg *Config) Validate() error {
	return cfg.Fifocache.Validate()
}

// IsMemcacheSet returns whether a non empty Memcache config is set or not, based on the configured
// host or addresses.
//
// Internally, this function is used to set Memcache as the cache storage to be used.
func IsMemcacheSet(cfg Config) bool {
	return cfg.MemcacheClient.Host != "" || cfg.MemcacheClient.Addresses != ""
}

// IsRedisSet returns whether a non empty Redis config is set or not, based on the configured endpoint.
//
// Internally, this function is used to set Redis as the cache storage to be used.
func IsRedisSet(cfg Config) bool {
	return cfg.Redis.Endpoint != ""
}

func IsEmbeddedCacheSet(cfg Config) bool {
	return cfg.EmbeddedCache.Enabled
}

func IsFifoCacheSet(cfg Config) bool {
	return cfg.EnableFifoCache
}

func IsSpecificImplementationSet(cfg Config) bool {
	return cfg.Cache != nil
}

// IsCacheConfigured determines if either of the following caches is configured:
// - memcached
// - redis
// - embedded-cache
// - fifo-cache
// - specific cache implementation
func IsCacheConfigured(cfg Config) bool {
	return IsMemcacheSet(cfg) || IsRedisSet(cfg) || IsEmbeddedCacheSet(cfg) || IsFifoCacheSet(cfg) || IsSpecificImplementationSet(cfg)
}

// New creates a new Cache using Config.
func New(cfg Config, reg prometheus.Registerer, logger log.Logger, cacheType stats.CacheType) (Cache, error) {

	// Have additional check for embeddedcache with distributed mode, because those cache will already be initialized in modules
	// but still need stats collector wrapper for it.
	if cfg.Cache != nil && !cfg.EmbeddedCache.IsEnabled() {
		return cfg.Cache, nil
	}

	var caches []Cache

	// Currently fifocache can be enabled in two ways.
	// 1. cfg.EnableFifocache (old deprecated way)
	// 2. cfg.EmbeddedCache.Enabled=true and cfg.EmbeddedCache.Distributed=false (new way)
	if cfg.EnableFifoCache || cfg.EmbeddedCache.IsEnabled() {
		var fifocfg FifoCacheConfig

		if cfg.EnableFifoCache {
			level.Warn(logger).Log("msg", "fifocache config is deprecated. use embedded-cache instead")
			fifocfg = cfg.Fifocache
		}

		if cfg.EmbeddedCache.IsEnabled() {
			fifocfg = FifoCacheConfig{
				MaxSizeBytes:  fmt.Sprint(cfg.EmbeddedCache.MaxSizeMB * 1e6),
				TTL:           cfg.EmbeddedCache.TTL,
				PurgeInterval: cfg.EmbeddedCache.PurgeInterval,
			}
		}

		if fifocfg.TTL == 0 && cfg.DefaultValidity != 0 {
			fifocfg.TTL = cfg.DefaultValidity
		}

		if cache := NewFifoCache(cfg.Prefix+"embedded-cache", fifocfg, reg, logger, cacheType); cache != nil {
			caches = append(caches, CollectStats(Instrument(cfg.Prefix+"embedded-cache", cache, reg)))
		}
	}

	if IsMemcacheSet(cfg) && IsRedisSet(cfg) {
		return nil, errors.New("use of multiple cache storage systems is not supported")
	}

	if IsMemcacheSet(cfg) {
		if cfg.Memcache.Expiration == 0 && cfg.DefaultValidity != 0 {
			cfg.Memcache.Expiration = cfg.DefaultValidity
		}

		client := NewMemcachedClient(cfg.MemcacheClient, cfg.Prefix, reg, logger)
		cache := NewMemcached(cfg.Memcache, client, cfg.Prefix, reg, logger, cacheType)

		cacheName := cfg.Prefix + "memcache"
		caches = append(caches, CollectStats(NewBackground(cacheName, cfg.Background, Instrument(cacheName, cache, reg), reg)))
	}

	if IsRedisSet(cfg) {
		if cfg.Redis.Expiration == 0 && cfg.DefaultValidity != 0 {
			cfg.Redis.Expiration = cfg.DefaultValidity
		}
		cacheName := cfg.Prefix + "redis"
		client, err := NewRedisClient(&cfg.Redis)
		if err != nil {
			return nil, fmt.Errorf("redis client setup failed: %w", err)
		}
		cache := NewRedisCache(cacheName, client, logger, cacheType)
		caches = append(caches, CollectStats(NewBackground(cacheName, cfg.Background, Instrument(cacheName, cache, reg), reg)))
	}

	cache := NewTiered(caches)
	if len(caches) > 1 {
		cache = Instrument(cfg.Prefix+"tiered", cache, reg)
	}
	return cache, nil
}
