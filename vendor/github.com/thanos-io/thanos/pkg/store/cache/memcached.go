// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package storecache

import (
	"context"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/oklog/ulid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/prometheus/pkg/labels"

	"github.com/thanos-io/thanos/pkg/cacheutil"
)

const (
	memcachedDefaultTTL = 24 * time.Hour
)

// MemcachedIndexCache is a memcached-based index cache.
type MemcachedIndexCache struct {
	logger    log.Logger
	memcached cacheutil.MemcachedClient

	// Metrics.
	requests *prometheus.CounterVec
	hits     *prometheus.CounterVec
}

// NewMemcachedIndexCache makes a new MemcachedIndexCache.
func NewMemcachedIndexCache(logger log.Logger, memcached cacheutil.MemcachedClient, reg prometheus.Registerer) (*MemcachedIndexCache, error) {
	c := &MemcachedIndexCache{
		logger:    logger,
		memcached: memcached,
	}

	c.requests = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Name: "thanos_store_index_cache_requests_total",
		Help: "Total number of items requests to the cache.",
	}, []string{"item_type"})
	c.requests.WithLabelValues(cacheTypePostings)
	c.requests.WithLabelValues(cacheTypeSeries)

	c.hits = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Name: "thanos_store_index_cache_hits_total",
		Help: "Total number of items requests to the cache that were a hit.",
	}, []string{"item_type"})
	c.hits.WithLabelValues(cacheTypePostings)
	c.hits.WithLabelValues(cacheTypeSeries)

	level.Info(logger).Log("msg", "created memcached index cache")

	return c, nil
}

// StorePostings sets the postings identified by the ulid and label to the value v.
// The function enqueues the request and returns immediately: the entry will be
// asynchronously stored in the cache.
func (c *MemcachedIndexCache) StorePostings(ctx context.Context, blockID ulid.ULID, l labels.Label, v []byte) {
	key := cacheKey{blockID, cacheKeyPostings(l)}.string()

	if err := c.memcached.SetAsync(ctx, key, v, memcachedDefaultTTL); err != nil {
		level.Error(c.logger).Log("msg", "failed to cache postings in memcached", "err", err)
	}
}

// FetchMultiPostings fetches multiple postings - each identified by a label -
// and returns a map containing cache hits, along with a list of missing keys.
// In case of error, it logs and return an empty cache hits map.
func (c *MemcachedIndexCache) FetchMultiPostings(ctx context.Context, blockID ulid.ULID, lbls []labels.Label) (hits map[labels.Label][]byte, misses []labels.Label) {
	// Build the cache keys, while keeping a map between input label and the cache key
	// so that we can easily reverse it back after the GetMulti().
	keys := make([]string, 0, len(lbls))
	keysMapping := map[labels.Label]string{}

	for _, lbl := range lbls {
		key := cacheKey{blockID, cacheKeyPostings(lbl)}.string()

		keys = append(keys, key)
		keysMapping[lbl] = key
	}

	// Fetch the keys from memcached in a single request.
	c.requests.WithLabelValues(cacheTypePostings).Add(float64(len(keys)))
	results := c.memcached.GetMulti(ctx, keys)
	if len(results) == 0 {
		return nil, lbls
	}

	// Construct the resulting hits map and list of missing keys. We iterate on the input
	// list of labels to be able to easily create the list of ones in a single iteration.
	hits = map[labels.Label][]byte{}

	for _, lbl := range lbls {
		key, ok := keysMapping[lbl]
		if !ok {
			level.Error(c.logger).Log("msg", "keys mapping inconsistency found in memcached index cache client", "type", "postings", "label", lbl.Name+":"+lbl.Value)
			continue
		}

		// Check if the key has been found in memcached. If not, we add it to the list
		// of missing keys.
		value, ok := results[key]
		if !ok {
			misses = append(misses, lbl)
			continue
		}

		hits[lbl] = value
	}

	c.hits.WithLabelValues(cacheTypePostings).Add(float64(len(hits)))
	return hits, misses
}

// StoreSeries sets the series identified by the ulid and id to the value v.
// The function enqueues the request and returns immediately: the entry will be
// asynchronously stored in the cache.
func (c *MemcachedIndexCache) StoreSeries(ctx context.Context, blockID ulid.ULID, id uint64, v []byte) {
	key := cacheKey{blockID, cacheKeySeries(id)}.string()

	if err := c.memcached.SetAsync(ctx, key, v, memcachedDefaultTTL); err != nil {
		level.Error(c.logger).Log("msg", "failed to cache series in memcached", "err", err)
	}
}

// FetchMultiSeries fetches multiple series - each identified by ID - from the cache
// and returns a map containing cache hits, along with a list of missing IDs.
// In case of error, it logs and return an empty cache hits map.
func (c *MemcachedIndexCache) FetchMultiSeries(ctx context.Context, blockID ulid.ULID, ids []uint64) (hits map[uint64][]byte, misses []uint64) {
	// Build the cache keys, while keeping a map between input id and the cache key
	// so that we can easily reverse it back after the GetMulti().
	keys := make([]string, 0, len(ids))
	keysMapping := map[uint64]string{}

	for _, id := range ids {
		key := cacheKey{blockID, cacheKeySeries(id)}.string()

		keys = append(keys, key)
		keysMapping[id] = key
	}

	// Fetch the keys from memcached in a single request.
	c.requests.WithLabelValues(cacheTypeSeries).Add(float64(len(ids)))
	results := c.memcached.GetMulti(ctx, keys)
	if len(results) == 0 {
		return nil, ids
	}

	// Construct the resulting hits map and list of missing keys. We iterate on the input
	// list of ids to be able to easily create the list of ones in a single iteration.
	hits = map[uint64][]byte{}

	for _, id := range ids {
		key, ok := keysMapping[id]
		if !ok {
			level.Error(c.logger).Log("msg", "keys mapping inconsistency found in memcached index cache client", "type", "series", "id", id)
			continue
		}

		// Check if the key has been found in memcached. If not, we add it to the list
		// of missing keys.
		value, ok := results[key]
		if !ok {
			misses = append(misses, id)
			continue
		}

		hits[id] = value
	}

	c.hits.WithLabelValues(cacheTypeSeries).Add(float64(len(hits)))
	return hits, misses
}
