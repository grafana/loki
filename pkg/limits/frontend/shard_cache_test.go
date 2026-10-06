package frontend

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/limits/proto"
)

func TestShardCacheLimitsClient(t *testing.T) {
	t.Run("miss goes to backend and is cached", func(t *testing.T) {
		onMiss := &mockLimitsClient{
			t: t,
			expectedCheckLimitsAndShardRequest: &proto.CheckLimitsAndShardRequest{
				Tenant:  "test",
				Streams: []*proto.StreamMetadata{{StreamHash: 0x1, TotalSize: 10}},
			},
			checkLimitsAndShardResponse: &proto.CheckLimitsAndShardResponse{
				Results: []*proto.StreamShardResult{{StreamHash: 0x1, Shards: 1}},
			},
		}
		c := newShardCacheLimitsClient(time.Minute, onMiss, prometheus.NewRegistry())
		resp, err := c.CheckLimitsAndShard(t.Context(), &proto.CheckLimitsAndShardRequest{
			Tenant:  "test",
			Streams: []*proto.StreamMetadata{{StreamHash: 0x1, TotalSize: 10}},
		})
		require.NoError(t, err)
		require.Equal(t, 1, onMiss.checkLimitsAndShardCalls)
		require.Len(t, resp.Results, 1)
		require.Equal(t, uint32(1), resp.Results[0].Shards)
	})

	t.Run("hit within ttl is served from cache and accumulates", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			onMiss := &mockLimitsClient{
				t: t,
				checkLimitsAndShardResponse: &proto.CheckLimitsAndShardResponse{
					Results: []*proto.StreamShardResult{{StreamHash: 0x1, Shards: 2}},
				},
			}
			c := newShardCacheLimitsClient(time.Minute, onMiss, prometheus.NewRegistry())
			_, err := c.CheckLimitsAndShard(t.Context(), &proto.CheckLimitsAndShardRequest{
				Tenant:  "test",
				Streams: []*proto.StreamMetadata{{StreamHash: 0x1, TotalSize: 10}},
			})
			require.NoError(t, err)
			require.Equal(t, 1, onMiss.checkLimitsAndShardCalls)

			resp, err := c.CheckLimitsAndShard(t.Context(), &proto.CheckLimitsAndShardRequest{
				Tenant:  "test",
				Streams: []*proto.StreamMetadata{{StreamHash: 0x1, TotalSize: 5}},
			})
			require.NoError(t, err)
			require.Equal(t, 1, onMiss.checkLimitsAndShardCalls)
			require.Len(t, resp.Results, 1)
			require.Equal(t, uint32(2), resp.Results[0].Shards)

			entry := c.entries[shardCacheKey{"test", 0x1}]
			require.Equal(t, uint64(5), entry.accumSize)
			require.Equal(t, uint32(1), entry.accumPushes)
		})
	})

	t.Run("stale entry combines accumulated pushes into one request", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			onMiss := &mockLimitsClient{
				t: t,
				checkLimitsAndShardResponse: &proto.CheckLimitsAndShardResponse{
					Results: []*proto.StreamShardResult{{StreamHash: 0x1, Shards: 3}},
				},
			}
			c := newShardCacheLimitsClient(time.Minute, onMiss, prometheus.NewRegistry())
			_, err := c.CheckLimitsAndShard(t.Context(), &proto.CheckLimitsAndShardRequest{
				Tenant:  "test",
				Streams: []*proto.StreamMetadata{{StreamHash: 0x1, TotalSize: 10}},
			})
			require.NoError(t, err)

			_, err = c.CheckLimitsAndShard(t.Context(), &proto.CheckLimitsAndShardRequest{
				Tenant:  "test",
				Streams: []*proto.StreamMetadata{{StreamHash: 0x1, TotalSize: 5}},
			})
			require.NoError(t, err)
			require.Equal(t, 1, onMiss.checkLimitsAndShardCalls)

			time.Sleep(time.Minute + time.Second)

			onMiss.expectedCheckLimitsAndShardRequest = &proto.CheckLimitsAndShardRequest{
				Tenant:  "test",
				Streams: []*proto.StreamMetadata{{StreamHash: 0x1, TotalSize: 12}},
			}
			resp, err := c.CheckLimitsAndShard(t.Context(), &proto.CheckLimitsAndShardRequest{
				Tenant:  "test",
				Streams: []*proto.StreamMetadata{{StreamHash: 0x1, TotalSize: 7}},
			})
			require.NoError(t, err)
			require.Equal(t, 2, onMiss.checkLimitsAndShardCalls)
			require.Equal(t, uint32(3), resp.Results[0].Shards)
		})
	})

	t.Run("backend error is not cached", func(t *testing.T) {
		onMiss := &mockLimitsClient{
			t:   t,
			err: errors.New("backend unavailable"),
		}
		c := newShardCacheLimitsClient(time.Minute, onMiss, prometheus.NewRegistry())
		_, err := c.CheckLimitsAndShard(t.Context(), &proto.CheckLimitsAndShardRequest{
			Tenant:  "test",
			Streams: []*proto.StreamMetadata{{StreamHash: 0x1, TotalSize: 10}},
		})
		require.Error(t, err)
		require.Len(t, c.entries, 1)
		require.Nil(t, c.entries[shardCacheKey{"test", 0x1}].result)
	})

	t.Run("a call dispatched earlier does not clobber one dispatched later, however they complete", func(t *testing.T) {
		// Two misses for the same stream dispatch independently (nothing yet
		// stops that), gated so the second dispatches only once the first is
		// confirmed in flight, and so the second completes before the first.
		onMiss := newGatedMockLimitsClient()
		c := newShardCacheLimitsClient(time.Minute, onMiss, prometheus.NewRegistry())

		firstDone := make(chan *proto.CheckLimitsAndShardResponse, 1)
		go func() {
			resp, err := c.CheckLimitsAndShard(t.Context(), &proto.CheckLimitsAndShardRequest{
				Tenant:  "test",
				Streams: []*proto.StreamMetadata{{StreamHash: 0x1, TotalSize: 10}},
			})
			require.NoError(t, err)
			firstDone <- resp
		}()
		// Wait until the first call has dispatched and is blocked in onMiss, so
		// the second call below is guaranteed to see no usable cached result
		// and dispatch a second, independent call rather than finding a hit.
		<-onMiss.calls

		secondDone := make(chan *proto.CheckLimitsAndShardResponse, 1)
		go func() {
			resp, err := c.CheckLimitsAndShard(t.Context(), &proto.CheckLimitsAndShardRequest{
				Tenant:  "test",
				Streams: []*proto.StreamMetadata{{StreamHash: 0x1, TotalSize: 20}},
			})
			require.NoError(t, err)
			secondDone <- resp
		}()
		<-onMiss.calls

		// Let the second, more recently dispatched call complete first.
		onMiss.release(20, &proto.StreamShardResult{StreamHash: 0x1, Shards: 2})
		resp := <-secondDone
		require.Equal(t, uint32(2), resp.Results[0].Shards)

		// The first, earlier call completes after, with a smaller shard count.
		// It must not overwrite the second call's result, which is already
		// cached.
		onMiss.release(10, &proto.StreamShardResult{StreamHash: 0x1, Shards: 1})
		<-firstDone

		require.Equal(t, uint32(2), c.entries[shardCacheKey{"test", 0x1}].result.Shards)
	})
}

