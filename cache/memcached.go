package cache

import (
	"context"
	"flag"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/prometheus/client_golang/prometheus"
	instr "github.com/weaveworks/common/instrument"
)

var (
	memcacheRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "cortex",
		Name:      "memcache_request_duration_seconds",
		Help:      "Total time spent in seconds doing memcache requests.",
		// Memecache requests are very quick: smallest bucket is 16us, biggest is 1s
		Buckets: prometheus.ExponentialBuckets(0.000016, 4, 8),
	}, []string{"method", "status_code"})
)

func init() {
	prometheus.MustRegister(memcacheRequestDuration)
}

// MemcachedConfig is config to make a Memcached
type MemcachedConfig struct {
	Expiration time.Duration
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *MemcachedConfig) RegisterFlags(f *flag.FlagSet) {
	f.DurationVar(&cfg.Expiration, "memcached.expiration", 0, "How long chunks stay in the memcache.")
}

// Memcached type caches chunks in memcached
type Memcached struct {
	cfg      MemcachedConfig
	memcache MemcachedClient
}

// NewMemcached makes a new Memcache
func NewMemcached(cfg MemcachedConfig, client MemcachedClient) *Memcached {
	c := &Memcached{
		cfg:      cfg,
		memcache: client,
	}
	return c
}

func memcacheStatusCode(err error) string {
	// See https://godoc.org/github.com/bradfitz/gomemcache/memcache#pkg-variables
	switch err {
	case nil:
		return "200"
	case memcache.ErrCacheMiss:
		return "404"
	case memcache.ErrMalformedKey:
		return "400"
	default:
		return "500"
	}
}

// FetchChunkData gets chunks from the chunk cache.
func (c *Memcached) FetchChunkData(ctx context.Context, keys []string) (found []string, bufs [][]byte, missed []string, err error) {
	var items map[string]*memcache.Item
	err = instr.TimeRequestHistogramStatus(ctx, "Memcache.Get", memcacheRequestDuration, memcacheStatusCode, func(_ context.Context) error {
		var err error
		items, err = c.memcache.GetMulti(keys)
		return err
	})
	if err != nil {
		return
	}
	for _, key := range keys {
		item, ok := items[key]
		if ok {
			found = append(found, key)
			bufs = append(bufs, item.Value)
		} else {
			missed = append(missed, key)
		}
	}
	return
}

// StoreChunk serializes and stores a chunk in the chunk cache.
func (c *Memcached) StoreChunk(ctx context.Context, key string, buf []byte) error {
	return instr.TimeRequestHistogramStatus(ctx, "Memcache.Put", memcacheRequestDuration, memcacheStatusCode, func(_ context.Context) error {
		item := memcache.Item{
			Key:        key,
			Value:      buf,
			Expiration: int32(c.cfg.Expiration.Seconds()),
		}
		return c.memcache.Set(&item)
	})
}

// Stop does nothing.
func (*Memcached) Stop() error {
	return nil
}
