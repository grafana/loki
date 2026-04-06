package frontend

import (
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
		cache := newAcceptedStreamsCache(time.Minute, 15*time.Second, prometheus.NewRegistry())
		onMiss := &mockLimitsClient{
			t: t,
			// Expect two stream 0x1 and 0x2 from the tenant "test".
			expectedExceedsLimitsRequest: &proto.ExceedsLimitsRequest{
				Tenant:  "test",
				Streams: []*proto.StreamMetadata{{StreamHash: 0x1}, {StreamHash: 0x2}},
			},
			// Reject stream 0x1.
			exceedsLimitsResponse: &proto.ExceedsLimitsResponse{
				Results: []*proto.ExceedsLimitsResult{{
					StreamHash: 0x1,
					Reason:     uint32(limits.ReasonMaxStreams),
				}},
			},
		}
		client := newCacheLimitsClient(cache, onMiss)
		resp, err := client.ExceedsLimits(t.Context(), &proto.ExceedsLimitsRequest{
			Tenant:  "test",
			Streams: []*proto.StreamMetadata{{StreamHash: 0x1}, {StreamHash: 0x2}},
		})
		// The stream 0x1 should have been rejected, and should be absent
		// from the cache.
		require.NoError(t, err)
		require.Len(t, resp.Results, 1)
		require.Equal(t, uint64(0x1), resp.Results[0].StreamHash)
		require.Len(t, cache.entries, 1)
		tenantEntries, ok := cache.entries["test"]
		require.True(t, ok)
		_, ok = tenantEntries[0x1]
		require.False(t, ok)
		// The cache should contain the stream 0x2 for the tenant "test".
		_, ok = tenantEntries[0x2]
		require.True(t, ok)
	})

	t.Run("cached streams are returned", func(t *testing.T) {
		cache := newAcceptedStreamsCache(time.Minute, 15*time.Second, prometheus.NewRegistry())
		cache.Update("test", []*proto.StreamMetadata{{StreamHash: 0x1}})
		onMiss := &mockLimitsClient{
			t: t,
			// Expect one stream 0x2 because 0x1 is cached.
			expectedExceedsLimitsRequest: &proto.ExceedsLimitsRequest{
				Tenant:  "test",
				Streams: []*proto.StreamMetadata{{StreamHash: 0x2}},
			},
			exceedsLimitsResponse: &proto.ExceedsLimitsResponse{},
		}
		client := newCacheLimitsClient(cache, onMiss)
		resp, err := client.ExceedsLimits(t.Context(), &proto.ExceedsLimitsRequest{
			Tenant:  "test",
			Streams: []*proto.StreamMetadata{{StreamHash: 0x1}, {StreamHash: 0x2}},
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Len(t, resp.Results, 0)
	})
}

func TestAcceptedStreamsCache(t *testing.T) {
	t.Run("cache is cleared after TTL elapsed", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			c := newAcceptedStreamsCache(time.Minute, 15*time.Second, prometheus.NewRegistry())
			// Remove jitter for tests.
			c.lastExpired = time.Now()

			now := time.Now()
			require.Equal(t, now, c.lastExpired)
			require.Len(t, c.entries, 0)
			require.Equal(t, 0, c.entriesSize)

			// Advance the clock, no reset should happen.
			time.Sleep(time.Second)
			c.ExpireTTL()
			require.Equal(t, now, c.lastExpired)

			// Add some data to the cache.
			tenantEntries := make(map[uint64]struct{})
			tenantEntries[1] = struct{}{}
			c.entries["test"] = tenantEntries
			c.entriesSize = 1

			// Advance the clock past the TTL (include the jitter).
			time.Sleep(time.Minute + (5 * time.Second))
			now = time.Now()
			c.ExpireTTL()
			require.Equal(t, now, c.lastExpired)
			require.Len(t, c.entries, 0)
			require.Equal(t, 0, c.entriesSize)
		})
	})

	t.Run("request is filtered in place, accepted streams are removed", func(t *testing.T) {
		c := newAcceptedStreamsCache(time.Minute, 15*time.Second, prometheus.NewRegistry())
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
		c := newAcceptedStreamsCache(time.Minute, 15*time.Second, prometheus.NewRegistry())
		// Create a stream, add it to the cache, and then check that it is
		// present.
		s1 := &proto.StreamMetadata{StreamHash: 0x1}
		c.Update("test", []*proto.StreamMetadata{s1})
		require.Len(t, c.entries, 1)
		tenantEntries, ok := c.entries["test"]
		require.True(t, ok)
		_, ok = tenantEntries[s1.StreamHash]
		require.True(t, ok)
		// Create a second stream without adding it to the cache, and then
		// check that it is absent.
		s2 := &proto.StreamMetadata{StreamHash: 0x2}
		_, ok = tenantEntries[s2.StreamHash]
		require.False(t, ok)
		// Add the second stream to the cache, and then check both streams
		// are present.
		c.Update("test", []*proto.StreamMetadata{s2})
		_, ok = tenantEntries[s2.StreamHash]
		require.True(t, ok)
		_, ok = tenantEntries[s1.StreamHash]
		require.True(t, ok)
	})
}
