package chunk

import (
	"context"
	"flag"
	"sync"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/weaveworks/common/instrument"

	"github.com/weaveworks/cortex/pkg/util"
)

var (
	memcacheRequests = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "cortex",
		Name:      "memcache_requests_total",
		Help:      "Total count of chunks requested from memcache.",
	})

	memcacheHits = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "cortex",
		Name:      "memcache_hits_total",
		Help:      "Total count of chunks found in memcache.",
	})

	memcacheCorrupt = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "cortex",
		Name:      "memcache_corrupt_chunks_total",
		Help:      "Total count of corrupt chunks found in memcache.",
	})

	memcacheDroppedWriteBack = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "cortex",
		Name:      "memcache_dropped_write_back",
		Help:      "Total count of dropped write backs to memcache.",
	})

	memcacheRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "cortex",
		Name:      "memcache_request_duration_seconds",
		Help:      "Total time spent in seconds doing memcache requests.",
		// Memecache requests are very quick: smallest bucket is 16us, biggest is 1s
		Buckets: prometheus.ExponentialBuckets(0.000016, 4, 8),
	}, []string{"method", "status_code"})
)

func init() {
	prometheus.MustRegister(memcacheRequests)
	prometheus.MustRegister(memcacheHits)
	prometheus.MustRegister(memcacheCorrupt)
	prometheus.MustRegister(memcacheDroppedWriteBack)
	prometheus.MustRegister(memcacheRequestDuration)
}

// Memcache caches things
type Memcache interface {
	GetMulti(keys []string) (map[string]*memcache.Item, error)
	Set(item *memcache.Item) error
}

// CacheConfig is config to make a Cache
type CacheConfig struct {
	Expiration          time.Duration
	WriteBackGoroutines int
	WriteBackBuffer     int
	memcacheConfig      MemcacheConfig
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *CacheConfig) RegisterFlags(f *flag.FlagSet) {
	f.DurationVar(&cfg.Expiration, "memcached.expiration", 0, "How long chunks stay in the memcache.")
	f.IntVar(&cfg.WriteBackGoroutines, "memcache.write-back-goroutines", 10, "How many goroutines to use to write back to memcache.")
	f.IntVar(&cfg.WriteBackBuffer, "memcache.write-back-buffer", 10000, "How many chunks to buffer for background write back.")
	cfg.memcacheConfig.RegisterFlags(f)
}

// Cache type caches chunks
type Cache struct {
	cfg      CacheConfig
	memcache Memcache

	wg       sync.WaitGroup
	quit     chan struct{}
	bgWrites chan backgroundWrite
}

type backgroundWrite struct {
	key string
	buf []byte
}

// NewCache makes a new Cache
func NewCache(cfg CacheConfig) *Cache {
	var memcache Memcache
	if cfg.memcacheConfig.Host != "" {
		memcache = NewMemcacheClient(cfg.memcacheConfig)
	}
	c := &Cache{
		cfg:      cfg,
		memcache: memcache,
		quit:     make(chan struct{}),
		bgWrites: make(chan backgroundWrite, cfg.WriteBackBuffer),
	}
	c.wg.Add(cfg.WriteBackGoroutines)
	for i := 0; i < cfg.WriteBackGoroutines; i++ {
		go c.writeBackLoop()
	}
	return c
}

// Stop the background flushing goroutines.
func (c *Cache) Stop() {
	close(c.quit)
	c.wg.Wait()
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
func (c *Cache) FetchChunkData(ctx context.Context, chunks []Chunk) (found []Chunk, missing []Chunk, err error) {
	if c.memcache == nil {
		return nil, chunks, nil
	}

	memcacheRequests.Add(float64(len(chunks)))

	keys := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		keys = append(keys, chunk.ExternalKey())
	}

	var items map[string]*memcache.Item
	err = instrument.TimeRequestHistogramStatus(ctx, "Memcache.Get", memcacheRequestDuration, memcacheStatusCode, func(_ context.Context) error {
		var err error
		items, err = c.memcache.GetMulti(keys)
		return err
	})
	if err != nil {
		return nil, chunks, err
	}

	decodeContext := NewDecodeContext()
	for i, externalKey := range keys {
		item, ok := items[externalKey]
		if !ok {
			missing = append(missing, chunks[i])
			continue
		}

		if err := chunks[i].Decode(decodeContext, item.Value); err != nil {
			memcacheCorrupt.Inc()
			level.Error(util.WithContext(ctx, util.Logger)).Log("msg", "failed to decode chunk from cache", "err", err)
			missing = append(missing, chunks[i])
			continue
		}

		found = append(found, chunks[i])
	}

	memcacheHits.Add(float64(len(found)))
	return found, missing, nil
}

// StoreChunk serializes and stores a chunk in the chunk cache.
func (c *Cache) StoreChunk(ctx context.Context, key string, buf []byte) error {
	if c.memcache == nil {
		return nil
	}

	return instrument.TimeRequestHistogramStatus(ctx, "Memcache.Put", memcacheRequestDuration, memcacheStatusCode, func(_ context.Context) error {
		item := memcache.Item{
			Key:        key,
			Value:      buf,
			Expiration: int32(c.cfg.Expiration.Seconds()),
		}
		return c.memcache.Set(&item)
	})
}

// BackgroundWrite writes chunks for the cache in the background
func (c *Cache) BackgroundWrite(key string, buf []byte) {
	bgWrite := backgroundWrite{
		key: key,
		buf: buf,
	}
	select {
	case c.bgWrites <- bgWrite:
	default:
		memcacheDroppedWriteBack.Inc()
	}
}

func (c *Cache) writeBackLoop() {
	defer c.wg.Done()

	for {
		select {
		case bgWrite := <-c.bgWrites:
			err := c.StoreChunk(context.Background(), bgWrite.key, bgWrite.buf)
			if err != nil {
				level.Error(util.Logger).Log("msg", "error writing to memcache", "err", err)
			}
		case <-c.quit:
			return
		}
	}
}
