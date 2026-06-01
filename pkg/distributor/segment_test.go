package distributor

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/kv/consul"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/distributor/rendezvous"
	"github.com/grafana/loki/v3/pkg/logproto"
)

// newTestPartitionWatcher creates a PartitionWatcher backed by an in-memory KV
// store seeded with the given active partition IDs. Passing no IDs produces a
// watcher whose sharder has zero active partitions.
func newTestPartitionWatcher(t *testing.T, partitionIDs ...int32) *rendezvous.PartitionWatcher {
	t.Helper()
	kvClient, closer := consul.NewInMemoryClient(ring.GetPartitionRingCodec(), log.NewNopLogger(), nil)
	t.Cleanup(func() { _ = closer.Close() })

	desc := ring.PartitionRingDesc{
		Partitions: make(map[int32]ring.PartitionDesc, len(partitionIDs)),
		Owners:     map[string]ring.OwnerDesc{},
	}
	for _, id := range partitionIDs {
		desc.Partitions[id] = ring.PartitionDesc{
			Id:             id,
			Tokens:         []uint32{uint32(id)},
			State:          ring.PartitionActive,
			StateTimestamp: time.Now().Unix(),
		}
	}
	err := kvClient.CAS(context.Background(), "test-ring", func(_ interface{}) (interface{}, bool, error) {
		return &desc, true, nil
	})
	require.NoError(t, err)

	watcher := rendezvous.New(rendezvous.Config{Key: "test-ring"}, kvClient, log.NewNopLogger())
	require.NoError(t, services.StartAndAwaitRunning(context.Background(), watcher))
	t.Cleanup(func() { _ = services.StopAndAwaitTerminated(context.Background(), watcher) })
	return watcher
}

func TestGetSegmentationKey(t *testing.T) {
	t.Run("stream without labels", func(t *testing.T) {
		key, err := getSegmentationKey(KeyedStream{})
		require.NoError(t, err)
		require.Equal(t, segmentationKey("unknown_service"), key)
	})

	t.Run("stream with invalid labels", func(t *testing.T) {
		key, err := getSegmentationKey(KeyedStream{
			Stream: logproto.Stream{
				Labels: "{",
			},
		})
		require.EqualError(t, err, "1:2: parse error: unexpected end of input inside braces")
		require.Equal(t, segmentationKey(""), key)
	})

	t.Run("stream with service_name", func(t *testing.T) {
		key, err := getSegmentationKey(KeyedStream{
			Stream: logproto.Stream{
				Labels: "{service_name=\"foo\"}",
			},
		})
		require.NoError(t, err)
		require.Equal(t, segmentationKey("foo"), key)
	})

	t.Run("stream without service_name", func(t *testing.T) {
		key, err := getSegmentationKey(KeyedStream{
			Stream: logproto.Stream{
				Labels: "{bar=\"baz\"}",
			},
		})
		require.NoError(t, err)
		require.Equal(t, segmentationKey("unknown_service"), key)
	})
}

func TestSegmentationKey_Sum64(t *testing.T) {
	k1 := segmentationKey("")
	require.Equal(t, uint64(6134230144364956955), k1.Sum64())
	k2 := segmentationKey("abc")
	require.Equal(t, uint64(5348611747852513221), k2.Sum64())
	// The same key always produces the same 64 bit sum.
	k3 := segmentationKey("abc")
	require.Equal(t, k2.Sum64(), k3.Sum64())
}

