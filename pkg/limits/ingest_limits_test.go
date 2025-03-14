package limits

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/ring"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestIngestLimits_GetStreamUsage(t *testing.T) {
	tests := []struct {
		name string

		// Setup data.
		assignedPartitionIDs []int32
		metadata             map[string]map[int32][]streamMetadata
		windowSize           time.Duration
		rateWindow           time.Duration
		bucketDuration       time.Duration

		// Request data for GetStreamUsage.
		tenantID     string
		partitionIDs []int32
		streamHashes []uint64

		// Expectations.
		expectedActive         uint64
		expectedRate           int64
		expectedUnknownStreams []uint64
	}{
		{
			name: "tenant not found",
			// setup data
			assignedPartitionIDs: []int32{0},
			metadata: map[string]map[int32][]streamMetadata{
				"tenant2": {
					0: []streamMetadata{
						{hash: 4, lastSeenAt: time.Now().UnixNano(), totalSize: 1000, rateBuckets: []rateBucket{{timestamp: time.Now().UnixNano(), size: 1000}}},
						{hash: 5, lastSeenAt: time.Now().UnixNano(), totalSize: 2000, rateBuckets: []rateBucket{{timestamp: time.Now().UnixNano(), size: 2000}}},
					},
				},
			},
			windowSize:     time.Hour,
			rateWindow:     5 * time.Minute,
			bucketDuration: time.Minute,
			// request data
			tenantID:     "tenant1",
			partitionIDs: []int32{0},
			streamHashes: []uint64{4, 5},
		},
		{
			name: "all streams active",
			// setup data
			assignedPartitionIDs: []int32{0},
			metadata: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: []streamMetadata{
						{hash: 1, lastSeenAt: time.Now().UnixNano(), totalSize: 1000, rateBuckets: []rateBucket{{timestamp: time.Now().UnixNano(), size: 1000}}},
						{hash: 2, lastSeenAt: time.Now().UnixNano(), totalSize: 2000, rateBuckets: []rateBucket{{timestamp: time.Now().UnixNano(), size: 2000}}},
						{hash: 3, lastSeenAt: time.Now().UnixNano(), totalSize: 3000, rateBuckets: []rateBucket{{timestamp: time.Now().UnixNano(), size: 3000}}},
						{hash: 4, lastSeenAt: time.Now().UnixNano(), totalSize: 4000, rateBuckets: []rateBucket{{timestamp: time.Now().UnixNano(), size: 4000}}},
					},
				},
			},
			windowSize:     time.Hour,
			rateWindow:     5 * time.Minute,
			bucketDuration: time.Minute,
			// request data
			tenantID:     "tenant1",
			partitionIDs: []int32{0},
			streamHashes: []uint64{1, 2, 3, 4},
			// expectations
			expectedActive: 4,
			expectedRate:   int64(10000) / int64(5*60), // 10000 bytes / 5 minutes in seconds
		},
		{
			name: "mixed active and expired streams",
			// setup data
			assignedPartitionIDs: []int32{0},
			metadata: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: []streamMetadata{
						{hash: 1, lastSeenAt: time.Now().UnixNano(), totalSize: 1000, rateBuckets: []rateBucket{{timestamp: time.Now().UnixNano(), size: 1000}}},
						{hash: 2, lastSeenAt: time.Now().Add(-2 * time.Hour).UnixNano(), totalSize: 2000}, // expired
						{hash: 3, lastSeenAt: time.Now().UnixNano(), totalSize: 3000, rateBuckets: []rateBucket{{timestamp: time.Now().UnixNano(), size: 3000}}},
						{hash: 4, lastSeenAt: time.Now().Add(-2 * time.Hour).UnixNano(), totalSize: 4000}, // expired
						{hash: 5, lastSeenAt: time.Now().UnixNano(), totalSize: 5000, rateBuckets: []rateBucket{{timestamp: time.Now().UnixNano(), size: 5000}}},
					},
				},
			},
			windowSize:     time.Hour,
			rateWindow:     5 * time.Minute,
			bucketDuration: time.Minute,
			// request data
			tenantID:     "tenant1",
			partitionIDs: []int32{0},
			streamHashes: []uint64{1, 3, 5},
			// expectations
			expectedActive: 3,
			expectedRate:   int64(9000) / int64(5*60), // 9000 bytes / 5 minutes in seconds
		},
		{
			name: "all streams expired",
			// setup data
			assignedPartitionIDs: []int32{0},
			metadata: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: []streamMetadata{
						{hash: 1, lastSeenAt: time.Now().Add(-2 * time.Hour).UnixNano(), totalSize: 1000},
						{hash: 2, lastSeenAt: time.Now().Add(-2 * time.Hour).UnixNano(), totalSize: 2000},
					},
				},
			},
			windowSize:     time.Hour,
			rateWindow:     5 * time.Minute,
			bucketDuration: time.Minute,
			// request data
			tenantID: "tenant1",
			// expectations
			expectedActive: 0,
			expectedRate:   0,
		},
		{
			name: "empty stream hashes",
			// setup data
			assignedPartitionIDs: []int32{0},
			metadata: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: []streamMetadata{
						{hash: 1, lastSeenAt: time.Now().UnixNano(), totalSize: 1000, rateBuckets: []rateBucket{{timestamp: time.Now().UnixNano(), size: 1000}}},
						{hash: 2, lastSeenAt: time.Now().UnixNano(), totalSize: 2000, rateBuckets: []rateBucket{{timestamp: time.Now().UnixNano(), size: 2000}}},
					},
				},
			},
			windowSize:     time.Hour,
			rateWindow:     5 * time.Minute,
			bucketDuration: time.Minute,
			// request data
			tenantID:     "tenant1",
			partitionIDs: []int32{0},
			streamHashes: []uint64{},
			//expectations
			expectedActive: 2,
			expectedRate:   int64(3000) / int64(5*60), // 3000 bytes / 5 minutes in seconds
		},
		{
			name: "unknown streams requested",
			// setup data
			assignedPartitionIDs: []int32{0},
			metadata: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: []streamMetadata{
						{hash: 1, lastSeenAt: time.Now().UnixNano(), totalSize: 1000, rateBuckets: []rateBucket{{timestamp: time.Now().UnixNano(), size: 1000}}},
						{hash: 2, lastSeenAt: time.Now().UnixNano(), totalSize: 2000, rateBuckets: []rateBucket{{timestamp: time.Now().UnixNano(), size: 2000}}},
						{hash: 3, lastSeenAt: time.Now().UnixNano(), totalSize: 3000, rateBuckets: []rateBucket{{timestamp: time.Now().UnixNano(), size: 3000}}},
						{hash: 4, lastSeenAt: time.Now().UnixNano(), totalSize: 4000, rateBuckets: []rateBucket{{timestamp: time.Now().UnixNano(), size: 4000}}},
						{hash: 5, lastSeenAt: time.Now().UnixNano(), totalSize: 5000, rateBuckets: []rateBucket{{timestamp: time.Now().UnixNano(), size: 5000}}},
					},
				},
			},
			windowSize:     time.Hour,
			rateWindow:     5 * time.Minute,
			bucketDuration: time.Minute,
			// request data
			tenantID:     "tenant1",
			partitionIDs: []int32{0},
			streamHashes: []uint64{6, 7, 8},
			// expecations
			expectedActive:         5,
			expectedUnknownStreams: []uint64{6, 7, 8},
			expectedRate:           int64(15000) / int64(5*60), // 15000 bytes / 5 minutes in seconds
		},
		{
			name: "multiple assigned partitions",
			// setup data
			assignedPartitionIDs: []int32{0, 1},
			metadata: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: []streamMetadata{
						{hash: 1, lastSeenAt: time.Now().UnixNano(), totalSize: 1000, rateBuckets: []rateBucket{{timestamp: time.Now().UnixNano(), size: 1000}}},
						{hash: 2, lastSeenAt: time.Now().UnixNano(), totalSize: 2000, rateBuckets: []rateBucket{{timestamp: time.Now().UnixNano(), size: 2000}}},
					},
					1: []streamMetadata{
						{hash: 3, lastSeenAt: time.Now().UnixNano(), totalSize: 3000, rateBuckets: []rateBucket{{timestamp: time.Now().UnixNano(), size: 3000}}},
						{hash: 4, lastSeenAt: time.Now().UnixNano(), totalSize: 4000, rateBuckets: []rateBucket{{timestamp: time.Now().UnixNano(), size: 4000}}},
						{hash: 5, lastSeenAt: time.Now().UnixNano(), totalSize: 5000, rateBuckets: []rateBucket{{timestamp: time.Now().UnixNano(), size: 5000}}},
					},
				},
			},
			windowSize:     time.Hour,
			rateWindow:     5 * time.Minute,
			bucketDuration: time.Minute,
			// request data
			tenantID:     "tenant1",
			partitionIDs: []int32{0, 1},
			streamHashes: []uint64{1, 2, 3, 4, 5},
			// expectations
			expectedActive: 5,
			expectedRate:   int64(15000) / int64(5*60), // 15000 bytes / 5 minutes in seconds
		},
		{
			name: "multiple partitions with unasigned partitions",
			// setup data
			assignedPartitionIDs: []int32{0},
			metadata: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: []streamMetadata{
						{hash: 1, lastSeenAt: time.Now().UnixNano(), totalSize: 1000, rateBuckets: []rateBucket{{timestamp: time.Now().UnixNano(), size: 1000}}},
						{hash: 2, lastSeenAt: time.Now().UnixNano(), totalSize: 2000, rateBuckets: []rateBucket{{timestamp: time.Now().UnixNano(), size: 2000}}},
					},
				},
			},
			windowSize:     time.Hour,
			rateWindow:     5 * time.Minute,
			bucketDuration: time.Minute,
			// request data
			tenantID:     "tenant1",
			partitionIDs: []int32{0, 1},
			streamHashes: []uint64{1, 2, 3, 4, 5},
			// expectations
			expectedActive:         2,
			expectedUnknownStreams: []uint64{3, 4, 5},
			expectedRate:           int64(3000) / int64(5*60), // 3000 bytes / 5 minutes in seconds
		},
		{
			name: "mixed buckets within and outside rate window",
			// setup data
			assignedPartitionIDs: []int32{0},
			metadata: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: []streamMetadata{
						{
							hash:       1,
							lastSeenAt: time.Now().UnixNano(),
							totalSize:  5000, // Total size includes all buckets
							rateBuckets: []rateBucket{
								{timestamp: time.Now().Add(-10 * time.Minute).UnixNano(), size: 1000}, // Outside rate window
								{timestamp: time.Now().Add(-6 * time.Minute).UnixNano(), size: 1500},  // Outside rate window
								{timestamp: time.Now().Add(-4 * time.Minute).UnixNano(), size: 1000},  // Inside rate window
								{timestamp: time.Now().Add(-2 * time.Minute).UnixNano(), size: 1500},  // Inside rate window
							},
						},
						{
							hash:       2,
							lastSeenAt: time.Now().UnixNano(),
							totalSize:  4000, // Total size includes all buckets
							rateBuckets: []rateBucket{
								{timestamp: time.Now().Add(-8 * time.Minute).UnixNano(), size: 1000}, // Outside rate window
								{timestamp: time.Now().Add(-3 * time.Minute).UnixNano(), size: 1500}, // Inside rate window
								{timestamp: time.Now().Add(-1 * time.Minute).UnixNano(), size: 1500}, // Inside rate window
							},
						},
					},
				},
			},
			windowSize:     time.Hour,
			rateWindow:     5 * time.Minute,
			bucketDuration: time.Minute,
			// request data
			tenantID:     "tenant1",
			partitionIDs: []int32{0},
			streamHashes: []uint64{1, 2},
			// expectations
			expectedActive: 2,
			// Only count size from buckets within rate window: 1000 + 1500 + 1500 + 1500 = 5500
			expectedRate: int64(5500) / int64(5*60), // 5500 bytes / 5 minutes in seconds = 18.33, truncated to 18
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &IngestLimits{
				cfg: Config{
					WindowSize:     tt.windowSize,
					RateWindow:     tt.rateWindow,
					BucketDuration: tt.bucketDuration,
					LifecyclerConfig: ring.LifecyclerConfig{
						RingConfig: ring.Config{
							KVStore: kv.Config{
								Store: "inmemory",
							},
							ReplicationFactor: 1,
						},
						NumTokens:       1,
						ID:              "test",
						Zone:            "test",
						FinalSleep:      0,
						HeartbeatPeriod: 100 * time.Millisecond,
						ObservePeriod:   100 * time.Millisecond,
					},
				},
				logger:           log.NewNopLogger(),
				metrics:          newMetrics(prometheus.NewRegistry()),
				metadata:         tt.metadata,
				partitionManager: NewPartitionManager(log.NewNopLogger()),
			}

			// Assign the Partition IDs.
			partitions := make(map[string][]int32)
			partitions["test"] = make([]int32, 0, len(tt.assignedPartitionIDs))
			partitions["test"] = append(partitions["test"], tt.assignedPartitionIDs...)
			s.partitionManager.Assign(context.Background(), nil, partitions)

			// Call GetStreamUsage.
			req := &logproto.GetStreamUsageRequest{
				Tenant:       tt.tenantID,
				Partitions:   tt.partitionIDs,
				StreamHashes: tt.streamHashes,
			}

			resp, err := s.GetStreamUsage(context.Background(), req)
			require.NoError(t, err)
			require.NotNil(t, resp)
			require.Equal(t, tt.tenantID, resp.Tenant)
			require.Equal(t, tt.expectedActive, resp.ActiveStreams)
			require.Len(t, resp.UnknownStreams, len(tt.expectedUnknownStreams))
			require.Equal(t, tt.expectedRate, resp.Rate)
		})
	}
}

