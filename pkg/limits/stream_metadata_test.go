package limits

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestStreamMetadata_All(t *testing.T) {
	now := time.Now()
	m := NewStreamMetadata(10)

	for i := range 10 {
		m.Store("tenant1", int32(i), uint64(i), 1000, now.UnixNano(), now.Truncate(time.Minute).UnixNano(), now.Add(-time.Hour).UnixNano())
	}

	expected := []uint64{0x0, 0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8, 0x9}

	actual := make([]uint64, 0, len(expected))
	m.All(func(_ string, _ int32, stream Stream) {
		actual = append(actual, stream.Hash)
	})

	require.ElementsMatch(t, expected, actual)
}

func TestStreamMetadata_All_Concurrent(t *testing.T) {
	now := time.Now()
	m := NewStreamMetadata(10)

	for i := range 10 {
		tenant := fmt.Sprintf("tenant%d", i)
		m.Store(tenant, 0, uint64(i), 1000, now.UnixNano(), now.Truncate(time.Minute).UnixNano(), now.Add(-time.Hour).UnixNano())
	}

	expected := []uint64{0x0, 0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7, 0x8, 0x9}

	actual := make([]uint64, 10)

	wg := sync.WaitGroup{}
	wg.Add(10)
	for i := range 10 {
		go func(i int) {
			defer wg.Done()
			m.All(func(tenant string, _ int32, stream Stream) {
				if tenant == fmt.Sprintf("tenant%d", i) {
					actual[i] = stream.Hash
				}
			})
		}(i)
	}
	wg.Wait()

	require.ElementsMatch(t, expected, actual)
}

func TestStreamMetadata_Usage(t *testing.T) {
	now := time.Now()
	m := NewStreamMetadata(10)

	for i := range 10 {
		if i%2 == 0 {
			m.Store("tenant1", int32(i), uint64(i), 1000, now.UnixNano(), now.Truncate(time.Minute).UnixNano(), now.Add(-time.Hour).UnixNano())
		} else {
			m.Store("tenant2", int32(i), uint64(i), 1000, now.UnixNano(), now.Truncate(time.Minute).UnixNano(), now.Add(-time.Hour).UnixNano())
		}
	}

	expected := []uint64{0x0, 0x2, 0x4, 0x6, 0x8}

	actual := make([]uint64, 0, 5)
	m.Usage("tenant1", func(_ int32, stream Stream) {
		actual = append(actual, stream.Hash)
	})

	require.ElementsMatch(t, expected, actual)
}

func TestStreamMetadata_Usage_Concurrent(t *testing.T) {
	now := time.Now()
	m := NewStreamMetadata(10)

	for i := range 10 {
		if i%2 == 0 {
			m.Store("tenant1", int32(i), uint64(i), 1000, now.UnixNano(), now.Truncate(time.Minute).UnixNano(), now.Add(-time.Hour).UnixNano())
		} else {
			m.Store("tenant2", int32(i), uint64(i), 1000, now.UnixNano(), now.Truncate(time.Minute).UnixNano(), now.Add(-time.Hour).UnixNano())
		}
	}

	expected := []int{5, 5, 5, 5, 5, 5, 5, 5, 5, 5}

	wg := sync.WaitGroup{}
	wg.Add(10)

	actual := make([]int, 10)
	for i := range 10 {
		go func(i int) {
			defer wg.Done()
			m.Usage("tenant1", func(_ int32, stream Stream) {
				if stream.Hash%2 == 0 {
					actual[i]++
				}
			})
		}(i)
	}
	wg.Wait()

	require.ElementsMatch(t, expected, actual)
}

