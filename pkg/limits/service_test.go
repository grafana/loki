package limits

import (
	"context"
	"testing"
	"time"

	"github.com/coder/quartz"
	"github.com/go-kit/log"
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
	streamShards, err := newStreamShardStore(activeWindow, time.Minute, 10*time.Second, numPartitions, "zone1", true, limits, reg)
	require.NoError(t, err)
	clock := quartz.NewMock(t)
	usage.clock = clock
	streamShards.clock = clock
	logger := log.NewNopLogger()
	return &Service{
		cfg:              Config{NumPartitions: numPartitions, ActiveWindow: activeWindow},
		limits:           limits,
		partitionManager: partitionManager,
		usage:            usage,
		streamShards:     streamShards,
		producer:         newProducer(&mockKafka{}, "topic", numPartitions, "zone1", logger, reg),
		metrics:          newMetrics(reg),
		logger:           logger,
		clock:            clock,
	}, clock
}

func TestService_CheckLimitsAndShard_ProducesRateBuckets(t *testing.T) {
	const bucketSize = 10 * time.Second
	limits := &mockLimits{
		ShardStreamsConfig: shardstreams.Config{Enabled: true},
	}
	require.NoError(t, limits.ShardStreamsConfig.DesiredRate.Set("1KB"))
	s, clock := newTestService(t, limits, 1)
	s.partitionManager.Assign([]int32{0})
	kafka := s.producer.client.(*mockKafka)

	req := &proto.CheckLimitsAndShardRequest{
		Tenant:  "test",
		Streams: []*proto.StreamMetadata{{StreamHash: 0x1, TotalSize: 100}},
	}
	// Have the usage store track the stream, as it would in shadow mode where
	// both endpoints are called for the same push.
	toProduce, _, _, err := s.usage.UpdateCond("test", req.Streams, clock.Now())
	require.NoError(t, err)
	require.Len(t, toProduce, 1)

	// Nothing is produced while the first bucket is still in flight.
	_, err = s.CheckLimitsAndShard(t.Context(), req)
	require.NoError(t, err)
	require.Empty(t, kafka.produced)

	// The first push of the next bucket publishes the complete one.
	bucketStart := clock.Now().Truncate(bucketSize)
	clock.Advance(bucketSize)
	_, err = s.CheckLimitsAndShard(t.Context(), req)
	require.NoError(t, err)
	require.Len(t, kafka.produced, 1)
	var rec proto.StreamMetadataRecord
	require.NoError(t, rec.Unmarshal(kafka.produced[0].Value))
	require.Equal(t, "zone1", rec.Zone)
	require.Equal(t, uint64(0x1), rec.Metadata.StreamHash)
	require.Equal(t, uint32(1), rec.ShardCount)
	require.Equal(t, &proto.ShardRateBucket{
		BucketStart: bucketStart.UnixNano(),
		Size_:       100,
		Pushes:      1,
	}, rec.ShardRateBucket)

	// That record carries the stream's metadata, so the usage store does not
	// produce a second one for it.
	clock.Advance(time.Minute)
	toProduce, _, _, err = s.usage.UpdateCond("test", req.Streams, clock.Now())
	require.NoError(t, err)
	require.Empty(t, toProduce)
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
