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
	EnableDiskcache bool
	EnableFifoCache bool

	DefaultValidity time.Duration

	background     BackgroundConfig
	memcache       MemcachedConfig
	memcacheClient MemcachedClientConfig
	diskcache      DiskcacheConfig
	fifocache      FifoCacheConfig

	// This is to name the cache metrics properly.
	prefix string

	// For tests to inject specific implementations.
	Cache Cache
}

// RegisterFlagsWithPrefix adds the flags required to config this to the given FlagSet
func (cfg *Config) RegisterFlagsWithPrefix(prefix string, description string, f *flag.FlagSet) {
	cfg.background.RegisterFlagsWithPrefix(prefix, description, f)
	cfg.memcache.RegisterFlagsWithPrefix(prefix, description, f)
	cfg.memcacheClient.RegisterFlagsWithPrefix(prefix, description, f)
	cfg.diskcache.RegisterFlagsWithPrefix(prefix, description, f)
	cfg.fifocache.RegisterFlagsWithPrefix(prefix, description, f)

	f.BoolVar(&cfg.EnableDiskcache, prefix+"cache.enable-diskcache", false, description+"Enable on-disk cache.")
	f.BoolVar(&cfg.EnableFifoCache, prefix+"cache.enable-fifocache", false, description+"Enable in-memory cache.")
	f.DurationVar(&cfg.DefaultValidity, prefix+"default-validity", 0, description+"The default validity of entries for caches unless overridden.")

	cfg.prefix = prefix
}

// New creates a new Cache using Config.
func New(cfg Config) (Cache, error) {
	if cfg.Cache != nil {
		return cfg.Cache, nil
	}

	caches := []Cache{}

	if cfg.EnableFifoCache {
		if cfg.fifocache.Validity == 0 && cfg.DefaultValidity != 0 {
			cfg.fifocache.Validity = cfg.DefaultValidity
		}

		cache := NewFifoCache(cfg.prefix+"fifocache", cfg.fifocache)
		caches = append(caches, Instrument(cfg.prefix+"fifocache", cache))
	}

	if cfg.EnableDiskcache {
		cache, err := NewDiskcache(cfg.diskcache)
		if err != nil {
			return nil, err
		}

		cacheName := cfg.prefix + "diskcache"
		caches = append(caches, NewBackground(cacheName, cfg.background, Instrument(cacheName, cache)))
	}

	if cfg.memcacheClient.Host != "" {
		if cfg.memcache.Expiration == 0 && cfg.DefaultValidity != 0 {
			cfg.memcache.Expiration = cfg.DefaultValidity
		}

		client := NewMemcachedClient(cfg.memcacheClient)
		cache := NewMemcached(cfg.memcache, client)

		cacheName := cfg.prefix + "memcache"
		caches = append(caches, NewBackground(cacheName, cfg.background, Instrument(cacheName, cache)))
	}

	cache := NewTiered(caches)
	if len(caches) > 1 {
		cache = Instrument(cfg.prefix+"tiered", cache)
	}
	return cache, nil
}
