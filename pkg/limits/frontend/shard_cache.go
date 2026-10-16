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

// pendingShard is resolved once, by whichever call dispatches the backend request
// for a stream's current version.
// Every other call that finds a [shardCacheEntry] with pending set waits until done,
// instead of dispatching a second backend call for the same stream.
type pendingShard struct {
	done   chan struct{}
	result *proto.StreamShardResult
	err    error
}

type shardCacheEntry struct {
	result      *proto.StreamShardResult
	accumSize   uint64
	accumPushes uint32
	cachedAt    time.Time

	// version is increased each time a new backend call is dispatched for this
	// stream. The write-back of the backend call's responsed is only applied while
	// its own version is still the entry's current one, so a call dispatched before
	// a newer one started cannot overwrite the newer call's result after the fact,
	// regardless of which of the two completes first.
	version uint64
	pending *pendingShard
}

// dispatchedShard is one stream the CheckLimitsAndShard call forwards to the backend itself,
// carrying the version and pendingShard recorded for it at dispatch time so
// the write-back can resolve them without re-reading the cache entry, which
// may have moved on to a newer version by then.
type dispatchedShard struct {
	metadata *proto.StreamMetadata
	version  uint64
	pending  *pendingShard
}

// shardCacheLimitsClient caches CheckLimitsAndShard results per stream.
// Pushes arriving while a cached result is within ttl are answered from the
// cache and their size and count are accumulated.
// Once the entry goes stale the accumulated pushes are combined with the triggering
// push into a single backend request.
// Pushes for a stream with no usable cached result, arriving
// while a backend call for it is already in flight, ride along on that call
// instead of each dispatching their own, see [pendingShard].
type shardCacheLimitsClient struct {
	ttl    time.Duration
	onMiss limitsClient // client that performs the downstream request to the backend

	mtx       sync.Mutex
	entries   map[shardCacheKey]*shardCacheEntry
	lastSwept time.Time
	seq       uint64

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
// This is unsed and would always pass-through to the downstream client.
func (c *shardCacheLimitsClient) ExceedsLimits(ctx context.Context, req *proto.ExceedsLimitsRequest) (*proto.ExceedsLimitsResponse, error) {
	return c.onMiss.ExceedsLimits(ctx, req)
}

// CheckLimitsAndShard implements the [limitsClient] interface.
func (c *shardCacheLimitsClient) CheckLimitsAndShard(ctx context.Context, req *proto.CheckLimitsAndShardRequest) (*proto.CheckLimitsAndShardResponse, error) {
	now := time.Now()
	// A stream repeated within req.Streams must still be processed once: the
	// first occurrence would otherwise dispatch and the second would await
	// that same, not-yet-dispatched call's pending, deadlocking this call on
	// itself until ctx runs out.
	streams := coalesceDuplicateHashes(req.Streams)
	results := make([]*proto.StreamShardResult, 0, len(streams))
	dispatched := make([]dispatchedShard, 0, len(streams))
	var awaited []*pendingShard

	c.mtx.Lock()
	c.sweep(now)
	for _, m := range streams {
		key := shardCacheKey{req.Tenant, m.StreamHash}
		entry, cached := c.entries[key]

		if cached && entry.pending != nil {
			// A backend call for this stream is already in flight: ride along
			// on its result instead of dispatching a second one. This push
			// still counts once the entry next goes stale.
			entry.accumSize += m.TotalSize
			entry.accumPushes++
			awaited = append(awaited, entry.pending)
			continue
		}
		if cached && entry.result != nil && now.Sub(entry.cachedAt) < c.ttl {
			// Found a valid cache entry for the stream, accumulate push/bytes and yield sharding result.
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
		c.seq++
		pending := &pendingShard{done: make(chan struct{})}
		c.entries[key] = &shardCacheEntry{cachedAt: now, version: c.seq, pending: pending}
		c.combinedPushes.Observe(float64(pushes))
		dispatched = append(dispatched, dispatchedShard{
			metadata: &proto.StreamMetadata{
				StreamHash:      m.StreamHash,
				TotalSize:       size,
				IngestionPolicy: m.IngestionPolicy,
			},
			version: c.seq,
			pending: pending,
		})
	}
	c.mtx.Unlock()

	// Wait for the calls inflight before dispatching our own,
	// so a slow backend cannot be asked twice for the same stream just
	// because this call also happened to lead on a different one. A failure
	// or cancellation here must not skip dispatching below: the placeholders
	// this call created in the loop above are this call's responsibility to
	// resolve, and any caller riding along on them would otherwise wait on a
	// done channel nobody closes.
	var awaitErr error
	for _, p := range awaited {
		select {
		case <-p.done:
			switch {
			case p.err != nil && awaitErr == nil:
				awaitErr = p.err
			case p.result != nil:
				results = append(results, p.result)
			}
		case <-ctx.Done():
			if awaitErr == nil {
				awaitErr = ctx.Err()
			}
		}
		if awaitErr != nil {
			break
		}
	}

	if len(dispatched) == 0 {
		if awaitErr != nil {
			return nil, awaitErr
		}
		return &proto.CheckLimitsAndShardResponse{Results: results}, nil
	}

	forward := make([]*proto.StreamMetadata, len(dispatched))
	for i, d := range dispatched {
		forward[i] = d.metadata
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
	defer c.mtx.Unlock()
	for _, d := range dispatched {
		key := shardCacheKey{req.Tenant, d.metadata.StreamHash}
		res := byHash[d.metadata.StreamHash]
		// Drop this write if a call dispatched after ours for the same stream
		// has already taken over: that call is strictly more recent and must
		// not be regressed by our older answer just because it finished
		// later. Carry over whatever this stream accumulated while our call
		// was in flight, since those pushes were not part of our request and
		// still need to reach the backend once the entry next goes stale.
		if cur, ok := c.entries[key]; !ok || d.version >= cur.version {
			var accumSize uint64
			var accumPushes uint32
			if ok {
				accumSize, accumPushes = cur.accumSize, cur.accumPushes
			}
			c.entries[key] = &shardCacheEntry{
				result:      res,
				cachedAt:    now,
				version:     d.version,
				accumSize:   accumSize,
				accumPushes: accumPushes,
			}
		}
		d.pending.result = res
		d.pending.err = err
		close(d.pending.done)
	}

	if err != nil {
		return nil, err
	}
	if awaitErr != nil {
		return nil, awaitErr
	}

	return &proto.CheckLimitsAndShardResponse{Results: append(results, resp.Results...)}, nil
}

// coalesceDuplicateHashes merges streams that share a StreamHash into one,
// summing their TotalSize, so a request listing the same stream more than
// once is still processed as a single entry per stream. It returns streams
// unmodified when there is nothing to merge.
func coalesceDuplicateHashes(streams []*proto.StreamMetadata) []*proto.StreamMetadata {
	index := make(map[uint64]int, len(streams))
	out := make([]*proto.StreamMetadata, 0, len(streams))
	dupe := false
	for _, m := range streams {
		if i, ok := index[m.StreamHash]; ok {
			out[i] = &proto.StreamMetadata{
				StreamHash:      m.StreamHash,
				TotalSize:       out[i].TotalSize + m.TotalSize,
				IngestionPolicy: out[i].IngestionPolicy,
			}
			dupe = true
			continue
		}
		index[m.StreamHash] = len(out)
		out = append(out, m)
	}
	if !dupe {
		return streams
	}
	return out
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
		if e.pending == nil && e.cachedAt.Before(cutoff) {
			delete(c.entries, k)
		}
	}
}
