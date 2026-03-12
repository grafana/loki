package distributor

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/limits/proto"
	"github.com/grafana/loki/v3/pkg/logproto"
)

type mockUpdateRatesClient struct {
	mu         sync.Mutex
	calls      int
	requests   []*proto.UpdateRatesRequest
	customRate uint64 // If non-zero, return this rate instead of 1000.
}

func (m *mockUpdateRatesClient) UpdateRatesRaw(_ context.Context, req *proto.UpdateRatesRequest) ([]*proto.UpdateRatesResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	m.requests = append(m.requests, req)

	// Return customRate if set, otherwise 1000.
	rate := m.customRate
	if rate == 0 {
		rate = 1000
	}

	results := make([]*proto.UpdateRatesResult, len(req.Streams))
	for i, stream := range req.Streams {
		results[i] = &proto.UpdateRatesResult{
			StreamHash: stream.StreamHash,
			Rate:       rate,
		}
	}
	return results, nil
}

func TestRateBatcher_Add_AccumulatesStreams(t *testing.T) {
	client := &mockUpdateRatesClient{}
	batcher := newRateBatcher(
		RateBatcherConfig{
			BatchWindow: time.Hour, // Long window so we control when flush happens
		},
		client,
		log.NewNopLogger(),
		prometheus.NewRegistry(),
	)

	// Add some streams.
	streams := []segmentedStream{
		{
			KeyedStream: KeyedStream{
				Stream: logproto.Stream{
					Labels:  `{app="test"}`,
					Entries: []logproto.Entry{{Timestamp: time.Now(), Line: "test"}},
				},
				Policy: "default",
			},
			SegmentationKeyHash: 123,
		},
		{
			KeyedStream: KeyedStream{
				Stream: logproto.Stream{
					Labels:  `{app="test2"}`,
					Entries: []logproto.Entry{{Timestamp: time.Now(), Line: "test2"}},
				},
				Policy: "default",
			},
			SegmentationKeyHash: 456,
		},
	}

	batcher.Add(context.Background(), "tenant1", streams)
	batcher.Add(context.Background(), "tenant1", streams) // Add same streams again - should accumulate size

	// One fallback call from the first Add (unknown hashes).
	// Second Add finds cached rates, so no additional call.
	require.Equal(t, 1, client.calls)

	// Manually flush.
	batcher.flush(context.Background())

	// One more call from flush.
	require.Equal(t, 2, client.calls)
	require.Len(t, client.requests, 2)
	// The flush request (index 1) should have both unique streams.
	require.Equal(t, "tenant1", client.requests[1].Tenant)
	require.Len(t, client.requests[1].Streams, 2) // 2 unique streams
}

func TestRateBatcher_AccumulatesSize(t *testing.T) {
	client := &mockUpdateRatesClient{}
	batcher := newRateBatcher(
		RateBatcherConfig{
			BatchWindow: time.Hour,
		},
		client,
		log.NewNopLogger(),
		prometheus.NewRegistry(),
	)

	// Add stream with some entries.
	stream1 := []segmentedStream{
		{
			KeyedStream: KeyedStream{
				Stream: logproto.Stream{
					Labels:  `{app="test"}`,
					Entries: []logproto.Entry{{Timestamp: time.Now(), Line: "hello"}},
				},
			},
			SegmentationKeyHash: 123,
		},
	}

	// Add stream again with more entries.
	stream2 := []segmentedStream{
		{
			KeyedStream: KeyedStream{
				Stream: logproto.Stream{
					Labels:  `{app="test"}`,
					Entries: []logproto.Entry{{Timestamp: time.Now(), Line: "world!"}},
				},
			},
			SegmentationKeyHash: 123, // Same hash
		},
	}

	batcher.Add(context.Background(), "tenant1", stream1)
	batcher.Add(context.Background(), "tenant1", stream2)

	batcher.flush(context.Background())

	// 1 fallback call from first Add (unknown hash) + 1 flush call = 2 total.
	// Second Add finds cached rate from fallback, so no additional call.
	require.Equal(t, 2, client.calls)

	// The flush request (index 1) should have one stream with accumulated size.
	flushReq := client.requests[1]
	require.Len(t, flushReq.Streams, 1)

	// Size should be accumulated: first Add size + second Add size.
	// The batcher uses stream.Stream.Size() (protobuf size).
	expectedTotal := uint64(stream1[0].Stream.Size()) + uint64(stream2[0].Stream.Size())
	require.Equal(t, expectedTotal, flushReq.Streams[0].TotalSize)
}

