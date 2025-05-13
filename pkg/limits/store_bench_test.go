package limits

import (
	"fmt"
	"testing"
	"time"

	"github.com/grafana/loki/v3/pkg/limits/proto"
)

func BenchmarkUsageStore_Store(b *testing.B) {
	const (
		windowSize     = time.Hour
		bucketDuration = time.Minute
		rateWindow     = 5 * time.Minute
		rateBuckets    = 5 // One bucket per minute in the 5-minute rate window
	)

	benchmarks := []struct {
		name                string
		numTenants          int
		numPartitions       int
		streamsPerPartition int
	}{
		{
			name:                "4_partitions_small_streams_single_tenant",
			numTenants:          1,
			numPartitions:       4,
			streamsPerPartition: 500,
		},
		{
			name:                "8_partitions_medium_streams_multi_tenant",
			numTenants:          10,
			numPartitions:       8,
			streamsPerPartition: 1000,
		},
		{
			name:                "16_partitions_large_streams_multi_tenant",
			numTenants:          50,
			numPartitions:       16,
			streamsPerPartition: 5000,
		},
		{
			name:                "32_partitions_xlarge_streams_multi_tenant",
			numTenants:          100,
			numPartitions:       32,
			streamsPerPartition: 10000,
		},
	}

	for _, bm := range benchmarks {
		s := NewUsageStore(Config{NumPartitions: bm.numPartitions})

		b.Run(fmt.Sprintf("%s_create", bm.name), func(b *testing.B) {
			now := time.Now()
			cutoff := now.Add(-windowSize).UnixNano()

			// Run the benchmark
			for i := range b.N {
				// For each iteration, update a random stream in a random partition for a random tenant
				tenant := fmt.Sprintf("benchmark-tenant-%d", i%bm.numTenants)
				streamIdx := i % bm.streamsPerPartition

				updateTime := now.Add(time.Duration(i) * time.Second)
				metadata := []*proto.StreamMetadata{{
					StreamHash: uint64(streamIdx),
					TotalSize:  1500,
				}}

				bucketStart := updateTime.Truncate(bucketDuration).UnixNano()
				bucketCutOff := updateTime.Add(-rateWindow).UnixNano()

				s.Update(tenant, metadata, updateTime.UnixNano(), cutoff, bucketStart, bucketCutOff, nil)
			}
		})

		b.Run(fmt.Sprintf("%s_update", bm.name), func(b *testing.B) {
			now := time.Now()
			cutoff := now.Add(-windowSize).UnixNano()

			// Run the benchmark
			for i := range b.N {
				// For each iteration, update a random stream in a random partition for a random tenant
				tenant := fmt.Sprintf("benchmark-tenant-%d", i%bm.numTenants)
				streamIdx := i % bm.streamsPerPartition

				updateTime := now.Add(time.Duration(i) * time.Second)
				metadata := []*proto.StreamMetadata{{
					StreamHash: uint64(streamIdx),
					TotalSize:  1500,
				}}

				bucketStart := updateTime.Truncate(bucketDuration).UnixNano()
				bucketCutOff := updateTime.Add(-rateWindow).UnixNano()

				s.Update(tenant, metadata, updateTime.UnixNano(), cutoff, bucketStart, bucketCutOff, nil)
			}
		})

		s = NewUsageStore(Config{NumPartitions: bm.numPartitions})

		// Run parallel benchmark
		b.Run(bm.name+"_create_parallel", func(b *testing.B) {
			now := time.Now()
			cutoff := now.Add(-windowSize).UnixNano()
			// Run parallel benchmark
			b.RunParallel(func(pb *testing.PB) {
				i := 0
				for pb.Next() {
					tenant := fmt.Sprintf("benchmark-tenant-%d", i%bm.numTenants)
					streamIdx := i % bm.streamsPerPartition

					updateTime := now.Add(time.Duration(i) * time.Second)
					metadata := []*proto.StreamMetadata{{
						StreamHash: uint64(streamIdx),
						TotalSize:  1500,
					}}

					bucketStart := updateTime.Truncate(bucketDuration).UnixNano()
					bucketCutOff := updateTime.Add(-rateWindow).UnixNano()

					s.Update(tenant, metadata, updateTime.UnixNano(), cutoff, bucketStart, bucketCutOff, nil)
					i++
				}
			})
		})

		b.Run(bm.name+"_update_parallel", func(b *testing.B) {
			now := time.Now()
			cutoff := now.Add(-windowSize).UnixNano()
			// Run parallel benchmark
			b.RunParallel(func(pb *testing.PB) {
				i := 0
				for pb.Next() {
					tenant := fmt.Sprintf("benchmark-tenant-%d", i%bm.numTenants)
					streamIdx := i % bm.streamsPerPartition

					updateTime := now.Add(time.Duration(i) * time.Second)
					metadata := []*proto.StreamMetadata{{
						StreamHash: uint64(streamIdx),
						TotalSize:  1500,
					}}

					bucketStart := updateTime.Truncate(bucketDuration).UnixNano()
					bucketCutOff := updateTime.Add(-rateWindow).UnixNano()

					s.Update(tenant, metadata, updateTime.UnixNano(), cutoff, bucketStart, bucketCutOff, nil)
					i++
				}
			})
		})
	}
}
