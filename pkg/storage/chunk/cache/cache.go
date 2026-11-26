package cache

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/pkg/errors"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/logql/model/stats"
)

// Cache byte arrays by key.
type Cache interface {
	Store(ctx context.Context, keys []string, buf [][]byte, opts ...Option) error
	Fetch(ctx context.Context, keys []string, opts ...Option) (found []string, bufs [][]byte, missing []string, err error)
	Stop()
	// GetCacheType returns a string indicating the cache "type" for the purpose of grouping cache usage statistics
	GetCacheType() stats.CacheType
}

// Options hold the settings of one Fetch or Store call.
type Options struct {
	// ShardKey, when it is not empty, picks the cache server of the call instead
	// of the cache key.
	ShardKey string
}

// Option changes the Options of one Fetch or Store call.
type Option func(*Options)

// WithShardKey creates an Option that picks the cache server of a call from
// shardKey, instead of from the cache key. Items that use the same shard key sit
// on the same server, so a Fetch for them reads from one server.
// An empty shardKey means no shard key. If a call has several WithShardKey
// options, the last one wins.
//
// A call must use the shard key that stored the item. Without it, the call picks
// the server of the cache key, which can be another server. A Fetch then misses
// the item, and a Store writes a second copy. The cache key alone identifies an
// item on a server, so keep cache keys unique across shard keys.
//
// The caller owns the choice of the shard key. The cache does not check that the
// items of a shard key fit on one server, or that one server can carry their
// load. A shard key that groups too many items can fill or overload its server.
//
// At the moment, only Memcached supports the shard key. A cache that does not
// support it ignores it, and a cache that wraps another cache passes it on.
func WithShardKey(shardKey string) Option {
	return func(o *Options) {
		o.ShardKey = shardKey
	}
}

// NewOptions returns the Options that opts set.
func NewOptions(opts ...Option) Options {
	var o Options
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// Config for building Caches.
type Config struct {
	DefaultValidity time.Duration `yaml:"default_validity"`

	Background     BackgroundConfig      `yaml:"background"`
	Memcache       MemcachedConfig       `yaml:"memcached"`
	MemcacheClient MemcachedClientConfig `yaml:"memcached_client"`
	Redis          RedisConfig           `yaml:"redis"`
	EmbeddedCache  EmbeddedCacheConfig   `yaml:"embedded_cache"`

	// This is to name the cache metrics properly.
	Prefix string `yaml:"prefix" doc:"hidden"`

	// For tests to inject specific implementations.
	Cache Cache `yaml:"-"`
}

// RegisterFlagsWithPrefix adds the flags required to config this to the given FlagSet
func (cfg *Config) RegisterFlagsWithPrefix(prefix string, description string, f *flag.FlagSet) {
	cfg.Background.RegisterFlagsWithPrefix(prefix, description, f)
	cfg.Memcache.RegisterFlagsWithPrefix(prefix, description, f)
	cfg.MemcacheClient.RegisterFlagsWithPrefix(prefix, description, f)
	cfg.Redis.RegisterFlagsWithPrefix(prefix, description, f)
	cfg.EmbeddedCache.RegisterFlagsWithPrefix(prefix+"embedded-cache.", description, f)
	f.DurationVar(&cfg.DefaultValidity, prefix+"default-validity", time.Hour, description+"The default validity of entries for caches unless overridden.")

	cfg.Prefix = prefix
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

func IsSpecificImplementationSet(cfg Config) bool {
	return cfg.Cache != nil
}

// IsCacheConfigured determines if either of the following caches is configured:
// - memcached
// - redis
// - embedded-cache
// - specific cache implementation
func IsCacheConfigured(cfg Config) bool {
	return IsMemcacheSet(cfg) || IsRedisSet(cfg) || IsEmbeddedCacheSet(cfg) || IsSpecificImplementationSet(cfg)
}

// New creates a new Cache using Config.
func New(cfg Config, reg prometheus.Registerer, logger log.Logger, cacheType stats.CacheType, metricsNamespace string) (Cache, error) {

	// Have additional check for embeddedcache with distributed mode, because those cache will already be initialized in modules
	// but still need stats collector wrapper for it.
	if cfg.Cache != nil && !cfg.EmbeddedCache.IsEnabled() {
		return cfg.Cache, nil
	}

	var caches []Cache
	if cfg.EmbeddedCache.IsEnabled() {
		if cfg.EmbeddedCache.TTL == 0 && cfg.DefaultValidity != 0 {
			cfg.EmbeddedCache.TTL = cfg.DefaultValidity
		}

		if cache := NewEmbeddedCache(cfg.Prefix+"embedded-cache", cfg.EmbeddedCache, reg, logger, cacheType); cache != nil {
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

		client := NewMemcachedClient(cfg.MemcacheClient, cfg.Prefix, reg, logger, metricsNamespace)
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
