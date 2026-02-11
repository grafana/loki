package frontend

import (
	"bytes"
	"context"
	"math/rand"
	"strconv"
	"sync"
	"time"

	"github.com/bits-and-blooms/bloom/v3"

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
	mtx          sync.RWMutex
	knownStreams *bloom.BloomFilter
	lastExpired  time.Time
}

// newCacheLimitsClient returns a new cache limits client.
func newCacheLimitsClient(
	ttl, maxJitter time.Duration,
	knownStreams *bloom.BloomFilter,
	onMiss limitsClient,
) *cacheLimitsClient {
	return &cacheLimitsClient{
		ttl:          ttl,
		knownStreams: knownStreams,
		lastExpired:  time.Now().Add(randDuration(maxJitter)),
		onMiss:       onMiss,
	}
}

// ExceedsLimits implements the [limitsClient] interface.
func (c *cacheLimitsClient) ExceedsLimits(ctx context.Context, req *proto.ExceedsLimitsRequest) ([]*proto.ExceedsLimitsResponse, error) {
	c.expireTTL()
	// If the exact same request has been seen before, and all streams were
	// accepted, we can assume it will continue to be accepted.
	if c.hasKnownStreams(req) {
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
			b.Write([]byte(req.Tenant))
			b.Write([]byte(strconv.FormatUint(s.StreamHash, 10)))
			c.knownStreams.Add(b.Bytes())
		}
	}
	return resps, nil
}

// UpdateRates implements the [limitsClient] interface.
func (c *cacheLimitsClient) UpdateRates(ctx context.Context, req *proto.UpdateRatesRequest) ([]*proto.UpdateRatesResponse, error) {
	return c.onMiss.UpdateRates(ctx, req)
}

// expireTTL expires the caches if the TTL has been exceeded.
func (c *cacheLimitsClient) expireTTL() {
	c.mtx.Lock()
	defer c.mtx.Unlock()
	if time.Since(c.lastExpired) > c.ttl {
		c.knownStreams.ClearAll()
		c.lastExpired = time.Now()
	}
}

// hasKnownStreams returns true if all streams in req are known streams.
func (c *cacheLimitsClient) hasKnownStreams(req *proto.ExceedsLimitsRequest) bool {
	// b is re-used. The data built from it MUST NOT escape this function.
	b := bytes.Buffer{}
	c.mtx.RLock()
	defer c.mtx.RUnlock()
	for _, s := range req.Streams {
		b.Reset()
		b.Write([]byte(req.Tenant))
		b.Write([]byte(strconv.FormatUint(s.StreamHash, 10)))
		if !c.knownStreams.Test(b.Bytes()) {
			return false
		}
	}
	return true
}

// randDuration returns a random duration between [0, d].
func randDuration(d time.Duration) time.Duration {
	return time.Duration(rand.Int63n(d.Nanoseconds()))
}
