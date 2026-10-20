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
	t.Run("pushes within ttl are served from the cache and forwarded combined once stale", func(t *testing.T) {
		tests := []struct {
			name              string
			cached            *proto.StreamShardResult
			expectedForwarded uint64
		}{{
			name:              "accepted stream",
			cached:            &proto.StreamShardResult{StreamHash: 0x1, Shards: 3},
			expectedForwarded: 12,
		}, {
			name:              "rejected stream, whose pushes are not ingested",
			cached:            &proto.StreamShardResult{StreamHash: 0x1, RejectReason: "max_streams"},
			expectedForwarded: 7,
		}}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					onMiss := &mockLimitsClient{
						t:                                  t,
						expectedCheckLimitsAndShardRequest: newShardRequest(0x1, 10),
						checkLimitsAndShardResponse: &proto.CheckLimitsAndShardResponse{
							Results: []*proto.StreamShardResult{test.cached},
						},
					}
					c := newShardCacheLimitsClient(time.Minute, onMiss, prometheus.NewRegistry())
					_, err := c.CheckLimitsAndShard(t.Context(), newShardRequest(0x1, 10))
					require.NoError(t, err)

					resp, err := c.CheckLimitsAndShard(t.Context(), newShardRequest(0x1, 5))
					require.NoError(t, err)
					require.Equal(t, 1, onMiss.checkLimitsAndShardCalls)
					require.Equal(t, []*proto.StreamShardResult{test.cached}, resp.Results)

					time.Sleep(time.Minute)

					onMiss.expectedCheckLimitsAndShardRequest = newShardRequest(0x1, test.expectedForwarded)
					_, err = c.CheckLimitsAndShard(t.Context(), newShardRequest(0x1, 7))
					require.NoError(t, err)
					require.Equal(t, 2, onMiss.checkLimitsAndShardCalls)
				})
			})
		}
	})

	t.Run("backend error keeps the cached results and forwards the failed pushes again", func(t *testing.T) {
		cached := &proto.StreamShardResult{StreamHash: 0x1, Shards: 4}
		onMiss := &mockLimitsClient{
			t: t,
			checkLimitsAndShardResponse: &proto.CheckLimitsAndShardResponse{
				Results: []*proto.StreamShardResult{cached},
			},
		}
		c := newShardCacheLimitsClient(time.Minute, onMiss, prometheus.NewRegistry())
		_, err := c.CheckLimitsAndShard(t.Context(), newShardRequest(0x1, 10))
		require.NoError(t, err)

		onMiss.checkLimitsAndShardResponse = nil
		onMiss.err = errors.New("backend unavailable")
		onMiss.expectedCheckLimitsAndShardRequest = newShardRequest(0x2, 20)
		resp, err := c.CheckLimitsAndShard(t.Context(), &proto.CheckLimitsAndShardRequest{
			Tenant:  "test",
			Streams: []*proto.StreamMetadata{{StreamHash: 0x1, TotalSize: 5}, {StreamHash: 0x2, TotalSize: 20}},
		})
		require.Error(t, err)
		require.Equal(t, []*proto.StreamShardResult{cached}, resp.Results)

		onMiss.err = nil
		onMiss.expectedCheckLimitsAndShardRequest = newShardRequest(0x2, 25)
		onMiss.checkLimitsAndShardResponse = &proto.CheckLimitsAndShardResponse{
			Results: []*proto.StreamShardResult{{StreamHash: 0x2, Shards: 1}},
		}
		_, err = c.CheckLimitsAndShard(t.Context(), newShardRequest(0x2, 5))
		require.NoError(t, err)
		require.Equal(t, 3, onMiss.checkLimitsAndShardCalls)
	})

	t.Run("a stream without an answer stops serving its previous result", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			onMiss := newGatedMockLimitsClient()
			c := newShardCacheLimitsClient(time.Minute, onMiss, prometheus.NewRegistry())

			warm := checkAsync(t.Context(), c, newShardRequest(0x1, 10))
			<-onMiss.calls
			onMiss.release(10, &proto.StreamShardResult{StreamHash: 0x1, Shards: 3})
			require.NoError(t, (<-warm).err)

			time.Sleep(time.Minute)

			unanswered := checkAsync(t.Context(), c, newShardRequest(0x1, 20))
			<-onMiss.calls
			onMiss.release(20, nil)
			require.Empty(t, (<-unanswered).resp.Results)

			// The previous result could only be served again to a push that
			// arrives while the retry is in flight.
			retry := checkAsync(t.Context(), c, newShardRequest(0x1, 5))
			require.Equal(t, newShardRequest(0x1, 25), <-onMiss.calls)
			concurrent := checkAsync(t.Context(), c, newShardRequest(0x1, 1))
			synctest.Wait()
			require.Len(t, onMiss.calls, 1)
			<-onMiss.calls

			onMiss.release(25, &proto.StreamShardResult{StreamHash: 0x1, Shards: 4})
			onMiss.release(1, &proto.StreamShardResult{StreamHash: 0x1, Shards: 4})
			require.NoError(t, (<-retry).err)
			require.NoError(t, (<-concurrent).err)
		})
	})

	t.Run("pushes during a refresh get the previous result without another backend call", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			onMiss := newGatedMockLimitsClient()
			c := newShardCacheLimitsClient(time.Minute, onMiss, prometheus.NewRegistry())

			warm := checkAsync(t.Context(), c, newShardRequest(0x1, 10))
			<-onMiss.calls
			onMiss.release(10, &proto.StreamShardResult{StreamHash: 0x1, Shards: 2})
			require.NoError(t, (<-warm).err)

			time.Sleep(time.Minute)

			refresh := checkAsync(t.Context(), c, newShardRequest(0x1, 20))
			<-onMiss.calls

			resp, err := c.CheckLimitsAndShard(t.Context(), newShardRequest(0x1, 5))
			require.NoError(t, err)
			require.Equal(t, uint32(2), resp.Results[0].Shards)
			require.Empty(t, onMiss.calls)

			onMiss.release(20, &proto.StreamShardResult{StreamHash: 0x1, Shards: 4})
			got := <-refresh
			require.NoError(t, got.err)
			require.Equal(t, uint32(4), got.resp.Results[0].Shards)

			entry := c.entries[shardCacheKey{"test", 0x1}]
			require.Equal(t, uint32(4), entry.result.Shards)
			require.Equal(t, uint64(5), entry.accumSize)
			require.Equal(t, uint32(1), entry.accumPushes)
			require.False(t, entry.inflight)
		})
	})

	t.Run("a duplicate stream hash in one request is forwarded once per occurrence", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			newRequest := func() *proto.CheckLimitsAndShardRequest {
				return &proto.CheckLimitsAndShardRequest{
					Tenant: "test",
					Streams: []*proto.StreamMetadata{
						{StreamHash: 0x1, TotalSize: 10},
						{StreamHash: 0x1, TotalSize: 20},
					},
				}
			}
			onMiss := &mockLimitsClient{
				t:                                  t,
				expectedCheckLimitsAndShardRequest: newRequest(),
				checkLimitsAndShardResponse: &proto.CheckLimitsAndShardResponse{
					Results: []*proto.StreamShardResult{
						{StreamHash: 0x1, Shards: 1},
						{StreamHash: 0x1, Shards: 1},
					},
				},
			}
			c := newShardCacheLimitsClient(time.Minute, onMiss, prometheus.NewRegistry())
			resp, err := c.CheckLimitsAndShard(t.Context(), newRequest())
			require.NoError(t, err)
			require.Equal(t, 1, onMiss.checkLimitsAndShardCalls)
			require.Len(t, resp.Results, 2)
		})
	})
}

