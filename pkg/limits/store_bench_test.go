package limits

import (
	"fmt"
	"sync"
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
		s := NewUsageStore(bm.numPartitions)

		b.Run(fmt.Sprintf("%s_create", bm.name), func(b *testing.B) {
			now := time.Now()

			// Run the benchmark
			for i := range b.N {
				// For each iteration, update a random stream in a random partition for a random tenant
				tenant := fmt.Sprintf("benchmark-tenant-%d", i%bm.numTenants)
				partition := int32(i % bm.numPartitions)
				streamIdx := i % bm.streamsPerPartition

				updateTime := now.Add(time.Duration(i) * time.Second)
				metadata := &proto.StreamMetadata{
					StreamHash: uint64(streamIdx),
					TotalSize:  1500,
				}

				bucketStart := updateTime.Truncate(bucketDuration).UnixNano()
				bucketCutOff := updateTime.Add(-rateWindow).UnixNano()

				s.Store(tenant, partition, metadata.StreamHash, metadata.TotalSize, updateTime.UnixNano(), bucketStart, bucketCutOff)
			}
		})

		b.Run(fmt.Sprintf("%s_update", bm.name), func(b *testing.B) {
			now := time.Now()

			// Run the benchmark
			for i := range b.N {
				// For each iteration, update a random stream in a random partition for a random tenant
				tenant := fmt.Sprintf("benchmark-tenant-%d", i%bm.numTenants)
				partition := int32(i % bm.numPartitions)
				streamIdx := i % bm.streamsPerPartition

				updateTime := now.Add(time.Duration(i) * time.Second)
				metadata := &proto.StreamMetadata{
					StreamHash: uint64(streamIdx),
					TotalSize:  1500,
				}

				bucketStart := updateTime.Truncate(bucketDuration).UnixNano()
				bucketCutOff := updateTime.Add(-rateWindow).UnixNano()

				s.Store(tenant, partition, metadata.StreamHash, metadata.TotalSize, updateTime.UnixNano(), bucketStart, bucketCutOff)
			}
		})

		s = NewUsageStore(bm.numPartitions)

		// Run parallel benchmark
		b.Run(bm.name+"_create_parallel", func(b *testing.B) {
			now := time.Now()
			// Run parallel benchmark
			b.RunParallel(func(pb *testing.PB) {
				i := 0
				for pb.Next() {
					tenant := fmt.Sprintf("benchmark-tenant-%d", i%bm.numTenants)
					partition := int32(i % bm.numPartitions)
					streamIdx := i % bm.streamsPerPartition

					updateTime := now.Add(time.Duration(i) * time.Second)
					metadata := &proto.StreamMetadata{
						StreamHash: uint64(streamIdx),
						TotalSize:  1500,
					}

					bucketStart := updateTime.Truncate(bucketDuration).UnixNano()
					bucketCutOff := updateTime.Add(-rateWindow).UnixNano()

					s.Store(tenant, partition, metadata.StreamHash, metadata.TotalSize, updateTime.UnixNano(), bucketStart, bucketCutOff)
					i++
				}
			})
		})

		b.Run(bm.name+"_update_parallel", func(b *testing.B) {
			now := time.Now()
			// Run parallel benchmark
			b.RunParallel(func(pb *testing.PB) {
				i := 0
				for pb.Next() {
					tenant := fmt.Sprintf("benchmark-tenant-%d", i%bm.numTenants)
					partition := int32(i % bm.numPartitions)
					streamIdx := i % bm.streamsPerPartition

					updateTime := now.Add(time.Duration(i) * time.Second)
					metadata := &proto.StreamMetadata{
						StreamHash: uint64(streamIdx),
						TotalSize:  1500,
					}

					bucketStart := updateTime.Truncate(bucketDuration).UnixNano()
					bucketCutOff := updateTime.Add(-rateWindow).UnixNano()

					s.Store(tenant, partition, metadata.StreamHash, metadata.TotalSize, updateTime.UnixNano(), bucketStart, bucketCutOff)
					i++
				}
			})
		})
	}
}

