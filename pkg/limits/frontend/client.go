package frontend

import (
	"bytes"
	"context"
	"encoding/binary"
	"math/rand"
	"sync"
	"time"

	"github.com/bits-and-blooms/bloom/v3"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/v3/pkg/limits/proto"
)

type limitsClient interface {
	// ExceedsLimits checks if the streams in the request have exceeded their
	// per-partition limits.
	ExceedsLimits(context.Context, *proto.ExceedsLimitsRequest) ([]*proto.ExceedsLimitsResponse, error)

	// UpdateRates updates the per-second rates for the streams.
	UpdateRates(context.Context, *proto.UpdateRatesRequest) ([]*proto.UpdateRatesResponse, error)
}

// A cacheLimitsClient uses caches to reduce the load on limits backends.
type cacheLimitsClient struct {
	acceptedStreamsCache *acceptedStreamsCache
	onMiss               limitsClient
}

// newCacheLimitsClient returns a new cache limits client.
func newCacheLimitsClient(acceptedStreamsCache *acceptedStreamsCache, onMiss limitsClient) *cacheLimitsClient {
	return &cacheLimitsClient{
		acceptedStreamsCache: acceptedStreamsCache,
		onMiss:               onMiss,
	}
}

// ExceedsLimits implements the [limitsClient] interface.
func (c *cacheLimitsClient) ExceedsLimits(ctx context.Context, req *proto.ExceedsLimitsRequest) ([]*proto.ExceedsLimitsResponse, error) {
	c.acceptedStreamsCache.ExpireTTL()
	// Remove streams that have been accepted from the request. This means
	// we just check streams we haven't seen before, which reduces the
	// number of requests we need to make to the limits backends.
	c.acceptedStreamsCache.FilterInPlace(req)
	if len(req.Streams) == 0 {
		return []*proto.ExceedsLimitsResponse{}, nil
	}
	// Need to check remaining streams with the limits service.
	resps, err := c.onMiss.ExceedsLimits(ctx, req)
	if err != nil {
		return nil, err
	}
	numRejected := 0
	for _, resp := range resps {
		numRejected += len(resp.Results)
	}
	// Fast path, all streams rejected.
	if numRejected == len(req.Streams) {
		return resps, nil
	}
	// There are some accepted streams we haven't seen before, so add them
	// to the cache. We do not cache rejected streams at this time, so
	// rejections must be filtered out before updating the cache.
	rejected := make(map[uint64]struct{})
	for _, resp := range resps {
		for _, res := range resp.Results {
			rejected[res.StreamHash] = struct{}{}
		}
	}
	accepted := make([]*proto.StreamMetadata, 0, len(req.Streams))
	for _, s := range req.Streams {
		if _, ok := rejected[s.StreamHash]; !ok {
			accepted = append(accepted, s)
		}
	}
	c.acceptedStreamsCache.Update(req.Tenant, accepted)
	return resps, nil
}

// UpdateRates implements the [limitsClient] interface.
func (c *cacheLimitsClient) UpdateRates(ctx context.Context, req *proto.UpdateRatesRequest) ([]*proto.UpdateRatesResponse, error) {
	return c.onMiss.UpdateRates(ctx, req)
}

type acceptedStreamsCache struct {
	ttl time.Duration

	// The fields below MUST NOT be used without mtx.
	mtx         sync.RWMutex
	bf          *bloom.BloomFilter
	lastExpired time.Time

	// Metrics.
	cacheSize         prometheus.Gauge
	cacheSizeEstimate prometheus.GaugeFunc
}

func newAcceptedStreamsCache(ttl, maxJitter time.Duration, cacheSize int, r prometheus.Registerer) *acceptedStreamsCache {
	c := &acceptedStreamsCache{
		ttl:         ttl,
		bf:          bloom.NewWithEstimates(uint(cacheSize), 0.01),
		lastExpired: time.Now().Add(randDuration(maxJitter)),
	}
	c.cacheSize = promauto.With(r).NewGauge(prometheus.GaugeOpts{
		Name: "loki_ingest_limits_frontend_accepted_streams_cache_size",
		Help: "Max size of the accepted streams cache.",
	})
	// This is static.
	c.cacheSize.Set(float64(cacheSize))
	// This is quite an expensive metric to compute so we moved it to a func.
	c.cacheSizeEstimate = promauto.With(r).NewGaugeFunc(prometheus.GaugeOpts{
		Name: "loki_ingest_limits_frontend_accepted_streams_cache_size_estimate",
		Help: "Estimate size of the accepted streams cache.",
	}, func() float64 {
		c.mtx.RLock()
		defer c.mtx.RUnlock()
		return float64(c.bf.ApproximatedSize())
	})
	return c
}

// ExpireTTL expires the caches if the TTL has been exceeded.
func (c *acceptedStreamsCache) ExpireTTL() {
	// Fast path, first check the TTL with a read lock.
	c.mtx.RLock()
	lastExpired := c.lastExpired
	c.mtx.RUnlock()
	if time.Since(lastExpired) <= c.ttl {
		return
	}
	// If we have reached here we need to reset the cache. However, before
	// we can do that we need to check the TTL a second time with an exclusive
	// lock as we could be in a data race.
	c.mtx.Lock()
	defer c.mtx.Unlock()
	if time.Since(c.lastExpired) > c.ttl {
		c.bf.ClearAll()
		c.lastExpired = time.Now()
	}
}

// FilterInPlace removes streams that are present in the cache.
func (c *acceptedStreamsCache) FilterInPlace(req *proto.ExceedsLimitsRequest) {
	// b is re-used. The data built from it MUST NOT escape this function.
	b := bytes.Buffer{}
	// See https://go.dev/wiki/SliceTricks.
	filtered := req.Streams[:0]
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	for _, s := range req.Streams {
		b.Reset()
		encodeStreamToBuf(&b, req.Tenant, s)
		if !c.bf.Test(b.Bytes()) {
			filtered = append(filtered, s)
		}
	}
	req.Streams = filtered
}

func (c *acceptedStreamsCache) Update(tenant string, streams []*proto.StreamMetadata) {
	// b is re-used. The data built from it MUST NOT escape this function.
	b := bytes.Buffer{}
	c.mtx.Lock()
	defer c.mtx.Unlock()
	for _, s := range streams {
		b.Reset()
		encodeStreamToBuf(&b, tenant, s)
		c.bf.Add(b.Bytes())
	}
}

// randDuration returns a random duration between [0, d].
func randDuration(d time.Duration) time.Duration {
	return time.Duration(rand.Int63n(d.Nanoseconds()))
}

// encodeStreamToBuf encodes the stream to the buffer.
func encodeStreamToBuf(b *bytes.Buffer, tenant string, s *proto.StreamMetadata) {
	b.Write([]byte(tenant))
	// [bytes.Buffer] never returns an error, it will panic instead.
	_ = binary.Write(b, binary.LittleEndian, s.StreamHash)
}
