package distributor

import (
	"bytes"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/ring"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestSegmentationPartitionResolver_Resolve_ConsistentHashing(t *testing.T) {
	// Set up a fake empty ring.
	emptyRing := mockPartitionRingReader{}
	emptyRing.ring, _ = ring.NewPartitionRing(ring.PartitionRingDesc{})

	// Set up a fake partition ring with a single active partition.
	ringWithActivePartition := mockPartitionRingReader{}
	ringWithActivePartition.ring, _ = ring.NewPartitionRing(ring.PartitionRingDesc{
		Partitions: map[int32]ring.PartitionDesc{
			1: {
				Id:             1,
				Tokens:         []uint32{1},
				State:          ring.PartitionActive,
				StateTimestamp: time.Now().Unix(),
			},
		},
		Owners: map[string]ring.OwnerDesc{
			"test": {
				OwnedPartition:   1,
				State:            ring.OwnerActive,
				UpdatedTimestamp: time.Now().Unix(),
			},
		},
	})

	t.Run("returns error if no active partitions", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		resolver := newSegmentationPartitionResolver(1024, false, emptyRing, nil, reg, log.NewNopLogger())
		partition, err := resolver.Resolve("tenant", "test", 0x1, 0, 0)
		require.EqualError(t, err, "no active partitions")
		require.Equal(t, int32(0), partition)
		// Check the metrics to make sure it fell back to random shuffle and
		// then failed.
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

	t.Run("uses random shuffle if rate unknown", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		resolver := newSegmentationPartitionResolver(1024, false, ringWithActivePartition, nil, reg, log.NewNopLogger())
		partition, err := resolver.Resolve("tenant", "test", 0x1, 0, 0)
		require.NoError(t, err)
		// Should return partition 1 since that is the only active partition.
		require.Equal(t, int32(1), partition)
		// Check the metrics to make sure it fell back to random shuffle.
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

	t.Run("shuffle shards on segmentation key if rate is known", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		resolver := newSegmentationPartitionResolver(1024, false, ringWithActivePartition, nil, reg, log.NewNopLogger())
		partition, err := resolver.Resolve("tenant", "test", 0x1, 512, 0)
		require.NoError(t, err)
		require.Equal(t, int32(1), partition)
		require.NoError(t, testutil.GatherAndCompare(reg, bytes.NewBufferString(`
			# HELP loki_distributor_segmentation_partition_resolver_keys_randomly_sharded_total Total number of segmentation keys that fell back to a random active partition due to absent rate.
			# TYPE loki_distributor_segmentation_partition_resolver_keys_randomly_sharded_total counter
			loki_distributor_segmentation_partition_resolver_keys_randomly_sharded_total 0
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
}

func TestSegmentationPartitionResolver_TenantShuffleShard_ConsistentHashing(t *testing.T) {
	// Set up a fake partition ring with two active partitions.
	ringWithActivePartitions := mockPartitionRingReader{}
	ring, err := ring.NewPartitionRing(ring.PartitionRingDesc{
		Partitions: map[int32]ring.PartitionDesc{
			1: {
				Id:             1,
				Tokens:         []uint32{1},
				State:          ring.PartitionActive,
				StateTimestamp: time.Now().Unix(),
			},
			2: {
				Id:             2,
				Tokens:         []uint32{2},
				State:          ring.PartitionActive,
				StateTimestamp: time.Now().Unix(),
			},
		},
		Owners: map[string]ring.OwnerDesc{
			"owner1": {
				OwnedPartition:   1,
				State:            ring.OwnerActive,
				UpdatedTimestamp: time.Now().Unix(),
			},
			"owner2": {
				OwnedPartition:   2,
				State:            ring.OwnerActive,
				UpdatedTimestamp: time.Now().Unix(),
			},
		},
	})
	require.NoError(t, err)
	ringWithActivePartitions.ring = ring

	t.Run("no rate returns full ring", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		resolver := newSegmentationPartitionResolver(1024, false, ringWithActivePartitions, nil, reg, log.NewNopLogger())
		partitionRing := ringWithActivePartitions.PartitionRing()
		subring, err := resolver.tenantShuffleShardConsistentHashing(partitionRing, "tenant", 0)
		require.NoError(t, err)
		require.Equal(t, partitionRing, subring)
	})

	t.Run("rate equals partition rate returns subring with one partition", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		resolver := newSegmentationPartitionResolver(1024, false, ringWithActivePartitions, nil, reg, log.NewNopLogger())
		partitionRing := ringWithActivePartitions.PartitionRing()
		subring, err := resolver.tenantShuffleShardConsistentHashing(partitionRing, "tenant", 1024)
		require.NoError(t, err)
		require.Equal(t, 1, subring.ActivePartitionsCount())
	})

	t.Run("rate exceeds partition rate returns subring with all partitions", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		resolver := newSegmentationPartitionResolver(1024, false, ringWithActivePartitions, nil, reg, log.NewNopLogger())
		partitionRing := ringWithActivePartitions.PartitionRing()
		subring, err := resolver.tenantShuffleShardConsistentHashing(partitionRing, "tenant", 2048)
		require.NoError(t, err)
		require.Equal(t, 2, subring.ActivePartitionsCount())
	})
}
