package limits

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/ring"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/limits/internal/testutil"
	"github.com/grafana/loki/v3/pkg/logproto"
)

func TestIngestLimits_ExceedsLimits(t *testing.T) {
	tests := []struct {
		name string

		// Setup data.
		assignedPartitionIDs []int32
		metadata             *streamMetadata
		windowSize           time.Duration
		rateWindow           time.Duration
		bucketDuration       time.Duration
		maxActiveStreams     int

		// Request data for ExceedsLimits.
		tenantID string
		streams  []*logproto.StreamMetadata

		// Expectations.
		expectedIngestedBytes float64
		expectedResults       []*logproto.ExceedsLimitsResult
	}{
		{
			name: "tenant not found",
			// setup data
			assignedPartitionIDs: []int32{0},
			metadata: &streamMetadata{
				stripes: []map[string]map[int32]map[uint64]Stream{
					{
						"tenant1": {
							0: {
								0x4: {Hash: 0x4, LastSeenAt: time.Now().UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: time.Now().UnixNano(), Size: 1000}}},
								0x5: {Hash: 0x5, LastSeenAt: time.Now().UnixNano(), TotalSize: 2000, RateBuckets: []RateBucket{{Timestamp: time.Now().UnixNano(), Size: 2000}}},
							},
						},
					},
				},
				locks: make([]stripeLock, 1),
			},
			windowSize:       time.Hour,
			rateWindow:       5 * time.Minute,
			bucketDuration:   time.Minute,
			maxActiveStreams: 10,
			// request data
			tenantID: "tenant2",
			streams: []*logproto.StreamMetadata{
				{
					StreamHash:             0x2,
					EntriesSize:            1000,
					StructuredMetadataSize: 10,
				},
			},
			// expect data
			expectedIngestedBytes: 1010,
		},
		{
			name: "all existing streams still active",
			// setup data
			assignedPartitionIDs: []int32{0},
			metadata: &streamMetadata{
				stripes: []map[string]map[int32]map[uint64]Stream{
					{
						"tenant1": {
							0: {
								1: {Hash: 1, LastSeenAt: time.Now().UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: time.Now().UnixNano(), Size: 1000}}},
								2: {Hash: 2, LastSeenAt: time.Now().UnixNano(), TotalSize: 2000, RateBuckets: []RateBucket{{Timestamp: time.Now().UnixNano(), Size: 2000}}},
								3: {Hash: 3, LastSeenAt: time.Now().UnixNano(), TotalSize: 3000, RateBuckets: []RateBucket{{Timestamp: time.Now().UnixNano(), Size: 3000}}},
								4: {Hash: 4, LastSeenAt: time.Now().UnixNano(), TotalSize: 4000, RateBuckets: []RateBucket{{Timestamp: time.Now().UnixNano(), Size: 4000}}},
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
			tenantID:         "tenant1",
			maxActiveStreams: 10,
			streams: []*logproto.StreamMetadata{
				{StreamHash: 0x1, EntriesSize: 1000, StructuredMetadataSize: 10},
				{StreamHash: 0x2, EntriesSize: 1000, StructuredMetadataSize: 10},
				{StreamHash: 0x3, EntriesSize: 1000, StructuredMetadataSize: 10},
				{StreamHash: 0x4, EntriesSize: 1000, StructuredMetadataSize: 10},
			},
			// expect data
			expectedIngestedBytes: 4040,
		},
		{
			name: "keep existing active streams and drop new streams",
			// setup data
			assignedPartitionIDs: []int32{0},
			metadata: &streamMetadata{
				stripes: []map[string]map[int32]map[uint64]Stream{
					{
						"tenant1": {
							0: {
								0x1: {Hash: 0x1, LastSeenAt: time.Now().UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: time.Now().UnixNano(), Size: 1000}}},
								0x3: {Hash: 0x3, LastSeenAt: time.Now().UnixNano(), TotalSize: 3000, RateBuckets: []RateBucket{{Timestamp: time.Now().UnixNano(), Size: 3000}}},
								0x5: {Hash: 0x5, LastSeenAt: time.Now().UnixNano(), TotalSize: 5000, RateBuckets: []RateBucket{{Timestamp: time.Now().UnixNano(), Size: 5000}}},
							},
						},
					},
				},
				locks: make([]stripeLock, 1),
			},
			windowSize:       time.Hour,
			rateWindow:       5 * time.Minute,
			bucketDuration:   time.Minute,
			maxActiveStreams: 3,
			// request data
			tenantID: "tenant1",
			streams: []*logproto.StreamMetadata{
				{StreamHash: 0x2, EntriesSize: 1000, StructuredMetadataSize: 10},
				{StreamHash: 0x4, EntriesSize: 1000, StructuredMetadataSize: 10},
			},
			// expect data
			expectedIngestedBytes: 0,
			expectedResults: []*logproto.ExceedsLimitsResult{
				{StreamHash: 0x2, Reason: uint32(ReasonExceedsMaxStreams)},
				{StreamHash: 0x4, Reason: uint32(ReasonExceedsMaxStreams)},
			},
		},
		{
			name: "update existing active streams and drop new streams",
			// setup data
			assignedPartitionIDs: []int32{0},
			metadata: &streamMetadata{
				stripes: []map[string]map[int32]map[uint64]Stream{
					{
						"tenant1": {
							0: {
								0x1: {Hash: 0x1, LastSeenAt: time.Now().UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: time.Now().UnixNano(), Size: 1000}}},
								0x3: {Hash: 0x3, LastSeenAt: time.Now().UnixNano(), TotalSize: 3000, RateBuckets: []RateBucket{{Timestamp: time.Now().UnixNano(), Size: 3000}}},
								0x5: {Hash: 0x5, LastSeenAt: time.Now().UnixNano(), TotalSize: 5000, RateBuckets: []RateBucket{{Timestamp: time.Now().UnixNano(), Size: 5000}}},
							},
						},
					},
				},
				locks: make([]stripeLock, 1),
			},
			windowSize:       time.Hour,
			rateWindow:       5 * time.Minute,
			bucketDuration:   time.Minute,
			maxActiveStreams: 3,
			// request data
			tenantID: "tenant1",
			streams: []*logproto.StreamMetadata{
				{StreamHash: 0x1, EntriesSize: 1000, StructuredMetadataSize: 10},
				{StreamHash: 0x2, EntriesSize: 1000, StructuredMetadataSize: 10},
				{StreamHash: 0x3, EntriesSize: 1000, StructuredMetadataSize: 10},
				{StreamHash: 0x4, EntriesSize: 1000, StructuredMetadataSize: 10},
				{StreamHash: 0x5, EntriesSize: 1000, StructuredMetadataSize: 10},
			},
			// expect data
			expectedIngestedBytes: 3030,
			expectedResults: []*logproto.ExceedsLimitsResult{
				{StreamHash: 0x2, Reason: uint32(ReasonExceedsMaxStreams)},
				{StreamHash: 0x4, Reason: uint32(ReasonExceedsMaxStreams)},
			},
		},
		{
			name: "update active streams and re-activate expired streams",
			// setup data
			assignedPartitionIDs: []int32{0},
			metadata: &streamMetadata{
				stripes: []map[string]map[int32]map[uint64]Stream{
					{
						"tenant1": {
							0: {
								0x1: {Hash: 0x1, LastSeenAt: time.Now().UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: time.Now().UnixNano(), Size: 1000}}},
								0x2: {Hash: 0x2, LastSeenAt: time.Now().Add(-120 * time.Minute).UnixNano(), TotalSize: 2000, RateBuckets: []RateBucket{{Timestamp: time.Now().UnixNano(), Size: 2000}}},
								0x3: {Hash: 0x3, LastSeenAt: time.Now().UnixNano(), TotalSize: 3000, RateBuckets: []RateBucket{{Timestamp: time.Now().UnixNano(), Size: 3000}}},
								0x4: {Hash: 0x4, LastSeenAt: time.Now().Add(-120 * time.Minute).UnixNano(), TotalSize: 4000, RateBuckets: []RateBucket{{Timestamp: time.Now().UnixNano(), Size: 4000}}},
								0x5: {Hash: 0x5, LastSeenAt: time.Now().UnixNano(), TotalSize: 5000, RateBuckets: []RateBucket{{Timestamp: time.Now().UnixNano(), Size: 5000}}},
							},
						},
					},
				},
				locks: make([]stripeLock, 1),
			},
			windowSize:       time.Hour,
			rateWindow:       5 * time.Minute,
			bucketDuration:   time.Minute,
			maxActiveStreams: 5,
			// request data
			tenantID: "tenant1",
			streams: []*logproto.StreamMetadata{
				{StreamHash: 0x1, EntriesSize: 1000, StructuredMetadataSize: 10},
				{StreamHash: 0x2, EntriesSize: 1000, StructuredMetadataSize: 10},
				{StreamHash: 0x3, EntriesSize: 1000, StructuredMetadataSize: 10},
				{StreamHash: 0x4, EntriesSize: 1000, StructuredMetadataSize: 10},
				{StreamHash: 0x5, EntriesSize: 1000, StructuredMetadataSize: 10},
			},
			// expect data
			expectedIngestedBytes: 5050,
		},
		{
			name: "drop streams per partition limit",
			// setup data
			assignedPartitionIDs: []int32{0, 1},
			metadata: &streamMetadata{
				locks: make([]stripeLock, 2),
				stripes: []map[string]map[int32]map[uint64]Stream{
					make(map[string]map[int32]map[uint64]Stream),
					make(map[string]map[int32]map[uint64]Stream),
				},
			},
			windowSize:       time.Hour,
			rateWindow:       5 * time.Minute,
			bucketDuration:   time.Minute,
			maxActiveStreams: 3,
			// request data
			tenantID: "tenant1",
			streams: []*logproto.StreamMetadata{
				{StreamHash: 0x1, EntriesSize: 1000, StructuredMetadataSize: 10},
				{StreamHash: 0x2, EntriesSize: 1000, StructuredMetadataSize: 10},
				{StreamHash: 0x3, EntriesSize: 1000, StructuredMetadataSize: 10},
				{StreamHash: 0x4, EntriesSize: 1000, StructuredMetadataSize: 10},
			},
			// expect data
			expectedIngestedBytes: 2020,
			expectedResults: []*logproto.ExceedsLimitsResult{
				{StreamHash: 0x3, Reason: uint32(ReasonExceedsMaxStreams)},
				{StreamHash: 0x4, Reason: uint32(ReasonExceedsMaxStreams)},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := prometheus.NewRegistry()
			limits := &testutil.MockLimits{
				MaxGlobalStreams: tt.maxActiveStreams,
			}

			s := &IngestLimits{
				cfg: Config{
					NumPartitions:  len(tt.assignedPartitionIDs),
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
				metrics:          newMetrics(reg),
				limits:           limits,
				metadata:         tt.metadata,
				partitionManager: NewPartitionManager(log.NewNopLogger()),
			}

			// Assign the Partition IDs.
			partitions := make(map[string][]int32)
			partitions["test"] = make([]int32, 0, len(tt.assignedPartitionIDs))
			partitions["test"] = append(partitions["test"], tt.assignedPartitionIDs...)
			s.partitionManager.Assign(context.Background(), nil, partitions)

			// Call ExceedsLimits.
			req := &logproto.ExceedsLimitsRequest{
				Tenant:  tt.tenantID,
				Streams: tt.streams,
			}

			resp, err := s.ExceedsLimits(context.Background(), req)
			require.NoError(t, err)
			require.NotNil(t, resp)
			require.Equal(t, tt.tenantID, resp.Tenant)
			require.ElementsMatch(t, tt.expectedResults, resp.Results)

			metrics, err := reg.Gather()
			require.NoError(t, err)

			for _, metric := range metrics {
				if metric.GetName() == "loki_ingest_limits_tenant_ingested_bytes_total" {
					require.Equal(t, tt.expectedIngestedBytes, metric.GetMetric()[0].GetCounter().GetValue())
					break
				}
			}
		})
	}
}

