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
		metadata             *streamMetadata
		windowSize           time.Duration
		rateWindow           time.Duration
		bucketDuration       time.Duration

		// Request data for GetStreamUsage.
		tenantID     string
		partitionIDs []int32
		streamHashes []uint64

		// Expectations.
		expectedActive         uint64
		expectedRate           uint64
		expectedUnknownStreams []uint64
	}{
		{
			name: "tenant not found",
			// setup data
			assignedPartitionIDs: []int32{0},
			metadata: &streamMetadata{
				stripes: []map[string]map[int32][]Stream{
					{
						"tenant2": {
							0: []Stream{
								{Hash: 4, LastSeenAt: time.Now().UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: time.Now().UnixNano(), Size: 1000}}},
								{Hash: 5, LastSeenAt: time.Now().UnixNano(), TotalSize: 2000, RateBuckets: []RateBucket{{Timestamp: time.Now().UnixNano(), Size: 2000}}},
							},
						},
					},
				},
				locks: make([]stripeLock, 1),
			},
			windowSize:     time.Hour,
			rateWindow:     5 * time.Minute,
			bucketDuration: time.Minute,
			// request data
			tenantID:               "tenant1",
			partitionIDs:           []int32{0},
			streamHashes:           []uint64{4, 5},
			expectedUnknownStreams: []uint64{4, 5},
		},
		{
			name: "all streams active",
			// setup data
			assignedPartitionIDs: []int32{0},
			metadata: &streamMetadata{
				stripes: []map[string]map[int32][]Stream{
					{
						"tenant1": {
							0: []Stream{
								{Hash: 1, LastSeenAt: time.Now().UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: time.Now().UnixNano(), Size: 1000}}},
								{Hash: 2, LastSeenAt: time.Now().UnixNano(), TotalSize: 2000, RateBuckets: []RateBucket{{Timestamp: time.Now().UnixNano(), Size: 2000}}},
								{Hash: 3, LastSeenAt: time.Now().UnixNano(), TotalSize: 3000, RateBuckets: []RateBucket{{Timestamp: time.Now().UnixNano(), Size: 3000}}},
								{Hash: 4, LastSeenAt: time.Now().UnixNano(), TotalSize: 4000, RateBuckets: []RateBucket{{Timestamp: time.Now().UnixNano(), Size: 4000}}},
							},
						},
					},
				},
				locks: make([]stripeLock, 1),
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
			expectedRate:   uint64(10000) / uint64(5*60), // 10000 bytes / 5 minutes in seconds
		},
		{
			name: "mixed active and expired streams",
			// setup data
			assignedPartitionIDs: []int32{0},
			metadata: &streamMetadata{
				stripes: []map[string]map[int32][]Stream{
					{
						"tenant1": {
							0: []Stream{
								{Hash: 1, LastSeenAt: time.Now().UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: time.Now().UnixNano(), Size: 1000}}},
								{Hash: 2, LastSeenAt: time.Now().Add(-2 * time.Hour).UnixNano(), TotalSize: 2000}, // expired
								{Hash: 3, LastSeenAt: time.Now().UnixNano(), TotalSize: 3000, RateBuckets: []RateBucket{{Timestamp: time.Now().UnixNano(), Size: 3000}}},
								{Hash: 4, LastSeenAt: time.Now().Add(-2 * time.Hour).UnixNano(), TotalSize: 4000}, // expired
								{Hash: 5, LastSeenAt: time.Now().UnixNano(), TotalSize: 5000, RateBuckets: []RateBucket{{Timestamp: time.Now().UnixNano(), Size: 5000}}},
							},
						},
					},
				},
				locks: make([]stripeLock, 1),
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
			expectedRate:   uint64(9000) / uint64(5*60), // 9000 bytes / 5 minutes in seconds
		},
		{
			name: "all streams expired",
			// setup data
			assignedPartitionIDs: []int32{0},
			metadata: &streamMetadata{
				stripes: []map[string]map[int32][]Stream{
					{
						"tenant1": {
							0: []Stream{
								{Hash: 1, LastSeenAt: time.Now().Add(-2 * time.Hour).UnixNano(), TotalSize: 1000},
								{Hash: 2, LastSeenAt: time.Now().Add(-2 * time.Hour).UnixNano(), TotalSize: 2000},
							},
						},
					},
				},
				locks: make([]stripeLock, 1),
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
			metadata: &streamMetadata{
				stripes: []map[string]map[int32][]Stream{
					{
						"tenant1": {
							0: []Stream{
								{Hash: 1, LastSeenAt: time.Now().UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: time.Now().UnixNano(), Size: 1000}}},
								{Hash: 2, LastSeenAt: time.Now().UnixNano(), TotalSize: 2000, RateBuckets: []RateBucket{{Timestamp: time.Now().UnixNano(), Size: 2000}}},
							},
						},
					},
				},
				locks: make([]stripeLock, 1),
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
			expectedRate:   uint64(3000) / uint64(5*60), // 3000 bytes / 5 minutes in seconds
		},
		{
			name: "unknown streams requested",
			// setup data
			assignedPartitionIDs: []int32{0},
			metadata: &streamMetadata{
				stripes: []map[string]map[int32][]Stream{
					{
						"tenant1": {
							0: []Stream{
								{Hash: 1, LastSeenAt: time.Now().UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: time.Now().UnixNano(), Size: 1000}}},
								{Hash: 2, LastSeenAt: time.Now().UnixNano(), TotalSize: 2000, RateBuckets: []RateBucket{{Timestamp: time.Now().UnixNano(), Size: 2000}}},
								{Hash: 3, LastSeenAt: time.Now().UnixNano(), TotalSize: 3000, RateBuckets: []RateBucket{{Timestamp: time.Now().UnixNano(), Size: 3000}}},
								{Hash: 4, LastSeenAt: time.Now().UnixNano(), TotalSize: 4000, RateBuckets: []RateBucket{{Timestamp: time.Now().UnixNano(), Size: 4000}}},
								{Hash: 5, LastSeenAt: time.Now().UnixNano(), TotalSize: 5000, RateBuckets: []RateBucket{{Timestamp: time.Now().UnixNano(), Size: 5000}}},
							},
						},
					},
				},
				locks: make([]stripeLock, 1),
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
			expectedRate:           uint64(15000) / uint64(5*60), // 15000 bytes / 5 minutes in seconds
		},
		{
			name: "multiple assigned partitions",
			// setup data
			assignedPartitionIDs: []int32{0, 1},
			metadata: &streamMetadata{
				stripes: []map[string]map[int32][]Stream{
					{
						"tenant1": {
							0: []Stream{
								{Hash: 1, LastSeenAt: time.Now().UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: time.Now().UnixNano(), Size: 1000}}},
								{Hash: 2, LastSeenAt: time.Now().UnixNano(), TotalSize: 2000, RateBuckets: []RateBucket{{Timestamp: time.Now().UnixNano(), Size: 2000}}},
							},
						},
					},
					{
						"tenant1": {
							1: []Stream{
								{Hash: 3, LastSeenAt: time.Now().UnixNano(), TotalSize: 3000, RateBuckets: []RateBucket{{Timestamp: time.Now().UnixNano(), Size: 3000}}},
								{Hash: 4, LastSeenAt: time.Now().UnixNano(), TotalSize: 4000, RateBuckets: []RateBucket{{Timestamp: time.Now().UnixNano(), Size: 4000}}},
								{Hash: 5, LastSeenAt: time.Now().UnixNano(), TotalSize: 5000, RateBuckets: []RateBucket{{Timestamp: time.Now().UnixNano(), Size: 5000}}},
							},
						},
					},
				},
				locks: make([]stripeLock, 2),
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
			expectedRate:   uint64(15000) / uint64(5*60), // 15000 bytes / 5 minutes in seconds
		},
		{
			name: "multiple partitions with unassigned partitions",
			// setup data
			assignedPartitionIDs: []int32{0},
			metadata: &streamMetadata{
				stripes: []map[string]map[int32][]Stream{
					{
						"tenant1": {
							0: []Stream{
								{Hash: 1, LastSeenAt: time.Now().UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: time.Now().UnixNano(), Size: 1000}}},
								{Hash: 2, LastSeenAt: time.Now().UnixNano(), TotalSize: 2000, RateBuckets: []RateBucket{{Timestamp: time.Now().UnixNano(), Size: 2000}}},
							},
						},
					},
				},
				locks: make([]stripeLock, 1),
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
			expectedRate:           uint64(3000) / uint64(5*60), // 3000 bytes / 5 minutes in seconds
		},
		{
			name: "mixed buckets within and outside rate window",
			// setup data
			assignedPartitionIDs: []int32{0},
			metadata: &streamMetadata{
				stripes: []map[string]map[int32][]Stream{
					{
						"tenant1": {
							0: []Stream{
								{
									Hash:       1,
									LastSeenAt: time.Now().UnixNano(),
									TotalSize:  5000, // Total size includes all buckets
									RateBuckets: []RateBucket{
										{Timestamp: time.Now().Add(-10 * time.Minute).UnixNano(), Size: 1000}, // Outside rate window
										{Timestamp: time.Now().Add(-6 * time.Minute).UnixNano(), Size: 1500},  // Outside rate window
										{Timestamp: time.Now().Add(-4 * time.Minute).UnixNano(), Size: 1000},  // Inside rate window
										{Timestamp: time.Now().Add(-2 * time.Minute).UnixNano(), Size: 1500},  // Inside rate window
									},
								},
								{
									Hash:       2,
									LastSeenAt: time.Now().UnixNano(),
									TotalSize:  4000, // Total size includes all buckets
									RateBuckets: []RateBucket{
										{Timestamp: time.Now().Add(-8 * time.Minute).UnixNano(), Size: 1000}, // Outside rate window
										{Timestamp: time.Now().Add(-3 * time.Minute).UnixNano(), Size: 1500}, // Inside rate window
										{Timestamp: time.Now().Add(-1 * time.Minute).UnixNano(), Size: 1500}, // Inside rate window
									},
								},
							},
						},
					},
				},
				locks: make([]stripeLock, 1),
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
			expectedRate: uint64(5500) / uint64(5*60), // 5500 bytes / 5 minutes in seconds = 18.33, truncated to 18
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
	metadata := &streamMetadata{
		stripes: []map[string]map[int32][]Stream{
			{
				"tenant1": {
					0: []Stream{
						{Hash: 1, LastSeenAt: now.UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: now.UnixNano(), Size: 1000}}},                        // active
						{Hash: 2, LastSeenAt: now.Add(-30 * time.Minute).UnixNano(), TotalSize: 2000, RateBuckets: []RateBucket{{Timestamp: now.UnixNano(), Size: 2000}}}, // active
						{Hash: 3, LastSeenAt: now.Add(-2 * time.Hour).UnixNano(), TotalSize: 3000},                                                                        // expired
						{Hash: 4, LastSeenAt: now.Add(-45 * time.Minute).UnixNano(), TotalSize: 4000, RateBuckets: []RateBucket{{Timestamp: now.UnixNano(), Size: 4000}}}, // active
						{Hash: 5, LastSeenAt: now.Add(-3 * time.Hour).UnixNano(), TotalSize: 5000},                                                                        // expired
					},
				},
			},
		},
		locks: make([]stripeLock, 1),
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
		logger:           log.NewNopLogger(),
		metadata:         metadata,
		partitionManager: NewPartitionManager(log.NewNopLogger()),
		metrics:          newMetrics(prometheus.NewRegistry()),
	}

	// Run concurrent requests
	concurrency := 10
	done := make(chan struct{})
	for range concurrency {
		go func() {
			defer func() { done <- struct{}{} }()

			req := &logproto.GetStreamUsageRequest{
				Tenant:       "tenant1",
				StreamHashes: []uint64{1, 2, 3, 4, 5},
			}

			// Assign the Partition IDs.
			partitions := map[string][]int32{"tenant1": {0}}
			s.partitionManager.Assign(context.Background(), nil, partitions)

			resp, err := s.GetStreamUsage(context.Background(), req)
			require.NoError(t, err)
			require.NotNil(t, resp)
			require.Equal(t, "tenant1", resp.Tenant)
			require.Equal(t, uint64(3), resp.ActiveStreams) // Should count only the 3 active streams

			expectedRate := uint64(7000) / uint64(5*60)
			require.Equal(t, expectedRate, resp.Rate)
		}()
	}

	// Wait for all goroutines to complete
	for range concurrency {
		<-done
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
