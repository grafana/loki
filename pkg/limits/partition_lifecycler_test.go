package limits

import (
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/distributor/shardstreams"
	"github.com/grafana/loki/v3/pkg/limits/proto"
)

func TestPartitionLifecycler_RevokeEvictsBothStores(t *testing.T) {
	const numPartitions = 2
	limits := &mockLimits{ShardStreamsConfig: shardstreams.Config{Enabled: true}}
	require.NoError(t, limits.ShardStreamsConfig.DesiredRate.Set("1KB"))

	reg := prometheus.NewRegistry()
	partitionManager, err := newPartitionManager(reg)
	require.NoError(t, err)
	usage, err := newUsageStore(time.Hour, time.Minute, 10*time.Second, numPartitions, limits, reg)
	require.NoError(t, err)
	streamShards, err := newStreamShardStore(time.Hour, time.Minute, 10*time.Second, numPartitions, "zone1", limits, reg)
	require.NoError(t, err)

	l := newPartitionLifecycler(partitionManager, nil, usage, streamShards, time.Hour, log.NewNopLogger())

	// Stream 0x2 hashes to partition 0, stream 0x1 to partition 1.
	now := time.Now()
	for _, streamHash := range []uint64{0x1, 0x2} {
		metadata := &proto.StreamMetadata{StreamHash: streamHash, TotalSize: 100}
		require.NoError(t, usage.Update("test", metadata, now))
		streamShards.checkAndShard(t.Context(), "test", []*proto.StreamMetadata{metadata}, now)
	}
	require.Equal(t, 2, countTrackedStreams(streamShards))

	// The shard state of a revoked partition has to go the same way as its
	// usage state: keeping it would hold shard budget for streams this
	// instance no longer decides for.
	l.Revoke(t.Context(), nil, map[string][]int32{"topic": {0}})

	require.Equal(t, 1, countTrackedStreams(streamShards))
	streamShards.withLock("test", func(i int) {
		require.NotContains(t, streamShards.stripes[i]["test"], int32(0))
		require.Contains(t, streamShards.stripes[i]["test"], int32(1))
	})
	usage.withRLock("test", func(i int) {
		require.NotContains(t, usage.stripes[i]["test"], int32(0))
		require.Contains(t, usage.stripes[i]["test"], int32(1))
	})
}

func TestPartitionLifecycler_RevokeWithoutStreamShardStore(t *testing.T) {
	reg := prometheus.NewRegistry()
	partitionManager, err := newPartitionManager(reg)
	require.NoError(t, err)
	usage, err := newUsageStore(time.Hour, time.Minute, 10*time.Second, 1, &mockLimits{}, reg)
	require.NoError(t, err)

	l := newPartitionLifecycler(partitionManager, nil, usage, nil, time.Hour, log.NewNopLogger())
	l.Revoke(t.Context(), nil, map[string][]int32{"topic": {0}})
}