func TestStreamMetadata_Store(t *testing.T) {
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
		expected map[string]map[int32]map[uint64]Stream
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
			expected: map[string]map[int32]map[uint64]Stream{
				"tenant1": {
					0: {
						123: {
							Hash:       123,
							LastSeenAt: time.Unix(100, 0).UnixNano(),
							TotalSize:  1500,
							RateBuckets: []RateBucket{
								{Timestamp: time.Unix(100, 0).Truncate(time.Minute).UnixNano(), Size: 1500},
							},
						},
					},
				},
			},
		},
		{
			name: "insert existing tenant and new partition",
			metadata: &streamMetadata{
				stripes: []map[string]map[int32]map[uint64]Stream{
					{
						"tenant1": {
							0: {
								123: {
									Hash:       123,
									LastSeenAt: time.Unix(100, 0).UnixNano(),
									TotalSize:  1000,
									RateBuckets: []RateBucket{
										{Timestamp: time.Unix(100, 0).Truncate(time.Minute).UnixNano(), Size: 1000},
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
			expected: map[string]map[int32]map[uint64]Stream{
				"tenant1": {
					0: {
						123: {
							Hash:       123,
							LastSeenAt: time.Unix(100, 0).UnixNano(),
							TotalSize:  1000,
							RateBuckets: []RateBucket{
								{Timestamp: time.Unix(100, 0).Truncate(time.Minute).UnixNano(), Size: 1000},
							},
						},
					},
					1: {
						456: {
							Hash:       456,
							LastSeenAt: time.Unix(200, 0).UnixNano(),
							TotalSize:  3000,
							RateBuckets: []RateBucket{
								{Timestamp: time.Unix(200, 0).Truncate(time.Minute).UnixNano(), Size: 3000},
							},
						},
					},
				},
			},
		},
		{
			name: "update existing stream",
			metadata: &streamMetadata{
				stripes: []map[string]map[int32]map[uint64]Stream{
					{
						"tenant1": {
							0: {
								123: {
									Hash:       123,
									LastSeenAt: time.Unix(100, 0).UnixNano(),
									TotalSize:  1000,
									RateBuckets: []RateBucket{
										{Timestamp: time.Unix(100, 0).Truncate(time.Minute).UnixNano(), Size: 1000},
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
			expected: map[string]map[int32]map[uint64]Stream{
				"tenant1": {
					0: {
						123: {
							Hash:       123,
							LastSeenAt: time.Unix(300, 0).UnixNano(),
							TotalSize:  5500,
							RateBuckets: []RateBucket{
								{Timestamp: time.Unix(100, 0).Truncate(time.Minute).UnixNano(), Size: 1000},
								{Timestamp: time.Unix(300, 0).Truncate(time.Minute).UnixNano(), Size: 4500},
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
			metadata: &streamMetadata{
				stripes: []map[string]map[int32]map[uint64]Stream{
					{
						"tenant1": {
							0: {
								888: {
									Hash:       888,
									LastSeenAt: time.Unix(850, 0).UnixNano(),
									TotalSize:  1500,
									RateBuckets: []RateBucket{
										{Timestamp: time.Unix(850, 0).Truncate(time.Minute).UnixNano(), Size: 1500},
									},
								},
							},
						},
					},
				},
				locks: make([]stripeLock, 1),
			},
			expected: map[string]map[int32]map[uint64]Stream{
				"tenant1": {
					0: {
						888: {
							Hash:       888,
							LastSeenAt: time.Unix(852, 0).UnixNano(),
							TotalSize:  3000,
							RateBuckets: []RateBucket{
								{Timestamp: time.Unix(850, 0).Truncate(time.Minute).UnixNano(), Size: 3000},
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
			metadata: &streamMetadata{
				stripes: []map[string]map[int32]map[uint64]Stream{
					{
						"tenant1": {
							0: {
								999: {
									Hash:       999,
									LastSeenAt: time.Unix(950, 0).UnixNano(),
									TotalSize:  5000,
									RateBuckets: []RateBucket{
										{Timestamp: time.Unix(1000, 0).Add(-5 * time.Minute).Truncate(time.Minute).UnixNano(), Size: 1000},  // Old, outside window
										{Timestamp: time.Unix(1000, 0).Add(-10 * time.Minute).Truncate(time.Minute).UnixNano(), Size: 1500}, // Outside rate window (>5 min old from 1000)
										{Timestamp: time.Unix(950, 0).Truncate(time.Minute).UnixNano(), Size: 2500},                         // Recent, within window
									},
								},
							},
						},
					},
				},
				locks: make([]stripeLock, 1),
			},
			expected: map[string]map[int32]map[uint64]Stream{
				"tenant1": {
					0: {
						999: {
							Hash:       999,
							LastSeenAt: time.Unix(1000, 0).UnixNano(),
							TotalSize:  8000, // Old total + new 3000
							RateBuckets: []RateBucket{
								{Timestamp: time.Unix(950, 0).Truncate(time.Minute).UnixNano(), Size: 2500},
								{Timestamp: time.Unix(1000, 0).Truncate(time.Minute).UnixNano(), Size: 3000},
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
			metadata: &streamMetadata{
				stripes: []map[string]map[int32]map[uint64]Stream{
					{
						"tenant1": {
							0: {
								555: {
									Hash:       555,
									LastSeenAt: time.Unix(1080, 0).UnixNano(), // Same minute as new data
									TotalSize:  2000,
									RateBuckets: []RateBucket{
										{Timestamp: time.Unix(1080, 0).Truncate(time.Minute).UnixNano(), Size: 2000},
									},
								},
							},
						},
					},
				},
				locks: make([]stripeLock, 1),
			},
			expected: map[string]map[int32]map[uint64]Stream{
				"tenant1": {
					0: {
						555: {
							Hash:       555,
							LastSeenAt: time.Unix(1100, 0).UnixNano(),
							TotalSize:  3500, // 2000 + 1500
							RateBuckets: []RateBucket{
								// Same bucket as before but updated with new size
								{Timestamp: time.Unix(1100, 0).Truncate(time.Minute).UnixNano(), Size: 3500},
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

			tt.metadata.Store(tt.tenantID, tt.partitionID, tt.record.StreamHash, totalSize, tt.lastSeenAt.UnixNano(), bucketStart, bucketCutOff)

			tt.metadata.All(func(tenant string, partitionID int32, stream Stream) {
				require.Contains(t, tt.expected, tenant)
				require.Contains(t, tt.expected[tenant], partitionID)
				require.Contains(t, tt.expected[tenant][partitionID], stream.Hash)
			})
		})
	}
}

func TestStreamMetadata_Store_Concurrent(t *testing.T) {
	var (
		numTenants     = 6
		bucketDuration = time.Minute
		rateWindow     = 5 * time.Minute

		lastSeenAt   = time.Unix(100, 0)
		bucketStart  = lastSeenAt.Truncate(bucketDuration).UnixNano()
		bucketCutOff = lastSeenAt.Add(-rateWindow).UnixNano()
	)

	m := NewStreamMetadata(numTenants)

	wg := sync.WaitGroup{}
	wg.Add(numTenants)

	for i := range numTenants {
		go func(i int) {
			defer wg.Done()

			tenantID := fmt.Sprintf("tenant%d", i)
			partitionID := int32(0)
			if i%2 == 0 {
				partitionID = 1
			}

			record := &logproto.StreamMetadata{
				StreamHash:             uint64(i),
				EntriesSize:            1000,
				StructuredMetadataSize: 500,
			}

			totalSize := record.EntriesSize + record.StructuredMetadataSize
			m.Store(tenantID, partitionID, record.StreamHash, totalSize, lastSeenAt.UnixNano(), bucketStart, bucketCutOff)
		}(i)
	}
	wg.Wait()

	expected := map[string]map[int32][]Stream{
		"tenant0": {
			1: []Stream{
				{Hash: 0x0, LastSeenAt: lastSeenAt.UnixNano(), TotalSize: 1500, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 1500}}},
			},
		},
		"tenant1": {
			0: []Stream{
				{Hash: 0x1, LastSeenAt: lastSeenAt.UnixNano(), TotalSize: 1500, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 1500}}},
			},
		},
		"tenant2": {
			1: []Stream{
				{Hash: 0x2, LastSeenAt: lastSeenAt.UnixNano(), TotalSize: 1500, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 1500}}},
			},
		},
		"tenant3": {
			0: []Stream{
				{Hash: 0x3, LastSeenAt: lastSeenAt.UnixNano(), TotalSize: 1500, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 1500}}},
			},
		},
		"tenant4": {
			1: []Stream{
				{Hash: 0x4, LastSeenAt: lastSeenAt.UnixNano(), TotalSize: 1500, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 1500}}},
			},
		},
		"tenant5": {
			0: []Stream{
				{Hash: 0x5, LastSeenAt: lastSeenAt.UnixNano(), TotalSize: 1500, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 1500}}},
			},
		},
	}

	actual := make(map[string]map[int32][]Stream)
	m.All(func(tenant string, partitionID int32, stream Stream) {
		if _, ok := actual[tenant]; !ok {
			actual[tenant] = make(map[int32][]Stream)
		}
		actual[tenant][partitionID] = append(actual[tenant][partitionID], stream)
	})

	require.Equal(t, expected, actual)
}

func TestStreamMetadata_TryStore(t *testing.T) {
	now := time.Now()
	cutoff := now.Add(-60 * time.Minute).UnixNano()
	bucketStart := now.Truncate(time.Minute).UnixNano()
	bucketCutOff := now.Add(-5 * time.Minute).UnixNano()

	tests := []struct {
		name string

		// setup data
		metadata         *streamMetadata
		streams          map[int32][]Stream
		maxActiveStreams uint64

		// expectations
		expectedStored  map[string]map[int32][]Stream
		expectedDropped map[Reason][]uint64
	}{
		{
			name: "no streams",
			metadata: &streamMetadata{
				stripes: []map[string]map[int32]map[uint64]Stream{make(map[string]map[int32]map[uint64]Stream)},
				locks:   make([]stripeLock, 1),
			},
			streams:          map[int32][]Stream{},
			maxActiveStreams: 10,
			expectedStored:   map[string]map[int32][]Stream{},
		},
		{
			name: "all streams within partition limit",
			metadata: &streamMetadata{
				stripes: []map[string]map[int32]map[uint64]Stream{make(map[string]map[int32]map[uint64]Stream)},
				locks:   make([]stripeLock, 1),
			},
			streams: map[int32][]Stream{0: {
				{Hash: 0x0, LastSeenAt: now.UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 1000}}},
				{Hash: 0x1, LastSeenAt: now.UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 1000}}},
			}},
			maxActiveStreams: 2,
			expectedStored: map[string]map[int32][]Stream{
				"tenant1": {
					0: []Stream{
						{Hash: 0x0, LastSeenAt: now.UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 1000}}},
						{Hash: 0x1, LastSeenAt: now.UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 1000}}},
					},
				},
			},
		},
		{
			name: "all stream within limit per partition",
			metadata: &streamMetadata{
				stripes: []map[string]map[int32]map[uint64]Stream{make(map[string]map[int32]map[uint64]Stream)},
				locks:   make([]stripeLock, 2),
			},
			streams: map[int32][]Stream{
				0: {
					{Hash: 0x0, LastSeenAt: now.UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 1000}}},
				},
				1: {
					{Hash: 0x1, LastSeenAt: now.UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 1000}}},
				},
			},
			maxActiveStreams: 2,
			expectedStored: map[string]map[int32][]Stream{
				"tenant1": {
					0: []Stream{
						{Hash: 0x0, LastSeenAt: now.UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 1000}}},
					},
					1: []Stream{
						{Hash: 0x1, LastSeenAt: now.UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 1000}}},
					},
				},
			},
		},
		{
			name: "some streams dropped",
			metadata: &streamMetadata{
				stripes: []map[string]map[int32]map[uint64]Stream{make(map[string]map[int32]map[uint64]Stream)},
				locks:   make([]stripeLock, 1),
			},
			streams: map[int32][]Stream{
				0: {
					{Hash: 0x0, LastSeenAt: now.UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 1000}}},
					{Hash: 0x1, LastSeenAt: now.UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 1000}}},
				},
			},
			maxActiveStreams: 1,
			expectedStored: map[string]map[int32][]Stream{
				"tenant1": {
					0: []Stream{
						{Hash: 0x0, LastSeenAt: now.UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 1000}}},
					},
				},
			},
			expectedDropped: map[Reason][]uint64{
				ReasonExceedsMaxStreams: {0x1},
			},
		},
		{
			name: "some streams dropped per partition",
			metadata: &streamMetadata{
				stripes: []map[string]map[int32]map[uint64]Stream{make(map[string]map[int32]map[uint64]Stream)},
				locks:   make([]stripeLock, 2),
			},
			streams: map[int32][]Stream{
				0: {
					{Hash: 0x0, LastSeenAt: now.UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 1000}}},
					{Hash: 0x1, LastSeenAt: now.UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 1000}}},
				},
				1: {
					{Hash: 0x2, LastSeenAt: now.UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 1000}}},
					{Hash: 0x3, LastSeenAt: now.UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 1000}}},
				},
			},
			maxActiveStreams: 1,
			expectedStored: map[string]map[int32][]Stream{
				"tenant1": {
					0: []Stream{
						{Hash: 0x0, LastSeenAt: now.UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 1000}}},
					},
					1: []Stream{
						{Hash: 0x2, LastSeenAt: now.UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 1000}}},
					},
				},
			},
			expectedDropped: map[Reason][]uint64{
				ReasonExceedsMaxStreams: {0x1, 0x3},
			},
		},
		{
			name: "some streams dropped from a single partition",
			metadata: &streamMetadata{
				stripes: []map[string]map[int32]map[uint64]Stream{
					{
						"tenant1": {
							0: {},
							1: {
								0x1: {Hash: 0x1, LastSeenAt: now.UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 1000}}},
							},
						},
					}},
				locks: make([]stripeLock, 2),
			},
			streams: map[int32][]Stream{
				0: {
					{Hash: 0x0, LastSeenAt: now.UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 1000}}},
				},
				1: {
					{Hash: 0x2, LastSeenAt: now.UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 1000}}},
					{Hash: 0x3, LastSeenAt: now.UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 1000}}},
				},
			},
			maxActiveStreams: 2,
			expectedStored: map[string]map[int32][]Stream{
				"tenant1": {
					0: []Stream{
						{Hash: 0x0, LastSeenAt: now.UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 1000}}},
					},
					1: []Stream{
						{Hash: 0x1, LastSeenAt: now.UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 1000}}},
						{Hash: 0x2, LastSeenAt: now.UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 1000}}},
					},
				},
			},
			expectedDropped: map[Reason][]uint64{
				ReasonExceedsMaxStreams: {0x3},
			},
		},
		{
			name: "drops new streams but updates existing streams",
			metadata: &streamMetadata{
				stripes: []map[string]map[int32]map[uint64]Stream{
					{
						"tenant1": {
							0: {
								0x0: {Hash: 0x0, LastSeenAt: now.UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 1000}}},
								0x1: {Hash: 0x1, LastSeenAt: now.UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 1000}}},
							},
							1: {
								0x3: {Hash: 0x3, LastSeenAt: now.UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 1000}}},
								0x4: {Hash: 0x4, LastSeenAt: now.UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 1000}}},
							},
						},
					},
				},
				locks: make([]stripeLock, 1),
			},
			streams: map[int32][]Stream{
				0: { // Mixed order of new and existing streams
					{Hash: 0x0, LastSeenAt: now.UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 1000}}},
					{Hash: 0x2, LastSeenAt: now.UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 1000}}},
					{Hash: 0x1, LastSeenAt: now.UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 1000}}},
				},
				1: { // Mixed order of new and existing streams
					{Hash: 0x3, LastSeenAt: now.UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 1000}}},
					{Hash: 0x5, LastSeenAt: now.UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 1000}}},
					{Hash: 0x4, LastSeenAt: now.UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 1000}}},
				},
			},
			maxActiveStreams: 2,
			expectedStored: map[string]map[int32][]Stream{
				"tenant1": {
					0: []Stream{
						{Hash: 0x0, LastSeenAt: now.UnixNano(), TotalSize: 2000, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 2000}}},
						{Hash: 0x1, LastSeenAt: now.UnixNano(), TotalSize: 2000, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 2000}}},
					},
					1: []Stream{
						{Hash: 0x3, LastSeenAt: now.UnixNano(), TotalSize: 2000, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 2000}}},
						{Hash: 0x4, LastSeenAt: now.UnixNano(), TotalSize: 2000, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 2000}}},
					},
				},
			},
			expectedDropped: map[Reason][]uint64{
				ReasonExceedsMaxStreams: {0x2, 0x5},
			},
		},
		{
			name: "reset expired but not evicted streams",
			metadata: &streamMetadata{
				stripes: []map[string]map[int32]map[uint64]Stream{
					{
						"tenant1": {
							0: {
								0x0: {Hash: 0x0, LastSeenAt: now.Add(-120 * time.Minute).UnixNano(), TotalSize: 3000, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 3000}}},
								0x1: {Hash: 0x1, LastSeenAt: now.UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 1000}}},
							},
						},
					},
				},
				locks: make([]stripeLock, 1),
			},
			maxActiveStreams: 2,
			streams: map[int32][]Stream{
				0: {
					{Hash: 0x0, LastSeenAt: now.UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 1000}}},
					{Hash: 0x1, LastSeenAt: now.UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 1000}}},
				},
			},
			expectedStored: map[string]map[int32][]Stream{
				"tenant1": {
					0: []Stream{
						{Hash: 0x0, LastSeenAt: now.UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 1000}}},
						{Hash: 0x1, LastSeenAt: now.UnixNano(), TotalSize: 2000, RateBuckets: []RateBucket{{Timestamp: bucketStart, Size: 2000}}},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actualDropped := tt.metadata.TryStore("tenant1", tt.streams, tt.maxActiveStreams, cutoff, bucketStart, bucketCutOff)

			actualStored := make(map[string]map[int32][]Stream)
			tt.metadata.All(func(tenant string, partitionID int32, stream Stream) {
				if _, ok := actualStored[tenant]; !ok {
					actualStored[tenant] = make(map[int32][]Stream)
				}
				actualStored[tenant][partitionID] = append(actualStored[tenant][partitionID], stream)
			})

			for tenant, partitions := range tt.expectedStored {
				require.Len(t, actualStored[tenant], len(partitions))

				for partitionID, streams := range partitions {
					require.Len(t, actualStored[tenant][partitionID], len(streams))
					require.ElementsMatch(t, streams, actualStored[tenant][partitionID])
				}
			}

			for reason, streamHashes := range tt.expectedDropped {
				require.Contains(t, actualDropped, reason)
				require.ElementsMatch(t, streamHashes, actualDropped[reason])
			}
		})
	}
}

