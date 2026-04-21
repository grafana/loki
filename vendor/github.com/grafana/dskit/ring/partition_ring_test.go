package ring

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	shardUtil "github.com/grafana/dskit/ring/shard"
)

func BenchmarkShuffleSharding(b *testing.B) {
	for _, numPartitions := range []int{8, 16, 32, 64} {
		b.Run(fmt.Sprintf("partitions=%d", numPartitions), func(b *testing.B) {
			desc := NewPartitionRingDesc()
			now := time.Now()
			for i := 0; i < numPartitions; i++ {
				desc.AddPartition(int32(i), PartitionActive, now)
			}
			ring, err := NewPartitionRing(*desc)
			if err != nil {
				b.Fatal(err)
			}
			shardSize := numPartitions / 4

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// Use unique identifiers to bypass cache and measure the algorithm itself.
				identifier := fmt.Sprintf("tenant-%d", i)
				_, err := ring.ShuffleShard(identifier, shardSize)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkSortTokens(b *testing.B) {
	desc := NewPartitionRingDesc()
	now := time.Now()
	for i := 0; i < 8; i++ {
		desc.AddPartition(int32(i), PartitionActive, now)
	}
	subDesc := desc
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = subDesc.tokens()
	}
}

func BenchmarkPRNGSetup(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		identifier := fmt.Sprintf("t%d", i)
		seed := shardUtil.ShuffleShardSeed(identifier, "")
		_ = rand.New(rand.NewSource(seed))
	}
}

func BenchmarkBuildPartitionByToken(b *testing.B) {
	desc := NewPartitionRingDesc()
	now := time.Now()
	for i := 0; i < 8; i++ {
		desc.AddPartition(int32(i), PartitionActive, now)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = desc.partitionByToken()
	}
}

func BenchmarkNewSubring(b *testing.B) {
	desc := NewPartitionRingDesc()
	now := time.Now()
	for i := 0; i < 32; i++ {
		desc.AddPartition(int32(i), PartitionActive, now)
	}
	selected := make(map[int32]struct{}, 8)
	for i := 0; i < 8; i++ {
		selected[int32(i)] = struct{}{}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = desc.WithPartitions(selected)
	}
}

// BenchmarkShuffleShardAlgorithmOnly benchmarks just the sharding algorithm by reusing a subring.
func BenchmarkShuffleShardAlgorithmOnly(b *testing.B) {
	desc := NewPartitionRingDesc()
	now := time.Now()
	for i := 0; i < 32; i++ {
		desc.AddPartition(int32(i), PartitionActive, now)
	}
	ring, _ := NewPartitionRing(*desc)

	// Pre-allocate a reuse buffer by calling once
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		identifier := fmt.Sprintf("t%d", i)
		// Only benchmark shuffleShard algorithm — builds subring each time (can't avoid)
		ring.shuffleShard(identifier, 8, 0, time.Time{})
	}
}

// BenchmarkNewSubringDirect benchmarks newSubring in isolation with a pre-built selected map.
func BenchmarkNewSubringDirect(b *testing.B) {
	desc := NewPartitionRingDesc()
	now := time.Now()
	for i := 0; i < 32; i++ {
		desc.AddPartition(int32(i), PartitionActive, now)
	}
	ring, _ := NewPartitionRing(*desc)
	selected := make(map[int32]struct{}, 8)
	for i := 0; i < 8; i++ {
		selected[int32(i)] = struct{}{}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ring.newSubring(selected)
	}
}

func BenchmarkSubringConstruction(b *testing.B) {
	desc := NewPartitionRingDesc()
	now := time.Now()
	for i := 0; i < 8; i++ {
		desc.AddPartition(int32(i), PartitionActive, now)
	}
	subDesc := desc.WithPartitions(map[int32]struct{}{0: {}, 1: {}, 2: {}, 3: {}, 4: {}, 5: {}, 6: {}, 7: {}})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		NewPartitionRingWithOptions(subDesc, DefaultPartitionRingOptions())
	}
}
