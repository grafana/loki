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

	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestGetSegmentationKey(t *testing.T) {
	t.Run("stream without labels", func(t *testing.T) {
		key, err := GetSegmentationKey(KeyedStream{})
		require.NoError(t, err)
		require.Equal(t, SegmentationKey("unknown_service"), key)
	})

	t.Run("stream with invalid labels", func(t *testing.T) {
		key, err := GetSegmentationKey(KeyedStream{
			Stream: logproto.Stream{
				Labels: "{",
			},
		})
		require.EqualError(t, err, "1:2: parse error: unexpected end of input inside braces")
		require.Equal(t, SegmentationKey(""), key)
	})

	t.Run("stream with service_name", func(t *testing.T) {
		key, err := GetSegmentationKey(KeyedStream{
			Stream: logproto.Stream{
				Labels: "{service_name=\"foo\"}",
			},
		})
		require.NoError(t, err)
		require.Equal(t, SegmentationKey("foo"), key)
	})

	t.Run("stream without service_name", func(t *testing.T) {
		key, err := GetSegmentationKey(KeyedStream{
			Stream: logproto.Stream{
				Labels: "{bar=\"baz\"}",
			},
		})
		require.NoError(t, err)
		require.Equal(t, SegmentationKey("unknown_service"), key)
	})
}

func TestSegmentationKey_Sum64(t *testing.T) {
	k1 := SegmentationKey("")
	require.Equal(t, uint64(0x552129d0d55dcd1b), k1.Sum64())
	k2 := SegmentationKey("abc")
	require.Equal(t, uint64(0x4a3a160be83aefc5), k2.Sum64())
	// The same key always produces the same 64 bit sum.
	k3 := SegmentationKey("abc")
	require.Equal(t, k2.Sum64(), k3.Sum64())
}

func TestSegmentationPartitionResolve_Resolve(t *testing.T) {
	// Set up a fake empty ring.
	emptyRing := mockPartitionRingReader{}
	emptyRing.ring = ring.NewPartitionRing(ring.PartitionRingDesc{})

	// Set up a fake partition ring with a single active partition.
	ringWithActivePartition := mockPartitionRingReader{}
	ringWithActivePartition.ring = ring.NewPartitionRing(ring.PartitionRingDesc{
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

	t.Run("uses random shuffle if rate unknown", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		resolver := NewSegmentationPartitionResolver(1024, ringWithActivePartition, reg, log.NewNopLogger())
		partition, err := resolver.Resolve(t.Context(), SegmentationKey("test"), 0)
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

	t.Run("returns error if rate unknown and no active partitions", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		resolver := NewSegmentationPartitionResolver(1024, emptyRing, reg, log.NewNopLogger())
		partition, err := resolver.Resolve(t.Context(), SegmentationKey("test"), 0)
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

	t.Run("shuffle shards on segmentation key if rate is known", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		resolver := NewSegmentationPartitionResolver(1024, ringWithActivePartition, reg, log.NewNopLogger())
		partition, err := resolver.Resolve(t.Context(), SegmentationKey("test"), 512)
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
