package rendezvous

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShard_NoPartitions(t *testing.T) {
	s := NewShuffleSharder(nil)
	_, err := s.Shard(42)
	require.Error(t, err)
}

func TestShard_OnePartition(t *testing.T) {
	s := NewShuffleSharder([]int32{7})
	for key := uint32(0); key < 100; key++ {
		result, err := s.Shard(key)
		require.NoError(t, err)
		assert.Equal(t, int32(7), result)
	}
}

func TestShard_ManyPartitions_FairDistribution(t *testing.T) {
	partitions := []int32{1, 2, 3, 4, 5}
	s := NewShuffleSharder(partitions)

	counts := make(map[int32]int)
	numKeys := 10_000
	for key := uint32(0); key < uint32(numKeys); key++ {
		result, err := s.Shard(key)
		require.NoError(t, err)
		counts[result]++
	}

	expected := numKeys / len(partitions)
	tolerance := float64(expected) * 0.3
	for _, p := range partitions {
		assert.InDelta(t, expected, counts[p], tolerance, "partition %d: got %d, expected ~%d", p, counts[p], expected)
	}
}

func TestShuffleShard_NoPartitions(t *testing.T) {
	s := NewShuffleSharder(nil)
	result := s.ShuffleShard("some-key", 3)
	assert.Empty(t, result.partitions)
}

func TestShuffleShard_FewerPartitionsThanRequested(t *testing.T) {
	partitions := []int32{1, 2, 3}
	s := NewShuffleSharder(partitions)
	result := s.ShuffleShard("some-key", 10)
	assert.ElementsMatch(t, partitions, result.partitions)
}

func TestShuffleShard_MorePartitionsThanRequested(t *testing.T) {
	partitions := []int32{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	s := NewShuffleSharder(partitions)
	numShards := 3

	result := s.ShuffleShard("some-key", numShards)
	require.Len(t, result.partitions, numShards)

	// Over many different shard keys, each partition should appear roughly equally
	// in the selected subsets.
	counts := make(map[int32]int)
	numKeys := 1000
	for i := 0; i < numKeys; i++ {
		sub := s.ShuffleShard(fmt.Sprintf("tenant-%d", i), numShards)
		for _, p := range sub.partitions {
			counts[p]++
		}
	}

	// Each call selects numShards out of len(partitions), so expected appearances
	// per partition = numKeys * numShards / len(partitions).
	expected := numKeys * numShards / len(partitions)
	tolerance := float64(expected) * 0.3
	for _, p := range partitions {
		assert.InDelta(t, expected, counts[p], tolerance, "partition %d: appeared %d times, expected ~%d", p, counts[p], expected)
	}
}

func TestShuffleShard_Stability(t *testing.T) {
	partitions := []int32{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	s := NewShuffleSharder(partitions)
	previousSubPartitions := make([]int32, 0)
	for i := 1; i <= 10; i++ {
		newSubPartitions := s.ShuffleShard("foo", i)
		// Verify the new shard contains all partitions from the previous one
		for _, p := range previousSubPartitions {
			assert.Contains(t, newSubPartitions.partitions, p, "shard of size %d should contain all partitions from shard of size %d", i, i-1)
		}
		previousSubPartitions = newSubPartitions.partitions
	}
}

func benchmarkShuffleShard(b *testing.B, partitionCount int, tenantShuffleShardSize int, segmentationKeyShuffleShardSize int) {
	b.Run(fmt.Sprintf("n=%d,k1=%d,k2=%d", partitionCount, tenantShuffleShardSize, segmentationKeyShuffleShardSize), func(b *testing.B) {
		require.GreaterOrEqual(b, partitionCount, tenantShuffleShardSize)
		require.GreaterOrEqual(b, tenantShuffleShardSize, segmentationKeyShuffleShardSize)
		partitions := make([]int32, partitionCount)
		for i := range partitionCount {
			partitions[i] = int32(i)
		}
		shuffleSharder := NewShuffleSharder(partitions)
		b.ResetTimer()
		for range b.N {
			shuffleSharderLayer1 := shuffleSharder.ShuffleShard("foo", tenantShuffleShardSize)
			shuffleSharderLayer2 := shuffleSharderLayer1.ShuffleShard("bar", segmentationKeyShuffleShardSize)
			_, err := shuffleSharderLayer2.Shard(123)
			require.NoError(b, err)
		}
	})
}

func BenchmarkShuffleShard(b *testing.B) {
	benchmarkShuffleShard(b, 200, 200, 200)
	benchmarkShuffleShard(b, 200, 190, 180)
	benchmarkShuffleShard(b, 200, 20, 10)
	benchmarkShuffleShard(b, 200, 100, 50)

	benchmarkShuffleShard(b, 1000, 990, 980)
	benchmarkShuffleShard(b, 1000, 500, 250)
	benchmarkShuffleShard(b, 1000, 20, 10)
}
