package frontend

import (
	"context"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/limits/proto"
)

// shardCacheMaxIdleFactor bounds the cache's size: an entry not refreshed for
// longer than ttl*shardCacheMaxIdleFactor is swept.
const shardCacheMaxIdleFactor = 10

type shardCacheKey struct {
	tenant string
	hash   uint64
}

type shardCacheEntry struct {
	result      *proto.StreamShardResult
	cachedAt    time.Time
	accumSize   uint64
	accumPushes uint32

	// inflight is set while a backend call refreshes the entry. Pushes that
	// arrive meanwhile are answered with the previous result, so that an
	// expired entry is refreshed by a single call.
	inflight bool
}

// shardRefresh is the bookkeeping for one stream of the request forwarded to
// the backend.
type shardRefresh struct {
	entry  *shardCacheEntry
	pushes uint32
}

// shardCacheLimitsClient caches CheckLimitsAndShard results per stream.
// Pushes for a stream whose result is younger than ttl, or whose result is
// being refreshed, are answered from the cache and their size is
// accumulated. The first push after ttl forwards the accumulated size
// together with its own in a single backend request.
//
// Concurrent pushes for a stream without a result, such as a new stream, are
// all forwarded.
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

// ExceedsLimits implements the [limitsClient] interface. It is not cached.
func (c *shardCacheLimitsClient) ExceedsLimits(ctx context.Context, req *proto.ExceedsLimitsRequest) (*proto.ExceedsLimitsResponse, error) {
	return c.onMiss.ExceedsLimits(ctx, req)
}

// CheckLimitsAndShard implements the [limitsClient] interface.
func (c *shardCacheLimitsClient) CheckLimitsAndShard(ctx context.Context, req *proto.CheckLimitsAndShardRequest) (*proto.CheckLimitsAndShardResponse, error) {
	now := time.Now()
	results := make([]*proto.StreamShardResult, 0, len(req.Streams))
	var (
		forward   []*proto.StreamMetadata
		refreshes []shardRefresh
	)

	c.mtx.Lock()
	c.sweep(now)
	for _, m := range req.Streams {
		key := shardCacheKey{req.Tenant, m.StreamHash}
		entry, ok := c.entries[key]
		if !ok {
			entry = &shardCacheEntry{}
			c.entries[key] = entry
		}
		if entry.result != nil && (entry.inflight || now.Sub(entry.cachedAt) < c.ttl) {
			entry.accumSize += m.TotalSize
			entry.accumPushes++
			results = append(results, entry.result)
			continue
		}
		forward = append(forward, &proto.StreamMetadata{
			StreamHash:      m.StreamHash,
			TotalSize:       m.TotalSize + entry.accumSize,
			IngestionPolicy: m.IngestionPolicy,
		})
		refreshes = append(refreshes, shardRefresh{entry: entry, pushes: entry.accumPushes + 1})
		entry.accumSize, entry.accumPushes, entry.inflight = 0, 0, true
	}
	c.mtx.Unlock()

	if len(forward) == 0 {
		return &proto.CheckLimitsAndShardResponse{Results: results}, nil
	}

	resp, err := c.onMiss.CheckLimitsAndShard(ctx, &proto.CheckLimitsAndShardRequest{
		Tenant:  req.Tenant,
		Streams: forward,
	})

	byHash := make(map[uint64]*proto.StreamShardResult, len(resp.GetResults()))
	for _, res := range resp.GetResults() {
		byHash[res.StreamHash] = res
	}

	c.mtx.Lock()
	for i, r := range refreshes {
		r.entry.inflight = false
		if res, ok := byHash[forward[i].StreamHash]; ok {
			r.entry.result = res
			r.entry.cachedAt = now
			c.combinedPushes.Observe(float64(r.pushes))
			continue
		}
		// A stream without an answer fails open, so its previous result,
		// which can be a rejection, is dropped rather than served during a
		// limits outage. Its pushes are forwarded again with the next push.
		r.entry.result = nil
		r.entry.accumSize += forward[i].TotalSize
		r.entry.accumPushes += r.pushes
	}
	c.mtx.Unlock()

	if err != nil {
		return nil, err
	}
	return &proto.CheckLimitsAndShardResponse{Results: append(results, resp.Results...)}, nil
}

// sweep removes the entries not refreshed within ttl*shardCacheMaxIdleFactor,
// except those being refreshed. It must be called with mtx held.
func (c *shardCacheLimitsClient) sweep(now time.Time) {
	maxAge := c.ttl * shardCacheMaxIdleFactor
	if now.Sub(c.lastSwept) < maxAge {
		return
	}
	c.lastSwept = now
	cutoff := now.Add(-maxAge)
	for k, e := range c.entries {
		if !e.inflight && e.cachedAt.Before(cutoff) {
			delete(c.entries, k)
		}
	}
}
