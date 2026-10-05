package frontend

import (
	"context"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/limits/proto"
)

// shardCacheMaxIdleFactor bounds the cache's size: an entry untouched for
// longer than ttl*shardCacheMaxIdleFactor is swept.
const shardCacheMaxIdleFactor = 10

type shardCacheKey struct {
	tenant string
	hash   uint64
}

type shardCacheEntry struct {
	result      *proto.StreamShardResult
	accumSize   uint64
	accumPushes uint32
	cachedAt    time.Time
}

// shardCacheLimitsClient caches CheckLimitsAndShard results per stream.
// Pushes arriving while a cached result is within ttl are answered from the
// cache and their size and count are accumulated; once the entry goes stale
// the accumulated pushes are combined with the triggering push into a single
// backend request.
type shardCacheLimitsClient struct {
	ttl    time.Duration
	onMiss limitsClient

	mtx       sync.Mutex
	entries   map[shardCacheKey]*shardCacheEntry
	lastSwept time.Time

	combinedPushes prometheus.Histogram
}

func newShardCacheLimitsClient(ttl time.Duration, onMiss limitsClient, reg prometheus.Registerer) *shardCacheLimitsClient {
	return &shardCacheLimitsClient{
		ttl:     ttl,
		onMiss:  onMiss,
		entries: make(map[shardCacheKey]*shardCacheEntry),
		combinedPushes: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:    "loki_ingest_limits_frontend_shard_cache_combined_pushes",
			Help:    "The number of pushes combined into a single CheckLimitsAndShard backend request.",
			Buckets: prometheus.ExponentialBuckets(1, 2, 10),
		}),
	}
}

// ExceedsLimits implements the [limitsClient] interface.
func (c *shardCacheLimitsClient) ExceedsLimits(ctx context.Context, req *proto.ExceedsLimitsRequest) (*proto.ExceedsLimitsResponse, error) {
	return c.onMiss.ExceedsLimits(ctx, req)
}

// CheckLimitsAndShard implements the [limitsClient] interface.
func (c *shardCacheLimitsClient) CheckLimitsAndShard(ctx context.Context, req *proto.CheckLimitsAndShardRequest) (*proto.CheckLimitsAndShardResponse, error) {
	now := time.Now()
	results := make([]*proto.StreamShardResult, 0, len(req.Streams))
	forward := make([]*proto.StreamMetadata, 0, len(req.Streams))

	c.mtx.Lock()
	c.sweep(now)
	for _, m := range req.Streams {
		key := shardCacheKey{req.Tenant, m.StreamHash}
		entry, cached := c.entries[key]
		if cached && entry.result != nil && now.Sub(entry.cachedAt) < c.ttl {
			entry.accumSize += m.TotalSize
			entry.accumPushes++
			results = append(results, entry.result)
			continue
		}

		size, pushes := m.TotalSize, uint32(1)
		if cached {
			size += entry.accumSize
			pushes += entry.accumPushes
		}
		c.entries[key] = &shardCacheEntry{cachedAt: now}
		c.combinedPushes.Observe(float64(pushes))
		forward = append(forward, &proto.StreamMetadata{
			StreamHash:      m.StreamHash,
			TotalSize:       size,
			IngestionPolicy: m.IngestionPolicy,
		})
	}
	c.mtx.Unlock()

	if len(forward) == 0 {
		return &proto.CheckLimitsAndShardResponse{Results: results}, nil
	}

	resp, err := c.onMiss.CheckLimitsAndShard(ctx, &proto.CheckLimitsAndShardRequest{
		Tenant:  req.Tenant,
		Streams: forward,
	})
	if err != nil {
		return nil, err
	}

	c.mtx.Lock()
	for _, res := range resp.Results {
		c.entries[shardCacheKey{req.Tenant, res.StreamHash}] = &shardCacheEntry{result: res, cachedAt: now}
	}
	c.mtx.Unlock()

	return &proto.CheckLimitsAndShardResponse{Results: append(results, resp.Results...)}, nil
}

// sweep must be called with mtx held.
func (c *shardCacheLimitsClient) sweep(now time.Time) {
	maxAge := c.ttl * shardCacheMaxIdleFactor
	if now.Sub(c.lastSwept) < maxAge {
		return
	}
	c.lastSwept = now
	cutoff := now.Add(-maxAge)
	for k, e := range c.entries {
		if e.cachedAt.Before(cutoff) {
			delete(c.entries, k)
		}
	}
}
