package limits

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/stretchr/testify/require"
)

func TestStreamMetadata_All(t *testing.T) {
	now := time.Now()
	m := NewStreamMetadata(10)

	for i := range 10 {
		m.Upsert("tenant1", int32(i), uint64(i), now.UnixNano(), 1000, now.Truncate(time.Minute).UnixNano(), now.Add(-time.Hour).UnixNano())
	}

	expected := []uint64{0x0, 0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8, 0x9}

	actual := make([]uint64, 0, len(expected))
	m.All(func(stream streamMetadata, _ string, _ int32) {
		actual = append(actual, stream.hash)
	})

	require.ElementsMatch(t, expected, actual)
}

func TestStreamMetadata_All_Concurrent(t *testing.T) {
	now := time.Now()
	m := NewStreamMetadata(10)

	for i := range 10 {
		tenant := fmt.Sprintf("tenant%d", i)
		m.Upsert(tenant, 0, uint64(i), now.UnixNano(), 1000, now.Truncate(time.Minute).UnixNano(), now.Add(-time.Hour).UnixNano())
	}

	expected := []uint64{0x0, 0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8, 0x9}

	actual := make([]uint64, 10)

	wg := sync.WaitGroup{}
	wg.Add(10)
	for i := range 10 {
		go func(i int) {
			defer wg.Done()
			m.All(func(stream streamMetadata, tenant string, partitionID int32) {
				if tenant == fmt.Sprintf("tenant%d", i) {
					actual[i] = stream.hash
				}
			})
		}(i)
	}
	wg.Wait()

	require.ElementsMatch(t, expected, actual)
}

func TestStreamMetadata_Collect(t *testing.T) {
	now := time.Now()
	m := NewStreamMetadata(10)

	for i := range 10 {
		if i%2 == 0 {
			m.Upsert("tenant1", int32(i), uint64(i), now.UnixNano(), 1000, now.Truncate(time.Minute).UnixNano(), now.Add(-time.Hour).UnixNano())
		} else {
			m.Upsert("tenant2", int32(i), uint64(i), now.UnixNano(), 1000, now.Truncate(time.Minute).UnixNano(), now.Add(-time.Hour).UnixNano())
		}
	}

	expected := []uint64{0x0, 0x2, 0x4, 0x6, 0x8}

	actual := make([]uint64, 0, 5)
	m.Collect("tenant1", func(stream streamMetadata, partitionID int32) {
		actual = append(actual, stream.hash)
	})

	require.ElementsMatch(t, expected, actual)
}

func TestStreamMetadata_Collect_Concurrent(t *testing.T) {
	now := time.Now()
	m := NewStreamMetadata(10)

	for i := range 10 {
		if i%2 == 0 {
			m.Upsert("tenant1", int32(i), uint64(i), now.UnixNano(), 1000, now.Truncate(time.Minute).UnixNano(), now.Add(-time.Hour).UnixNano())
		} else {
			m.Upsert("tenant2", int32(i), uint64(i), now.UnixNano(), 1000, now.Truncate(time.Minute).UnixNano(), now.Add(-time.Hour).UnixNano())
		}
	}

	expected := []int{5, 5, 5, 5, 5, 5, 5, 5, 5, 5}

	wg := sync.WaitGroup{}
	wg.Add(10)

	actual := make([]int, 10)
	for i := range 10 {
		go func(i int) {
			defer wg.Done()
			m.Collect("tenant1", func(stream streamMetadata, partitionID int32) {
				if partitionID%2 == 0 {
					actual[i]++
				}
			})
		}(i)
	}
	wg.Wait()

	require.ElementsMatch(t, expected, actual)
}

