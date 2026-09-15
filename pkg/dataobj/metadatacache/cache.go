package metadatacache

import (
	"context"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"golang.org/x/sync/singleflight"
	"golang.org/x/time/rate"

	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
)

// keyPrefix namespaces entries and versions the on-wire format. Bump the version to invalidate old
// entries after a format change.
const keyPrefix = "dataobj-metadata/v1:"

// loadTimeout bounds a shared load. It runs on a context detached from every caller, so without this a
// stuck backend could strand the load and its goroutine after all callers have left.
const loadTimeout = 30 * time.Second

// DefaultMaxItemBytes is the size limit New falls back to when the caller does not know the backend's
// real one.
const DefaultMaxItemBytes = 64 << 20 // 64 MiB

// Cache adapts a cache.Cache backend into a dataobj.MetadataCache. It prefixes and versions keys, and
// coalesces concurrent misses for the same key into a single load via a singleflight.Group. It is safe
// for concurrent use.
type Cache struct {
	cache        cache.Cache
	maxItemBytes int64
	logger       log.Logger
	sf           singleflight.Group

	// A misconfigured or oversize object hits the same error on every open; rate-limit the logs so it
	// cannot flood, while the counters below still record every occurrence.
	fetchErrLog rate.Sometimes
	storeErrLog rate.Sometimes

	hits        prometheus.Counter
	misses      prometheus.Counter
	errors      *prometheus.CounterVec
	storedBytes prometheus.Counter
}

// New wraps c as a dataobj.MetadataCache. reg may be nil.
//
// maxItemBytes is the largest item c's backend will actually store. A non-positive value falls back to
// DefaultMaxItemBytes.
func New(c cache.Cache, maxItemBytes int64, reg prometheus.Registerer, logger log.Logger) *Cache {
	if logger == nil {
		logger = log.NewNopLogger()
	}
	if maxItemBytes <= 0 {
		maxItemBytes = DefaultMaxItemBytes
	}
	cc := &Cache{
		cache:        c,
		maxItemBytes: maxItemBytes,
		logger:       logger,
		fetchErrLog:  rate.Sometimes{Interval: time.Minute},
		storeErrLog:  rate.Sometimes{Interval: time.Minute},
		hits: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_metadata_cache_hits_total",
			Help: "Data-object metadata regions served from the cache.",
		}),
		misses: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_metadata_cache_misses_total",
			Help: "Data-object metadata regions not found in the cache and loaded from object storage.",
		}),
		errors: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_dataobj_metadata_cache_errors_total",
			Help: "Data-object metadata cache errors by operation (fetch, store).",
		}, []string{"operation"}),
		storedBytes: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_metadata_cache_stored_bytes_total",
			Help: "Total data-object metadata bytes written to the cache.",
		}),
	}

	// Pre-create both label values so a healthy process still exposes a zero series for each
	// operation; a metric that only appears on the first error is indistinguishable from a scrape gap.
	cc.errors.WithLabelValues("fetch")
	cc.errors.WithLabelValues("store")

	return cc
}

// GetOrLoadMetadataRegion returns the metadata region for key, loading and caching it via load on a miss.
// Concurrent misses for the same key share a single load. A cache fetch or store error is counted (and
// logged, rate limited) but never fails the call: it degrades to a load from object storage.
//
// Each caller returns on its own context cancellation. The shared load itself runs on a context
// detached from any single caller: it keeps request-scoped values, drops cancellation, and is bounded
// by loadTimeout. One caller giving up neither aborts the load nor fails the others waiting on it.
func (c *Cache) GetOrLoadMetadataRegion(ctx context.Context, key string, load func(context.Context) ([]byte, error)) ([]byte, error) {
	k := keyPrefix + key

	_, bufs, _, err := c.cache.Fetch(ctx, []string{k})
	switch {
	case err != nil && ctx.Err() != nil:
		// A Fetch error alongside an already-canceled caller context is not a fact about the cache
		// backend, so it must not count as a hit, a miss, or a backend error; return the caller's own
		// error instead of dispatching a load it cannot use. This only covers a Fetch that errors: a
		// clean miss with an already-canceled context still dispatches a load, since the miss itself
		// carries no such signal to act on.
		return nil, ctx.Err()
	case err != nil:
		// A hit or a miss is a fact about the key; an error means Fetch could not establish that fact
		// at all, so it must not also count as a miss.
		c.errors.WithLabelValues("fetch").Inc()
		c.fetchErrLog.Do(func() {
			level.Warn(c.logger).Log("msg", "data object metadata cache fetch failed", "key", key, "err", err)
		})
	case len(bufs) == 1:
		c.hits.Inc()
		return bufs[0], nil
	default:
		c.misses.Inc()
	}

	ch := c.sf.DoChan(k, func() (any, error) {
		// Detach from the caller that happened to trigger this load: its cancellation must not abort a
		// load the other waiters still need. WithoutCancel keeps request-scoped values (tracing,
		// stats.Context) so the read is still attributed and traced.
		loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), loadTimeout)
		defer cancel()

		md, err := load(loadCtx)
		if err != nil {
			return nil, err
		}
		if err := c.cache.Store(loadCtx, []string{k}, [][]byte{md}); err != nil {
			c.errors.WithLabelValues("store").Inc()
			c.storeErrLog.Do(func() {
				level.Warn(c.logger).Log("msg", "data object metadata cache store failed", "key", key, "err", err)
			})
		} else {
			c.storedBytes.Add(float64(len(md)))
		}
		return md, nil
	})

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-ch:
		if res.Err != nil {
			return nil, res.Err
		}
		return res.Val.([]byte), nil
	}
}

// MaxItemBytes returns the largest item the backend will store: the value configured on New, or
// DefaultMaxItemBytes when that was not positive. The result is always positive.
func (c *Cache) MaxItemBytes() int64 {
	return c.maxItemBytes
}

// Stop releases the underlying cache.
func (c *Cache) Stop() {
	c.cache.Stop()
}
