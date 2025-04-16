package limits

// import (
// 	"context"
// 	"fmt"
// 	"testing"
// 	"time"

// 	"github.com/go-kit/log"
// 	"github.com/prometheus/client_golang/prometheus"

// 	"github.com/grafana/loki/v3/pkg/logproto"
// )

// func BenchmarkIngestLimits_updateMetadata(b *testing.B) {
// 	const (
// 		windowSize  = time.Hour
// 		rateWindow  = 5 * time.Minute
// 		rateBuckets = 5 // One bucket per minute in the 5-minute rate window
// 	)

// 	benchmarks := []struct {
// 		name                string
// 		numTenants          int
// 		numPartitions       int
// 		streamsPerPartition int
// 	}{
// 		{
// 			name:                "4_partitions_small_streams_single_tenant",
// 			numTenants:          1,
// 			numPartitions:       4,
// 			streamsPerPartition: 500,
// 		},
// 		{
// 			name:                "8_partitions_medium_streams_multi_tenant",
// 			numTenants:          10,
// 			numPartitions:       8,
// 			streamsPerPartition: 1000,
// 		},
// 		{
// 			name:                "16_partitions_large_streams_multi_tenant",
// 			numTenants:          50,
// 			numPartitions:       16,
// 			streamsPerPartition: 5000,
// 		},
// 		{
// 			name:                "32_partitions_xlarge_streams_multi_tenant",
// 			numTenants:          100,
// 			numPartitions:       32,
// 			streamsPerPartition: 10000,
// 		},
// 	}

// 	for _, bm := range benchmarks {
// 		b.Run(bm.name, func(b *testing.B) {
// 			// Setup IngestLimits instance
// 			s := &IngestLimits{
// 				cfg: Config{
// 					WindowSize:     windowSize,
// 					RateWindow:     rateWindow,
// 					BucketDuration: time.Minute,
// 				},
// 				metadata:         NewStreamMetadata(bm.numPartitions),
// 				metrics:          newMetrics(prometheus.NewRegistry()),
// 				partitionManager: NewPartitionManager(log.NewNopLogger()),
// 			}

// 			// Initialize metadata with existing streams and buckets
// 			now := time.Now()

// 			// Initialize metadata for each tenant
// 			for t := range bm.numTenants {
// 				tenant := fmt.Sprintf("benchmark-tenant-%d", t)

// 				// Create partitions with streams
// 				for p := range bm.numPartitions {
// 					i := uint64(p) & uint64(len(s.metadata.locks)-1)

// 					if _, ok := s.metadata.stripes[i][tenant]; !ok {
// 						s.metadata.stripes[i][tenant] = make(map[int32][]streamMetadata)
// 					}

// 					if _, ok := s.metadata.stripes[i][tenant][int32(p)]; !ok {
// 						s.metadata.stripes[i][tenant][int32(p)] = make([]streamMetadata, bm.streamsPerPartition)
// 					}

// 					for j := range bm.streamsPerPartition {
// 						// Create stream with multiple rate buckets
// 						stream := streamMetadata{
// 							hash:        uint64(j),
// 							lastSeenAt:  now.Add(-time.Duration(j) * time.Minute).UnixNano(),
// 							totalSize:   uint64(1000 * (j + 1)),
// 							rateBuckets: make([]rateBucket, rateBuckets),
// 						}

// 						// Add rate buckets with some inside and outside the rate window
// 						for k := range rateBuckets {
// 							bucketTime := now.Add(-time.Duration(k*2) * time.Minute)
// 							stream.rateBuckets[k] = rateBucket{
// 								timestamp: bucketTime.UnixNano(),
// 								size:      uint64(500 * (k + 1)),
// 							}
// 						}
// 						s.metadata.stripes[i][tenant][int32(p)][j] = stream
// 					}
// 				}
// 			}

// 			// Assign partitions
// 			partitions := make(map[string][]int32)
// 			partitions["test"] = make([]int32, bm.numPartitions)
// 			for i := 0; i < bm.numPartitions; i++ {
// 				partitions["test"] = append(partitions["test"], int32(i))
// 			}
// 			s.partitionManager.Assign(context.Background(), nil, partitions)