func TestSegmentationPartitionResolver_Resolve(t *testing.T) {
	t.Run("returns error if no active partitions", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		watcher := newTestPartitionWatcher(t) // empty ring — zero active partitions
		resolver := newSegmentationPartitionResolver(1024, true, nil, watcher, reg, log.NewNopLogger())
		_, err := resolver.Resolve("tenant", segmentationKey("test"), 0x1, 0, 0)
		require.EqualError(t, err, "no active partitions")

		require.NoError(t, testutil.GatherAndCompare(reg, bytes.NewBufferString(`
			# HELP loki_distributor_segmentation_partition_resolver_keys_randomly_sharded_total Total number of segmentation keys that fell back to a random active partition due to absent rate.
			# TYPE loki_distributor_segmentation_partition_resolver_keys_randomly_sharded_total counter
			loki_distributor_segmentation_partition_resolver_keys_randomly_sharded_total 1
			# HELP loki_distributor_segmentation_partition_resolver_keys_failed_total Total number of segmentation keys that could not be resolved.
			# TYPE loki_distributor_segmentation_partition_resolver_keys_failed_total counter
			loki_distributor_segmentation_partition_resolver_keys_failed_total 1
			# HELP loki_distributor_segmentation_partition_resolver_keys_total Total number of segmentation keys passed to the resolver.
			# TYPE loki_distributor_segmentation_partition_resolver_keys_total counter
			loki_distributor_segmentation_partition_resolver_keys_total 1
		`),
			"loki_distributor_segmentation_partition_resolver_keys_allback_total",
			"loki_distributor_segmentation_partition_resolver_keys_failed_total",
			"loki_distributor_segmentation_partition_resolver_keys_total",
		))
	})

	t.Run("resolves to correct partition when rate is unknown", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		watcher := newTestPartitionWatcher(t, 1)
		resolver := newSegmentationPartitionResolver(1024, true, nil, watcher, reg, log.NewNopLogger())
		partition, err := resolver.Resolve("tenant", "test", 0x1, 0, 0)
		require.NoError(t, err)
		// Should return partition 1 since that is the only active partition.
		require.Equal(t, int32(1), partition)

		require.NoError(t, testutil.GatherAndCompare(reg, bytes.NewBufferString(`
			# HELP loki_distributor_segmentation_partition_resolver_keys_randomly_sharded_total Total number of segmentation keys that fell back to a random active partition due to absent rate.
			# TYPE loki_distributor_segmentation_partition_resolver_keys_randomly_sharded_total counter
			loki_distributor_segmentation_partition_resolver_keys_randomly_sharded_total 1
			# HELP loki_distributor_segmentation_partition_resolver_keys_failed_total Total number of segmentation keys that could not be resolved.
			# TYPE loki_distributor_segmentation_partition_resolver_keys_failed_total counter
			loki_distributor_segmentation_partition_resolver_keys_failed_total 0
			# HELP loki_distributor_segmentation_partition_resolver_keys_total Total number of segmentation keys passed to the resolver.
			# TYPE loki_distributor_segmentation_partition_resolver_keys_total counter
			loki_distributor_segmentation_partition_resolver_keys_total 1
		`),
			"loki_distributor_segmentation_partition_resolver_keys_allback_total",
			"loki_distributor_segmentation_partition_resolver_keys_failed_total",
			"loki_distributor_segmentation_partition_resolver_keys_total",
		))
	})

	t.Run("resolution is deterministic for same inputs", func(t *testing.T) {
		watcher := newTestPartitionWatcher(t, 1, 2, 3)
		resolver := newSegmentationPartitionResolver(1024, true, nil, watcher, prometheus.NewRegistry(), log.NewNopLogger())
		p1, err := resolver.Resolve("tenant-a", "svc", 0x1, 0, 0)
		require.NoError(t, err)
		p2, err := resolver.Resolve("tenant-a", "svc", 0x1, 0, 0)
		require.NoError(t, err)
		require.Equal(t, p1, p2)
	})
}

func TestSegmentationPartitionResolver_TenantShuffleShard(t *testing.T) {
	watcher := newTestPartitionWatcher(t, 1, 2, 3, 4, 5)
	resolver := newSegmentationPartitionResolver(1024, true, nil, watcher, prometheus.NewRegistry(), log.NewNopLogger())

	tests := []struct {
		rateBytes         uint64
		tenantRateBytes   uint64
		expectedNumShards int
	}{
		{500, 500, 1},
		{0, 0, 1},
		{1500, 1500, 2},
		{3000, 1500, 2},
		{4000, 3000, 3},
		{1500, 4000, 2},
		{1_000_000, 1_000_000, 5},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("perPartitionRateBytes=1024,rateBytes=%d,tenantRateBytes=%d", test.rateBytes, test.tenantRateBytes), func(t *testing.T) {
			seen := make(map[int32]struct{}, 2)
			for i := 0; i < 100; i++ {
				partition, err := resolver.Resolve("tenant", "test", uint32(i), test.rateBytes, test.tenantRateBytes)
				require.NoError(t, err)
				seen[partition] = struct{}{}
				require.InDelta(t, 3, partition, 2)
			}
			require.Len(t, seen, test.expectedNumShards)
		})
	}
}