func TestStreamMetadata_Upsert(t *testing.T) {
	var (
		bucketDuration = time.Minute
		rateWindow     = 5 * time.Minute
	)

	tests := []struct {
		name string

		// Setup data.
		metadata StreamMetadata

		// The test case.
		tenantID    string
		partitionID int32
		lastSeenAt  time.Time
		record      *logproto.StreamMetadata

		// Expectations.
		expected map[string]map[int32][]streamMetadata
	}{
		{
			name:        "insert new tenant and new partition",
			metadata:    NewStreamMetadata(1),
			tenantID:    "tenant1",
			partitionID: 0,
			lastSeenAt:  time.Unix(100, 0),
			record: &logproto.StreamMetadata{
				StreamHash:             123,
				EntriesSize:            1000,
				StructuredMetadataSize: 500,
			},
			expected: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: {
						{
							hash:       123,
							lastSeenAt: time.Unix(100, 0).UnixNano(),
							totalSize:  1500,
							rateBuckets: []rateBucket{
								{timestamp: time.Unix(100, 0).Truncate(time.Minute).UnixNano(), size: 1500},
							},
						},
					},
				},
			},
		},
		{
			name: "insert existing tenant and new partition",
			metadata: &streamMetadataStripes{
				stripes: []map[string]map[int32][]streamMetadata{
					{
						"tenant1": {
							0: {
								{
									hash:       123,
									lastSeenAt: time.Unix(100, 0).UnixNano(),
									totalSize:  1000,
									rateBuckets: []rateBucket{
										{timestamp: time.Unix(100, 0).Truncate(time.Minute).UnixNano(), size: 1000},
									},
								},
							},
						},
					},
					{},
				},
				locks: make([]stripeLock, 2),
			},
			tenantID:    "tenant1",
			partitionID: 1,
			record: &logproto.StreamMetadata{
				StreamHash:             456,
				EntriesSize:            2000,
				StructuredMetadataSize: 1000,
			},
			lastSeenAt: time.Unix(200, 0),
			expected: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: {
						{
							hash:       123,
							lastSeenAt: time.Unix(100, 0).UnixNano(),
							totalSize:  1000,
							rateBuckets: []rateBucket{
								{timestamp: time.Unix(100, 0).Truncate(time.Minute).UnixNano(), size: 1000},
							},
						},
					},
					1: {
						{
							hash:       456,
							lastSeenAt: time.Unix(200, 0).UnixNano(),
							totalSize:  3000,
							rateBuckets: []rateBucket{
								{timestamp: time.Unix(200, 0).Truncate(time.Minute).UnixNano(), size: 3000},
							},
						},
					},
				},
			},
		},
		{
			name: "update existing stream",
			metadata: &streamMetadataStripes{
				stripes: []map[string]map[int32][]streamMetadata{
					{
						"tenant1": {
							0: {
								{
									hash:       123,
									lastSeenAt: time.Unix(100, 0).UnixNano(),
									totalSize:  1000,
									rateBuckets: []rateBucket{
										{timestamp: time.Unix(100, 0).Truncate(time.Minute).UnixNano(), size: 1000},
									},
								},
							},
						},
					},
				},
				locks: make([]stripeLock, 1),
			},
			tenantID:    "tenant1",
			partitionID: 0,
			record: &logproto.StreamMetadata{
				StreamHash:             123,
				EntriesSize:            3000,
				StructuredMetadataSize: 1500,
			},
			lastSeenAt: time.Unix(300, 0),
			expected: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: {
						{
							hash:       123,
							lastSeenAt: time.Unix(300, 0).UnixNano(),
							totalSize:  5500,
							rateBuckets: []rateBucket{
								{timestamp: time.Unix(100, 0).Truncate(time.Minute).UnixNano(), size: 1000},
								{timestamp: time.Unix(300, 0).Truncate(time.Minute).UnixNano(), size: 4500},
							},
						},
					},
				},
			},
		},
		{
			name:     "update existing bucket",
			tenantID: "tenant1",
			record: &logproto.StreamMetadata{
				StreamHash:             888,
				EntriesSize:            1000,
				StructuredMetadataSize: 500,
			},
			lastSeenAt: time.Unix(852, 0),
			metadata: &streamMetadataStripes{
				stripes: []map[string]map[int32][]streamMetadata{
					{
						"tenant1": {
							0: {
								{
									hash:       888,
									lastSeenAt: time.Unix(850, 0).UnixNano(),
									totalSize:  1500,
									rateBuckets: []rateBucket{
										{timestamp: time.Unix(850, 0).Truncate(time.Minute).UnixNano(), size: 1500},
									},
								},
							},
						},
					},
				},
				locks: make([]stripeLock, 1),
			},
			expected: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: {
						{
							hash:       888,
							lastSeenAt: time.Unix(852, 0).UnixNano(),
							totalSize:  3000,
							rateBuckets: []rateBucket{
								{timestamp: time.Unix(850, 0).Truncate(time.Minute).UnixNano(), size: 3000},
							},
						},
					},
				},
			},
		},
		{
			name:     "clean up buckets outside rate window",
			tenantID: "tenant1",
			record: &logproto.StreamMetadata{
				StreamHash:             999,
				EntriesSize:            2000,
				StructuredMetadataSize: 1000,
			},
			lastSeenAt: time.Unix(1000, 0), // Current time reference
			metadata: &streamMetadataStripes{
				stripes: []map[string]map[int32][]streamMetadata{
					{
						"tenant1": {
							0: {
								{
									hash:       999,
									lastSeenAt: time.Unix(950, 0).UnixNano(),
									totalSize:  5000,
									rateBuckets: []rateBucket{
										{timestamp: time.Unix(1000, 0).Add(-5 * time.Minute).Truncate(time.Minute).UnixNano(), size: 1000},  // Old, outside window
										{timestamp: time.Unix(1000, 0).Add(-10 * time.Minute).Truncate(time.Minute).UnixNano(), size: 1500}, // Outside rate window (>5 min old from 1000)
										{timestamp: time.Unix(950, 0).Truncate(time.Minute).UnixNano(), size: 2500},                         // Recent, within window
									},
								},
							},
						},
					},
				},
				locks: make([]stripeLock, 1),
			},
			expected: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: {
						{
							hash:       999,
							lastSeenAt: time.Unix(1000, 0).UnixNano(),
							totalSize:  8000, // Old total + new 3000
							rateBuckets: []rateBucket{
								{timestamp: time.Unix(950, 0).Truncate(time.Minute).UnixNano(), size: 2500},
								{timestamp: time.Unix(1000, 0).Truncate(time.Minute).UnixNano(), size: 3000},
							},
						},
					},
				},
			},
		},
		{
			name:     "update same minute bucket",
			tenantID: "tenant1",
			record: &logproto.StreamMetadata{
				StreamHash:             555,
				EntriesSize:            1000,
				StructuredMetadataSize: 500,
			},
			lastSeenAt: time.Unix(1100, 0),
			metadata: &streamMetadataStripes{
				stripes: []map[string]map[int32][]streamMetadata{
					{
						"tenant1": {
							0: {
								{
									hash:       555,
									lastSeenAt: time.Unix(1080, 0).UnixNano(), // Same minute as new data
									totalSize:  2000,
									rateBuckets: []rateBucket{
										{timestamp: time.Unix(1080, 0).Truncate(time.Minute).UnixNano(), size: 2000},
									},
								},
							},
						},
					},
				},
				locks: make([]stripeLock, 1),
			},
			expected: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: {
						{
							hash:       555,
							lastSeenAt: time.Unix(1100, 0).UnixNano(),
							totalSize:  3500, // 2000 + 1500
							rateBuckets: []rateBucket{
								// Same bucket as before but updated with new size
								{timestamp: time.Unix(1100, 0).Truncate(time.Minute).UnixNano(), size: 3500},
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			totalSize := tt.record.EntriesSize + tt.record.StructuredMetadataSize
			bucketStart := tt.lastSeenAt.Truncate(bucketDuration).UnixNano()
			bucketCutOff := tt.lastSeenAt.Add(-rateWindow).UnixNano()

			tt.metadata.Upsert(tt.tenantID, tt.partitionID, tt.record.StreamHash, tt.lastSeenAt.UnixNano(), totalSize, bucketStart, bucketCutOff)

			tt.metadata.All(func(stream streamMetadata, tenant string, partitionID int32) {
				require.Contains(t, tt.expected, tenant)
				require.Contains(t, tt.expected[tenant], partitionID)
				require.Contains(t, tt.expected[tenant][partitionID], stream)
			})
		})
	}
}

