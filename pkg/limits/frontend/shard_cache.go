package frontend

import (
	"context"
	"hash/fnv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/limits/proto"
)

// An entry without a backend request for longer than ttl*shardCacheMaxIdleFactor is swept.
const shardCacheMaxIdleFactor = 10

type shardCacheKey struct {
	tenant string
	hash   uint64
}

type streamShardPushCount struct {
	entry  *shardCacheEntry
	pushes uint32
}

type shardCacheEntry struct {
	result      *proto.StreamShardResult
	accumSize   uint64
	accumPushes uint32

	// refreshedAt is the time of the last backend call for the stream,
	// whether it was answered or not.
	refreshedAt time.Time

	// inflight is set while a backend call refreshes the entry. Pushes that
	// arrive meanwhile are answered with the previous result, so that an
	// expired entry is refreshed by a single call.
	inflight bool
}

type stripeLock struct {
	sync.RWMutex
	_ [40]byte // Padding to avoid multiple locks being on the same cache line.
}

type keyedMutex []stripeLock

func newStripedLock(n int) keyedMutex {
	return make(keyedMutex, n)
}

func (l keyedMutex) getStripe(tenant string) int {
	h := fnv.New32()
	_, _ = h.Write([]byte(tenant))
	return int(h.Sum32() % uint32(len(l)))
}

func (l keyedMutex) getLock(tenant string) *stripeLock {
	i := l.getStripe(tenant)
	return &l[i]
}

// shardCacheLimitsClient caches CheckLimitsAndShard results per stream.
// Pushes for a stream whose result is younger than ttl, or whose result is
// being refreshed, are answered from the cache. If the cached result accepts
// the stream, their size is accumulated. The first push after ttl forwards
// the accumulated size together with its own in a single backend request.
//
// The tradeoff is, that concurrent pushes for a stream without a result, such as
// a new stream, are all forwarded.
type shardCacheLimitsClient struct {
	ttl  time.Duration
	next limitsClient // the client invoked on cache miss

	tenantMtx keyedMutex
	entries   map[shardCacheKey]*shardCacheEntry
	lastSwept map[string]time.Time

	combinedPushes prometheus.Histogram
}

func newShardCacheLimitsClient(ttl time.Duration, next limitsClient, reg prometheus.Registerer) *shardCacheLimitsClient {
	return &shardCacheLimitsClient{
		ttl:       ttl,
		next:      next,
		entries:   make(map[shardCacheKey]*shardCacheEntry),
		tenantMtx: newStripedLock(128),
		lastSwept: make(map[string]time.Time),
		combinedPushes: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:    "loki_ingest_limits_frontend_shard_cache_combined_pushes",
			Help:    "The number of pushes combined into a single CheckLimitsAndShard backend request.",
			Buckets: prometheus.ExponentialBuckets(1, 2, 10),
		}),
	}
}

// ExceedsLimits implements the [limitsClient] interface. It never caches.
func (c *shardCacheLimitsClient) ExceedsLimits(ctx context.Context, req *proto.ExceedsLimitsRequest) (*proto.ExceedsLimitsResponse, error) {
	return c.next.ExceedsLimits(ctx, req)
}

// CheckLimitsAndShard implements the [limitsClient] interface.
func (c *shardCacheLimitsClient) CheckLimitsAndShard(ctx context.Context, req *proto.CheckLimitsAndShardRequest) (*proto.CheckLimitsAndShardResponse, error) {
	now := time.Now()
	results := make([]*proto.StreamShardResult, 0, len(req.Streams))
	var (
		forward   []*proto.StreamMetadata
		refreshes []streamShardPushCount
	)

	mtx := c.tenantMtx.getLock(req.Tenant)
	mtx.Lock()
	c.sweep(now, req.Tenant)
	for _, m := range req.Streams {
		key := shardCacheKey{req.Tenant, m.StreamHash}
		entry, ok := c.entries[key]
		if !ok {
			entry = &shardCacheEntry{}
			c.entries[key] = entry
		}
		if entry.result != nil && (entry.inflight || now.Sub(entry.refreshedAt) < c.ttl) {
			// The limits service does not record the size of a push it
			// rejects, so a push answered with a cached rejection must not
			// reach it with the next refresh either.
			if entry.result.RejectReason == "" {
				entry.accumSize += m.TotalSize
				entry.accumPushes++
			}
			results = append(results, entry.result)
			continue
		}
		forward = append(forward, &proto.StreamMetadata{
			StreamHash:      m.StreamHash,
			TotalSize:       m.TotalSize + entry.accumSize,
			IngestionPolicy: m.IngestionPolicy,
		})
		refreshes = append(refreshes, streamShardPushCount{entry: entry, pushes: entry.accumPushes + 1})
		entry.accumSize, entry.accumPushes, entry.inflight = 0, 0, true
	}
	mtx.Unlock()

	if len(forward) == 0 {
		return &proto.CheckLimitsAndShardResponse{Results: results}, nil
	}

	resp, err := c.next.CheckLimitsAndShard(ctx, &proto.CheckLimitsAndShardRequest{
		Tenant:  req.Tenant,
		Streams: forward,
	})

	resultsByHash := make(map[uint64]*proto.StreamShardResult, len(resp.GetResults()))
	for _, res := range resp.GetResults() {
		resultsByHash[res.StreamHash] = res
	}

	mtx.Lock()
	for i, r := range refreshes {
		r.entry.inflight = false
		r.entry.refreshedAt = now
		if res, ok := resultsByHash[forward[i].StreamHash]; ok {
			r.entry.result = res
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
	mtx.Unlock()

	if err != nil {
		return &proto.CheckLimitsAndShardResponse{Results: results}, err
	}
	return &proto.CheckLimitsAndShardResponse{Results: append(results, resp.Results...)}, nil
}

// sweep removes the entries without a backend call within
// ttl*shardCacheMaxIdleFactor, except those being refreshed. The size an
// idle stream accumulated since its last backend call is dropped with it.
// It must be called with mtx held.
func (c *shardCacheLimitsClient) sweep(now time.Time, tenant string) {
	maxAge := c.ttl * shardCacheMaxIdleFactor
	lastSwept, _ := c.lastSwept[tenant]
	if now.Sub(lastSwept) < maxAge {
		return
	}
	c.lastSwept[tenant] = now
	cutoff := now.Add(-maxAge)
	for k, e := range c.entries {
		if k.tenant == tenant && !e.inflight && e.refreshedAt.Before(cutoff) {
			delete(c.entries, k)
		}
	}
}
