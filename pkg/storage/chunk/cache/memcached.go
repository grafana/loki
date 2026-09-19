package cache

import (
	"context"
	"encoding/hex"
	"flag"
	"hash/fnv"
	"sync"
	"time"

	"github.com/go-kit/log"
	instr "github.com/grafana/dskit/instrument"
	"github.com/grafana/gomemcache/memcache"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

// MemcachedConfig is config to make a Memcached
type MemcachedConfig struct {
	Expiration time.Duration `yaml:"expiration"`

	// BatchSize is the number of keys fetched per worker request. When
	// non-zero, Fetch splits keys into batches of this size and processes them
	// in parallel. If the context is cancelled mid-flight, already-completed
	// batches are returned as partial results and the rest are reported missed.
	BatchSize   int `yaml:"batch_size"`
	Parallelism int `yaml:"parallelism"`
}

// RegisterFlagsWithPrefix adds the flags required to config this to the given FlagSet
func (cfg *MemcachedConfig) RegisterFlagsWithPrefix(prefix, description string, f *flag.FlagSet) {
	f.DurationVar(&cfg.Expiration, prefix+"memcached.expiration", 0, description+"How long keys stay in the memcache.")
	f.IntVar(&cfg.BatchSize, prefix+"memcached.batchsize", 4, description+"How many keys to fetch in each batch.")
	f.IntVar(&cfg.Parallelism, prefix+"memcached.parallelism", 5, description+"Maximum active requests to memcache.")
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

	// `closed` is closed by Stop() to signal that the cache is stopping.
	// It is closed before inputCh so that senders can safely check it before
	// attempting a send and avoid a "send on closed channel" panic.
	closed chan struct{}

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
				Namespace: constants.Loki,
				Name:      "memcache_request_duration_seconds",
				Help:      "Total time spent in seconds doing memcache requests.",
				// 16us, 64us, 256us, 1.024ms, 4.096ms, 16.384ms, 65.536ms, 150ms, 250ms, 500ms, 1s
				Buckets: append(prometheus.ExponentialBuckets(0.000016, 4, 7), []float64{
					(150 * time.Millisecond).Seconds(),
					(250 * time.Millisecond).Seconds(),
					(500 * time.Millisecond).Seconds(),
					(time.Second).Seconds(),
				}...),
				ConstLabels: prometheus.Labels{"name": name},
			}, []string{"method", "status_code"}),
		),
		closed: make(chan struct{}),
	}

	if cfg.BatchSize == 0 || cfg.Parallelism == 0 {
		return c
	}

	c.inputCh = make(chan *work)
	c.wg.Add(cfg.Parallelism)

	for i := 0; i < cfg.Parallelism; i++ {
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
	// See https://godoc.org/github.com/grafana/gomemcache/memcache#pkg-variables
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

// Fetch gets keys from the cache. The keys that are found must be in the order
// of the keys requested.
//
// When BatchSize is non-zero, Fetch submits keys in batches to a worker pool.
// If the context is cancelled before all batches complete, Fetch returns
// whatever results have been collected so far together with ctx.Err(). Keys
// whose batch did not complete in time are reported as missed.
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
	items, err = c.memcache.GetMulti(ctx, keys)
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
	batchSize := c.cfg.BatchSize
	numBatches := (len(keys) + batchSize - 1) / batchSize

	// resultsCh is buffered to numBatches so workers can always deliver without
	// blocking, even when we stop collecting early due to cancellation or Stop.
	// This lets us submit batches synchronously: if inputCh is full we block
	// until a worker picks up a batch (it can always send its result to the
	// buffered resultsCh first), so there is no deadlock risk.
	resultsCh := make(chan *result, numBatches)

	var (
		sent  int
		abort bool
	)
	for i := 0; i < len(keys) && !abort; i += batchSize {
		// Stop() closes c.closed before c.inputCh, so c.closed is always ready
		// by the time inputCh is closed. The default branch (and its send to
		// inputCh) is therefore never reached when inputCh is unsafe to use.
		select {
		case <-c.closed:
			abort = true
		case <-ctx.Done():
			abort = true
			err = ctx.Err()
		default:
			c.inputCh <- &work{
				keys:     keys[i:min(i+batchSize, len(keys))],
				ctx:      ctx,
				resultCh: resultsCh,
				batchID:  sent,
			}
			sent++
		}
	}

	// If Stop() was called, workers may not complete — return nothing.
	select {
	case <-c.closed:
		return
	default:
	}

	// Collect exactly as many results as were submitted,
	// then aggregate in batch order to preserve input key ordering.
	results := make([]*result, sent)
	for i := 0; i < sent; i++ {
		r := <-resultsCh
		results[r.batchID] = r
	}
	for _, r := range results {
		found = append(found, r.found...)
		bufs = append(bufs, r.bufs...)
		missed = append(missed, r.missed...)
		if r.err != nil {
			// NOTE: this will overwrite the error from the context if any.
			// I considered using a multierror, but if there are many batches,
			// this may result in too many errors for the likely same reason
			err = r.err
		}
	}

	if sent < numBatches {
		// Keys in batches that were never submitted are all missed.
		missed = append(missed, keys[sent*batchSize:]...)
	}

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

func (c *Memcached) Stop() {
	if c.inputCh == nil {
		return
	}

	close(c.closed)  // signal senders to stop before closing inputCh
	close(c.inputCh) // stop workers
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
