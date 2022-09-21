package cache

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"hash/fnv"
	"sync"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	instr "github.com/weaveworks/common/instrument"

	"github.com/grafana/loki/pkg/logqlmodel/stats"
	"github.com/grafana/loki/pkg/util/math"
)

// MemcachedConfig is config to make a Memcached
type MemcachedConfig struct {
	Expiration time.Duration `yaml:"expiration"`

	BatchSize   int `yaml:"batch_size"`
	Parallelism int `yaml:"parallelism"`
}

// RegisterFlagsWithPrefix adds the flags required to config this to the given FlagSet
func (cfg *MemcachedConfig) RegisterFlagsWithPrefix(prefix, description string, f *flag.FlagSet) {
	f.DurationVar(&cfg.Expiration, prefix+"memcached.expiration", 0, description+"How long keys stay in the memcache.")
	f.IntVar(&cfg.BatchSize, prefix+"memcached.batchsize", 1024, description+"How many keys to fetch in each batch.")
	f.IntVar(&cfg.Parallelism, prefix+"memcached.parallelism", 100, description+"Maximum active requests to memcache.")
}

// Memcached type caches chunks in memcached
type Memcached struct {
	cfg       MemcachedConfig
	memcache  MemcachedClient
	name      string
	cacheType stats.CacheType

	requestDuration *instr.HistogramCollector

	wg      sync.WaitGroup
	inputCh chan *work

	close chan struct{}

	logger log.Logger
}

// NewMemcached makes a new Memcached.
func NewMemcached(cfg MemcachedConfig, client MemcachedClient, name string, reg prometheus.Registerer, logger log.Logger, cacheType stats.CacheType) *Memcached {
	c := &Memcached{
		cfg:       cfg,
		memcache:  client,
		name:      name,
		logger:    logger,
		cacheType: cacheType,
		requestDuration: instr.NewHistogramCollector(
			promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
				Namespace: "loki",
				Name:      "memcache_request_duration_seconds",
				Help:      "Total time spent in seconds doing memcache requests.",
				// Memcached requests are very quick: smallest bucket is 16us, biggest is 1s
				Buckets:     prometheus.ExponentialBuckets(0.000016, 4, 8),
				ConstLabels: prometheus.Labels{"name": name},
			}, []string{"method", "status_code"}),
		),
		close: make(chan struct{}),
	}

	if cfg.BatchSize == 0 || cfg.Parallelism == 0 {
		return c
	}

	c.inputCh = make(chan *work)

	for i := 0; i < cfg.Parallelism; i++ {
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			for input := range c.inputCh {
				res := &result{
					batchID: input.batchID,
				}
				res.found, res.bufs, res.missed, res.err = c.fetch(input.ctx, input.keys)
				input.resultCh <- res
			}
		}()
	}
	return c
}

type work struct {
	keys     []string
	ctx      context.Context
	resultCh chan<- *result
	batchID  int // For ordering results.
}

type result struct {
	found   []string
	bufs    [][]byte
	missed  []string
	err     error
	batchID int // For ordering results.
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

// Fetch gets keys from the cache. The keys that are found must be in the order of the keys requested.
func (c *Memcached) Fetch(ctx context.Context, keys []string) (found []string, bufs [][]byte, missed []string, err error) {
	if c.cfg.BatchSize == 0 {
		found, bufs, missed, err = c.fetch(ctx, keys)
		return
	}

	start := time.Now()
	found, bufs, missed, err = c.fetchKeysBatched(ctx, keys)
	c.requestDuration.After(ctx, "Memcache.GetBatched", memcacheStatusCode(err), start)
	return
}

func (c *Memcached) fetch(ctx context.Context, keys []string) (found []string, bufs [][]byte, missed []string, err error) {
	var (
		start = time.Now()
		items map[string]*memcache.Item
	)
	items, err = c.memcache.GetMulti(keys)
	c.requestDuration.After(ctx, "Memcache.GetMulti", memcacheStatusCode(err), start)
	if err != nil {
		return found, bufs, keys, err
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

func (c *Memcached) fetchKeysBatched(ctx context.Context, keys []string) (found []string, bufs [][]byte, missed []string, err error) {
	// Read all values from this channel to avoid blocking upstream.
	batchSize := c.cfg.BatchSize

	maxResults := len(keys) / batchSize
	if len(keys)%batchSize != 0 {
		maxResults++
	}

	resultsCh := make(chan *result, maxResults)

	go func() {
		for i, j := 0, 0; i < len(keys); i += batchSize {
			batchKeys := keys[i:math.Min(i+batchSize, len(keys))]
			// This go routines owns the `c.inputCh` in a way that it is the only actor writing to it.
			// It's better to close that channel by this writer goroutine rather than in `Close()` method.
			// Because doing so would have inherint race between "closing" `c.inputCh` and "writing" to it.
			select {
			case <-c.close:
				// fmt.Println("in close case, fetchKeysBatched", i, j)
				if c.inputCh != nil {
					close(c.inputCh)
					c.inputCh = nil
				}
				return
			default:
				// fmt.Println("in default case, fetchKeysBatched")
				c.inputCh <- &work{
					keys:     batchKeys,
					ctx:      ctx,
					resultCh: resultsCh,
					batchID:  j,
				}
				j++
			}
		}
	}()

	// fmt.Println("fetchKeysBatched before")

	// actualResults defines the actual results that got into resultsCh before `Stop()` is called.
	actualResults := len(resultsCh)

	// We need to order found by the input keys order.
	results := make([]*result, actualResults)

	for i := 0; i < actualResults; i++ {
		result := <-resultsCh // this may block if nothing written to resultsCh, can happen if `Stop()` is called before anything written to `c.inputCh`
		// fmt.Println("fetchKeysBatched after")
		results[result.batchID] = result
	}

	// foundMap := make(map[string]bool)

	// for _, result := range results {
	// 	found = append(found, result.found...)
	// 	bufs = append(bufs, result.bufs...)
	// 	missed = append(missed, result.missed...)
	// 	if result.err != nil {
	// 		err = result.err
	// 	}
	// }

	// for _, v := range found {
	// 	foundMap[v] = true
	// }

	// // If `Stop()` called before the batch fetch is completed, then still there could be missing keys that didn't even made request for.
	// // Add those to the missed slice.
	// for _, k := range keys {
	// 	if !foundMap[k] {
	// 		missed = append(missed, k)
	// 	}
	// }

	return
}

// Store stores the key in the cache.
func (c *Memcached) Store(ctx context.Context, keys []string, bufs [][]byte) error {
	var err error
	for i := range keys {
		cacheErr := instr.CollectedRequest(ctx, "Memcache.Put", c.requestDuration, memcacheStatusCode, func(_ context.Context) error {
			item := memcache.Item{
				Key:        keys[i],
				Value:      bufs[i],
				Expiration: int32(c.cfg.Expiration.Seconds()),
			}
			return c.memcache.Set(&item)
		})
		if cacheErr != nil {
			err = cacheErr
		}
	}
	return err
}

// Stop does nothing.
func (c *Memcached) Stop() {
	if c.inputCh == nil {
		return
	}
	fmt.Println("Closing the close channel")
	close(c.close)
	c.wg.Wait()
}

func (c *Memcached) GetCacheType() stats.CacheType {
	return c.cacheType
}

// HashKey hashes key into something you can store in memcached.
func HashKey(key string) string {
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(key)) // This'll never error.

	// Hex because memcache errors for the bytes produced by the hash.
	return hex.EncodeToString(hasher.Sum(nil))
}
