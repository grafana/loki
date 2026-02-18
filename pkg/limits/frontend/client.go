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

// A cacheLimitsClient uses a cache to reduce the load on limits backends.
type cacheLimitsClient struct {
	ttl    time.Duration
	onMiss limitsClient

	// The fields below MUST NOT be used without mtx.
	mtx                  sync.RWMutex
	acceptedStreamsCache *bloom.BloomFilter
	lastExpired          time.Time

	// Metrics.
	acceptedStreamsCacheSize         prometheus.Gauge
	acceptedStreamsCacheSizeEstimate prometheus.Gauge
}

// newCacheLimitsClient returns a new cache limits client.
func newCacheLimitsClient(
	ttl, maxJitter time.Duration,
	acceptedStreamsCache *bloom.BloomFilter,
	onMiss limitsClient,
	r prometheus.Registerer,
) *cacheLimitsClient {
	c := &cacheLimitsClient{
		ttl:                  ttl,
		acceptedStreamsCache: acceptedStreamsCache,
		lastExpired:          time.Now().Add(randDuration(maxJitter)),
		onMiss:               onMiss,
		acceptedStreamsCacheSize: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "loki_ingest_limits_frontend_accepted_streams_cache_size",
			Help: "Max size of the accepted streams cache.",
		}),
		acceptedStreamsCacheSizeEstimate: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "loki_ingest_limits_frontend_accepted_streams_cache_size_estimate",
			Help: "Estimate size of the accepted streams cache.",
		}),
	}
	c.acceptedStreamsCacheSize.Set(float64(acceptedStreamsCache.Cap()))
	return c
}

// ExceedsLimits implements the [limitsClient] interface.
func (c *cacheLimitsClient) ExceedsLimits(ctx context.Context, req *proto.ExceedsLimitsRequest) ([]*proto.ExceedsLimitsResponse, error) {
	c.expireTTL()
	// If the exact same request has been seen before, and all streams were
	// accepted, we can assume it will continue to be accepted.
	if c.hasAcceptedStreams(req) {
		return []*proto.ExceedsLimitsResponse{}, nil
	}
	// Need to check with the limits service.
	resps, err := c.onMiss.ExceedsLimits(ctx, req)
	if err != nil {
		return resps, err
	}
	// We do not cache rejected streams at this time, so rejections must be
	// filtered out before updating the cache.
	rejected := make(map[uint64]struct{})
	for _, resp := range resps {
		for _, res := range resp.Results {
			rejected[res.StreamHash] = struct{}{}
		}
	}
	c.mtx.Lock()
	defer c.mtx.Unlock()
	for _, s := range req.Streams {
		// If the stream was not rejected, add it to the cache.
		if _, ok := rejected[s.StreamHash]; !ok {
			b := bytes.Buffer{}
			encodeStreamToBuf(&b, req.Tenant, s)
			c.acceptedStreamsCache.Add(b.Bytes())
		}
	}
	c.acceptedStreamsCacheSizeEstimate.Set(float64(c.acceptedStreamsCache.ApproximatedSize()))
	return resps, nil
}

// UpdateRates implements the [limitsClient] interface.
func (c *cacheLimitsClient) UpdateRates(ctx context.Context, req *proto.UpdateRatesRequest) ([]*proto.UpdateRatesResponse, error) {
	return c.onMiss.UpdateRates(ctx, req)
}

// expireTTL expires the caches if the TTL has been exceeded.
func (c *cacheLimitsClient) expireTTL() {
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
		c.acceptedStreamsCache.ClearAll()
		c.lastExpired = time.Now()
	}
}

// hasAcceptedStreams returns true if all streams are in the accepted streams cache.
func (c *cacheLimitsClient) hasAcceptedStreams(req *proto.ExceedsLimitsRequest) bool {
	// b is re-used. The data built from it MUST NOT escape this function.
	b := bytes.Buffer{}
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	for _, s := range req.Streams {
		b.Reset()
		encodeStreamToBuf(&b, req.Tenant, s)
		if !c.acceptedStreamsCache.Test(b.Bytes()) {
			return false
		}
	}
	return true
}

// randDuration returns a random duration between [0, d].
func randDuration(d time.Duration) time.Duration {
	return time.Duration(rand.Int63n(d.Nanoseconds()))
}

// encodeStreamToBuf encodes the stream to the buffer.
func encodeStreamToBuf(b *bytes.Buffer, tenant string, s *proto.StreamMetadata) {
	b.Write([]byte(tenant))
	// [bytes.Buffer] never return an error, it will panic instead.
	_ = binary.Write(b, binary.LittleEndian, s.StreamHash)
}