func TestRateBatcher_MultipleTenants(t *testing.T) {
	client := &mockUpdateRatesClient{}
	batcher := newRateBatcher(
		RateBatcherConfig{
			BatchWindow: time.Hour,
		},
		client,
		log.NewNopLogger(),
		prometheus.NewRegistry(),
	)

	// Add streams for multiple tenants.
	batcher.Add(context.Background(), "tenant1", []segmentedStream{{SegmentationKeyHash: 123}})
	batcher.Add(context.Background(), "tenant2", []segmentedStream{{SegmentationKeyHash: 456}})

	batcher.flush(context.Background())

	// 2 fallback calls (one per Add for unknown hashes) + 2 flush calls (one per tenant) = 4 total.
	require.Equal(t, 4, client.calls)

	// Find requests by tenant.
	tenantRequests := make(map[string]*proto.UpdateRatesRequest)
	for _, req := range client.requests {
		tenantRequests[req.Tenant] = req
	}

	require.Contains(t, tenantRequests, "tenant1")
	require.Contains(t, tenantRequests, "tenant2")
	require.Len(t, tenantRequests["tenant1"].Streams, 1)
	require.Len(t, tenantRequests["tenant2"].Streams, 1)
}

func TestRateBatcher_EmptyFlush(t *testing.T) {
	client := &mockUpdateRatesClient{}
	batcher := newRateBatcher(
		RateBatcherConfig{
			BatchWindow: time.Hour,
		},
		client,
		log.NewNopLogger(),
		prometheus.NewRegistry(),
	)

	// Flush with nothing pending.
	batcher.flush(context.Background())

	// Should not have made any calls.
	require.Equal(t, 0, client.calls)
}

func TestRateBatcher_ServiceLifecycle(t *testing.T) {
	client := &mockUpdateRatesClient{}
	batcher := newRateBatcher(
		RateBatcherConfig{
			BatchWindow: 50 * time.Millisecond,
		},
		client,
		log.NewNopLogger(),
		prometheus.NewRegistry(),
	)

	// Start the service.
	ctx := context.Background()
	require.NoError(t, services.StartAndAwaitRunning(ctx, batcher))

	// Add some streams.
	batcher.Add(context.Background(), "tenant1", []segmentedStream{{SegmentationKeyHash: 123}})

	// Wait for automatic flush.
	time.Sleep(100 * time.Millisecond)

	// Should have flushed automatically.
	client.mu.Lock()
	calls := client.calls
	client.mu.Unlock()
	require.GreaterOrEqual(t, calls, 1)

	// Stop the service.
	require.NoError(t, services.StopAndAwaitTerminated(ctx, batcher))
}

func TestRateBatcher_StoresRatesFromFlush(t *testing.T) {
	client := &mockUpdateRatesClient{}
	batcher := newRateBatcher(
		RateBatcherConfig{
			BatchWindow: time.Hour,
		},
		client,
		log.NewNopLogger(),
		prometheus.NewRegistry(),
	)

	// Initially, rates are unknown (0).
	rate := batcher.GetRate("tenant1", 123)
	require.Equal(t, uint64(0), rate)

	// Add streams and flush.
	batcher.Add(context.Background(), "tenant1", []segmentedStream{{SegmentationKeyHash: 123}})
	batcher.Add(context.Background(), "tenant1", []segmentedStream{{SegmentationKeyHash: 456}})
	batcher.flush(context.Background())

	// After flush, rates should be stored (mock returns 1000 for all).
	rate = batcher.GetRate("tenant1", 123)
	require.Equal(t, uint64(1000), rate)

	rate = batcher.GetRate("tenant1", 456)
	require.Equal(t, uint64(1000), rate)

	// Unknown stream still returns 0.
	rate = batcher.GetRate("tenant1", 789)
	require.Equal(t, uint64(0), rate)

	// Different tenant returns 0.
	rate = batcher.GetRate("tenant2", 123)
	require.Equal(t, uint64(0), rate)
}

