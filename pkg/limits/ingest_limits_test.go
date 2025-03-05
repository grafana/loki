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
		name                   string
		tenant                 string
		partitions             []int32
		streamHashes           []uint64
		setupMetadata          map[string]map[int32][]streamMetadata
		windowSize             time.Duration
		expectedActive         uint64
		expectedUnknownStreams []uint64
		assignedPartitions     map[int32]int64
		expectedRate           int64
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
						{hash: 4, lastSeenAt: time.Now().UnixNano(), totalSize: 1000},
						{hash: 5, lastSeenAt: time.Now().UnixNano(), totalSize: 2000},
					},
				},
			},
			windowSize:     time.Hour,
			expectedActive: 0,
			expectedRate:   0,
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
						{hash: 1, lastSeenAt: time.Now().UnixNano(), totalSize: 1000},
						{hash: 2, lastSeenAt: time.Now().UnixNano(), totalSize: 2000},
						{hash: 3, lastSeenAt: time.Now().UnixNano(), totalSize: 3000},
						{hash: 4, lastSeenAt: time.Now().UnixNano(), totalSize: 4000},
					},
				},
			},
			windowSize:     time.Hour,
			expectedActive: 4,
			expectedRate:   int64(10000 / uint64(time.Hour.Seconds())),
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
						{hash: 1, lastSeenAt: time.Now().UnixNano(), totalSize: 1000},
						{hash: 2, lastSeenAt: time.Now().Add(-2 * time.Hour).UnixNano(), totalSize: 2000}, // expired
						{hash: 3, lastSeenAt: time.Now().UnixNano(), totalSize: 3000},
						{hash: 4, lastSeenAt: time.Now().Add(-2 * time.Hour).UnixNano(), totalSize: 4000}, // expired
						{hash: 5, lastSeenAt: time.Now().UnixNano(), totalSize: 5000},
					},
				},
			},
			windowSize:     time.Hour,
			expectedActive: 3,
			expectedRate:   int64(9000 / uint64(time.Hour.Seconds())),
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
						{hash: 1, lastSeenAt: time.Now().Add(-2 * time.Hour).UnixNano(), totalSize: 1000},
						{hash: 2, lastSeenAt: time.Now().Add(-2 * time.Hour).UnixNano(), totalSize: 2000},
					},
				},
			},
			windowSize:     time.Hour,
			expectedActive: 0,
			expectedRate:   0,
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
						{hash: 1, lastSeenAt: time.Now().UnixNano(), totalSize: 1000},
						{hash: 2, lastSeenAt: time.Now().UnixNano(), totalSize: 2000},
					},
				},
			},
			windowSize:     time.Hour,
			expectedActive: 2,
			expectedRate:   int64(3000 / uint64(time.Hour.Seconds())),
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
						{hash: 1, lastSeenAt: time.Now().UnixNano(), totalSize: 1000},
						{hash: 2, lastSeenAt: time.Now().UnixNano(), totalSize: 2000},
						{hash: 3, lastSeenAt: time.Now().UnixNano(), totalSize: 3000},
						{hash: 4, lastSeenAt: time.Now().UnixNano(), totalSize: 4000},
						{hash: 5, lastSeenAt: time.Now().UnixNano(), totalSize: 5000},
					},
				},
			},
			windowSize:             time.Hour,
			expectedActive:         5,
			expectedUnknownStreams: []uint64{6, 7, 8},
			expectedRate:           int64(15000 / uint64(time.Hour.Seconds())),
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
						{hash: 1, lastSeenAt: time.Now().UnixNano(), totalSize: 1000},
						{hash: 2, lastSeenAt: time.Now().UnixNano(), totalSize: 2000},
					},
					1: []streamMetadata{
						{hash: 3, lastSeenAt: time.Now().UnixNano(), totalSize: 3000},
						{hash: 4, lastSeenAt: time.Now().UnixNano(), totalSize: 4000},
						{hash: 5, lastSeenAt: time.Now().UnixNano(), totalSize: 5000},
					},
				},
			},
			windowSize:     time.Hour,
			expectedActive: 5,
			expectedRate:   int64(15000 / uint64(time.Hour.Seconds())),
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
						{hash: 1, lastSeenAt: time.Now().UnixNano(), totalSize: 1000},
						{hash: 2, lastSeenAt: time.Now().UnixNano(), totalSize: 2000},
					},
				},
			},
			windowSize:             time.Hour,
			expectedActive:         2,
			expectedUnknownStreams: []uint64{3, 4, 5},
			expectedRate:           int64(3000 / uint64(time.Hour.Seconds())),
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
				{hash: 1, lastSeenAt: now.UnixNano(), totalSize: 1000},                        // active
				{hash: 2, lastSeenAt: now.Add(-30 * time.Minute).UnixNano(), totalSize: 2000}, // active
				{hash: 3, lastSeenAt: now.Add(-2 * time.Hour).UnixNano(), totalSize: 3000},    // expired
				{hash: 4, lastSeenAt: now.Add(-45 * time.Minute).UnixNano(), totalSize: 4000}, // active
				{hash: 5, lastSeenAt: now.Add(-3 * time.Hour).UnixNano(), totalSize: 5000},    // expired
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
			// Total size of active streams: 1000 + 2000 + 4000 = 7000 bytes
			expectedRate := int64(7000 / uint64(time.Hour.Seconds()))
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
		name               string
		tenant             string
		partition          int32
		metadata           *logproto.StreamMetadata
		assignedPartitions map[int32]int64
		lastSeenAt         time.Time
		existingData       map[string]map[int32][]streamMetadata
		expectedData       map[string]map[int32][]streamMetadata
	}{
		{
			name:      "new tenant, new partition",
			tenant:    "tenant1",
			partition: 0,
			metadata: &logproto.StreamMetadata{
				StreamHash:             123,
				EntriesSize:            1000,
				StructuredMetadataSize: 500,
			},
			assignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
			lastSeenAt:   time.Unix(100, 0),
			existingData: map[string]map[int32][]streamMetadata{},
			expectedData: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: {
						{hash: 123, lastSeenAt: time.Unix(100, 0).UnixNano(), totalSize: 1500},
					},
				},
			},
		},
		{
			name:      "existing tenant, new partition",
			tenant:    "tenant1",
			partition: 1,
			metadata: &logproto.StreamMetadata{
				StreamHash:             456,
				EntriesSize:            2000,
				StructuredMetadataSize: 1000,
			},
			assignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
				1: time.Now().UnixNano(),
			},
			lastSeenAt: time.Unix(200, 0),
			existingData: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: {
						{hash: 123, lastSeenAt: time.Unix(100, 0).UnixNano(), totalSize: 1500},
					},
				},
			},
			expectedData: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: {
						{hash: 123, lastSeenAt: time.Unix(100, 0).UnixNano(), totalSize: 1500},
					},
					1: {
						{hash: 456, lastSeenAt: time.Unix(200, 0).UnixNano(), totalSize: 3000},
					},
				},
			},
		},
		{
			name:      "update existing stream",
			tenant:    "tenant1",
			partition: 0,
			metadata: &logproto.StreamMetadata{
				StreamHash:             123,
				EntriesSize:            3000,
				StructuredMetadataSize: 1500,
			},
			assignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
			lastSeenAt: time.Unix(300, 0),
			existingData: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: {
						{hash: 123, lastSeenAt: time.Unix(100, 0).UnixNano(), totalSize: 1500},
					},
				},
			},
			expectedData: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: {
						{hash: 123, lastSeenAt: time.Unix(300, 0).UnixNano(), totalSize: 6000},
					},
				},
			},
		},
		{
			name:      "evict stream from partition",
			tenant:    "tenant1",
			partition: 0,
			metadata: &logproto.StreamMetadata{
				StreamHash:             123,
				EntriesSize:            4000,
				StructuredMetadataSize: 2000,
			},
			assignedPartitions: map[int32]int64{
				1: time.Now().UnixNano(),
			},
			lastSeenAt: time.Unix(400, 0),
			existingData: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: {
						{hash: 123, lastSeenAt: time.Unix(100, 0).UnixNano(), totalSize: 1500},
						{hash: 456, lastSeenAt: time.Unix(200, 0).UnixNano(), totalSize: 3000},
					},
				},
			},
			expectedData: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: {
						{hash: 456, lastSeenAt: time.Unix(200, 0).UnixNano(), totalSize: 3000},
					},
				},
			},
		},
		{
			name:      "zero structured metadata size",
			tenant:    "tenant1",
			partition: 0,
			metadata: &logproto.StreamMetadata{
				StreamHash:             789,
				EntriesSize:            5000,
				StructuredMetadataSize: 0,
			},
			assignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
			lastSeenAt:   time.Unix(500, 0),
			existingData: map[string]map[int32][]streamMetadata{},
			expectedData: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: {
						{hash: 789, lastSeenAt: time.Unix(500, 0).UnixNano(), totalSize: 5000},
					},
				},
			},
		},
		{
			name:      "zero entries size",
			tenant:    "tenant1",
			partition: 0,
			metadata: &logproto.StreamMetadata{
				StreamHash:             999,
				EntriesSize:            0,
				StructuredMetadataSize: 3000,
			},
			assignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
			lastSeenAt:   time.Unix(600, 0),
			existingData: map[string]map[int32][]streamMetadata{},
			expectedData: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: {
						{hash: 999, lastSeenAt: time.Unix(600, 0).UnixNano(), totalSize: 3000},
					},
				},
			},
		},
		{
			name:      "update with larger sizes",
			tenant:    "tenant1",
			partition: 0,
			metadata: &logproto.StreamMetadata{
				StreamHash:             123,
				EntriesSize:            10000,
				StructuredMetadataSize: 5000,
			},
			assignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
			lastSeenAt: time.Unix(700, 0),
			existingData: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: {
						{hash: 123, lastSeenAt: time.Unix(100, 0).UnixNano(), totalSize: 1500},
					},
				},
			},
			expectedData: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: {
						{hash: 123, lastSeenAt: time.Unix(700, 0).UnixNano(), totalSize: 16500},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &IngestLimits{
				assingedPartitions: tt.assignedPartitions,
				metadata:           tt.existingData,
				metrics:            newMetrics(prometheus.NewRegistry()),
			}

			s.updateMetadata(tt.metadata, tt.tenant, tt.partition, tt.lastSeenAt)

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
	s, err := NewIngestLimits(cfg, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)
	require.NotNil(t, s)
	require.NotNil(t, s.client)

	// The NewIngestLimits function sets a default RateWindow value of 5 minutes
	// if it's not specified in the config
	expectedCfg := cfg
	expectedCfg.RateWindow = 5 * time.Minute
	require.Equal(t, expectedCfg, s.cfg)

	require.NotNil(t, s.metadata)
	require.NotNil(t, s.lifecycler)
}

func TestIngestLimits_evictOldStreams(t *testing.T) {
	tests := []struct {
		name               string
		initialMetadata    map[string]map[int32][]streamMetadata
		windowSize         time.Duration
		assignedPartitions map[int32]int64
		expectedMetadata   map[string]map[int32][]streamMetadata
		expectedEvictions  map[string]int
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
			windowSize: time.Hour,
			assignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
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
			windowSize: time.Hour,
			assignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
			expectedMetadata: map[string]map[int32][]streamMetadata{},
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
			windowSize: time.Hour,
			assignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
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
			windowSize: time.Hour,
			assignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
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
			windowSize: time.Hour,
			assignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
				1: time.Now().UnixNano(),
				2: time.Now().UnixNano(),
			},
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
			windowSize: time.Hour,
			assignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
				// Partition 1 is not assigned
			},
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
				logger:             log.NewNopLogger(),
				metrics:            newMetrics(reg),
				metadata:           deepCopyMetadata(tt.initialMetadata),
				assingedPartitions: tt.assignedPartitions,
			}

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
