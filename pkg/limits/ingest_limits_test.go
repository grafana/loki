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

	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestIngestLimits_GetStreamUsage(t *testing.T) {
	tests := []struct {
		name string

		// Setup data.
		assignedPartitionIDs []int32
		metadata             map[string]map[int32][]streamMetadata
		windowSize           time.Duration

		// Request data for GetStreamUsage.
		tenantID     string
		partitionIDs []int32
		streamHashes []uint64

		// Expectations.
		expectedActive         uint64
		expectedUnknownStreams []uint64
	}{
		{
			name:                 "tenant not found",
			assignedPartitionIDs: []int32{0},
			metadata: map[string]map[int32][]streamMetadata{
				"tenant2": {
					0: []streamMetadata{
						{hash: 4, lastSeenAt: time.Now().UnixNano()},
						{hash: 5, lastSeenAt: time.Now().UnixNano()},
					},
				},
			},
			windowSize:   time.Hour,
			tenantID:     "tenant1",
			partitionIDs: []int32{0},
			streamHashes: []uint64{4, 5},
		},
		{
			name:                 "all streams active",
			assignedPartitionIDs: []int32{0},
			metadata: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: []streamMetadata{
						{hash: 1, lastSeenAt: time.Now().UnixNano()},
						{hash: 2, lastSeenAt: time.Now().UnixNano()},
						{hash: 3, lastSeenAt: time.Now().UnixNano()},
						{hash: 4, lastSeenAt: time.Now().UnixNano()},
					},
				},
			},
			windowSize:     time.Hour,
			tenantID:       "tenant1",
			partitionIDs:   []int32{0},
			streamHashes:   []uint64{1, 2, 3, 4},
			expectedActive: 4,
		},
		{
			name:                 "mixed active and expired streams",
			assignedPartitionIDs: []int32{0},
			metadata: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: []streamMetadata{
						{hash: 1, lastSeenAt: time.Now().UnixNano()},
						{hash: 2, lastSeenAt: time.Now().Add(-2 * time.Hour).UnixNano()}, // expired
						{hash: 3, lastSeenAt: time.Now().UnixNano()},
						{hash: 4, lastSeenAt: time.Now().Add(-2 * time.Hour).UnixNano()}, // expired
						{hash: 5, lastSeenAt: time.Now().UnixNano()},
					},
				},
			},
			windowSize:     time.Hour,
			tenantID:       "tenant1",
			partitionIDs:   []int32{0},
			streamHashes:   []uint64{1, 3, 5},
			expectedActive: 3,
		},
		{
			name:                 "all streams expired",
			assignedPartitionIDs: []int32{0},
			metadata: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: []streamMetadata{
						{hash: 1, lastSeenAt: time.Now().Add(-2 * time.Hour).UnixNano()},
						{hash: 2, lastSeenAt: time.Now().Add(-2 * time.Hour).UnixNano()},
					},
				},
			},
			windowSize: time.Hour,
			tenantID:   "tenant1",
		},
		{
			name:                 "empty stream hashes",
			assignedPartitionIDs: []int32{0},
			metadata: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: []streamMetadata{
						{hash: 1, lastSeenAt: time.Now().UnixNano()},
						{hash: 2, lastSeenAt: time.Now().UnixNano()},
					},
				},
			},
			windowSize:     time.Hour,
			tenantID:       "tenant1",
			partitionIDs:   []int32{0},
			streamHashes:   []uint64{},
			expectedActive: 2,
		},
		{
			name:                 "unknown streams requested",
			assignedPartitionIDs: []int32{0},
			metadata: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: []streamMetadata{
						{hash: 1, lastSeenAt: time.Now().UnixNano()},
						{hash: 2, lastSeenAt: time.Now().UnixNano()},
						{hash: 3, lastSeenAt: time.Now().UnixNano()},
						{hash: 4, lastSeenAt: time.Now().UnixNano()},
						{hash: 5, lastSeenAt: time.Now().UnixNano()},
					},
				},
			},
			windowSize:             time.Hour,
			tenantID:               "tenant1",
			partitionIDs:           []int32{0},
			streamHashes:           []uint64{6, 7, 8},
			expectedActive:         5,
			expectedUnknownStreams: []uint64{6, 7, 8},
		},
		{
			name:                 "multiple assigned partitions",
			assignedPartitionIDs: []int32{0, 1},
			metadata: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: []streamMetadata{
						{hash: 1, lastSeenAt: time.Now().UnixNano()},
						{hash: 2, lastSeenAt: time.Now().UnixNano()},
					},
					1: []streamMetadata{
						{hash: 3, lastSeenAt: time.Now().UnixNano()},
						{hash: 4, lastSeenAt: time.Now().UnixNano()},
						{hash: 5, lastSeenAt: time.Now().UnixNano()},
					},
				},
			},
			tenantID:       "tenant1",
			partitionIDs:   []int32{0, 1},
			streamHashes:   []uint64{1, 2, 3, 4, 5},
			windowSize:     time.Hour,
			expectedActive: 5,
		},
		{
			name:                 "multiple partitions with unasigned partitions",
			assignedPartitionIDs: []int32{0},
			metadata: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: []streamMetadata{
						{hash: 1, lastSeenAt: time.Now().UnixNano()},
						{hash: 2, lastSeenAt: time.Now().UnixNano()},
					},
				},
			},
			windowSize:             time.Hour,
			tenantID:               "tenant1",
			partitionIDs:           []int32{0, 1},
			streamHashes:           []uint64{1, 2, 3, 4, 5},
			expectedActive:         2,
			expectedUnknownStreams: []uint64{3, 4, 5},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &IngestLimits{
				cfg: Config{
					WindowSize: tt.windowSize,
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
		})
	}
}

