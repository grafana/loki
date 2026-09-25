package limits

import (
	"context"
	"testing"
	"time"

	"github.com/coder/quartz"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/distributor/shardstreams"
	"github.com/grafana/loki/v3/pkg/limits/proto"
)

func newTestService(t *testing.T, limits Limits, numPartitions int) (*Service, *quartz.Mock) {
	t.Helper()
	const activeWindow = time.Hour
	reg := prometheus.NewRegistry()
	partitionManager, err := newPartitionManager(reg)
	require.NoError(t, err)
	usage, err := newUsageStore(activeWindow, time.Minute, 10*time.Second, numPartitions, limits, reg)
	require.NoError(t, err)
	streamShards, err := newStreamShardStore(activeWindow, time.Minute, 10*time.Second, numPartitions, "zone1", limits, reg)
	require.NoError(t, err)
	clock := quartz.NewMock(t)
	usage.clock = clock
	streamShards.clock = clock
	return &Service{
		cfg:              Config{NumPartitions: numPartitions, ActiveWindow: activeWindow},
		limits:           limits,
		partitionManager: partitionManager,
		usage:            usage,
		streamShards:     streamShards,
		metrics:          newMetrics(reg),
		clock:            clock,
	}, clock
}

func TestService_CheckLimitsAndShard(t *testing.T) {
	limits := &mockLimits{
		ShardStreamsConfig: shardstreams.Config{Enabled: true},
	}
	require.NoError(t, limits.ShardStreamsConfig.DesiredRate.Set("1KB"))

	// Stream 0x1 hashes to partition 1, stream 0x2 to partition 0.
	s, _ := newTestService(t, limits, 2)
	s.partitionManager.Assign([]int32{0})

	req := &proto.CheckLimitsAndShardRequest{
		Tenant: "test",
		Streams: []*proto.StreamMetadata{
			{StreamHash: 0x1, TotalSize: 100},
			{StreamHash: 0x2, TotalSize: 100},
		},
	}
	resp, err := s.CheckLimitsAndShard(t.Context(), req)
	require.NoError(t, err)

	// The stream whose partition is not assigned is reported as not owned,
	// and is not evaluated. The other stream is brand new, so it gets one
	// shard.
	require.Equal(t, []*proto.StreamShardResult{{
		StreamHash: 0x1,
		Shards:     1,
		Stats:      &proto.ShardStats{ShardDecisionContext: uint32(ReasonNotOwned)},
	}, {
		StreamHash: 0x2,
		Shards:     1,
		Stats:      &proto.ShardStats{},
	}}, resp.Results)

	// The stream that reached the wrong instance is counted, per partition, so
	// that misrouting can be told apart from an unreachable instance.
	require.Equal(t, float64(1), testutil.ToFloat64(s.metrics.streamShardStreamsNotOwnedTotal.WithLabelValues("1")))
	require.Equal(t, float64(0), testutil.ToFloat64(s.metrics.streamShardStreamsNotOwnedTotal.WithLabelValues("0")))

	// The request's streams are left as they were: the caller owns that slice.
	require.Equal(t, []*proto.StreamMetadata{
		{StreamHash: 0x1, TotalSize: 100},
		{StreamHash: 0x2, TotalSize: 100},
	}, req.Streams)
}

func TestService_EvictOldStreams(t *testing.T) {
	limits := &mockLimits{ShardStreamsConfig: shardstreams.Config{Enabled: true}}
	require.NoError(t, limits.ShardStreamsConfig.DesiredRate.Set("1KB"))
	s, clock := newTestService(t, limits, 1)
	s.partitionManager.Assign([]int32{0})
	s.cfg.EvictionInterval = 10 * time.Millisecond

	_, err := s.CheckLimitsAndShard(t.Context(), &proto.CheckLimitsAndShardRequest{
		Tenant:  "test",
		Streams: []*proto.StreamMetadata{{StreamHash: 0x1, TotalSize: 100}},
	})
	require.NoError(t, err)
	require.NoError(t, s.usage.Update("test", &proto.StreamMetadata{StreamHash: 0x1, TotalSize: 100}, clock.Now()))

	// The periodic eviction covers the shard store as well as the usage store,
	// otherwise a tenant's shard budget would never be released.
	clock.Advance(2 * s.cfg.ActiveWindow)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go s.evictOldStreamsPeriodic(ctx)

	require.Eventually(t, func() bool {
		return testutil.ToFloat64(s.metrics.streamShardEvictionsTotal.WithLabelValues("test")) == 1 &&
			testutil.ToFloat64(s.metrics.streamEvictionsTotal.WithLabelValues("test")) == 1
	}, time.Second, 10*time.Millisecond)
}

func TestService_CheckLimitsAndShard_NoStreams(t *testing.T) {
	s, _ := newTestService(t, &mockLimits{}, 1)
	resp, err := s.CheckLimitsAndShard(t.Context(), &proto.CheckLimitsAndShardRequest{Tenant: "test"})
	require.NoError(t, err)
	require.Empty(t, resp.Results)
}