// gatedMockLimitsClient blocks every CheckLimitsAndShard call until release
// is called for it, identified by the TotalSize of its first stream, so a
// test can control completion order independently of dispatch order.
type gatedMockLimitsClient struct {
	calls chan *proto.CheckLimitsAndShardRequest

	mu    sync.Mutex
	gates map[uint64]chan *proto.StreamShardResult
}

func newGatedMockLimitsClient() *gatedMockLimitsClient {
	return &gatedMockLimitsClient{
		calls: make(chan *proto.CheckLimitsAndShardRequest, 2),
		gates: make(map[uint64]chan *proto.StreamShardResult),
	}
}

func (m *gatedMockLimitsClient) ExceedsLimits(context.Context, *proto.ExceedsLimitsRequest) (*proto.ExceedsLimitsResponse, error) {
	return nil, nil
}

func (m *gatedMockLimitsClient) CheckLimitsAndShard(_ context.Context, req *proto.CheckLimitsAndShardRequest) (*proto.CheckLimitsAndShardResponse, error) {
	gate := make(chan *proto.StreamShardResult)
	m.mu.Lock()
	m.gates[req.Streams[0].TotalSize] = gate
	m.mu.Unlock()
	m.calls <- req
	result := <-gate
	return &proto.CheckLimitsAndShardResponse{Results: []*proto.StreamShardResult{result}}, nil
}

// release unblocks the call whose first stream had the given size.
func (m *gatedMockLimitsClient) release(size uint64, result *proto.StreamShardResult) {
	m.mu.Lock()
	gate := m.gates[size]
	delete(m.gates, size)
	m.mu.Unlock()
	gate <- result
}