func newShardRequest(hash, size uint64) *proto.CheckLimitsAndShardRequest {
	return &proto.CheckLimitsAndShardRequest{
		Tenant:  "test",
		Streams: []*proto.StreamMetadata{{StreamHash: hash, TotalSize: size}},
	}
}

type checkResult struct {
	resp *proto.CheckLimitsAndShardResponse
	err  error
}

// checkAsync calls CheckLimitsAndShard in a new goroutine, so that a test can
// assert its outcome from the test goroutine.
func checkAsync(ctx context.Context, c limitsClient, req *proto.CheckLimitsAndShardRequest) <-chan checkResult {
	ch := make(chan checkResult, 1)
	go func() {
		resp, err := c.CheckLimitsAndShard(ctx, req)
		ch <- checkResult{resp: resp, err: err}
	}()
	return ch
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
	resp := &proto.CheckLimitsAndShardResponse{}
	if result := <-gate; result != nil {
		resp.Results = append(resp.Results, result)
	}
	return resp, nil
}

// release unblocks the call whose first stream had the given size. A nil
// result leaves the call without an answer.
func (m *gatedMockLimitsClient) release(size uint64, result *proto.StreamShardResult) {
	m.mu.Lock()
	gate := m.gates[size]
	delete(m.gates, size)
	m.mu.Unlock()
	gate <- result
}
