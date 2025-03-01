package limits

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/limiter"
	"github.com/grafana/dskit/ring"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/logproto"
)

type mockLimits struct {
	ingestionRate  float64
	ingestionBurst int
}

func (m *mockLimits) IngestionRateBytes(_ string) float64 {
	return m.ingestionRate
}

func (m *mockLimits) IngestionBurstSizeBytes(_ string) int {
	return m.ingestionBurst
}

func TestIngestLimits_GetStreamUsage_ActiveStreams(t *testing.T) {
	tests := []struct {
		name                   string
		tenant                 string
		partitions             []int32
		streamUsages           []*logproto.StreamUsage
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
			streamUsages: []*logproto.StreamUsage{
				{StreamHash: 4, TotalSize: 100},
				{StreamHash: 5, TotalSize: 200},
			},
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
			streamUsages: []*logproto.StreamUsage{
				{StreamHash: 1, TotalSize: 100},
				{StreamHash: 2, TotalSize: 200},
				{StreamHash: 3, TotalSize: 300},
				{StreamHash: 4, TotalSize: 400},
			},
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
			streamUsages: []*logproto.StreamUsage{
				{StreamHash: 1, TotalSize: 100},
				{StreamHash: 3, TotalSize: 300},
				{StreamHash: 5, TotalSize: 500},
			},
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
			streamUsages: []*logproto.StreamUsage{},
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
			streamUsages: []*logproto.StreamUsage{
				{StreamHash: 6, TotalSize: 600},
				{StreamHash: 7, TotalSize: 700},
				{StreamHash: 8, TotalSize: 800},
			},
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
			streamUsages: []*logproto.StreamUsage{
				{StreamHash: 1, TotalSize: 100},
				{StreamHash: 2, TotalSize: 200},
				{StreamHash: 3, TotalSize: 300},
				{StreamHash: 4, TotalSize: 400},
				{StreamHash: 5, TotalSize: 500},
			},
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
			streamUsages: []*logproto.StreamUsage{
				{StreamHash: 1, TotalSize: 100},
				{StreamHash: 2, TotalSize: 200},
				{StreamHash: 3, TotalSize: 300},
				{StreamHash: 4, TotalSize: 400},
				{StreamHash: 5, TotalSize: 500},
			},
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
			limits := &mockLimits{
				ingestionRate:  1000,
				ingestionBurst: 1000,
			}

			rateLimiter := limiter.NewRateLimiter(newIngestionRateStrategy(limits), 10*time.Second)

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
				rateLimiter:        rateLimiter,
				logger:             log.NewNopLogger(),
				metrics:            newMetrics(prometheus.NewRegistry()),
				metadata:           tt.setupMetadata,
				assingedPartitions: tt.assignedPartitions,
			}

			// Create request
			req := &logproto.GetStreamUsageRequest{
				Tenant:       tt.tenant,
				Partitions:   tt.partitions,
				StreamUsages: tt.streamUsages,
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

func TestIngestLimits_GetStreamUsage_ActiveStreams_Concurrent(t *testing.T) {
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

	limits := &mockLimits{
		ingestionRate:  1000,
		ingestionBurst: 1000,
	}

	rateLimiter := limiter.NewRateLimiter(newIngestionRateStrategy(limits), 10*time.Second)

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
		logger:      log.NewNopLogger(),
		metadata:    metadata,
		metrics:     newMetrics(prometheus.NewRegistry()),
		rateLimiter: rateLimiter,
	}

	// Run concurrent requests
	concurrency := 10
	done := make(chan struct{})
	for i := 0; i < concurrency; i++ {
		go func() {
			defer func() { done <- struct{}{} }()

			req := &logproto.GetStreamUsageRequest{
				Tenant:     "tenant1",
				Partitions: []int32{0},
				StreamUsages: []*logproto.StreamUsage{
					{StreamHash: 1, TotalSize: 100},
					{StreamHash: 2, TotalSize: 200},
					{StreamHash: 3, TotalSize: 300},
					{StreamHash: 4, TotalSize: 400},
					{StreamHash: 5, TotalSize: 500},
				},
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
				LineSize:               1000,
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
				LineSize:               2000,
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
				LineSize:               3000,
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
						{hash: 123, lastSeenAt: time.Unix(300, 0).UnixNano(), totalSize: 4500},
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
				LineSize:               4000,
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
				LineSize:               5000,
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
			name:      "zero line size",
			tenant:    "tenant1",
			partition: 0,
			metadata: &logproto.StreamMetadata{
				StreamHash:             999,
				LineSize:               0,
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
				LineSize:               10000,
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
						{hash: 123, lastSeenAt: time.Unix(700, 0).UnixNano(), totalSize: 15000},
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

	limits := &mockLimits{
		ingestionRate:  1000,
		ingestionBurst: 1000,
	}

	s, err := NewIngestLimits(cfg, limits, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)
	require.NotNil(t, s)
	require.NotNil(t, s.client)
	require.Equal(t, cfg, s.cfg)
	require.NotNil(t, s.metadata)
	require.NotNil(t, s.lifecycler)
}

func TestIngestLimits_GetStreamUsage_RateLimiter(t *testing.T) {
	tests := []struct {
		name                   string
		tenant                 string
		partitions             []int32
		streamUsages           []*logproto.StreamUsage
		setupMetadata          map[string]map[int32][]streamMetadata
		windowSize             time.Duration
		tenantLimits           map[string]*mockLimits
		expectedRateLimited    []uint64
		expectedNotRateLimited []uint64
		assignedPartitions     map[int32]int64
	}{
		{
			name:       "no streams rate limited",
			tenant:     "tenant1",
			partitions: []int32{0},
			assignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
			streamUsages: []*logproto.StreamUsage{
				{StreamHash: 1, TotalSize: 100},
				{StreamHash: 2, TotalSize: 200},
				{StreamHash: 3, TotalSize: 300},
			},
			setupMetadata: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: []streamMetadata{
						{hash: 1, lastSeenAt: time.Now().UnixNano()},
						{hash: 2, lastSeenAt: time.Now().UnixNano()},
						{hash: 3, lastSeenAt: time.Now().UnixNano()},
					},
				},
			},
			windowSize: time.Hour,
			tenantLimits: map[string]*mockLimits{
				"tenant1": {
					ingestionRate:  1000,
					ingestionBurst: 1000,
				},
			},
			expectedRateLimited:    []uint64{},
			expectedNotRateLimited: []uint64{1, 2, 3},
		},
		{
			name:       "all streams rate limited",
			tenant:     "tenant1",
			partitions: []int32{0},
			assignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
			streamUsages: []*logproto.StreamUsage{
				{StreamHash: 1, TotalSize: 100},
				{StreamHash: 2, TotalSize: 200},
				{StreamHash: 3, TotalSize: 300},
			},
			setupMetadata: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: []streamMetadata{
						{hash: 1, lastSeenAt: time.Now().UnixNano()},
						{hash: 2, lastSeenAt: time.Now().UnixNano()},
						{hash: 3, lastSeenAt: time.Now().UnixNano()},
					},
				},
			},
			windowSize: time.Hour,
			tenantLimits: map[string]*mockLimits{
				"tenant1": {
					ingestionRate:  10,
					ingestionBurst: 10,
				},
			},
			expectedRateLimited:    []uint64{1, 2, 3},
			expectedNotRateLimited: []uint64{},
		},
		{
			name:       "some streams rate limited",
			tenant:     "tenant1",
			partitions: []int32{0},
			assignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
			streamUsages: []*logproto.StreamUsage{
				{StreamHash: 1, TotalSize: 50},
				{StreamHash: 2, TotalSize: 150},
				{StreamHash: 3, TotalSize: 250},
			},
			setupMetadata: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: []streamMetadata{
						{hash: 1, lastSeenAt: time.Now().UnixNano()},
						{hash: 2, lastSeenAt: time.Now().UnixNano()},
						{hash: 3, lastSeenAt: time.Now().UnixNano()},
					},
				},
			},
			windowSize: time.Hour,
			tenantLimits: map[string]*mockLimits{
				"tenant1": {
					ingestionRate:  100,
					ingestionBurst: 100,
				},
			},
			expectedRateLimited:    []uint64{2, 3},
			expectedNotRateLimited: []uint64{1},
		},
		{
			name:       "unknown streams not rate limited",
			tenant:     "tenant1",
			partitions: []int32{0},
			assignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
			streamUsages: []*logproto.StreamUsage{
				{StreamHash: 4, TotalSize: 500},
				{StreamHash: 5, TotalSize: 600},
			},
			setupMetadata: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: []streamMetadata{
						{hash: 1, lastSeenAt: time.Now().UnixNano()},
						{hash: 2, lastSeenAt: time.Now().UnixNano()},
						{hash: 3, lastSeenAt: time.Now().UnixNano()},
					},
				},
			},
			windowSize: time.Hour,
			tenantLimits: map[string]*mockLimits{
				"tenant1": {
					ingestionRate:  10,
					ingestionBurst: 10,
				},
			},
			expectedRateLimited:    []uint64{},
			expectedNotRateLimited: []uint64{},
		},
		{
			name:       "expired streams not rate limited",
			tenant:     "tenant1",
			partitions: []int32{0},
			assignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
			streamUsages: []*logproto.StreamUsage{
				{StreamHash: 1, TotalSize: 100},
				{StreamHash: 2, TotalSize: 200},
			},
			setupMetadata: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: []streamMetadata{
						{hash: 1, lastSeenAt: time.Now().Add(-2 * time.Hour).UnixNano()},
						{hash: 2, lastSeenAt: time.Now().Add(-2 * time.Hour).UnixNano()},
					},
				},
			},
			windowSize: time.Hour,
			tenantLimits: map[string]*mockLimits{
				"tenant1": {
					ingestionRate:  10,
					ingestionBurst: 10,
				},
			},
			expectedRateLimited:    []uint64{},
			expectedNotRateLimited: []uint64{},
		},
		{
			name:       "different tenants with different rate limits",
			tenant:     "tenant2",
			partitions: []int32{0},
			assignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
			streamUsages: []*logproto.StreamUsage{
				{StreamHash: 101, TotalSize: 200},
			},
			setupMetadata: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: []streamMetadata{
						{hash: 1, lastSeenAt: time.Now().UnixNano()},
						{hash: 2, lastSeenAt: time.Now().UnixNano()},
					},
				},
				"tenant2": {
					0: []streamMetadata{
						{hash: 101, lastSeenAt: time.Now().UnixNano()},
						{hash: 102, lastSeenAt: time.Now().UnixNano()},
					},
				},
			},
			windowSize: time.Hour,
			tenantLimits: map[string]*mockLimits{
				"tenant1": {
					ingestionRate:  1000,
					ingestionBurst: 1000,
				},
				"tenant2": {
					ingestionRate:  100,
					ingestionBurst: 100,
				},
			},
			expectedRateLimited:    []uint64{101},
			expectedNotRateLimited: []uint64{},
		},
		{
			name:       "multiple tenants with different limits",
			tenant:     "tenant3",
			partitions: []int32{0},
			assignedPartitions: map[int32]int64{
				0: time.Now().UnixNano(),
			},
			streamUsages: []*logproto.StreamUsage{
				{StreamHash: 201, TotalSize: 150},
				{StreamHash: 202, TotalSize: 250},
			},
			setupMetadata: map[string]map[int32][]streamMetadata{
				"tenant1": {
					0: []streamMetadata{
						{hash: 1, lastSeenAt: time.Now().UnixNano()},
					},
				},
				"tenant2": {
					0: []streamMetadata{
						{hash: 101, lastSeenAt: time.Now().UnixNano()},
					},
				},
				"tenant3": {
					0: []streamMetadata{
						{hash: 201, lastSeenAt: time.Now().UnixNano()},
						{hash: 202, lastSeenAt: time.Now().UnixNano()},
					},
				},
			},
			windowSize: time.Hour,
			tenantLimits: map[string]*mockLimits{
				"tenant1": {
					ingestionRate:  50,
					ingestionBurst: 50,
				},
				"tenant2": {
					ingestionRate:  100,
					ingestionBurst: 100,
				},
				"tenant3": {
					ingestionRate:  200,
					ingestionBurst: 200,
				},
			},
			expectedRateLimited:    []uint64{202},
			expectedNotRateLimited: []uint64{201},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a multi-tenant limits implementation
			limits := &multiTenantMockLimits{
				tenantLimits: tt.tenantLimits,
			}

			rateLimiter := limiter.NewRateLimiter(newIngestionRateStrategy(limits), 10*time.Second)

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
				rateLimiter:        rateLimiter,
				logger:             log.NewNopLogger(),
				metrics:            newMetrics(prometheus.NewRegistry()),
				metadata:           tt.setupMetadata,
				assingedPartitions: tt.assignedPartitions,
			}

			// Create request
			req := &logproto.GetStreamUsageRequest{
				Tenant:       tt.tenant,
				Partitions:   tt.partitions,
				StreamUsages: tt.streamUsages,
			}

			// Call GetStreamUsage
			resp, err := s.GetStreamUsage(context.Background(), req)
			require.NoError(t, err)
			require.NotNil(t, resp)

			// Check rate limited streams
			require.Len(t, resp.RateLimitedStreams, len(tt.expectedRateLimited), "Unexpected number of rate limited streams")

			// Verify each expected rate limited stream is in the response
			for _, hash := range tt.expectedRateLimited {
				require.Contains(t, resp.RateLimitedStreams, hash, "Expected stream %d to be rate limited", hash)
			}

			// Verify each expected non-rate limited stream is not in the response
			for _, hash := range tt.expectedNotRateLimited {
				require.NotContains(t, resp.RateLimitedStreams, hash, "Expected stream %d to not be rate limited", hash)
			}
		})
	}
}

// multiTenantMockLimits implements the Limits interface with different limits per tenant
type multiTenantMockLimits struct {
	tenantLimits map[string]*mockLimits
}

func (m *multiTenantMockLimits) IngestionRateBytes(userID string) float64 {
	if limits, ok := m.tenantLimits[userID]; ok {
		return limits.ingestionRate
	}
	return 0 // Default to zero if tenant not found
}

func (m *multiTenantMockLimits) IngestionBurstSizeBytes(userID string) int {
	if limits, ok := m.tenantLimits[userID]; ok {
		return limits.ingestionBurst
	}
	return 0 // Default to zero if tenant not found
}
