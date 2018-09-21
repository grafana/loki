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

	prefix string

	// For tests to inject specific implementations.
	Cache Cache
}

// RegisterFlags adds the flags required to config this to the given FlagSet.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&cfg.EnableDiskcache, "cache.enable-diskcache", false, "Enable on-disk cache")
	f.BoolVar(&cfg.EnableFifoCache, "cache.enable-fifocache", false, "Enable in-mem cache")
	f.DurationVar(&cfg.DefaultValidity, "cache.default-validity", 0, "The default validity of entries for caches unless overridden.")

	cfg.background.RegisterFlags(f)
	cfg.memcache.RegisterFlags(f)
	cfg.memcacheClient.RegisterFlags(f)
	cfg.diskcache.RegisterFlags(f)
	cfg.fifocache.RegisterFlags(f)
}

// New creates a new Cache using Config.
func New(cfg Config) (Cache, error) {
	if cfg.Cache != nil {
		return cfg.Cache, nil
	}

	caches := []Cache{}

	if cfg.EnableFifoCache {
		prefix := ""
		if cfg.prefix != "" {
			prefix = cfg.prefix
		}

		if cfg.fifocache.Validity == 0 && cfg.DefaultValidity != 0 {
			cfg.fifocache.Validity = cfg.DefaultValidity
		}

		cache := NewFifoCache(prefix, cfg.fifocache)
		caches = append(caches, Instrument(cfg.prefix+"fifocache", cache))
	}

	if cfg.EnableDiskcache {
		cache, err := NewDiskcache(cfg.diskcache)
		if err != nil {
			return nil, err
		}
		caches = append(caches, Instrument(cfg.prefix+"diskcache", cache))
	}

	if cfg.memcacheClient.Host != "" {
		if cfg.memcache.Expiration == 0 && cfg.DefaultValidity != 0 {
			cfg.memcache.Expiration = cfg.DefaultValidity
		}

		client := NewMemcachedClient(cfg.memcacheClient)
		cache := NewMemcached(cfg.memcache, client)
		caches = append(caches, Instrument(cfg.prefix+"memcache", cache))
	}

	cache := NewTiered(caches)
	if len(caches) > 1 {
		cache = Instrument(cfg.prefix+"tiered", cache)
	}

	cache = NewBackground(cfg.background, cache)
	return cache, nil
}
