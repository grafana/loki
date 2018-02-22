package cache

import (
	"context"
	"flag"
)

// Cache byte arrays by key.
type Cache interface {
	StoreChunk(ctx context.Context, key string, buf []byte) error
	FetchChunkData(ctx context.Context, keys []string) (found []string, bufs [][]byte, missing []string, err error)
	Stop() error
}

// Config for building Caches.
type Config struct {
	EnableDiskcache bool

	background     BackgroundConfig
	memcache       MemcachedConfig
	memcacheClient MemcachedClientConfig
	diskcache      DiskcacheConfig
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
	caches := []Cache{}

	if cfg.EnableDiskcache {
		cache, err := NewDiskcache(cfg.diskcache)
		if err != nil {
			return nil, err
		}
		caches = append(caches, instrument("diskcache", cache))
	}

	if cfg.memcacheClient.Host != "" {
		client := newMemcachedClient(cfg.memcacheClient)
		cache := NewMemcached(cfg.memcache, client)
		caches = append(caches, instrument("memcache", cache))
	}

	var cache Cache = tiered(caches)
	if len(caches) > 1 {
		cache = instrument("tiered", cache)
	}

	cache = NewBackground(cfg.background, cache)
	return cache, nil
}
