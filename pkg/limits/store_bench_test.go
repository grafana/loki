package limits

import (
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/limits/proto"
)

func BenchmarkUsageStore_Store(b *testing.B) {
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
		s, err := newUsageStore(DefaultActiveWindow, DefaultRateWindow, DefaultBucketSize, bm.numPartitions, prometheus.NewRegistry())
		require.NoError(b, err)
		b.Run(fmt.Sprintf("%s_create", bm.name), func(b *testing.B) {
			now := time.Now()

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

				_, _, _, err := s.UpdateCond(tenant, metadata, updateTime, nil)
				require.NoError(b, err)
			}
		})

		b.Run(fmt.Sprintf("%s_update", bm.name), func(b *testing.B) {
			now := time.Now()

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

				_, _, _, err := s.UpdateCond(tenant, metadata, updateTime, nil)
				require.NoError(b, err)
			}
		})

		s, err = newUsageStore(DefaultActiveWindow, DefaultRateWindow, DefaultBucketSize, bm.numPartitions, prometheus.NewRegistry())
		require.NoError(b, err)

		// Run parallel benchmark
		b.Run(bm.name+"_create_parallel", func(b *testing.B) {
			now := time.Now()
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

					_, _, _, err := s.UpdateCond(tenant, metadata, updateTime, nil)
					require.NoError(b, err)
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
					streamIdx := i % bm.streamsPerPartition

					updateTime := now.Add(time.Duration(i) * time.Second)
					metadata := []*proto.StreamMetadata{{
						StreamHash: uint64(streamIdx),
						TotalSize:  1500,
					}}

					_, _, _, err := s.UpdateCond(tenant, metadata, updateTime, nil)
					require.NoError(b, err)
					i++
				}
			})
		})
	}
}
