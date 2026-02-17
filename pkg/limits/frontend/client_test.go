package frontend

import (
	"bytes"
	"encoding/binary"
	"testing"
	"testing/synctest"
	"time"

	"github.com/bits-and-blooms/bloom/v3"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/limits"
	"github.com/grafana/loki/v3/pkg/limits/proto"
)

func TestCacheLimitsClient(t *testing.T) {
	t.Run("streams accepted", func(t *testing.T) {
		// When a stream is accepted, it should be inserted into known streams.
		// We will assert this behavior later.
		knownStreams := bloom.NewWithEstimates(10, 0.01)
		onMiss := &mockLimitsClient{
			t: t,
			// Expect one stream 0x1 from the tenant "test".
			expectedExceedsLimitsRequest: &proto.ExceedsLimitsRequest{
				Tenant:  "test",
				Streams: []*proto.StreamMetadata{{StreamHash: 0x1}},
			},
			// All streams accepted.
			exceedsLimitsResponses: []*proto.ExceedsLimitsResponse{},
		}
		client := newCacheLimitsClient(time.Minute, 15*time.Second, knownStreams, onMiss)
		resps, err := client.ExceedsLimits(t.Context(), &proto.ExceedsLimitsRequest{
			Tenant:  "test",
			Streams: []*proto.StreamMetadata{{StreamHash: 0x1}},
		})
		// No streams should be rejected.
		require.NoError(t, err)
		require.Len(t, resps, 0)
		// The cache should contain the stream 0x1 for the tenant "test".
		// We don't use [encodeStreamToBuf] so we can test it.
		b := bytes.Buffer{}
		b.Write([]byte("test"))
		_ = binary.Write(&b, binary.LittleEndian, uint64(1))
		require.True(t, knownStreams.Test(b.Bytes()))
	})

	t.Run("streams rejected", func(t *testing.T) {
		// When a stream is rejected, it should not be cached.
		knownStreams := bloom.NewWithEstimates(10, 0.01)
		onMiss := &mockLimitsClient{
			t: t,
			// Expect one stream 0x1 from the tenant "test".
			expectedExceedsLimitsRequest: &proto.ExceedsLimitsRequest{
				Tenant:  "test",
				Streams: []*proto.StreamMetadata{{StreamHash: 0x1}},
			},
			// The stream should be rejected.
			exceedsLimitsResponses: []*proto.ExceedsLimitsResponse{{
				Results: []*proto.ExceedsLimitsResult{{
					StreamHash: 0x1,
					Reason:     uint32(limits.ReasonMaxStreams),
				}},
			}},
		}
		client := newCacheLimitsClient(time.Minute, 15*time.Second, knownStreams, onMiss)
		resps, err := client.ExceedsLimits(t.Context(), &proto.ExceedsLimitsRequest{
			Tenant:  "test",
			Streams: []*proto.StreamMetadata{{StreamHash: 0x1}},
		})
		require.NoError(t, err)
		require.Len(t, resps, 1)
		// No bits should have been set.
		require.Equal(t, uint(0), knownStreams.BitSet().Count())
	})

	t.Run("cache is expired after TTL", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			knownStreams := bloom.NewWithEstimates(10, 0.01)
			client := newCacheLimitsClient(time.Minute, 15*time.Second, knownStreams, &mockLimitsClient{})
			// Remove jitter for tests.
			client.lastExpired = time.Now()

			now := time.Now()
			require.Equal(t, now, client.lastExpired)
			// No bits should have been set.
			require.Equal(t, uint(0), knownStreams.BitSet().Count())

			// Advance the clock, no reset should happen.
			time.Sleep(time.Second)
			client.expireTTL()
			require.Equal(t, now, client.lastExpired)

			// Add some data to the cache.
			knownStreams.Add([]byte("test"))
			require.Greater(t, knownStreams.BitSet().Count(), uint(0))

			// Advance the clock past the TTL (include the jitter).
			time.Sleep(time.Minute + (5 * time.Second))
			now = time.Now()
			client.expireTTL()
			require.Equal(t, now, client.lastExpired)
			// The bits should have been reset.
			require.Equal(t, uint(0), knownStreams.BitSet().Count())
		})
	})
}
