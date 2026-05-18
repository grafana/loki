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
