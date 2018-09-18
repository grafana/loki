package cache

import (
	"context"
	"flag"
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

	background     BackgroundConfig
	memcache       MemcachedConfig
	memcacheClient MemcachedClientConfig
	diskcache      DiskcacheConfig

	// For tests to inject specific implementations.
	Cache Cache
}

// RegisterFlags adds the flags required to config this to the given FlagSet.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&cfg.EnableDiskcache, "cache.enable-diskcache", false, "Enable on-disk cache")

	cfg.background.RegisterFlags(f)
	cfg.memcache.RegisterFlags(f)
	cfg.memcacheClient.RegisterFlags(f)
	cfg.diskcache.RegisterFlags(f)
}

// New creates a new Cache using Config.
func New(cfg Config) (Cache, error) {
	if cfg.Cache != nil {
		return cfg.Cache, nil
	}

	caches := []Cache{}

	if cfg.EnableDiskcache {
		cache, err := NewDiskcache(cfg.diskcache)
		if err != nil {
			return nil, err
		}
		caches = append(caches, Instrument("diskcache", cache))
	}

	if cfg.memcacheClient.Host != "" {
		client := NewMemcachedClient(cfg.memcacheClient)
		cache := NewMemcached(cfg.memcache, client)
		caches = append(caches, Instrument("memcache", cache))
	}

	cache := NewTiered(caches)
	if len(caches) > 1 {
		cache = Instrument("tiered", cache)
	}

	cache = NewBackground(cfg.background, cache)
	return cache, nil
}
