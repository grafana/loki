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

type mockPartitionRing struct {
	ring.PartitionRingReader
}

func (m *mockPartitionRing) PartitionRing() *ring.PartitionRing {
	return ring.NewPartitionRing(ring.PartitionRingDesc{
		Partitions: map[int32]ring.PartitionDesc{
			0: {Id: 0, Tokens: []uint32{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}},
		},
	})
}

func TestIngestLimits_GetStreamUsage(t *testing.T) {
	tests := []struct {
		name                   string
		tenant                 string
		partitions             []int32
		streamHashes           []uint64
		setupMetadata          map[string]map[int32][]streamMetadata
		windowSize             time.Duration
		expectedActive         uint64
		expectedUnknownStreams []uint64
		assignedPartitions     map[int32]int64
	}{
		{
			name:       "tenant not found",
			tenant:     "tenant1",
			partitions: []int32{0},
			assignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
			streamHashes: []uint64{4, 5},
			setupMetadata: map[string]map[int32][]streamMetadata{
				"tenant2": {
					0: []streamMetadata{
						{hash: 4, lastSeenAt: time.Now().UnixNano()},
						{hash: 5, lastSeenAt: time.Now().UnixNano()},
					},
				},
			},
			windowSize:     time.Hour,
			expectedActive: 0,
		},
		{
			name:       "all streams active",
			tenant:     "tenant1",
			partitions: []int32{0},
			assignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
			streamHashes: []uint64{1, 2, 3, 4},
			setupMetadata: map[string]map[int32][]streamMetadata{
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
			expectedActive: 4,
		},
		{
			name:       "mixed active and expired streams",
			tenant:     "tenant1",
			partitions: []int32{0},
			assignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
			streamHashes: []uint64{1, 3, 5},
			setupMetadata: map[string]map[int32][]streamMetadata{
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
			expectedActive: 3,
		},
		{
			name:   "all streams expired",
			tenant: "tenant1",
			assignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
			setupMetadata: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: []streamMetadata{
						{hash: 1, lastSeenAt: time.Now().Add(-2 * time.Hour).UnixNano()},
						{hash: 2, lastSeenAt: time.Now().Add(-2 * time.Hour).UnixNano()},
					},
				},
			},
			windowSize:     time.Hour,
			expectedActive: 0,
		},
		{
			name:       "empty stream hashes",
			tenant:     "tenant1",
			partitions: []int32{0},
			assignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
			streamHashes: []uint64{},
			setupMetadata: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: []streamMetadata{
						{hash: 1, lastSeenAt: time.Now().UnixNano()},
						{hash: 2, lastSeenAt: time.Now().UnixNano()},
					},
				},
			},
			windowSize:     time.Hour,
			expectedActive: 2,
		},
		{
			name:       "unknown streams requested",
			tenant:     "tenant1",
			partitions: []int32{0},
			assignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
			streamHashes: []uint64{6, 7, 8},
			setupMetadata: map[string]map[int32][]streamMetadata{
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
			expectedActive:         5,
			expectedUnknownStreams: []uint64{6, 7, 8},
		},
		{
			name:       "multiple assigned partitions",
			tenant:     "tenant1",
			partitions: []int32{0, 1},
			assignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
				1: time.Now().UnixNano(),
			},
			streamHashes: []uint64{1, 2, 3, 4, 5},
			setupMetadata: map[string]map[int32][]streamMetadata{
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
			windowSize:     time.Hour,
			expectedActive: 5,
		},
		{
			name:       "multiple partitions with unasigned partitions",
			tenant:     "tenant1",
			partitions: []int32{0, 1},
			assignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
			streamHashes: []uint64{1, 2, 3, 4, 5},
			setupMetadata: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: []streamMetadata{
						{hash: 1, lastSeenAt: time.Now().UnixNano()},
						{hash: 2, lastSeenAt: time.Now().UnixNano()},
					},
				},
			},
			windowSize:             time.Hour,
			expectedActive:         2,
			expectedUnknownStreams: []uint64{3, 4, 5},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create IngestLimits instance with mock data
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
				partitionRing:      &mockPartitionRing{},
				logger:             log.NewNopLogger(),
				metrics:            newMetrics(prometheus.NewRegistry()),
				metadata:           tt.setupMetadata,
				assingedPartitions: tt.assignedPartitions,
			}

			// Create request
			req := &logproto.GetStreamUsageRequest{
				Tenant:       tt.tenant,
				Partitions:   tt.partitions,
				StreamHashes: tt.streamHashes,
			}

			// Call GetStreamUsage
			resp, err := s.GetStreamUsage(context.Background(), req)
			require.NoError(t, err)
			require.NotNil(t, resp)
			require.Equal(t, tt.tenant, resp.Tenant)
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
		partitionRing: &mockPartitionRing{},
		logger:        log.NewNopLogger(),
		metadata:      metadata,
		metrics:       newMetrics(prometheus.NewRegistry()),
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
		name         string
		tenant       string
		partition    int32
		metadata     *logproto.StreamMetadata
		lastSeenAt   time.Time
		evict        bool
		existingData map[string]map[int32][]streamMetadata
		expectedData map[string]map[int32][]streamMetadata
	}{
		{
			name:      "new tenant, new partition",
			tenant:    "tenant1",
			partition: 0,
			metadata: &logproto.StreamMetadata{
				StreamHash: 123,
			},
			lastSeenAt:   time.Unix(100, 0),
			evict:        false,
			existingData: map[string]map[int32][]streamMetadata{},
			expectedData: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: {
						{hash: 123, lastSeenAt: time.Unix(100, 0).UnixNano()},
					},
				},
			},
		},
		{
			name:      "existing tenant, new partition",
			tenant:    "tenant1",
			partition: 1,
			metadata: &logproto.StreamMetadata{
				StreamHash: 456,
			},
			lastSeenAt: time.Unix(200, 0),
			evict:      false,
			existingData: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: {
						{hash: 123, lastSeenAt: time.Unix(100, 0).UnixNano()},
					},
				},
			},
			expectedData: map[string]map[int32][]streamMetadata{
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
			name:      "update existing stream",
			tenant:    "tenant1",
			partition: 0,
			metadata: &logproto.StreamMetadata{
				StreamHash: 123,
			},
			lastSeenAt: time.Unix(300, 0),
			evict:      false,
			existingData: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: {
						{hash: 123, lastSeenAt: time.Unix(100, 0).UnixNano()},
					},
				},
			},
			expectedData: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: {
						{hash: 123, lastSeenAt: time.Unix(300, 0).UnixNano()},
					},
				},
			},
		},
		{
			name:      "evict stream from partition",
			tenant:    "tenant1",
			partition: 0,
			metadata: &logproto.StreamMetadata{
				StreamHash: 123,
			},
			lastSeenAt: time.Unix(400, 0),
			evict:      true,
			existingData: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: {
						{hash: 123, lastSeenAt: time.Unix(100, 0).UnixNano()},
						{hash: 456, lastSeenAt: time.Unix(200, 0).UnixNano()},
					},
				},
			},
			expectedData: map[string]map[int32][]streamMetadata{
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
				metadata: tt.existingData,
				metrics:  newMetrics(prometheus.NewRegistry()),
			}

			s.updateMetadata(tt.metadata, tt.tenant, tt.partition, tt.lastSeenAt, tt.evict)

			require.Equal(t, tt.expectedData, s.metadata)
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

	s, err := NewIngestLimits(cfg, &mockPartitionRing{}, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)
	require.NotNil(t, s)
	require.NotNil(t, s.client)
	require.Equal(t, cfg, s.cfg)
	require.NotNil(t, s.metadata)
	require.NotNil(t, s.lifecycler)
}