func TestIngestLimits_GetStreamUsage_Concurrent(t *testing.T) {
	now := time.Now()
	// Setup test data with a mix of active and expired streams>
	metadata := map[string]map[int32][]streamMetadata{
		"tenant1": {
			0: []streamMetadata{
				{hash: 1, lastSeenAt: now.UnixNano(), totalSize: 1000, rateBuckets: []rateBucket{{timestamp: now.UnixNano(), size: 1000}}},                        // active
				{hash: 2, lastSeenAt: now.Add(-30 * time.Minute).UnixNano(), totalSize: 2000, rateBuckets: []rateBucket{{timestamp: now.UnixNano(), size: 2000}}}, // active
				{hash: 3, lastSeenAt: now.Add(-2 * time.Hour).UnixNano(), totalSize: 3000},                                                                        // expired
				{hash: 4, lastSeenAt: now.Add(-45 * time.Minute).UnixNano(), totalSize: 4000, rateBuckets: []rateBucket{{timestamp: now.UnixNano(), size: 4000}}}, // active
				{hash: 5, lastSeenAt: now.Add(-3 * time.Hour).UnixNano(), totalSize: 5000},                                                                        // expired
			},
		},
	}

	s := &IngestLimits{
		cfg: Config{
			WindowSize:     time.Hour,
			RateWindow:     5 * time.Minute,
			BucketDuration: time.Minute,
			LifecyclerConfig: ring.LifecyclerConfig{
				RingConfig: ring.Config{
					KVStore: kv.Config{
						Store: "inmemory",
					},
					ReplicationFactor: 1,
				},
				NumTokens:       1,
				ID:              "test",
				Zone:            "test",
				FinalSleep:      0,
				HeartbeatPeriod: 100 * time.Millisecond,
				ObservePeriod:   100 * time.Millisecond,
			},
		},
		logger:   log.NewNopLogger(),
		metadata: metadata,
		metrics:  newMetrics(prometheus.NewRegistry()),
	}

	// Run concurrent requests
	concurrency := 10
	done := make(chan struct{})
	for i := 0; i < concurrency; i++ {
		go func() {
			defer func() { done <- struct{}{} }()

			req := &logproto.GetStreamUsageRequest{
				Tenant:       "tenant1",
				Partitions:   []int32{0},
				StreamHashes: []uint64{1, 2, 3, 4, 5},
			}

			resp, err := s.GetStreamUsage(context.Background(), req)
			require.NoError(t, err)
			require.NotNil(t, resp)
			require.Equal(t, "tenant1", resp.Tenant)
			require.Equal(t, uint64(3), resp.ActiveStreams) // Should count only the 3 active streams

			expectedRate := int64(7000) / int64(5*60)
			require.Equal(t, expectedRate, resp.Rate)
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < concurrency; i++ {
		<-done
	}
}

func TestIngestLimits_UpdateMetadata(t *testing.T) {

	tests := []struct {
		name string

		// Setup data.
		assignedPartitionIDs []int32
		metadata             map[string]map[int32][]streamMetadata

		// config
		bucketDuration time.Duration
		rateWindow     time.Duration

		// The test case.
		tenantID       string
		partitionID    int32
		lastSeenAt     time.Time
		updateMetadata *logproto.StreamMetadata

		// Expectations.
		expected map[string]map[int32][]streamMetadata
	}{
		{
			name:                 "new tenant, new partition",
			assignedPartitionIDs: []int32{0},
			metadata:             map[string]map[int32][]streamMetadata{},
			tenantID:             "tenant1",
			partitionID:          0,
			lastSeenAt:           time.Unix(100, 0),
			bucketDuration:       time.Minute,
			rateWindow:           5 * time.Minute,
			updateMetadata: &logproto.StreamMetadata{
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
			name:                 "existing tenant, new partition",
			assignedPartitionIDs: []int32{0, 1},
			metadata: map[string]map[int32][]streamMetadata{
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
			tenantID:    "tenant1",
			partitionID: 1,
			updateMetadata: &logproto.StreamMetadata{
				StreamHash:             456,
				EntriesSize:            2000,
				StructuredMetadataSize: 1000,
			},
			lastSeenAt:     time.Unix(200, 0),
			bucketDuration: time.Minute,
			rateWindow:     5 * time.Minute,
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
			name:                 "update existing stream",
			assignedPartitionIDs: []int32{0},
			metadata: map[string]map[int32][]streamMetadata{
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
			tenantID:    "tenant1",
			partitionID: 0,
			updateMetadata: &logproto.StreamMetadata{
				StreamHash:             123,
				EntriesSize:            3000,
				StructuredMetadataSize: 1500,
			},
			lastSeenAt:     time.Unix(300, 0),
			bucketDuration: time.Minute,
			rateWindow:     5 * time.Minute,
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
			name:                 "evict stream from partition",
			assignedPartitionIDs: []int32{1},
			metadata: map[string]map[int32][]streamMetadata{
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
			tenantID:    "tenant1",
			partitionID: 0,
			updateMetadata: &logproto.StreamMetadata{
				StreamHash:             123,
				EntriesSize:            4000,
				StructuredMetadataSize: 2000,
			},
			lastSeenAt:     time.Unix(400, 0),
			bucketDuration: time.Minute,
			rateWindow:     5 * time.Minute,
			expected: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: {
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
			name:           "update existing bucket",
			tenantID:       "tenant1",
			bucketDuration: time.Minute,
			rateWindow:     5 * time.Minute,
			updateMetadata: &logproto.StreamMetadata{
				StreamHash:             888,
				EntriesSize:            1000,
				StructuredMetadataSize: 500,
			},
			assignedPartitionIDs: []int32{0},
			lastSeenAt:           time.Unix(852, 0),
			metadata: map[string]map[int32][]streamMetadata{
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
			name:           "clean up buckets outside rate window",
			tenantID:       "tenant1",
			bucketDuration: time.Minute,
			rateWindow:     5 * time.Minute,
			updateMetadata: &logproto.StreamMetadata{
				StreamHash:             999,
				EntriesSize:            2000,
				StructuredMetadataSize: 1000,
			},
			assignedPartitionIDs: []int32{0},
			lastSeenAt:           time.Unix(1000, 0), // Current time reference
			metadata: map[string]map[int32][]streamMetadata{
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
			name:           "update same minute bucket",
			tenantID:       "tenant1",
			bucketDuration: time.Minute,
			rateWindow:     5 * time.Minute,
			updateMetadata: &logproto.StreamMetadata{
				StreamHash:             555,
				EntriesSize:            1000,
				StructuredMetadataSize: 500,
			},
			assignedPartitionIDs: []int32{0},
			lastSeenAt:           time.Unix(1100, 0),
			metadata: map[string]map[int32][]streamMetadata{
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
			s := &IngestLimits{
				cfg: Config{
					BucketDuration: tt.bucketDuration,
					RateWindow:     tt.rateWindow,
				},
				metadata:         tt.metadata,
				metrics:          newMetrics(prometheus.NewRegistry()),
				partitionManager: NewPartitionManager(log.NewNopLogger()),
			}
			// Assign the Partition IDs.
			partitions := make(map[string][]int32)
			partitions["test"] = make([]int32, 0, len(tt.assignedPartitionIDs))
			partitions["test"] = append(partitions["test"], tt.assignedPartitionIDs...)
			s.partitionManager.Assign(context.Background(), nil, partitions)

			s.updateMetadata(tt.updateMetadata, tt.tenantID, tt.partitionID, tt.lastSeenAt)

			// For tests with sizeBuckets, we need to check specifically
			if len(tt.expected) > 0 {
				for tenant, partitions := range tt.expected {
					for partition, streams := range partitions {
						for i, expectedStream := range streams {
							if len(expectedStream.rateBuckets) > 0 {
								require.Equal(t, len(expectedStream.rateBuckets), len(s.metadata[tenant][partition][i].rateBuckets),
									"Number of size buckets does not match for stream %d", expectedStream.hash)

								// Check each bucket
								for j, expectedBucket := range expectedStream.rateBuckets {
									require.Equal(t, expectedBucket.timestamp, s.metadata[tenant][partition][i].rateBuckets[j].timestamp,
										"Bucket timestamp mismatch for stream %d, bucket %d", expectedStream.hash, j)
									require.Equal(t, expectedBucket.size, s.metadata[tenant][partition][i].rateBuckets[j].size,
										"Bucket size mismatch for stream %d, bucket %d", expectedStream.hash, j)
								}
							}
						}
					}
				}
			}

			require.Equal(t, tt.expected, s.metadata)
		})
	}
}

func TestNewIngestLimits(t *testing.T) {
	cfg := Config{
		KafkaConfig: kafka.Config{
			Topic: "test-topic",
		},
		WindowSize: time.Hour,
		LifecyclerConfig: ring.LifecyclerConfig{
			RingConfig: ring.Config{
				KVStore: kv.Config{
					Store: "inmemory",
				},
				ReplicationFactor: 1,
			},
			NumTokens:       1,
			ID:              "test",
			Zone:            "test",
			FinalSleep:      0,
			HeartbeatPeriod: 100 * time.Millisecond,
			ObservePeriod:   100 * time.Millisecond,
		},
	}
	s, err := NewIngestLimits(cfg, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)
	require.NotNil(t, s)
	require.NotNil(t, s.client)

	require.Equal(t, cfg, s.cfg)

	require.NotNil(t, s.metadata)
	require.NotNil(t, s.lifecycler)
}

func TestIngestLimits_evictOldStreams(t *testing.T) {
	tests := []struct {
		name                 string
		initialMetadata      map[string]map[int32][]streamMetadata
		windowSize           time.Duration
		assignedPartitionIDs []int32
		expectedMetadata     map[string]map[int32][]streamMetadata
		expectedEvictions    map[string]int
	}{
		{
			name: "all streams active",
			initialMetadata: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: []streamMetadata{
						{hash: 1, lastSeenAt: time.Now().UnixNano(), totalSize: 1000},
						{hash: 2, lastSeenAt: time.Now().UnixNano(), totalSize: 2000},
					},
				},
			},
			windowSize:           time.Hour,
			assignedPartitionIDs: []int32{0},
			expectedMetadata: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: []streamMetadata{
						{hash: 1, lastSeenAt: time.Now().UnixNano(), totalSize: 1000},
						{hash: 2, lastSeenAt: time.Now().UnixNano(), totalSize: 2000},
					},
				},
			},
			expectedEvictions: map[string]int{
				"tenant1": 0,
			},
		},
		{
			name: "all streams expired",
			initialMetadata: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: []streamMetadata{
						{hash: 1, lastSeenAt: time.Now().Add(-2 * time.Hour).UnixNano(), totalSize: 1000},
						{hash: 2, lastSeenAt: time.Now().Add(-2 * time.Hour).UnixNano(), totalSize: 2000},
					},
				},
			},
			windowSize:           time.Hour,
			assignedPartitionIDs: []int32{0},
			expectedMetadata:     map[string]map[int32][]streamMetadata{},
			expectedEvictions: map[string]int{
				"tenant1": 2,
			},
		},
		{
			name: "mixed active and expired streams",
			initialMetadata: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: []streamMetadata{
						{hash: 1, lastSeenAt: time.Now().UnixNano(), totalSize: 1000},
						{hash: 2, lastSeenAt: time.Now().Add(-2 * time.Hour).UnixNano(), totalSize: 2000},
						{hash: 3, lastSeenAt: time.Now().UnixNano(), totalSize: 3000},
					},
				},
			},
			windowSize:           time.Hour,
			assignedPartitionIDs: []int32{0},
			expectedMetadata: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: []streamMetadata{
						{hash: 1, lastSeenAt: time.Now().UnixNano(), totalSize: 1000},
						{hash: 3, lastSeenAt: time.Now().UnixNano(), totalSize: 3000},
					},
				},
			},
			expectedEvictions: map[string]int{
				"tenant1": 1,
			},
		},
		{
			name: "multiple tenants with mixed streams",
			initialMetadata: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: []streamMetadata{
						{hash: 1, lastSeenAt: time.Now().UnixNano(), totalSize: 1000},
						{hash: 2, lastSeenAt: time.Now().Add(-2 * time.Hour).UnixNano(), totalSize: 2000},
					},
				},
				"tenant2": {
					0: []streamMetadata{
						{hash: 3, lastSeenAt: time.Now().Add(-2 * time.Hour).UnixNano(), totalSize: 3000},
						{hash: 4, lastSeenAt: time.Now().Add(-2 * time.Hour).UnixNano(), totalSize: 4000},
					},
				},
				"tenant3": {
					0: []streamMetadata{
						{hash: 5, lastSeenAt: time.Now().UnixNano(), totalSize: 5000},
					},
				},
			},
			windowSize:           time.Hour,
			assignedPartitionIDs: []int32{0},
			expectedMetadata: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: []streamMetadata{
						{hash: 1, lastSeenAt: time.Now().UnixNano(), totalSize: 1000},
					},
				},
				"tenant3": {
					0: []streamMetadata{
						{hash: 5, lastSeenAt: time.Now().UnixNano(), totalSize: 5000},
					},
				},
			},
			expectedEvictions: map[string]int{
				"tenant1": 1,
				"tenant2": 2,
				"tenant3": 0,
			},
		},
		{
			name: "multiple partitions with some empty after eviction",
			initialMetadata: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: []streamMetadata{
						{hash: 1, lastSeenAt: time.Now().UnixNano(), totalSize: 1000},
						{hash: 2, lastSeenAt: time.Now().Add(-2 * time.Hour).UnixNano(), totalSize: 2000},
					},
					1: []streamMetadata{
						{hash: 3, lastSeenAt: time.Now().Add(-2 * time.Hour).UnixNano(), totalSize: 3000},
					},
					2: []streamMetadata{
						{hash: 4, lastSeenAt: time.Now().UnixNano(), totalSize: 4000},
					},
				},
			},
			windowSize:           time.Hour,
			assignedPartitionIDs: []int32{0, 1, 2},
			expectedMetadata: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: []streamMetadata{
						{hash: 1, lastSeenAt: time.Now().UnixNano(), totalSize: 1000},
					},
					2: []streamMetadata{
						{hash: 4, lastSeenAt: time.Now().UnixNano(), totalSize: 4000},
					},
				},
			},
			expectedEvictions: map[string]int{
				"tenant1": 2,
			},
		},
		{
			name: "unassigned partitions should still be evicted",
			initialMetadata: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: []streamMetadata{
						{hash: 1, lastSeenAt: time.Now().UnixNano(), totalSize: 1000},
					},
					1: []streamMetadata{
						{hash: 2, lastSeenAt: time.Now().Add(-2 * time.Hour).UnixNano(), totalSize: 2000},
					},
				},
			},
			windowSize:           time.Hour,
			assignedPartitionIDs: []int32{0},
			expectedMetadata: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: []streamMetadata{
						{hash: 1, lastSeenAt: time.Now().UnixNano(), totalSize: 1000},
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
			// Create a registry to capture metrics
			reg := prometheus.NewRegistry()

			// Create IngestLimits instance with mock data
			s := &IngestLimits{
				cfg: Config{
					WindowSize: tt.windowSize,
				},
				logger:           log.NewNopLogger(),
				metrics:          newMetrics(reg),
				metadata:         deepCopyMetadata(tt.initialMetadata),
				partitionManager: NewPartitionManager(log.NewNopLogger()),
			}

			// Assign the Partition IDs.
			partitions := make(map[string][]int32)
			partitions["test"] = make([]int32, 0, len(tt.assignedPartitionIDs))
			for _, partitionID := range tt.assignedPartitionIDs {
				partitions["test"] = append(partitions["test"], partitionID)
			}
			s.partitionManager.Assign(context.Background(), nil, partitions)

			// Call evictOldStreams
			s.evictOldStreams(context.Background())

			// Verify metadata after eviction
			require.Equal(t, len(tt.expectedMetadata), len(s.metadata), "number of tenants after eviction")

			for tenant, expectedPartitions := range tt.expectedMetadata {
				require.Contains(t, s.metadata, tenant, "tenant should exist after eviction")

				actualPartitions := s.metadata[tenant]
				require.Equal(t, len(expectedPartitions), len(actualPartitions),
					"number of partitions for tenant %s after eviction", tenant)

				for partitionID, expectedStreams := range expectedPartitions {
					require.Contains(t, actualPartitions, partitionID,
						"partition %d should exist for tenant %s after eviction", partitionID, tenant)

					actualStreams := actualPartitions[partitionID]
					require.Equal(t, len(expectedStreams), len(actualStreams),
						"number of streams for tenant %s partition %d after eviction", tenant, partitionID)

					// Check that all expected streams exist
					// Note: We don't check exact lastSeenAt timestamps as they're generated at test time
					streamMap := make(map[uint64]bool)
					for _, stream := range actualStreams {
						streamMap[stream.hash] = true
					}

					for _, expectedStream := range expectedStreams {
						require.True(t, streamMap[expectedStream.hash],
							"stream with hash %d should exist for tenant %s partition %d after eviction",
							expectedStream.hash, tenant, partitionID)
					}
				}
			}

			// Verify that tenants not in expectedMetadata don't exist in actual metadata
			for tenant := range tt.initialMetadata {
				if _, exists := tt.expectedMetadata[tenant]; !exists {
					require.NotContains(t, s.metadata, tenant,
						"tenant %s should not exist after eviction", tenant)
				}
			}
		})
	}
}

// Helper function to deep copy metadata map for testing
func deepCopyMetadata(src map[string]map[int32][]streamMetadata) map[string]map[int32][]streamMetadata {
	dst := make(map[string]map[int32][]streamMetadata)
	for tenant, partitions := range src {
		dst[tenant] = make(map[int32][]streamMetadata)
		for partitionID, streams := range partitions {
			dst[tenant][partitionID] = make([]streamMetadata, len(streams))
			copy(dst[tenant][partitionID], streams)
		}
	}
	return dst
}
