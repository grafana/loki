package ring

import (
	"fmt"
	"testing"
	"time"
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
