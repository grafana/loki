package frontend

import (
	"errors"
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
}
