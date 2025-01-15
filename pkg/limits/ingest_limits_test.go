package limits

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestIngestLimits_HasStreams(t *testing.T) {
	tests := []struct {
		name           string
		tenant         string
		streamHashes   []uint64
		setupMetadata  map[string]map[uint64]int64
		windowSize     time.Duration
		expectedAnswer []bool
	}{
		{
			name:         "tenant not found",
			tenant:       "tenant1",
			streamHashes: []uint64{1, 2, 3},
			setupMetadata: map[string]map[uint64]int64{
				"tenant2": {
					4: time.Now().UnixNano(),
					5: time.Now().UnixNano(),
				},
			},
			windowSize:     time.Hour,
			expectedAnswer: []bool{false, false, false},
		},
		{
			name:         "all streams found and active",
			tenant:       "tenant1",
			streamHashes: []uint64{1, 2, 3},
			setupMetadata: map[string]map[uint64]int64{
				"tenant1": {
					1: time.Now().UnixNano(),
					2: time.Now().UnixNano(),
					3: time.Now().UnixNano(),
				},
			},
			windowSize:     time.Hour,
			expectedAnswer: []bool{true, true, true},
		},
		{
			name:         "some streams expired",
			tenant:       "tenant1",
			streamHashes: []uint64{1, 2, 3},
			setupMetadata: map[string]map[uint64]int64{
				"tenant1": {
					1: time.Now().UnixNano(),
					2: time.Now().Add(-2 * time.Hour).UnixNano(), // expired
					3: time.Now().UnixNano(),
				},
			},
			windowSize:     time.Hour,
			expectedAnswer: []bool{true, false, true},
		},
		{
			name:         "some streams not found",
			tenant:       "tenant1",
			streamHashes: []uint64{1, 2, 3},
			setupMetadata: map[string]map[uint64]int64{
				"tenant1": {
					1: time.Now().UnixNano(),
					3: time.Now().UnixNano(),
				},
			},
			windowSize:     time.Hour,
			expectedAnswer: []bool{true, false, true},
		},
		{
			name:         "empty stream hashes",
			tenant:       "tenant1",
			streamHashes: []uint64{},
			setupMetadata: map[string]map[uint64]int64{
				"tenant1": {
					1: time.Now().UnixNano(),
					2: time.Now().UnixNano(),
				},
			},
			windowSize:     time.Hour,
			expectedAnswer: []bool{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create IngestLimits instance with mock data
			s := &IngestLimits{
				cfg: kafka.IngestLimitsConfig{
					WindowSize: tt.windowSize,
				},
				logger:   log.NewNopLogger(),
				metadata: tt.setupMetadata,
			}

			// Create request
			req := &logproto.HasStreamsRequest{
				Tenant:     tt.tenant,
				StreamHash: tt.streamHashes,
			}

			// Call HasStreams
			resp, err := s.HasStreams(context.Background(), req)
			require.NoError(t, err)
			require.NotNil(t, resp)
			require.Equal(t, tt.tenant, resp.Tenant)
			require.Len(t, resp.Answer, len(tt.streamHashes))

			// Verify answers
			for i, expected := range tt.expectedAnswer {
				require.Equal(t, expected, resp.Answer[i].Answer, "stream hash %d", tt.streamHashes[i])
				require.Equal(t, tt.streamHashes[i], resp.Answer[i].StreamHash)
			}
		})
	}
}

func TestIngestLimits_HasStreams_Concurrent(t *testing.T) {
	// Setup test data
	now := time.Now()
	metadata := map[string]map[uint64]int64{
		"tenant1": {
			1: now.UnixNano(),
			2: now.Add(-30 * time.Minute).UnixNano(),
			3: now.Add(-2 * time.Hour).UnixNano(), // expired
		},
	}

	s := &IngestLimits{
		cfg: kafka.IngestLimitsConfig{
			WindowSize: time.Hour,
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

			req := &logproto.HasStreamsRequest{
				Tenant:     "tenant1",
				StreamHash: []uint64{1, 2, 3},
			}

			resp, err := s.HasStreams(context.Background(), req)
			require.NoError(t, err)
			require.NotNil(t, resp)
			require.Equal(t, "tenant1", resp.Tenant)
			require.Len(t, resp.Answer, 3)

			// Verify answers
			require.True(t, resp.Answer[0].Answer)  // active
			require.True(t, resp.Answer[1].Answer)  // active within window
			require.False(t, resp.Answer[2].Answer) // expired
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < concurrency; i++ {
		<-done
	}
}

func TestIngestLimits_NumStreams(t *testing.T) {
	tests := []struct {
		name          string
		tenant        string
		setupMetadata map[string]map[uint64]int64
		windowSize    time.Duration
		expectedNum   uint64
	}{
		{
			name:   "tenant not found",
			tenant: "tenant1",
			setupMetadata: map[string]map[uint64]int64{
				"tenant2": {
					1: time.Now().UnixNano(),
					2: time.Now().UnixNano(),
				},
			},
			windowSize:  time.Hour,
			expectedNum: 0,
		},
		{
			name:   "all streams active",
			tenant: "tenant1",
			setupMetadata: map[string]map[uint64]int64{
				"tenant1": {
					1: time.Now().UnixNano(),
					2: time.Now().UnixNano(),
					3: time.Now().UnixNano(),
				},
			},
			windowSize:  time.Hour,
			expectedNum: 3,
		},
		{
			name:   "some streams expired",
			tenant: "tenant1",
			setupMetadata: map[string]map[uint64]int64{
				"tenant1": {
					1: time.Now().UnixNano(),
					2: time.Now().Add(-2 * time.Hour).UnixNano(), // expired
					3: time.Now().UnixNano(),
					4: time.Now().Add(-2 * time.Hour).UnixNano(), // expired
				},
			},
			windowSize:  time.Hour,
			expectedNum: 2,
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
			windowSize:  time.Hour,
			expectedNum: 0,
		},
		{
			name:   "no streams",
			tenant: "tenant1",
			setupMetadata: map[string]map[uint64]int64{
				"tenant1": {},
			},
			windowSize:  time.Hour,
			expectedNum: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create IngestLimits instance with mock data
			s := &IngestLimits{
				cfg: kafka.IngestLimitsConfig{
					WindowSize: tt.windowSize,
				},
				logger:   log.NewNopLogger(),
				metadata: tt.setupMetadata,
			}

			// Create request
			req := &logproto.NumStreamsRequest{
				Tenant: tt.tenant,
			}

			// Call NumStreams
			resp, err := s.NumStreams(context.Background(), req)
			require.NoError(t, err)
			require.NotNil(t, resp)
			require.Equal(t, tt.tenant, resp.Tenant)
			require.Equal(t, tt.expectedNum, resp.Num)
		})
	}
}

func TestIngestLimits_NumStreams_Concurrent(t *testing.T) {
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
		cfg: kafka.IngestLimitsConfig{
			WindowSize: time.Hour,
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

			req := &logproto.NumStreamsRequest{
				Tenant: "tenant1",
			}

			resp, err := s.NumStreams(context.Background(), req)
			require.NoError(t, err)
			require.NotNil(t, resp)
			require.Equal(t, "tenant1", resp.Tenant)
			require.Equal(t, uint64(3), resp.Num) // Should count only the 3 active streams
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < concurrency; i++ {
		<-done
	}
}
