package limits

import (
	"testing"
	"time"

	"github.com/coder/quartz"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/distributor/shardstreams"
	"github.com/grafana/loki/v3/pkg/limits/proto"
)

func newTestService(t *testing.T, limits Limits, numPartitions int) *Service {
	t.Helper()
	reg := prometheus.NewRegistry()
	partitionManager, err := newPartitionManager(reg)
	require.NoError(t, err)
	streamShards, err := newStreamShardStore(time.Hour, time.Minute, 10*time.Second, numPartitions, limits, reg)
	require.NoError(t, err)
	clock := quartz.NewMock(t)
	streamShards.clock = clock
	return &Service{
		cfg:              Config{NumPartitions: numPartitions},
		limits:           limits,
		partitionManager: partitionManager,
		streamShards:     streamShards,
		streamShardsDiscardedTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{Name: "discarded"},
			[]string{"partition"},
		),
		clock: clock,
	}
}

func TestService_CheckLimitsAndShard(t *testing.T) {
	limits := &mockLimits{
		ShardStreamsConfig: shardstreams.Config{Enabled: true},
	}
	require.NoError(t, limits.ShardStreamsConfig.DesiredRate.Set("1KB"))

	// Stream 0x1 hashes to partition 1, stream 0x2 to partition 0.
	s := newTestService(t, limits, 2)
	s.partitionManager.Assign([]int32{0})

	resp, err := s.CheckLimitsAndShard(t.Context(), &proto.CheckLimitsAndShardRequest{
		Tenant: "test",
		Streams: []*proto.StreamMetadata{
			{StreamHash: 0x1, TotalSize: 100},
			{StreamHash: 0x2, TotalSize: 100},
		},
	})
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
}

func TestService_CheckLimitsAndShard_NoStreams(t *testing.T) {
	s := newTestService(t, &mockLimits{}, 1)
	resp, err := s.CheckLimitsAndShard(t.Context(), &proto.CheckLimitsAndShardRequest{Tenant: "test"})
	require.NoError(t, err)
	require.Empty(t, resp.Results)
}
