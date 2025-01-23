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
		name                    string
		tenant                  string
		setupMetadata           map[string]map[uint64]int64
		windowSize              time.Duration
		expectedActive          uint64
		expectedRecordedStreams []uint64
	}{
		{
			name:   "tenant not found",
			tenant: "tenant1",
			setupMetadata: map[string]map[uint64]int64{
				"tenant2": {
					4: time.Now().UnixNano(),
					5: time.Now().UnixNano(),
				},
			},
			windowSize:              time.Hour,
			expectedActive:          0,
			expectedRecordedStreams: []uint64{},
		},
		{
			name:   "all streams active",
			tenant: "tenant1",
			setupMetadata: map[string]map[uint64]int64{
				"tenant1": {
					1: time.Now().UnixNano(),
					2: time.Now().UnixNano(),
					3: time.Now().UnixNano(),
					4: time.Now().UnixNano(), // Additional active stream
				},
			},
			windowSize:              time.Hour,
			expectedActive:          4, // Total active streams for tenant
			expectedRecordedStreams: []uint64{1, 2, 3, 4},
		},
		{
			name:   "mixed active and expired streams",
			tenant: "tenant1",
			setupMetadata: map[string]map[uint64]int64{
				"tenant1": {
					1: time.Now().UnixNano(),
					2: time.Now().Add(-2 * time.Hour).UnixNano(), // expired
					3: time.Now().UnixNano(),
					4: time.Now().Add(-2 * time.Hour).UnixNano(), // expired
					5: time.Now().UnixNano(),                     // Additional active stream
				},
			},
			windowSize:              time.Hour,
			expectedActive:          3, // Total active streams for tenant
			expectedRecordedStreams: []uint64{1, 3, 5},
		},
		{
			name:   "all streams expired",
			tenant: "tenant1",
			setupMetadata: map[string]map[uint64]int64{
				"tenant1": {
					1: time.Now().Add(-2 * time.Hour).UnixNano(),
					2: time.Now().Add(-2 * time.Hour).UnixNano(),
				},
			},
			windowSize:              time.Hour,
			expectedActive:          0,
			expectedRecordedStreams: []uint64{},
		},
		{
			name:   "empty stream hashes",
			tenant: "tenant1",
			setupMetadata: map[string]map[uint64]int64{
				"tenant1": {
					1: time.Now().UnixNano(),
					2: time.Now().UnixNano(),
				},
			},
			windowSize:              time.Hour,
			expectedActive:          2,
			expectedRecordedStreams: []uint64{1, 2},
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
				logger:   log.NewNopLogger(),
				metadata: tt.setupMetadata,
			}

			// Create request
			req := &logproto.GetStreamUsageRequest{
				Tenant: tt.tenant,
			}

			// Call GetStreamUsage
			resp, err := s.GetStreamUsage(context.Background(), req)
			require.NoError(t, err)
			require.NotNil(t, resp)
			require.Equal(t, tt.tenant, resp.Tenant)
			require.Equal(t, tt.expectedActive, resp.ActiveStreams)
			require.Len(t, resp.RecordedStreams, len(tt.expectedRecordedStreams))
		})
	}
}

func TestIngestLimits_GetStreamUsage_Concurrent(t *testing.T) {
	// Setup test data with a mix of active and expired streams
	now := time.Now()
	metadata := map[string]map[uint64]int64{
		"tenant1": {
			1: now.UnixNano(),                        // active
			2: now.Add(-30 * time.Minute).UnixNano(), // active
			3: now.Add(-2 * time.Hour).UnixNano(),    // expired
			4: now.Add(-45 * time.Minute).UnixNano(), // active
			5: now.Add(-3 * time.Hour).UnixNano(),    // expired
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
	}

	// Run concurrent requests
	concurrency := 10
	done := make(chan struct{})
	for i := 0; i < concurrency; i++ {
		go func() {
			defer func() { done <- struct{}{} }()

			req := &logproto.GetStreamUsageRequest{
				Tenant: "tenant1",
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
