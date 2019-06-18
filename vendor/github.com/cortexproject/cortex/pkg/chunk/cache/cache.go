package cache

import (
	"context"
	"flag"
	"time"
)

// Cache byte arrays by key.
type Cache interface {
	Store(ctx context.Context, key []string, buf [][]byte)
	Fetch(ctx context.Context, keys []string) (found []string, bufs [][]byte, missing []string)
	Stop() error
}

// Config for building Caches.
type Config struct {
	EnableFifoCache bool `yaml:"enable_fifocache,omitempty"`

	DefaultValidity time.Duration `yaml:"defaul_validity,omitempty"`

	Background     BackgroundConfig      `yaml:"background,omitempty"`
	Memcache       MemcachedConfig       `yaml:"memcached,omitempty"`
	MemcacheClient MemcachedClientConfig `yaml:"memcached_client,omitempty"`
	Fifocache      FifoCacheConfig       `yaml:"fifocache,omitempty"`

	// This is to name the cache metrics properly.
	Prefix string `yaml:"prefix,omitempty"`

	// For tests to inject specific implementations.
	Cache Cache
}

// RegisterFlagsWithPrefix adds the flags required to config this to the given FlagSet
func (cfg *Config) RegisterFlagsWithPrefix(prefix string, description string, f *flag.FlagSet) {
	cfg.Background.RegisterFlagsWithPrefix(prefix, description, f)
	cfg.Memcache.RegisterFlagsWithPrefix(prefix, description, f)
	cfg.MemcacheClient.RegisterFlagsWithPrefix(prefix, description, f)
	cfg.Fifocache.RegisterFlagsWithPrefix(prefix, description, f)

	f.BoolVar(&cfg.EnableFifoCache, prefix+"cache.enable-fifocache", false, description+"Enable in-memory cache.")
	f.DurationVar(&cfg.DefaultValidity, prefix+"default-validity", 0, description+"The default validity of entries for caches unless overridden.")

	cfg.Prefix = prefix
}

// New creates a new Cache using Config.
func New(cfg Config) (Cache, error) {
	if cfg.Cache != nil {
		return cfg.Cache, nil
	}

	caches := []Cache{}

	if cfg.EnableFifoCache {
		if cfg.Fifocache.Validity == 0 && cfg.DefaultValidity != 0 {
			cfg.Fifocache.Validity = cfg.DefaultValidity
		}

		cache := NewFifoCache(cfg.Prefix+"fifocache", cfg.Fifocache)
		caches = append(caches, Instrument(cfg.Prefix+"fifocache", cache))
	}

	if cfg.MemcacheClient.Host != "" {
		if cfg.Memcache.Expiration == 0 && cfg.DefaultValidity != 0 {
			cfg.Memcache.Expiration = cfg.DefaultValidity
		}

		client := NewMemcachedClient(cfg.MemcacheClient)
		cache := NewMemcached(cfg.Memcache, client, cfg.Prefix)

		cacheName := cfg.Prefix + "memcache"
		caches = append(caches, NewBackground(cacheName, cfg.Background, Instrument(cacheName, cache)))
	}

	cache := NewTiered(caches)
	if len(caches) > 1 {
		cache = Instrument(cfg.Prefix+"tiered", cache)
	}
	return cache, nil
}