func TestStreamMetadata_Evict(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name                 string
		metadata             *streamMetadata
		cutOff               int64
		assignedPartitionIDs []int32
		expectedMetadata     map[string]map[int32]map[uint64]Stream
		expectedEvictions    map[string]int
	}{
		{
			name: "all streams active",
			metadata: &streamMetadata{
				stripes: []map[string]map[int32]map[uint64]Stream{
					{
						"tenant1": {
							0: {
								1: {Hash: 1, LastSeenAt: now.UnixNano(), TotalSize: 1000},
								2: {Hash: 2, LastSeenAt: now.UnixNano(), TotalSize: 2000},
							},
						},
					},
				},
				locks: make([]stripeLock, 1),
			},
			cutOff:               now.Add(-time.Hour).UnixNano(),
			assignedPartitionIDs: []int32{0},
			expectedMetadata: map[string]map[int32]map[uint64]Stream{
				"tenant1": {
					0: {
						1: {Hash: 1, LastSeenAt: now.UnixNano(), TotalSize: 1000},
						2: {Hash: 2, LastSeenAt: now.UnixNano(), TotalSize: 2000},
					},
				},
			},
			expectedEvictions: map[string]int{},
		},
		{
			name: "all streams expired",
			metadata: &streamMetadata{
				stripes: []map[string]map[int32]map[uint64]Stream{
					{
						"tenant1": {
							0: {
								1: {Hash: 1, LastSeenAt: now.Add(-2 * time.Hour).UnixNano(), TotalSize: 1000},
								2: {Hash: 2, LastSeenAt: now.Add(-2 * time.Hour).UnixNano(), TotalSize: 2000},
							},
						},
					},
				},
				locks: make([]stripeLock, 1),
			},
			cutOff:               now.Add(-time.Hour).UnixNano(),
			assignedPartitionIDs: []int32{0},
			expectedMetadata:     map[string]map[int32]map[uint64]Stream{},
			expectedEvictions: map[string]int{
				"tenant1": 2,
			},
		},
		{
			name: "mixed active and expired streams",
			metadata: &streamMetadata{
				stripes: []map[string]map[int32]map[uint64]Stream{
					{
						"tenant1": {
							0: {
								1: {Hash: 1, LastSeenAt: now.UnixNano(), TotalSize: 1000},
								2: {Hash: 2, LastSeenAt: now.Add(-2 * time.Hour).UnixNano(), TotalSize: 2000},
								3: {Hash: 3, LastSeenAt: now.UnixNano(), TotalSize: 3000},
							},
						},
					},
				},
				locks: make([]stripeLock, 1),
			},
			cutOff:               now.Add(-time.Hour).UnixNano(),
			assignedPartitionIDs: []int32{0},
			expectedMetadata: map[string]map[int32]map[uint64]Stream{
				"tenant1": {
					0: {
						1: {Hash: 1, LastSeenAt: now.UnixNano(), TotalSize: 1000},
						3: {Hash: 3, LastSeenAt: now.UnixNano(), TotalSize: 3000},
					},
				},
			},
			expectedEvictions: map[string]int{
				"tenant1": 1,
			},
		},
		{
			name: "multiple tenants with mixed streams",
			metadata: &streamMetadata{
				stripes: []map[string]map[int32]map[uint64]Stream{
					{
						"tenant1": {
							0: {
								1: {Hash: 1, LastSeenAt: now.UnixNano(), TotalSize: 1000},
								2: {Hash: 2, LastSeenAt: now.Add(-2 * time.Hour).UnixNano(), TotalSize: 2000},
							},
						},
						"tenant2": {
							0: {
								3: {Hash: 3, LastSeenAt: now.Add(-2 * time.Hour).UnixNano(), TotalSize: 3000},
								4: {Hash: 4, LastSeenAt: now.Add(-2 * time.Hour).UnixNano(), TotalSize: 4000},
							},
						},
						"tenant3": {
							0: {
								5: {Hash: 5, LastSeenAt: now.UnixNano(), TotalSize: 5000},
							},
						},
					},
				},
				locks: make([]stripeLock, 1),
			},
			cutOff:               now.Add(-time.Hour).UnixNano(),
			assignedPartitionIDs: []int32{0},
			expectedMetadata: map[string]map[int32]map[uint64]Stream{
				"tenant1": {
					0: {
						1: {Hash: 1, LastSeenAt: now.UnixNano(), TotalSize: 1000},
					},
				},
				"tenant3": {
					0: {
						5: {Hash: 5, LastSeenAt: now.UnixNano(), TotalSize: 5000},
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
			metadata: &streamMetadata{
				stripes: []map[string]map[int32]map[uint64]Stream{
					{
						"tenant1": {
							0: {
								1: {Hash: 1, LastSeenAt: now.UnixNano(), TotalSize: 1000},
								2: {Hash: 2, LastSeenAt: now.Add(-2 * time.Hour).UnixNano(), TotalSize: 2000},
							},
						},
					},
					{
						"tenant1": {
							1: {
								3: {Hash: 3, LastSeenAt: now.Add(-2 * time.Hour).UnixNano(), TotalSize: 3000},
							},
						},
					},
					{
						"tenant1": {
							2: {
								4: {Hash: 4, LastSeenAt: now.UnixNano(), TotalSize: 4000},
							},
						},
					},
				},
				locks: make([]stripeLock, 3),
			},
			cutOff:               now.Add(-time.Hour).UnixNano(),
			assignedPartitionIDs: []int32{0, 1, 2},
			expectedMetadata: map[string]map[int32]map[uint64]Stream{
				"tenant1": {
					0: {
						1: {Hash: 1, LastSeenAt: now.UnixNano(), TotalSize: 1000},
					},
					2: {
						4: {Hash: 4, LastSeenAt: now.UnixNano(), TotalSize: 4000},
					},
				},
			},
			expectedEvictions: map[string]int{
				"tenant1": 2,
			},
		},
		{
			name: "unassigned partitions should still be evicted",
			metadata: &streamMetadata{
				stripes: []map[string]map[int32]map[uint64]Stream{
					{
						"tenant1": {
							0: {
								1: {Hash: 1, LastSeenAt: now.UnixNano(), TotalSize: 1000},
							},
						},
					},
					{
						"tenant1": {
							1: {
								2: {Hash: 2, LastSeenAt: now.Add(-2 * time.Hour).UnixNano(), TotalSize: 2000},
							},
						},
					},
				},
				locks: make([]stripeLock, 2),
			},
			cutOff:               now.Add(-time.Hour).UnixNano(),
			assignedPartitionIDs: []int32{0},
			expectedMetadata: map[string]map[int32]map[uint64]Stream{
				"tenant1": {
					0: {
						1: {Hash: 1, LastSeenAt: now.UnixNano(), TotalSize: 1000},
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

			actualMetadata := make(map[string]map[int32]map[uint64]Stream)
			tt.metadata.All(func(tenant string, partitionID int32, stream Stream) {
				if actualMetadata[tenant] == nil {
					actualMetadata[tenant] = make(map[int32]map[uint64]Stream)
				}
				if actualMetadata[tenant][partitionID] == nil {
					actualMetadata[tenant][partitionID] = make(map[uint64]Stream)
				}
				actualMetadata[tenant][partitionID][stream.Hash] = stream
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
		m.Store("tenant1", int32(i), 1, 1000, time.Now().UnixNano(), time.Now().Truncate(time.Minute).UnixNano(), time.Now().Add(-time.Hour).UnixNano())
	}

	m.EvictPartitions([]int32{1, 3, 5, 7, 9})

	expected := []int32{0, 2, 4, 6, 8}
	actual := make([]int32, 0, len(expected))
	m.All(func(_ string, partitionID int32, _ Stream) {
		actual = append(actual, partitionID)
	})
	require.ElementsMatch(t, expected, actual)
}

func TestStreamMetadata_EvictPartitions_Concurrent(t *testing.T) {
	numPartitions := 10
	m := NewStreamMetadata(numPartitions)

	for i := range numPartitions {
		m.Store("tenant1", int32(i), 1, 1000, time.Now().UnixNano(), time.Now().Truncate(time.Minute).UnixNano(), time.Now().Add(-time.Hour).UnixNano())
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
	m.All(func(_ string, partitionID int32, _ Stream) {
		actual = append(actual, partitionID)
	})
	require.ElementsMatch(t, expected, actual)
}