func TestIngestLimits_ExceedsLimits_Concurrent(t *testing.T) {
	now := time.Now()

	limits := &testutil.MockLimits{
		MaxGlobalStreams: 5,
	}

	// Setup test data with a mix of active and expired streams>
	metadata := &streamMetadata{
		stripes: []map[string]map[int32]map[uint64]Stream{
			{
				"tenant1": {
					0: {
						1: {Hash: 1, LastSeenAt: now.UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: now.UnixNano(), Size: 1000}}},                        // active
						2: {Hash: 2, LastSeenAt: now.Add(-30 * time.Minute).UnixNano(), TotalSize: 2000, RateBuckets: []RateBucket{{Timestamp: now.UnixNano(), Size: 2000}}}, // active
						3: {Hash: 3, LastSeenAt: now.Add(-2 * time.Hour).UnixNano(), TotalSize: 3000},                                                                        // expired
						4: {Hash: 4, LastSeenAt: now.Add(-45 * time.Minute).UnixNano(), TotalSize: 4000, RateBuckets: []RateBucket{{Timestamp: now.UnixNano(), Size: 4000}}}, // active
						5: {Hash: 5, LastSeenAt: now.Add(-3 * time.Hour).UnixNano(), TotalSize: 5000},                                                                        // expired
					},
				},
			},
		},
		locks: make([]stripeLock, 1),
	}

	s := &IngestLimits{
		cfg: Config{
			NumPartitions:  1,
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
		limits:           limits,
	}

	// Assign the Partition IDs.
	partitions := map[string][]int32{"tenant1": {0}}
	s.partitionManager.Assign(context.Background(), nil, partitions)

	// Run concurrent requests
	concurrency := 10
	wg := sync.WaitGroup{}
	wg.Add(concurrency)

	for range concurrency {
		go func() {
			defer wg.Done()
			req := &logproto.ExceedsLimitsRequest{
				Tenant:  "tenant1",
				Streams: []*logproto.StreamMetadata{{StreamHash: 1}, {StreamHash: 2}, {StreamHash: 3}, {StreamHash: 4}, {StreamHash: 5}},
			}

			resp, err := s.ExceedsLimits(context.Background(), req)
			require.NoError(t, err)
			require.NotNil(t, resp)
			require.Equal(t, "tenant1", resp.Tenant)
			require.Nil(t, resp.Results)
		}()
	}

	// Wait for all goroutines to complete
	wg.Wait()
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

	limits := &testutil.MockLimits{
		MaxGlobalStreams: 100,
		IngestionRate:    1000,
	}

	s, err := NewIngestLimits(cfg, limits, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)
	require.NotNil(t, s)
	require.NotNil(t, s.client)

	require.Equal(t, cfg, s.cfg)

	require.NotNil(t, s.metadata)
	require.NotNil(t, s.lifecycler)
}