func TestRateBatcher_AddReturnsRates(t *testing.T) {
	client := &mockUpdateRatesClient{}
	batcher := newRateBatcher(
		RateBatcherConfig{
			BatchWindow: time.Hour,
		},
		client,
		log.NewNopLogger(),
		prometheus.NewRegistry(),
	)

	// First Add triggers a fallback lookup since rates are not cached.
	// The mock returns 1000, so all streams get rate=1000.
	rates := batcher.Add(context.Background(), "tenant1", []segmentedStream{
		{SegmentationKeyHash: 123},
		{SegmentationKeyHash: 456},
	})
	require.Equal(t, uint64(1000), rates[123])
	require.Equal(t, uint64(1000), rates[456])

	// Flush to populate rates from the batch path as well.
	batcher.flush(context.Background())

	// Second Add returns cached rates (no fallback needed for known streams).
	rates = batcher.Add(context.Background(), "tenant1", []segmentedStream{
		{SegmentationKeyHash: 123},
		{SegmentationKeyHash: 456},
		{SegmentationKeyHash: 789}, // New stream triggers fallback.
	})
	require.Equal(t, uint64(1000), rates[123])
	require.Equal(t, uint64(1000), rates[456])
	require.Equal(t, uint64(1000), rates[789]) // Fallback returns 1000.
}

func TestRateBatcher_RatesUpdatedOnSubsequentFlush(t *testing.T) {
	// Custom client that returns different rates based on call count.
	client := &mockUpdateRatesClient{}
	batcher := newRateBatcher(
		RateBatcherConfig{
			BatchWindow: time.Hour,
		},
		client,
		log.NewNopLogger(),
		prometheus.NewRegistry(),
	)

	// First Add triggers fallback + flush.
	batcher.Add(context.Background(), "tenant1", []segmentedStream{{SegmentationKeyHash: 123}})
	batcher.flush(context.Background())
	require.Equal(t, uint64(1000), batcher.GetRate("tenant1", 123))

	// Modify mock to return different rate.
	client.mu.Lock()
	client.customRate = 5000
	client.mu.Unlock()

	// Second flush updates the rate.
	batcher.Add(context.Background(), "tenant1", []segmentedStream{{SegmentationKeyHash: 123}})
	batcher.flush(context.Background())
	require.Equal(t, uint64(5000), batcher.GetRate("tenant1", 123))
}

func TestRateBatcher_RatesClearedForInactiveStreams(t *testing.T) {
	client := &mockUpdateRatesClient{}
	batcher := newRateBatcher(
		RateBatcherConfig{
			BatchWindow: time.Hour,
		},
		client,
		log.NewNopLogger(),
		prometheus.NewRegistry(),
	)

	// First flush with streams 123 and 456.
	batcher.Add(context.Background(), "tenant1", []segmentedStream{
		{SegmentationKeyHash: 123},
		{SegmentationKeyHash: 456},
	})
	batcher.flush(context.Background())
	require.Equal(t, uint64(1000), batcher.GetRate("tenant1", 123))
	require.Equal(t, uint64(1000), batcher.GetRate("tenant1", 456))

	// Second flush with only stream 123 (456 became inactive).
	batcher.Add(context.Background(), "tenant1", []segmentedStream{{SegmentationKeyHash: 123}})
	batcher.flush(context.Background())

	// Stream 123 still has a rate.
	require.Equal(t, uint64(1000), batcher.GetRate("tenant1", 123))
	// Stream 456 was cleared by storeRates (not in last batch).
	require.Equal(t, uint64(0), batcher.GetRate("tenant1", 456))
}

