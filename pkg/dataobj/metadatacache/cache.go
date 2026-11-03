package metadatacache

import (
	"context"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/singleflight"
	"golang.org/x/time/rate"

	"github.com/grafana/loki/v3/pkg/logql/model/stats"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

// keyPrefix namespaces entries and versions the on-wire format. Bump the version to invalidate old
// entries after a format change.
const keyPrefix = "dataobj-metadata/v1:"

// loadTimeout bounds a shared load. It runs on a context detached from every caller, so without this a
// stuck backend could strand the load and its goroutine after all callers have left.
const loadTimeout = 30 * time.Second

// DefaultMaxItemBytes is the size limit NewCache falls back to when the caller does not know the
// backend's real one.
const DefaultMaxItemBytes = 8 << 20 // 8 MiB

// KindLogs and KindIndex label the counters of a Cache by the kind of object it serves. One backend
// can serve several Caches, and each Cache reports its own hits, misses and errors.
const (
	KindLogs  = "logs"
	KindIndex = "index"
)

// NewBackend builds the cache backend that cfg describes. It also returns the largest region that the
// backend stores, to pass to NewCache.
//
// The backend ignores a repeated Stop, so several Caches can share it. The limit is the memcached max
// item size. Other backends, and a zero value, use DefaultMaxItemBytes.
func NewBackend(cfg cache.Config, reg prometheus.Registerer, logger log.Logger) (cache.Cache, int64, error) {
	c, err := cache.New(cfg, reg, logger, stats.DataObjMetadataCache, constants.Loki)
	if err != nil {
		return nil, 0, err
	}

	maxItemBytes := int64(DefaultMaxItemBytes)
	if cache.IsMemcacheSet(cfg) && cfg.MemcacheClient.MaxItemSize > 0 {
		maxItemBytes = int64(cfg.MemcacheClient.MaxItemSize)
	}
	return cache.StopOnce(c), maxItemBytes, nil
}

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

	hits   prometheus.Counter
	misses prometheus.Counter
	errors *prometheus.CounterVec
}

// NewCache wraps c as a dataobj.MetadataCache for objects of the given kind. The Cache counts its
// hits, misses and errors under kind in m, which several Caches can share.
//
// Several Caches can share one backend c. Stop on each of them calls c.Stop, so c must ignore a
// repeated Stop. The backend that NewBackend returns does. Keys are object paths, so objects of
// different kinds that share a backend must not share a path.
//
// maxItemBytes is the largest item c's backend will actually store. A non-positive value falls back to
// DefaultMaxItemBytes.
func NewCache(c cache.Cache, maxItemBytes int64, kind string, m *Metrics, logger log.Logger) *Cache {
	if logger == nil {
		logger = log.NewNopLogger()
	}
	if maxItemBytes <= 0 {
		maxItemBytes = DefaultMaxItemBytes
	}
	return &Cache{
		cache:        c,
		maxItemBytes: maxItemBytes,
		logger:       logger,
		fetchErrLog:  rate.Sometimes{Interval: 10 * time.Second},
		storeErrLog:  rate.Sometimes{Interval: 10 * time.Second},
		hits:         m.hits.WithLabelValues(kind),
		misses:       m.misses.WithLabelValues(kind),
		errors:       m.errors.MustCurryWith(prometheus.Labels{"kind": kind}),
	}
}

// GetOrLoadMetadataRegion returns the metadata region for key, loading and caching it via load on a miss.
// Concurrent misses for the same key share a single load. A cache fetch or store error is counted (and
// logged, rate limited) but never fails the call: it degrades to a load from object storage.
//
// A loaded region larger than MaxItemBytes is never stored, even if the backend would accept it; load's
// result is still returned to the caller.
//
// Each caller returns on its own context cancellation. The shared load itself runs on a context
// detached from any single caller: it keeps request-scoped values, drops cancellation, and is bounded
// by loadTimeout. One caller giving up neither aborts the load nor fails the others waiting on it.
func (c *Cache) GetOrLoadMetadataRegion(ctx context.Context, key string, load func(context.Context) ([]byte, error)) ([]byte, error) {
	k := keyPrefix + key

	_, bufs, _, err := c.cache.Fetch(ctx, []string{k})
	switch {
	case ctx.Err() != nil:
		// A context canceled / timed out always takes precedence.
		return nil, ctx.Err()
	case err != nil:
		// A hit or a miss is a fact about the key; an error means Fetch could not establish that fact
		// at all, so it must not also count as a miss.
		c.errors.WithLabelValues(operationFetch).Inc()
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

		if int64(len(md)) > c.maxItemBytes {
			c.errors.WithLabelValues(operationStore).Inc()
			c.storeErrLog.Do(func() {
				level.Warn(c.logger).Log("msg", "data object metadata cache store skipped: region exceeds configured max item size",
					"key", key, "region_bytes", len(md), "max_item_bytes", c.maxItemBytes)
			})
			return md, nil
		}

		if err := c.cache.Store(loadCtx, []string{k}, [][]byte{md}); err != nil {
			c.errors.WithLabelValues(operationStore).Inc()
			c.storeErrLog.Do(func() {
				level.Warn(c.logger).Log("msg", "data object metadata cache store failed", "key", key, "err", err)
			})
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

// MaxItemBytes returns the largest item the backend will store: the value configured on NewCache, or
// DefaultMaxItemBytes when that was not positive. The result is always positive.
func (c *Cache) MaxItemBytes() int64 {
	return c.maxItemBytes
}

// Stop releases the underlying cache. Caches that share a backend each call it, so the backend must
// ignore a repeated Stop. See cache.StopOnce.
func (c *Cache) Stop() {
	c.cache.Stop()
}