func TestStreamMetadata_Evict(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name                 string
		metadata             *streamMetadataStripes
		cutOff               int64
		assignedPartitionIDs []int32
		expectedMetadata     map[string]map[int32][]streamMetadata
		expectedEvictions    map[string]int
	}{
		{
			name: "all streams active",
			metadata: &streamMetadataStripes{
				stripes: []map[string]map[int32][]streamMetadata{
					{
						"tenant1": {
							0: []streamMetadata{
								{hash: 1, lastSeenAt: now.UnixNano(), totalSize: 1000},
								{hash: 2, lastSeenAt: now.UnixNano(), totalSize: 2000},
							},
						},
					},
				},
				locks: make([]stripeLock, 1),
			},
			cutOff:               now.Add(-time.Hour).UnixNano(),
			assignedPartitionIDs: []int32{0},
			expectedMetadata: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: []streamMetadata{
						{hash: 1, lastSeenAt: now.UnixNano(), totalSize: 1000},
						{hash: 2, lastSeenAt: now.UnixNano(), totalSize: 2000},
					},
				},
			},
			expectedEvictions: map[string]int{},
		},
		{
			name: "all streams expired",
			metadata: &streamMetadataStripes{
				stripes: []map[string]map[int32][]streamMetadata{
					{
						"tenant1": {
							0: []streamMetadata{
								{hash: 1, lastSeenAt: now.Add(-2 * time.Hour).UnixNano(), totalSize: 1000},
								{hash: 2, lastSeenAt: now.Add(-2 * time.Hour).UnixNano(), totalSize: 2000},
							},
						},
					},
				},
				locks: make([]stripeLock, 1),
			},
			cutOff:               now.Add(-time.Hour).UnixNano(),
			assignedPartitionIDs: []int32{0},
			expectedMetadata:     map[string]map[int32][]streamMetadata{},
			expectedEvictions: map[string]int{
				"tenant1": 2,
			},
		},
		{
			name: "mixed active and expired streams",
			metadata: &streamMetadataStripes{
				stripes: []map[string]map[int32][]streamMetadata{
					{
						"tenant1": {
							0: []streamMetadata{
								{hash: 1, lastSeenAt: now.UnixNano(), totalSize: 1000},
								{hash: 2, lastSeenAt: now.Add(-2 * time.Hour).UnixNano(), totalSize: 2000},
								{hash: 3, lastSeenAt: now.UnixNano(), totalSize: 3000},
							},
						},
					},
				},
				locks: make([]stripeLock, 1),
			},
			cutOff:               now.Add(-time.Hour).UnixNano(),
			assignedPartitionIDs: []int32{0},
			expectedMetadata: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: []streamMetadata{
						{hash: 1, lastSeenAt: now.UnixNano(), totalSize: 1000},
						{hash: 3, lastSeenAt: now.UnixNano(), totalSize: 3000},
					},
				},
			},
			expectedEvictions: map[string]int{
				"tenant1": 1,
			},
		},
		{
			name: "multiple tenants with mixed streams",
			metadata: &streamMetadataStripes{
				stripes: []map[string]map[int32][]streamMetadata{
					{
						"tenant1": {
							0: []streamMetadata{
								{hash: 1, lastSeenAt: now.UnixNano(), totalSize: 1000},
								{hash: 2, lastSeenAt: now.Add(-2 * time.Hour).UnixNano(), totalSize: 2000},
							},
						},
						"tenant2": {
							0: []streamMetadata{
								{hash: 3, lastSeenAt: now.Add(-2 * time.Hour).UnixNano(), totalSize: 3000},
								{hash: 4, lastSeenAt: now.Add(-2 * time.Hour).UnixNano(), totalSize: 4000},
							},
						},
						"tenant3": {
							0: []streamMetadata{
								{hash: 5, lastSeenAt: now.UnixNano(), totalSize: 5000},
							},
						},
					},
				},
				locks: make([]stripeLock, 1),
			},
			cutOff:               now.Add(-time.Hour).UnixNano(),
			assignedPartitionIDs: []int32{0},
			expectedMetadata: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: []streamMetadata{
						{hash: 1, lastSeenAt: now.UnixNano(), totalSize: 1000},
					},
				},
				"tenant3": {
					0: []streamMetadata{
						{hash: 5, lastSeenAt: now.UnixNano(), totalSize: 5000},
					},
				},
			},
			expectedEvictions: map[string]int{
				"tenant1": 1,
				"tenant2": 2,
			},
		},
		{
			name: "multiple partitions with some empty after eviction",
			metadata: &streamMetadataStripes{
				stripes: []map[string]map[int32][]streamMetadata{
					{
						"tenant1": {
							0: []streamMetadata{
								{hash: 1, lastSeenAt: now.UnixNano(), totalSize: 1000},
								{hash: 2, lastSeenAt: now.Add(-2 * time.Hour).UnixNano(), totalSize: 2000},
							},
						},
					},
					{
						"tenant1": {
							1: []streamMetadata{
								{hash: 3, lastSeenAt: now.Add(-2 * time.Hour).UnixNano(), totalSize: 3000},
							},
						},
					},
					{
						"tenant1": {
							2: []streamMetadata{
								{hash: 4, lastSeenAt: now.UnixNano(), totalSize: 4000},
							},
						},
					},
				},
				locks: make([]stripeLock, 3),
			},
			cutOff:               now.Add(-time.Hour).UnixNano(),
			assignedPartitionIDs: []int32{0, 1, 2},
			expectedMetadata: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: []streamMetadata{
						{hash: 1, lastSeenAt: now.UnixNano(), totalSize: 1000},
					},
					2: []streamMetadata{
						{hash: 4, lastSeenAt: now.UnixNano(), totalSize: 4000},
					},
				},
			},
			expectedEvictions: map[string]int{
				"tenant1": 2,
			},
		},
		{
			name: "unassigned partitions should still be evicted",
			metadata: &streamMetadataStripes{
				stripes: []map[string]map[int32][]streamMetadata{
					{
						"tenant1": {
							0: []streamMetadata{
								{hash: 1, lastSeenAt: now.UnixNano(), totalSize: 1000},
							},
						},
					},
					{
						"tenant1": {
							1: []streamMetadata{
								{hash: 2, lastSeenAt: now.Add(-2 * time.Hour).UnixNano(), totalSize: 2000},
							},
						},
					},
				},
				locks: make([]stripeLock, 2),
			},
			cutOff:               now.Add(-time.Hour).UnixNano(),
			assignedPartitionIDs: []int32{0},
			expectedMetadata: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: []streamMetadata{
						{hash: 1, lastSeenAt: now.UnixNano(), totalSize: 1000},
					},
				},
			},
			expectedEvictions: map[string]int{
				"tenant1": 1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actualEvictions := tt.metadata.Evict(tt.cutOff)

			actualMetadata := make(map[string]map[int32][]streamMetadata)
			tt.metadata.All(func(stream streamMetadata, tenant string, partitionID int32) {
				if actualMetadata[tenant] == nil {
					actualMetadata[tenant] = make(map[int32][]streamMetadata)
				}
				if actualMetadata[tenant][partitionID] == nil {
					actualMetadata[tenant][partitionID] = make([]streamMetadata, 0)
				}
				actualMetadata[tenant][partitionID] = append(actualMetadata[tenant][partitionID], stream)
			})

			require.Equal(t, tt.expectedEvictions, actualEvictions)
			require.Equal(t, tt.expectedMetadata, actualMetadata)
		})
	}
}
func TestStreamMetadata_EvictPartitions(t *testing.T) {
	numPartitions := 10
	m := NewStreamMetadata(numPartitions)

	for i := range numPartitions {
		m.Upsert("tenant1", int32(i), 1, time.Now().UnixNano(), 1000, time.Now().Truncate(time.Minute).UnixNano(), time.Now().Add(-time.Hour).UnixNano())
	}

	m.EvictPartitions([]int32{1, 3, 5, 7, 9})

	expected := []int32{0, 2, 4, 6, 8}
	actual := make([]int32, 0, len(expected))
	m.All(func(stream streamMetadata, tenant string, partitionID int32) {
		actual = append(actual, partitionID)
	})
	require.ElementsMatch(t, expected, actual)
}

func TestStreamMetadata_EvictPartitions_Concurrent(t *testing.T) {
	numPartitions := 10
	m := NewStreamMetadata(numPartitions)

	for i := range numPartitions {
		m.Upsert("tenant1", int32(i), 1, time.Now().UnixNano(), 1000, time.Now().Truncate(time.Minute).UnixNano(), time.Now().Add(-time.Hour).UnixNano())
	}

	wg := sync.WaitGroup{}
	wg.Add(numPartitions / 2)
	for i := range numPartitions {
		if i%2 == 0 {
			continue
		}
		go func(i int) {
			defer wg.Done()
			m.EvictPartitions([]int32{int32(i)})
		}(i)
	}
	wg.Wait()

	expected := []int32{0, 2, 4, 6, 8}
	actual := make([]int32, 0, len(expected))
	m.All(func(stream streamMetadata, tenant string, partitionID int32) {
		actual = append(actual, partitionID)
	})
	require.ElementsMatch(t, expected, actual)
}