func TestRateBatcher_FallbackLookupForUnknownStreams(t *testing.T) {
	client := &mockUpdateRatesClient{customRate: 2000}
	batcher := newRateBatcher(
		RateBatcherConfig{
			BatchWindow: time.Hour,
		},
		client,
		log.NewNopLogger(),
		prometheus.NewRegistry(),
	)

	// First Add: no rates cached, so all streams trigger a fallback lookup.
	rates := batcher.Add(context.Background(), "tenant1", []segmentedStream{
		{SegmentationKeyHash: 100},
		{SegmentationKeyHash: 200},
	})

	// Fallback should have returned the rate from the client.
	require.Equal(t, uint64(2000), rates[100])
	require.Equal(t, uint64(2000), rates[200])

	// The fallback should have cached the rates via mergeRates.
	require.Equal(t, uint64(2000), batcher.GetRate("tenant1", 100))
	require.Equal(t, uint64(2000), batcher.GetRate("tenant1", 200))

	// The client should have been called once for the fallback (not yet flushed).
	client.mu.Lock()
	require.Equal(t, 1, client.calls)
	client.mu.Unlock()

	// Second Add: rates are cached, so no additional fallback call is needed.
	rates = batcher.Add(context.Background(), "tenant1", []segmentedStream{
		{SegmentationKeyHash: 100},
		{SegmentationKeyHash: 200},
	})
	require.Equal(t, uint64(2000), rates[100])
	require.Equal(t, uint64(2000), rates[200])

	// Still only one call (no new fallback).
	client.mu.Lock()
	require.Equal(t, 1, client.calls)
	client.mu.Unlock()
}

func TestRateBatcher_FallbackOnlyForMissingHashes(t *testing.T) {
	client := &mockUpdateRatesClient{customRate: 3000}
	batcher := newRateBatcher(
		RateBatcherConfig{
			BatchWindow: time.Hour,
		},
		client,
		log.NewNopLogger(),
		prometheus.NewRegistry(),
	)

	// Seed the cache with a known rate via Add + flush.
	batcher.Add(context.Background(), "tenant1", []segmentedStream{
		{SegmentationKeyHash: 100},
	})
	batcher.flush(context.Background())

	// Reset call count to track only subsequent fallback calls.
	client.mu.Lock()
	callsBefore := client.calls
	client.mu.Unlock()

	// Add a mix of known (100) and unknown (200) streams.
	rates := batcher.Add(context.Background(), "tenant1", []segmentedStream{
		{SegmentationKeyHash: 100},
		{SegmentationKeyHash: 200},
	})

	// Known stream should use the cached rate.
	require.Equal(t, uint64(3000), rates[100])
	// Unknown stream should have been fetched via fallback.
	require.Equal(t, uint64(3000), rates[200])

	// Only one additional call for the fallback (stream 200 only).
	client.mu.Lock()
	require.Equal(t, callsBefore+1, client.calls)
	// The fallback request should contain only the unknown stream.
	lastReq := client.requests[len(client.requests)-1]
	require.Len(t, lastReq.Streams, 1)
	require.Equal(t, uint64(200), lastReq.Streams[0].StreamHash)
	client.mu.Unlock()
}

func TestRateBatcher_FallbackDeduplicatesHashes(t *testing.T) {
	client := &mockUpdateRatesClient{customRate: 4000}
	batcher := newRateBatcher(
		RateBatcherConfig{
			BatchWindow: time.Hour,
		},
		client,
		log.NewNopLogger(),
		prometheus.NewRegistry(),
	)

	// Add multiple streams sharing the same unknown hash.
	rates := batcher.Add(context.Background(), "tenant1", []segmentedStream{
		{SegmentationKeyHash: 100},
		{SegmentationKeyHash: 100},
		{SegmentationKeyHash: 100},
	})

	require.Equal(t, uint64(4000), rates[100])

	// The fallback request should contain only one entry for the hash.
	client.mu.Lock()
	require.Equal(t, 1, client.calls)
	require.Len(t, client.requests[0].Streams, 1)
	client.mu.Unlock()
}
