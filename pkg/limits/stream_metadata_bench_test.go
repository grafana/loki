package limits

import (
	"fmt"
	"testing"
	"time"

	"github.com/grafana/loki/v3/pkg/logproto"
)

func BenchmarkStreamMetadata_Store(b *testing.B) {
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
			name:                "32_partitions_xlarge_streams_multi_tenant	",
			numTenants:          100,
			numPartitions:       32,
			streamsPerPartition: 10000,
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			// Setup StreamMetadata instance
			s := NewStreamMetadata(bm.numPartitions)

			// Initialize metadata with existing streams and buckets
			now := time.Now()

			// Initialize metadata for each tenant
			for t := range bm.numTenants {
				tenant := fmt.Sprintf("benchmark-tenant-%d", t)

				// Create partitions with streams
				for p := range bm.numPartitions {

					for j := range bm.streamsPerPartition {
						// Create stream with multiple rate buckets
						stream := Stream{
							Hash:        uint64(j),
							LastSeenAt:  now.Add(-time.Duration(j) * time.Minute).UnixNano(),
							TotalSize:   uint64(1000 * (j + 1)),
							RateBuckets: make([]RateBucket, rateBuckets),
						}

						// Add rate buckets with some inside and outside the rate window
						for k := range rateBuckets {
							bucketTime := now.Add(-time.Duration(k*2) * time.Minute)
							stream.RateBuckets[k] = RateBucket{
								Timestamp: bucketTime.UnixNano(),
								Size:      uint64(500 * (k + 1)),
							}
						}

						bucketStart := stream.RateBuckets[0].Timestamp
						bucketCutOff := stream.RateBuckets[len(stream.RateBuckets)-1].Timestamp

						s.Store(tenant, int32(p), stream.Hash, stream.TotalSize, stream.LastSeenAt, bucketStart, bucketCutOff, false)
					}
				}
			}

			// Reset timer before the actual benchmark
			b.ResetTimer()

			// Run the benchmark
			for i := range b.N {
				// For each iteration, update a random stream in a random partition for a random tenant
				tenant := fmt.Sprintf("benchmark-tenant-%d", i%bm.numTenants)
				partition := int32(i % bm.numPartitions)
				streamIdx := i % bm.streamsPerPartition

				updateTime := now.Add(time.Duration(i) * time.Second)
				metadata := &logproto.StreamMetadata{
					StreamHash:             uint64(streamIdx),
					EntriesSize:            1000,
					StructuredMetadataSize: 500,
				}

				bucketStart := updateTime.Truncate(bucketDuration).UnixNano()
				bucketCutOff := updateTime.Add(-rateWindow).UnixNano()
				totalSize := metadata.EntriesSize + metadata.StructuredMetadataSize

				s.Store(tenant, partition, metadata.StreamHash, totalSize, updateTime.UnixNano(), bucketStart, bucketCutOff, false)
			}
		})

		// Run parallel benchmark
		b.Run(bm.name+"_parallel", func(b *testing.B) {
			// Setup similar to above
			s := NewStreamMetadata(bm.numPartitions)

			// Initialize metadata for each tenant
			now := time.Now()

			// Initialize metadata for each tenant
			for t := range bm.numTenants {
				tenant := fmt.Sprintf("benchmark-tenant-%d", t)

				// Create partitions with streams
				for p := range bm.numPartitions {
					for j := range bm.streamsPerPartition {
						// Create stream with multiple rate buckets
						stream := Stream{
							Hash:        uint64(j),
							LastSeenAt:  now.Add(-time.Duration(j) * time.Minute).UnixNano(),
							TotalSize:   uint64(1000 * (j + 1)),
							RateBuckets: make([]RateBucket, rateBuckets),
						}

						// Add rate buckets with some inside and outside the rate window
						for k := range rateBuckets {
							bucketTime := now.Add(-time.Duration(k*2) * time.Minute)
							stream.RateBuckets[k] = RateBucket{
								Timestamp: bucketTime.UnixNano(),
								Size:      uint64(500 * (k + 1)),
							}
						}

						bucketStart := stream.RateBuckets[0].Timestamp
						bucketCutOff := stream.RateBuckets[len(stream.RateBuckets)-1].Timestamp

						s.Store(tenant, int32(p), stream.Hash, stream.TotalSize, stream.LastSeenAt, bucketStart, bucketCutOff, false)
					}
				}
			}

			b.ResetTimer()

			// Run parallel benchmark
			b.RunParallel(func(pb *testing.PB) {
				i := 0
				for pb.Next() {
					tenant := fmt.Sprintf("benchmark-tenant-%d", i%bm.numTenants)
					partition := int32(i % bm.numPartitions)
					streamIdx := i % bm.streamsPerPartition

					updateTime := now.Add(time.Duration(i) * time.Second)
					metadata := &logproto.StreamMetadata{
						StreamHash:             uint64(streamIdx),
						EntriesSize:            1000,
						StructuredMetadataSize: 500,
					}

					bucketStart := updateTime.Truncate(bucketDuration).UnixNano()
					bucketCutOff := updateTime.Add(-rateWindow).UnixNano()
					totalSize := metadata.EntriesSize + metadata.StructuredMetadataSize

					s.Store(tenant, partition, metadata.StreamHash, totalSize, updateTime.UnixNano(), bucketStart, bucketCutOff, false)
					i++
				}
			})
		})
	}
}
