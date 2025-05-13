package limits

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/coder/quartz"
	"github.com/go-kit/log"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/ring"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/kafka"
	"github.com/grafana/loki/v3/pkg/limits/proto"
)

func TestIngestLimits_ExceedsLimits(t *testing.T) {
	clock := quartz.NewMock(t)
	now := clock.Now()

	tests := []struct {
		name string

		// Setup data.
		assignedPartitionIDs []int32
		numPartitions        int
		usage                *UsageStore
		windowSize           time.Duration
		rateWindow           time.Duration
		bucketDuration       time.Duration
		maxActiveStreams     int

		// Request data for ExceedsLimits.
		tenantID string
		streams  []*proto.StreamMetadata

		// Expectations.
		expectedIngestedBytes float64
		expectedResults       []*proto.ExceedsLimitsResult
		expectedAppendsTotal  int
	}{
		{
			name: "tenant not found",
			// setup data
			assignedPartitionIDs: []int32{0},
			numPartitions:        1,
			usage: &UsageStore{
				numPartitions: 1,
				stripes: []map[string]tenantUsage{
					{
						"tenant1": {
							0: {
								0x4: {Hash: 0x4, LastSeenAt: now.UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: now.UnixNano(), Size: 1000}}},
								0x5: {Hash: 0x5, LastSeenAt: now.UnixNano(), TotalSize: 2000, RateBuckets: []RateBucket{{Timestamp: now.UnixNano(), Size: 2000}}},
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
			streams: []*proto.StreamMetadata{
				{
					StreamHash: 0x2,
					TotalSize:  1010,
				},
			},
			// expect data
			expectedIngestedBytes: 1010,
			expectedAppendsTotal:  1,
		},
		{
			name: "all existing streams still active",
			// setup data
			assignedPartitionIDs: []int32{0},
			numPartitions:        1,
			usage: &UsageStore{
				numPartitions: 1,
				stripes: []map[string]tenantUsage{
					{
						"tenant1": {
							0: {
								1: {Hash: 1, LastSeenAt: now.UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: now.UnixNano(), Size: 1000}}},
								2: {Hash: 2, LastSeenAt: now.UnixNano(), TotalSize: 2000, RateBuckets: []RateBucket{{Timestamp: now.UnixNano(), Size: 2000}}},
								3: {Hash: 3, LastSeenAt: now.UnixNano(), TotalSize: 3000, RateBuckets: []RateBucket{{Timestamp: now.UnixNano(), Size: 3000}}},
								4: {Hash: 4, LastSeenAt: now.UnixNano(), TotalSize: 4000, RateBuckets: []RateBucket{{Timestamp: now.UnixNano(), Size: 4000}}},
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
			streams: []*proto.StreamMetadata{
				{StreamHash: 0x1, TotalSize: 1010},
				{StreamHash: 0x2, TotalSize: 1010},
				{StreamHash: 0x3, TotalSize: 1010},
				{StreamHash: 0x4, TotalSize: 1010},
			},
			// expect data
			expectedIngestedBytes: 4040,
			expectedAppendsTotal:  4,
		},
		{
			name: "keep existing active streams and drop new streams",
			// setup data
			assignedPartitionIDs: []int32{0},
			numPartitions:        1,
			usage: &UsageStore{
				numPartitions: 1,
				stripes: []map[string]tenantUsage{
					{
						"tenant1": {
							0: {
								0x1: {Hash: 0x1, LastSeenAt: now.UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: now.UnixNano(), Size: 1000}}},
								0x3: {Hash: 0x3, LastSeenAt: now.UnixNano(), TotalSize: 3000, RateBuckets: []RateBucket{{Timestamp: now.UnixNano(), Size: 3000}}},
								0x5: {Hash: 0x5, LastSeenAt: now.UnixNano(), TotalSize: 5000, RateBuckets: []RateBucket{{Timestamp: now.UnixNano(), Size: 5000}}},
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
			streams: []*proto.StreamMetadata{
				{StreamHash: 0x2, TotalSize: 1010},
				{StreamHash: 0x4, TotalSize: 1010},
			},
			// expect data
			expectedIngestedBytes: 0,
			expectedResults: []*proto.ExceedsLimitsResult{
				{StreamHash: 0x2, Reason: uint32(ReasonExceedsMaxStreams)},
				{StreamHash: 0x4, Reason: uint32(ReasonExceedsMaxStreams)},
			},
		},
		{
			name: "update existing active streams and drop new streams",
			// setup data
			assignedPartitionIDs: []int32{0},
			numPartitions:        1,
			usage: &UsageStore{
				numPartitions: 1,
				stripes: []map[string]tenantUsage{
					{
						"tenant1": {
							0: {
								0x1: {Hash: 0x1, LastSeenAt: now.UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: now.UnixNano(), Size: 1000}}},
								0x3: {Hash: 0x3, LastSeenAt: now.UnixNano(), TotalSize: 3000, RateBuckets: []RateBucket{{Timestamp: now.UnixNano(), Size: 3000}}},
								0x5: {Hash: 0x5, LastSeenAt: now.UnixNano(), TotalSize: 5000, RateBuckets: []RateBucket{{Timestamp: now.UnixNano(), Size: 5000}}},
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
			streams: []*proto.StreamMetadata{
				{StreamHash: 0x1, TotalSize: 1010},
				{StreamHash: 0x2, TotalSize: 1010},
				{StreamHash: 0x3, TotalSize: 1010},
				{StreamHash: 0x4, TotalSize: 1010},
				{StreamHash: 0x5, TotalSize: 1010},
			},
			// expect data
			expectedIngestedBytes: 3030,
			expectedResults: []*proto.ExceedsLimitsResult{
				{StreamHash: 0x2, Reason: uint32(ReasonExceedsMaxStreams)},
				{StreamHash: 0x4, Reason: uint32(ReasonExceedsMaxStreams)},
			},
			expectedAppendsTotal: 3,
		},
		{
			name: "update active streams and re-activate expired streams",
			// setup data
			assignedPartitionIDs: []int32{0},
			numPartitions:        1,
			usage: &UsageStore{
				numPartitions: 1,
				stripes: []map[string]tenantUsage{
					{
						"tenant1": {
							0: {
								0x1: {Hash: 0x1, LastSeenAt: now.UnixNano(), TotalSize: 1000, RateBuckets: []RateBucket{{Timestamp: now.UnixNano(), Size: 1000}}},
								0x2: {Hash: 0x2, LastSeenAt: now.Add(-120 * time.Minute).UnixNano(), TotalSize: 2000, RateBuckets: []RateBucket{{Timestamp: now.UnixNano(), Size: 2000}}},
								0x3: {Hash: 0x3, LastSeenAt: now.UnixNano(), TotalSize: 3000, RateBuckets: []RateBucket{{Timestamp: now.UnixNano(), Size: 3000}}},
								0x4: {Hash: 0x4, LastSeenAt: now.Add(-120 * time.Minute).UnixNano(), TotalSize: 4000, RateBuckets: []RateBucket{{Timestamp: now.UnixNano(), Size: 4000}}},
								0x5: {Hash: 0x5, LastSeenAt: now.UnixNano(), TotalSize: 5000, RateBuckets: []RateBucket{{Timestamp: now.UnixNano(), Size: 5000}}},
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
			streams: []*proto.StreamMetadata{
				{StreamHash: 0x1, TotalSize: 1010},
				{StreamHash: 0x2, TotalSize: 1010},
				{StreamHash: 0x3, TotalSize: 1010},
				{StreamHash: 0x4, TotalSize: 1010},
				{StreamHash: 0x5, TotalSize: 1010},
			},
			// expect data
			expectedIngestedBytes: 5050,
			expectedAppendsTotal:  5,
		},
		{
			name: "drop streams per partition limit",
			// setup data
			assignedPartitionIDs: []int32{0, 1},
			numPartitions:        2,
			usage: &UsageStore{
				numPartitions: 2,
				locks:         make([]stripeLock, 2),
				stripes: []map[string]tenantUsage{
					make(map[string]tenantUsage),
					make(map[string]tenantUsage),
				},
			},
			windowSize:       time.Hour,
			rateWindow:       5 * time.Minute,
			bucketDuration:   time.Minute,
			maxActiveStreams: 3,
			// request data
			tenantID: "tenant1",
			streams: []*proto.StreamMetadata{
				{StreamHash: 0x1, TotalSize: 1010},
				{StreamHash: 0x2, TotalSize: 1010},
				{StreamHash: 0x3, TotalSize: 1010},
				{StreamHash: 0x4, TotalSize: 1010},
			},
			// expect data
			expectedIngestedBytes: 2020,
			expectedResults: []*proto.ExceedsLimitsResult{
				{StreamHash: 0x3, Reason: uint32(ReasonExceedsMaxStreams)},
				{StreamHash: 0x4, Reason: uint32(ReasonExceedsMaxStreams)},
			},
			expectedAppendsTotal: 2,
		},
		{
			name: "skip streams assigned to partitions not owned by instance but enforce limit",
			// setup data
			assignedPartitionIDs: []int32{0},
			numPartitions:        2,
			usage: &UsageStore{
				numPartitions: 2,
				locks:         make([]stripeLock, 2),
				stripes: []map[string]tenantUsage{
					make(map[string]tenantUsage),
					make(map[string]tenantUsage),
				},
			},
			windowSize:       time.Hour,
			rateWindow:       5 * time.Minute,
			bucketDuration:   time.Minute,
			maxActiveStreams: 3,
			// request data
			tenantID: "tenant1",
			streams: []*proto.StreamMetadata{
				{StreamHash: 0x1, TotalSize: 1010}, // Unassigned
				{StreamHash: 0x2, TotalSize: 1010}, // Assigned
				{StreamHash: 0x3, TotalSize: 1010}, // Unassigned
				{StreamHash: 0x4, TotalSize: 1010}, // Assigned  but exceeds stream limit
			},
			// expect data
			expectedIngestedBytes: 1010,
			expectedResults: []*proto.ExceedsLimitsResult{
				{StreamHash: 0x4, Reason: uint32(ReasonExceedsMaxStreams)},
			},
			expectedAppendsTotal: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := prometheus.NewRegistry()
			limits := &MockLimits{
				MaxGlobalStreams: tt.maxActiveStreams,
			}

			wal := &mockWAL{t: t, ExpectedAppendsTotal: tt.expectedAppendsTotal}

			s := &IngestLimits{
				cfg: Config{
					NumPartitions:  tt.numPartitions,
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
				usage:            tt.usage,
				partitionManager: NewPartitionManager(log.NewNopLogger()),
				clock:            clock,
				wal:              wal,
			}

			// Assign the Partition IDs.
			partitions := make(map[string][]int32)
			partitions["test"] = make([]int32, 0, len(tt.assignedPartitionIDs))
			partitions["test"] = append(partitions["test"], tt.assignedPartitionIDs...)
			s.partitionManager.Assign(context.Background(), nil, partitions)

			// Call ExceedsLimits.
			req := &proto.ExceedsLimitsRequest{
				Tenant:  tt.tenantID,
				Streams: tt.streams,
			}

			resp, err := s.ExceedsLimits(context.Background(), req)
			require.NoError(t, err)
			require.NotNil(t, resp)
			require.ElementsMatch(t, tt.expectedResults, resp.Results)

			metrics, err := reg.Gather()
			require.NoError(t, err)

			for _, metric := range metrics {
				if metric.GetName() == "loki_ingest_limits_tenant_ingested_bytes_total" {
					require.Equal(t, tt.expectedIngestedBytes, metric.GetMetric()[0].GetCounter().GetValue())
					break
				}
			}

			wal.AssertAppendsTotal()
		})
	}
}

func TestIngestLimits_ExceedsLimits_Concurrent(t *testing.T) {
	clock := quartz.NewMock(t)
	now := clock.Now()

	limits := &MockLimits{
		MaxGlobalStreams: 5,
	}

	wal := &mockWAL{t: t, ExpectedAppendsTotal: 50}

	// Setup test data with a mix of active and expired streams>
	usage := &UsageStore{
		numPartitions: 1,
		stripes: []map[string]tenantUsage{
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
		usage:            usage,
		partitionManager: NewPartitionManager(log.NewNopLogger()),
		metrics:          newMetrics(prometheus.NewRegistry()),
		limits:           limits,
		clock:            clock,
		wal:              wal,
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
			req := &proto.ExceedsLimitsRequest{
				Tenant:  "tenant1",
				Streams: []*proto.StreamMetadata{{StreamHash: 1}, {StreamHash: 2}, {StreamHash: 3}, {StreamHash: 4}, {StreamHash: 5}},
			}

			resp, err := s.ExceedsLimits(context.Background(), req)
			require.NoError(t, err)
			require.NotNil(t, resp)
			require.Empty(t, resp.Results)
		}()
	}

	// Wait for all goroutines to complete
	wg.Wait()
	wal.AssertAppendsTotal()
}

func TestNewIngestLimits(t *testing.T) {
	cfg := Config{
		KafkaConfig: kafka.Config{
			Topic:        "test-topic",
			WriteTimeout: 10 * time.Second,
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

	limits := &MockLimits{
		MaxGlobalStreams: 100,
		IngestionRate:    1000,
	}

	s, err := NewIngestLimits(cfg, limits, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)
	require.NotNil(t, s)
	require.NotNil(t, s.reader)

	require.Equal(t, cfg, s.cfg)

	require.NotNil(t, s.usage)
	require.NotNil(t, s.lifecycler)
}
