package limits

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/prometheus/client_golang/prometheus"
)

func BenchmarkIngestLimits_updateMetadata(b *testing.B) {
	const (
		windowSize  = time.Hour
		rateWindow  = 5 * time.Minute
		rateBuckets = 5 // One bucket per minute in the 5-minute rate window
	)

	benchmarks := []struct {
		name           string
		numPartitions  int
		streamsPerPart int
		numTenants     int
	}{
		{
			name:           "4_partitions_small_streams_single_tenant",
			numPartitions:  4,
			streamsPerPart: 500,
			numTenants:     1,
		},
		{
			name:           "8_partitions_medium_streams_multi_tenant",
			numPartitions:  8,
			streamsPerPart: 1000,
			numTenants:     10,
		},
		{
			name:           "16_partitions_large_streams_multi_tenant",
			numPartitions:  16,
			streamsPerPart: 5000,
			numTenants:     50,
		},
		{
			name:           "32_partitions_xlarge_streams_multi_tenant",
			numPartitions:  32,
			streamsPerPart: 10000,
			numTenants:     100,
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			// Setup IngestLimits instance
			s := &IngestLimits{
				cfg: Config{
					WindowSize:     windowSize,
					RateWindow:     rateWindow,
					BucketDuration: time.Minute,
				},
				metadata:         make(map[string]map[int32][]streamMetadata),
				metrics:          newMetrics(prometheus.NewRegistry()),
				partitionManager: NewPartitionManager(log.NewNopLogger()),
			}

			// Initialize metadata with existing streams and buckets
			now := time.Now()

			// Initialize metadata for each tenant
			for t := 0; t < bm.numTenants; t++ {
				tenant := fmt.Sprintf("benchmark-tenant-%d", t)
				s.metadata[tenant] = make(map[int32][]streamMetadata)

				// Create partitions with streams
				for p := int32(0); p < int32(bm.numPartitions); p++ {
					s.metadata[tenant][p] = make([]streamMetadata, bm.streamsPerPart)
					for i := 0; i < bm.streamsPerPart; i++ {
						// Create stream with multiple rate buckets
						stream := streamMetadata{
							hash:        uint64(i),
							lastSeenAt:  now.Add(-time.Duration(i) * time.Minute).UnixNano(),
							totalSize:   uint64(1000 * (i + 1)),
							rateBuckets: make([]rateBucket, rateBuckets),
						}

						// Add rate buckets with some inside and outside the rate window
						for j := 0; j < rateBuckets; j++ {
							bucketTime := now.Add(-time.Duration(j*2) * time.Minute)
							stream.rateBuckets[j] = rateBucket{
								timestamp: bucketTime.UnixNano(),
								size:      uint64(500 * (j + 1)),
							}
						}
						s.metadata[tenant][p][i] = stream
					}
				}
			}

			// Assign partitions
			partitions := make(map[string][]int32)
			partitions["test"] = make([]int32, bm.numPartitions)
			for i := 0; i < bm.numPartitions; i++ {
				partitions["test"] = append(partitions["test"], int32(i))
			}
			s.partitionManager.Assign(context.Background(), nil, partitions)

			// Reset timer before the actual benchmark
			b.ResetTimer()

			// Run the benchmark
			for i := 0; i < b.N; i++ {
				// For each iteration, update a random stream in a random partition for a random tenant
				tenant := fmt.Sprintf("benchmark-tenant-%d", i%bm.numTenants)
				partition := int32(i % bm.numPartitions)
				streamIdx := i % bm.streamsPerPart

				updateTime := now.Add(time.Duration(i) * time.Second)
				metadata := &logproto.StreamMetadata{
					StreamHash:             uint64(streamIdx),
					EntriesSize:            1000,
					StructuredMetadataSize: 500,
				}

				s.updateMetadata(metadata, tenant, partition, updateTime)
			}
		})

		// Run parallel benchmark
		b.Run(bm.name+"_parallel", func(b *testing.B) {
			// Setup similar to above
			s := &IngestLimits{
				cfg: Config{
					WindowSize:     windowSize,
					RateWindow:     rateWindow,
					BucketDuration: time.Minute,
				},
				metadata:         make(map[string]map[int32][]streamMetadata),
				metrics:          newMetrics(prometheus.NewRegistry()),
				partitionManager: NewPartitionManager(log.NewNopLogger()),
			}

			// Initialize metadata for each tenant
			now := time.Now()
			for t := 0; t < bm.numTenants; t++ {
				tenant := fmt.Sprintf("benchmark-tenant-%d", t)
				s.metadata[tenant] = make(map[int32][]streamMetadata)

				for p := int32(0); p < int32(bm.numPartitions); p++ {
					s.metadata[tenant][p] = make([]streamMetadata, bm.streamsPerPart)
					for i := 0; i < bm.streamsPerPart; i++ {
						stream := streamMetadata{
							hash:        uint64(i),
							lastSeenAt:  now.Add(-time.Duration(i) * time.Minute).UnixNano(),
							totalSize:   uint64(1000 * (i + 1)),
							rateBuckets: make([]rateBucket, rateBuckets),
						}

						for j := 0; j < rateBuckets; j++ {
							bucketTime := now.Add(-time.Duration(j*2) * time.Minute)
							stream.rateBuckets[j] = rateBucket{
								timestamp: bucketTime.UnixNano(),
								size:      uint64(500 * (j + 1)),
							}
						}
						s.metadata[tenant][p][i] = stream
					}
				}
			}

			// Assign partitions
			partitions := make(map[string][]int32)
			partitions["test"] = make([]int32, bm.numPartitions)
			for i := 0; i < bm.numPartitions; i++ {
				partitions["test"] = append(partitions["test"], int32(i))
			}
			s.partitionManager.Assign(context.Background(), nil, partitions)

			b.ResetTimer()

			// Run parallel benchmark
			b.RunParallel(func(pb *testing.PB) {
				i := 0
				for pb.Next() {
					tenant := fmt.Sprintf("benchmark-tenant-%d", i%bm.numTenants)
					partition := int32(i % bm.numPartitions)
					streamIdx := i % bm.streamsPerPart

					updateTime := now.Add(time.Duration(i) * time.Second)
					metadata := &logproto.StreamMetadata{
						StreamHash:             uint64(streamIdx),
						EntriesSize:            1000,
						StructuredMetadataSize: 500,
					}

					s.updateMetadata(metadata, tenant, partition, updateTime)
					i++
				}
			})
		})
	}
}