// 			// Reset timer before the actual benchmark
// 			b.ResetTimer()

// 			// Run the benchmark
// 			for i := 0; i < b.N; i++ {
// 				// For each iteration, update a random stream in a random partition for a random tenant
// 				tenant := fmt.Sprintf("benchmark-tenant-%d", i%bm.numTenants)
// 				partition := int32(i % bm.numPartitions)
// 				streamIdx := i % bm.streamsPerPartition

// 				updateTime := now.Add(time.Duration(i) * time.Second)
// 				metadata := &logproto.StreamMetadata{
// 					StreamHash:             uint64(streamIdx),
// 					EntriesSize:            1000,
// 					StructuredMetadataSize: 500,
// 				}

// 				s.updateMetadata(metadata, tenant, partition, updateTime)
// 			}
// 		})

// 		// Run parallel benchmark
// 		b.Run(bm.name+"_parallel", func(b *testing.B) {
// 			// Setup similar to above
// 			s := &IngestLimits{
// 				cfg: Config{
// 					WindowSize:     windowSize,
// 					RateWindow:     rateWindow,
// 					BucketDuration: time.Minute,
// 				},
// 				metadata:         NewStreamMetadata(bm.numPartitions),
// 				metrics:          newMetrics(prometheus.NewRegistry()),
// 				partitionManager: NewPartitionManager(log.NewNopLogger()),
// 			}

// 			// Initialize metadata for each tenant
// 			now := time.Now()

// 			// Initialize metadata for each tenant
// 			for t := range bm.numTenants {
// 				tenant := fmt.Sprintf("benchmark-tenant-%d", t)

// 				// Create partitions with streams
// 				for p := range bm.numPartitions {
// 					i := uint64(p) & uint64(len(s.metadata.locks)-1)

// 					if _, ok := s.metadata.stripes[i][tenant]; !ok {
// 						s.metadata.stripes[i][tenant] = make(map[int32][]streamMetadata)
// 					}

// 					if _, ok := s.metadata.stripes[i][tenant][int32(p)]; !ok {
// 						s.metadata.stripes[i][tenant][int32(p)] = make([]streamMetadata, bm.streamsPerPartition)
// 					}

// 					for j := range bm.streamsPerPartition {
// 						// Create stream with multiple rate buckets
// 						stream := streamMetadata{
// 							hash:        uint64(j),
// 							lastSeenAt:  now.Add(-time.Duration(j) * time.Minute).UnixNano(),
// 							totalSize:   uint64(1000 * (j + 1)),
// 							rateBuckets: make([]rateBucket, rateBuckets),
// 						}

// 						// Add rate buckets with some inside and outside the rate window
// 						for k := range rateBuckets {
// 							bucketTime := now.Add(-time.Duration(k*2) * time.Minute)
// 							stream.rateBuckets[k] = rateBucket{
// 								timestamp: bucketTime.UnixNano(),
// 								size:      uint64(500 * (k + 1)),
// 							}
// 						}
// 						s.metadata.stripes[i][tenant][int32(p)][j] = stream
// 					}
// 				}
// 			}

// 			// Assign partitions
// 			partitions := make(map[string][]int32)
// 			partitions["test"] = make([]int32, bm.numPartitions)
// 			for i := 0; i < bm.numPartitions; i++ {
// 				partitions["test"] = append(partitions["test"], int32(i))
// 			}
// 			s.partitionManager.Assign(context.Background(), nil, partitions)

// 			b.ResetTimer()

// 			// Run parallel benchmark
// 			b.RunParallel(func(pb *testing.PB) {
// 				i := 0
// 				for pb.Next() {
// 					tenant := fmt.Sprintf("benchmark-tenant-%d", i%bm.numTenants)
// 					partition := int32(i % bm.numPartitions)
// 					streamIdx := i % bm.streamsPerPartition

// 					updateTime := now.Add(time.Duration(i) * time.Second)
// 					metadata := &logproto.StreamMetadata{
// 						StreamHash:             uint64(streamIdx),
// 						EntriesSize:            1000,
// 						StructuredMetadataSize: 500,
// 					}

// 					s.updateMetadata(metadata, tenant, partition, updateTime)
// 					i++
// 				}
// 			})
// 		})
// 	}
// }