func BenchmarkStreamMetadata_UsageAndStore(b *testing.B) {
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
		updatesPerPartition int
		readQPS             int
	}{
		{
			name:                "4_partitions_small_streams_single_tenant",
			numTenants:          1,
			numPartitions:       4,
			streamsPerPartition: 500,
			updatesPerPartition: 10,
			readQPS:             100,
		},
		{
			name:                "8_partitions_medium_streams_multi_tenant",
			numTenants:          10,
			numPartitions:       8,
			streamsPerPartition: 1000,
			updatesPerPartition: 10,
			readQPS:             100,
		},
		{
			name:                "16_partitions_large_streams_multi_tenant",
			numTenants:          50,
			numPartitions:       16,
			streamsPerPartition: 5000,
			updatesPerPartition: 10,
			readQPS:             100,
		},
		{
			name:                "32_partitions_xlarge_streams_multi_tenant",
			numTenants:          100,
			numPartitions:       32,
			streamsPerPartition: 10000,
			updatesPerPartition: 10,
			readQPS:             100,
		},
	}

	for _, bm := range benchmarks {
		s := NewUsageStore(bm.numPartitions)

		now := time.Now()

		// Run the benchmark
		for partition := range bm.numPartitions {
			// For each iteration, update a random stream in a random partition for a random tenant
			tenant := fmt.Sprintf("benchmark-tenant-%d", partition%bm.numTenants)
			streamIdx := partition % bm.streamsPerPartition

			updateTime := now.Add(time.Duration(partition) * time.Second)
			metadata := &proto.StreamMetadata{
				StreamHash: uint64(streamIdx),
				TotalSize:  1500,
			}

			bucketStart := updateTime.Truncate(bucketDuration).UnixNano()
			bucketCutOff := updateTime.Add(-rateWindow).UnixNano()

			s.Store(tenant, int32(partition), metadata.StreamHash, metadata.TotalSize, updateTime.UnixNano(), bucketStart, bucketCutOff)
		}

		b.Run(fmt.Sprintf("%s_create", bm.name), func(b *testing.B) {
			// Setup StreamMetadata instance

			now := time.Now()

			// Run the benchmark
			for range b.N {
				writeGroup := sync.WaitGroup{}
				writeGroup.Add(bm.numPartitions)

				for i := range bm.numPartitions {
					go func(i int) {
						defer writeGroup.Done()

						for range bm.updatesPerPartition {
							// For each iteration, update a random stream in a random partition for a random tenant
							tenant := fmt.Sprintf("benchmark-tenant-%d", i%bm.numTenants)
							partition := int32(i % bm.numPartitions)
							streamIdx := i % bm.streamsPerPartition

							updateTime := now.Add(time.Duration(i) * time.Second)
							metadata := &proto.StreamMetadata{
								StreamHash: uint64(streamIdx),
								TotalSize:  1500,
							}

							bucketStart := updateTime.Truncate(bucketDuration).UnixNano()
							bucketCutOff := updateTime.Add(-rateWindow).UnixNano()

							s.Store(tenant, partition, metadata.StreamHash, metadata.TotalSize, updateTime.UnixNano(), bucketStart, bucketCutOff)
						}
					}(i)
				}

				readConcurrency := bm.numTenants * bm.readQPS
				readGroup := sync.WaitGroup{}
				readGroup.Add(readConcurrency)

				for i := range readConcurrency {
					tenant := fmt.Sprintf("benchmark-tenant-%d", i%bm.numTenants)
					go func(tenant string) {
						defer readGroup.Done()

						s.ForTenant(tenant, func(_ string, _ int32, _ Stream) {
							// Do nothing
						})
					}(tenant)
				}

				writeGroup.Wait()
				readGroup.Wait()
			}
		})
	}
}
