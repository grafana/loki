package cache

import (
	"context"
	"errors"
	"flag"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/prometheus/client_golang/prometheus"
)

// Cache byte arrays by key.
//
// NB we intentionally do not return errors in this interface - caching is best
// effort by definition.  We found that when these methods did return errors,
// the caller would just log them - so its easier for implementation to do that.
// Whatsmore, we found partially successful Fetchs were often treated as failed
// when they returned an error.
type Cache interface {
	Store(ctx context.Context, key []string, buf [][]byte)
	Fetch(ctx context.Context, keys []string) (found []string, bufs [][]byte, missing []string)
	Stop()
}

// Config for building Caches.
type Config struct {
	EnableFifoCache bool `yaml:"enable_fifocache"`

	DefaultValidity time.Duration `yaml:"default_validity"`

	Background     BackgroundConfig      `yaml:"background"`
	Memcache       MemcachedConfig       `yaml:"memcached"`
	MemcacheClient MemcachedClientConfig `yaml:"memcached_client"`
	Redis          RedisConfig           `yaml:"redis"`
	Fifocache      FifoCacheConfig       `yaml:"fifocache"`

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
	cfg.Fifocache.RegisterFlagsWithPrefix(prefix, description, f)

	f.BoolVar(&cfg.EnableFifoCache, prefix+"cache.enable-fifocache", false, description+"Enable in-memory cache.")
	f.DurationVar(&cfg.DefaultValidity, prefix+"default-validity", 0, description+"The default validity of entries for caches unless overridden.")

	cfg.Prefix = prefix
}

func (cfg *Config) Validate() error {
	return cfg.Fifocache.Validate()
}

// New creates a new Cache using Config.
func New(cfg Config, reg prometheus.Registerer, logger log.Logger) (Cache, error) {
	if cfg.Cache != nil {
		return cfg.Cache, nil
	}

	caches := []Cache{}

	if cfg.EnableFifoCache {
		if cfg.Fifocache.Validity == 0 && cfg.DefaultValidity != 0 {
			cfg.Fifocache.Validity = cfg.DefaultValidity
		}

		if cache := NewFifoCache(cfg.Prefix+"fifocache", cfg.Fifocache, reg, logger); cache != nil {
			caches = append(caches, Instrument(cfg.Prefix+"fifocache", cache, reg))
		}
	}

	if (cfg.MemcacheClient.Host != "" || cfg.MemcacheClient.Addresses != "") && cfg.Redis.Endpoint != "" {
		return nil, errors.New("use of multiple cache storage systems is not supported")
	}

	if cfg.MemcacheClient.Host != "" || cfg.MemcacheClient.Addresses != "" {
		if cfg.Memcache.Expiration == 0 && cfg.DefaultValidity != 0 {
			cfg.Memcache.Expiration = cfg.DefaultValidity
		}

		client := NewMemcachedClient(cfg.MemcacheClient, cfg.Prefix, reg, logger)
		cache := NewMemcached(cfg.Memcache, client, cfg.Prefix, reg, logger)

		cacheName := cfg.Prefix + "memcache"
		caches = append(caches, NewBackground(cacheName, cfg.Background, Instrument(cacheName, cache, reg), reg))
	}

	if cfg.Redis.Endpoint != "" {
		if cfg.Redis.Expiration == 0 && cfg.DefaultValidity != 0 {
			cfg.Redis.Expiration = cfg.DefaultValidity
		}
		cacheName := cfg.Prefix + "redis"
		cache := NewRedisCache(cacheName, NewRedisClient(&cfg.Redis), reg, logger)
		caches = append(caches, NewBackground(cacheName, cfg.Background, Instrument(cacheName, cache, reg), reg))
	}

	cache := NewTiered(caches)
	if len(caches) > 1 {
		cache = Instrument(cfg.Prefix+"tiered", cache, reg)
	}
	return cache, nil
}
