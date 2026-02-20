package frontend

import (
	"bytes"
	"testing"
	"testing/synctest"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/limits"
	"github.com/grafana/loki/v3/pkg/limits/proto"
)

func TestCacheLimitsClient(t *testing.T) {
	t.Run("accepted streams cached, rejected streams returned", func(t *testing.T) {
		// When a stream is accepted, it should be inserted into the cache.
		// We will assert this behavior later.
		cache := newAcceptedStreamsCache(time.Minute, 15*time.Second, 10, prometheus.NewRegistry())
		onMiss := &mockLimitsClient{
			t: t,
			// Expect two stream 0x1 and 0x2 from the tenant "test".
			expectedExceedsLimitsRequest: &proto.ExceedsLimitsRequest{
				Tenant:  "test",
				Streams: []*proto.StreamMetadata{{StreamHash: 0x1}, {StreamHash: 0x2}},
			},
			// Reject stream 0x1.
			exceedsLimitsResponses: []*proto.ExceedsLimitsResponse{{
				Results: []*proto.ExceedsLimitsResult{{
					StreamHash: 0x1,
					Reason:     uint32(limits.ReasonMaxStreams),
				}},
			}},
		}
		client := newCacheLimitsClient(cache, onMiss)
		resps, err := client.ExceedsLimits(t.Context(), &proto.ExceedsLimitsRequest{
			Tenant:  "test",
			Streams: []*proto.StreamMetadata{{StreamHash: 0x1}, {StreamHash: 0x2}},
		})
		// The stream 0x1 should have been rejected, and should be absent
		// from the cache.
		require.NoError(t, err)
		require.Len(t, resps, 1)
		require.Len(t, resps[0].Results, 1)
		require.Equal(t, uint64(0x1), resps[0].Results[0].StreamHash)
		b := bytes.Buffer{}
		encodeStreamToBuf(&b, "test", &proto.StreamMetadata{StreamHash: 0x1})
		require.False(t, cache.bf.Test(b.Bytes()))
		// The cache should contain the stream 0x2 for the tenant "test".
		b.Reset()
		encodeStreamToBuf(&b, "test", &proto.StreamMetadata{StreamHash: 0x2})
		require.True(t, cache.bf.Test(b.Bytes()))
	})

	t.Run("cached streams are returned", func(t *testing.T) {
		cache := newAcceptedStreamsCache(time.Minute, 15*time.Second, 10, prometheus.NewRegistry())
		cache.Update("test", []*proto.StreamMetadata{{StreamHash: 0x1}})
		onMiss := &mockLimitsClient{
			t: t,
			// Expect one stream 0x2 because 0x1 is cached.
			expectedExceedsLimitsRequest: &proto.ExceedsLimitsRequest{
				Tenant:  "test",
				Streams: []*proto.StreamMetadata{{StreamHash: 0x2}},
			},
			exceedsLimitsResponses: []*proto.ExceedsLimitsResponse{},
		}
		client := newCacheLimitsClient(cache, onMiss)
		resps, err := client.ExceedsLimits(t.Context(), &proto.ExceedsLimitsRequest{
			Tenant:  "test",
			Streams: []*proto.StreamMetadata{{StreamHash: 0x1}, {StreamHash: 0x2}},
		})
		require.NoError(t, err)
		require.Len(t, resps, 0)
	})
}

func TestAcceptedStreamsCache(t *testing.T) {
	t.Run("cache is cleared after TTL elapsed", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			c := newAcceptedStreamsCache(time.Minute, 15*time.Second, 10, prometheus.NewRegistry())
			// Remove jitter for tests.
			c.lastExpired = time.Now()

			now := time.Now()
			require.Equal(t, now, c.lastExpired)
			// No bits should have been set.
			require.Equal(t, uint(0), c.bf.BitSet().Count())

			// Advance the clock, no reset should happen.
			time.Sleep(time.Second)
			c.ExpireTTL()
			require.Equal(t, now, c.lastExpired)

			// Add some data to the cache.
			c.bf.Add([]byte("test"))
			require.Greater(t, c.bf.BitSet().Count(), uint(0))

			// Advance the clock past the TTL (include the jitter).
			time.Sleep(time.Minute + (5 * time.Second))
			now = time.Now()
			c.ExpireTTL()
			require.Equal(t, now, c.lastExpired)
			// The bits should have been reset.
			require.Equal(t, uint(0), c.bf.BitSet().Count())
		})
	})

	t.Run("request is filtered in place, accepted streams are removed", func(t *testing.T) {
		c := newAcceptedStreamsCache(time.Minute, 15*time.Second, 10, prometheus.NewRegistry())
		// Create a stream and add it to the cache.
		s1 := &proto.StreamMetadata{StreamHash: 0x1}
		c.Update("test", []*proto.StreamMetadata{s1})
		// Create a second stream but do not add it to the cache.
		s2 := &proto.StreamMetadata{StreamHash: 0x2}
		req := &proto.ExceedsLimitsRequest{
			Tenant:  "test",
			Streams: []*proto.StreamMetadata{s1, s2},
		}
		c.FilterInPlace(req)
		require.Len(t, req.Streams, 1)
		require.Equal(t, uint64(0x2), req.Streams[0].StreamHash)
	})

	t.Run("cache contains streams", func(t *testing.T) {
		c := newAcceptedStreamsCache(time.Minute, 15*time.Second, 10, prometheus.NewRegistry())
		// Create a stream, add it to the cache, and then check that it is
		// present.
		s1 := &proto.StreamMetadata{StreamHash: 0x1}
		c.Update("test", []*proto.StreamMetadata{s1})
		b := bytes.Buffer{}
		encodeStreamToBuf(&b, "test", s1)
		require.True(t, c.bf.Test(b.Bytes()))
		// Create a second stream without adding it to the cache, and then
		// check that it is absent.
		s2 := &proto.StreamMetadata{StreamHash: 0x2}
		b.Reset()
		encodeStreamToBuf(&b, "test", s2)
		require.False(t, c.bf.Test(b.Bytes()))
		// Add the second stream to the cache, and then check both streams
		// are present.
		c.Update("test", []*proto.StreamMetadata{s2})
		b.Reset()
		encodeStreamToBuf(&b, "test", s1)
		require.True(t, c.bf.Test(b.Bytes()))
		b.Reset()
		encodeStreamToBuf(&b, "test", s2)
		require.True(t, c.bf.Test(b.Bytes()))
	})
}
