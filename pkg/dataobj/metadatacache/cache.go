// Package metadatacache provides a dataobj.MetadataCache backed by a cache.Cache: it caches the immutable
// metadata prefix of data objects so opening an object does not read its metadata from object storage on
// every open.
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

// Cache caches data-object metadata prefixes in a cache.Cache. Objects are immutable, so entries never go
// stale. It is safe for concurrent use.
type Cache struct {
	cache  cache.Cache
	logger log.Logger
	sf     singleflight.Group

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
func New(c cache.Cache, reg prometheus.Registerer, logger log.Logger) *Cache {
	if logger == nil {
		logger = log.NewNopLogger()
	}
	return &Cache{
		cache:       c,
		logger:      logger,
		fetchErrLog: rate.Sometimes{Interval: time.Minute},
		storeErrLog: rate.Sometimes{Interval: time.Minute},
		hits: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_metadata_cache_hits_total",
			Help: "Data-object metadata prefixes served from the cache.",
		}),
		misses: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_metadata_cache_misses_total",
			Help: "Data-object metadata prefixes not found in the cache and loaded from object storage.",
		}),
		errors: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_dataobj_metadata_cache_errors_total",
			Help: "Data-object metadata cache errors by operation (fetch, store).",
		}, []string{"op"}),
		storedBytes: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_metadata_cache_stored_bytes_total",
			Help: "Total metadata bytes written to the cache.",
		}),
	}
}

// GetMetadata returns the metadata prefix for key, loading and caching it via load on a miss. Concurrent
// misses for the same key share a single load. A cache fetch or store error is counted (and logged, rate
// limited) but never fails the call: it degrades to a load from object storage.
//
// Each caller returns on its own context cancellation. The shared load runs on a context detached from any
// single caller (values preserved, cancellation dropped, bounded by loadTimeout), so one caller giving up
// neither aborts the load nor fails the others waiting on it.
func (c *Cache) GetMetadata(ctx context.Context, key string, load func(context.Context) ([]byte, error)) ([]byte, error) {
	k := keyPrefix + key

	_, bufs, _, err := c.cache.Fetch(ctx, []string{k})
	switch {
	case err != nil:
		c.errors.WithLabelValues("fetch").Inc()
		c.fetchErrLog.Do(func() {
			level.Warn(c.logger).Log("msg", "metadata cache fetch failed", "key", key, "err", err)
		})
	case len(bufs) == 1:
		c.hits.Inc()
		return bufs[0], nil
	default:
		c.misses.Inc()
	}

	ch := c.sf.DoChan(k, func() (any, error) {
		// Detach from the caller that happened to trigger this load: its cancellation must not abort a
		// load the other waiters still need. WithoutCancel keeps request-scoped values (tracing, the read
		// stats region) so the read is still attributed and traced.
		loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), loadTimeout)
		defer cancel()

		md, err := load(loadCtx)
		if err != nil {
			return nil, err
		}
		if err := c.cache.Store(loadCtx, []string{k}, [][]byte{md}); err != nil {
			c.errors.WithLabelValues("store").Inc()
			c.storeErrLog.Do(func() {
				level.Warn(c.logger).Log("msg", "metadata cache store failed", "key", key, "err", err)
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

// Stop releases the underlying cache.
func (c *Cache) Stop() { c.cache.Stop() }