func TestIngestLimits_GetStreamUsage_Concurrent(t *testing.T) {
	// Setup test data with a mix of active and expired streams
	now := time.Now()
	metadata := map[string]map[int32][]streamMetadata{
		"tenant1": {
			0: []streamMetadata{
				{hash: 1, lastSeenAt: now.UnixNano()},                        // active
				{hash: 2, lastSeenAt: now.Add(-30 * time.Minute).UnixNano()}, // active
				{hash: 3, lastSeenAt: now.Add(-2 * time.Hour).UnixNano()},    // expired
				{hash: 4, lastSeenAt: now.Add(-45 * time.Minute).UnixNano()}, // active
				{hash: 5, lastSeenAt: now.Add(-3 * time.Hour).UnixNano()},    // expired
			},
		},
	}

	s := &IngestLimits{
		cfg: Config{
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
			updateMetadata: &logproto.StreamMetadata{
				StreamHash: 123,
			},
			expected: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: {
						{hash: 123, lastSeenAt: time.Unix(100, 0).UnixNano()},
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
						{hash: 123, lastSeenAt: time.Unix(100, 0).UnixNano()},
					},
				},
			},
			tenantID:    "tenant1",
			partitionID: 1,
			updateMetadata: &logproto.StreamMetadata{
				StreamHash: 456,
			},
			lastSeenAt: time.Unix(200, 0),
			expected: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: {
						{hash: 123, lastSeenAt: time.Unix(100, 0).UnixNano()},
					},
					1: {
						{hash: 456, lastSeenAt: time.Unix(200, 0).UnixNano()},
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
						{hash: 123, lastSeenAt: time.Unix(100, 0).UnixNano()},
					},
				},
			},
			tenantID:    "tenant1",
			partitionID: 0,
			updateMetadata: &logproto.StreamMetadata{
				StreamHash: 123,
			},
			lastSeenAt: time.Unix(300, 0),
			expected: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: {
						{hash: 123, lastSeenAt: time.Unix(300, 0).UnixNano()},
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
						{hash: 123, lastSeenAt: time.Unix(100, 0).UnixNano()},
						{hash: 456, lastSeenAt: time.Unix(200, 0).UnixNano()},
					},
				},
			},
			tenantID:    "tenant1",
			partitionID: 0,
			updateMetadata: &logproto.StreamMetadata{
				StreamHash: 123,
			},
			lastSeenAt: time.Unix(400, 0),
			expected: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: {
						{hash: 456, lastSeenAt: time.Unix(200, 0).UnixNano()},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &IngestLimits{
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

			require.Equal(t, tt.expected, s.metadata)
		})
	}
}
