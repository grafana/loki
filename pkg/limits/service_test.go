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

func TestService_ExceedsLimits(t *testing.T) {
	clock := quartz.NewMock(t)
	now := clock.Now()

	tests := []struct {
		name string

		// Setup data.
		assignedPartitions []int32
		numPartitions      int
		usage              *usageStore
		ActiveWindow       time.Duration
		rateWindow         time.Duration
		BucketSize         time.Duration
		maxActiveStreams   int

		// Request data for ExceedsLimits.
		tenantID string
		streams  []*proto.StreamMetadata

		// Expectations.
		expectedIngestedBytes float64
		expectedResults       []*proto.ExceedsLimitsResult
		expectedNumRecords    int
	}{
		{
			name: "tenant not found",
			// setup data
			assignedPartitions: []int32{0},
			numPartitions:      1,
			usage: &usageStore{
				numPartitions: 1,
				stripes: []map[string]tenantUsage{
					{
						"tenant1": {
							0: {
								0x4: {hash: 0x4, lastSeenAt: now.UnixNano(), totalSize: 1000, rateBuckets: []rateBucket{{timestamp: now.UnixNano(), size: 1000}}},
								0x5: {hash: 0x5, lastSeenAt: now.UnixNano(), totalSize: 2000, rateBuckets: []rateBucket{{timestamp: now.UnixNano(), size: 2000}}},
							},
						},
					},
				},
				locks: make([]stripeLock, 1),
			},
			ActiveWindow:     time.Hour,
			rateWindow:       5 * time.Minute,
			BucketSize:       time.Minute,
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
			expectedNumRecords:    1,
		},
		{
			name: "all existing streams still active",
			// setup data
			assignedPartitions: []int32{0},
			numPartitions:      1,
			usage: &usageStore{
				numPartitions: 1,
				stripes: []map[string]tenantUsage{
					{
						"tenant1": {
							0: {
								1: {hash: 1, lastSeenAt: now.UnixNano(), totalSize: 1000, rateBuckets: []rateBucket{{timestamp: now.UnixNano(), size: 1000}}},
								2: {hash: 2, lastSeenAt: now.UnixNano(), totalSize: 2000, rateBuckets: []rateBucket{{timestamp: now.UnixNano(), size: 2000}}},
								3: {hash: 3, lastSeenAt: now.UnixNano(), totalSize: 3000, rateBuckets: []rateBucket{{timestamp: now.UnixNano(), size: 3000}}},
								4: {hash: 4, lastSeenAt: now.UnixNano(), totalSize: 4000, rateBuckets: []rateBucket{{timestamp: now.UnixNano(), size: 4000}}},
							},
						},
					},
				},
				locks: make([]stripeLock, 1),
			},
			ActiveWindow: time.Hour,
			rateWindow:   5 * time.Minute,
			BucketSize:   time.Minute,
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
			expectedNumRecords:    4,
		},
		{
			name: "keep existing active streams and drop new streams",
			// setup data
			assignedPartitions: []int32{0},
			numPartitions:      1,
			usage: &usageStore{
				numPartitions: 1,
				stripes: []map[string]tenantUsage{
					{
						"tenant1": {
							0: {
								0x1: {hash: 0x1, lastSeenAt: now.UnixNano(), totalSize: 1000, rateBuckets: []rateBucket{{timestamp: now.UnixNano(), size: 1000}}},
								0x3: {hash: 0x3, lastSeenAt: now.UnixNano(), totalSize: 3000, rateBuckets: []rateBucket{{timestamp: now.UnixNano(), size: 3000}}},
								0x5: {hash: 0x5, lastSeenAt: now.UnixNano(), totalSize: 5000, rateBuckets: []rateBucket{{timestamp: now.UnixNano(), size: 5000}}},
							},
						},
					},
				},
				locks: make([]stripeLock, 1),
			},
			ActiveWindow:     time.Hour,
			rateWindow:       5 * time.Minute,
			BucketSize:       time.Minute,
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
			assignedPartitions: []int32{0},
			numPartitions:      1,
			usage: &usageStore{
				numPartitions: 1,
				stripes: []map[string]tenantUsage{
					{
						"tenant1": {
							0: {
								0x1: {hash: 0x1, lastSeenAt: now.UnixNano(), totalSize: 1000, rateBuckets: []rateBucket{{timestamp: now.UnixNano(), size: 1000}}},
								0x3: {hash: 0x3, lastSeenAt: now.UnixNano(), totalSize: 3000, rateBuckets: []rateBucket{{timestamp: now.UnixNano(), size: 3000}}},
								0x5: {hash: 0x5, lastSeenAt: now.UnixNano(), totalSize: 5000, rateBuckets: []rateBucket{{timestamp: now.UnixNano(), size: 5000}}},
							},
						},
					},
				},
				locks: make([]stripeLock, 1),
			},
			ActiveWindow:     time.Hour,
			rateWindow:       5 * time.Minute,
			BucketSize:       time.Minute,
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
			expectedNumRecords: 3,
		},
		{
			name: "update active streams and re-activate expired streams",
			// setup data
			assignedPartitions: []int32{0},
			numPartitions:      1,
			usage: &usageStore{
				numPartitions: 1,
				stripes: []map[string]tenantUsage{
					{
						"tenant1": {
							0: {
								0x1: {hash: 0x1, lastSeenAt: now.UnixNano(), totalSize: 1000, rateBuckets: []rateBucket{{timestamp: now.UnixNano(), size: 1000}}},
								0x2: {hash: 0x2, lastSeenAt: now.Add(-120 * time.Minute).UnixNano(), totalSize: 2000, rateBuckets: []rateBucket{{timestamp: now.UnixNano(), size: 2000}}},
								0x3: {hash: 0x3, lastSeenAt: now.UnixNano(), totalSize: 3000, rateBuckets: []rateBucket{{timestamp: now.UnixNano(), size: 3000}}},
								0x4: {hash: 0x4, lastSeenAt: now.Add(-120 * time.Minute).UnixNano(), totalSize: 4000, rateBuckets: []rateBucket{{timestamp: now.UnixNano(), size: 4000}}},
								0x5: {hash: 0x5, lastSeenAt: now.UnixNano(), totalSize: 5000, rateBuckets: []rateBucket{{timestamp: now.UnixNano(), size: 5000}}},
							},
						},
					},
				},
				locks: make([]stripeLock, 1),
			},
			ActiveWindow:     time.Hour,
			rateWindow:       5 * time.Minute,
			BucketSize:       time.Minute,
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
			expectedNumRecords:    5,
		},
		{
			name: "drop streams per partition limit",
			// setup data
			assignedPartitions: []int32{0, 1},
			numPartitions:      2,
			usage: &usageStore{
				numPartitions: 2,
				locks:         make([]stripeLock, 2),
				stripes: []map[string]tenantUsage{
					make(map[string]tenantUsage),
					make(map[string]tenantUsage),
				},
			},
			ActiveWindow:     time.Hour,
			rateWindow:       5 * time.Minute,
			BucketSize:       time.Minute,
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
			expectedNumRecords: 2,
		},
		{
			name: "skip streams assigned to partitions not owned by instance but enforce limit",
			// setup data
			assignedPartitions: []int32{0},
			numPartitions:      2,
			usage: &usageStore{
				numPartitions: 1,
				locks:         make([]stripeLock, 2),
				stripes: []map[string]tenantUsage{
					make(map[string]tenantUsage),
					make(map[string]tenantUsage),
				},
			},
			ActiveWindow:     time.Hour,
			rateWindow:       5 * time.Minute,
			BucketSize:       time.Minute,
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
			expectedNumRecords: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := prometheus.NewRegistry()
			limits := &MockLimits{
				MaxGlobalStreams: tt.maxActiveStreams,
			}

			kafkaClient := mockKafka{}

			s := &Service{
				cfg: Config{
					NumPartitions: tt.numPartitions,
					ActiveWindow:  tt.ActiveWindow,
					RateWindow:    tt.rateWindow,
					BucketSize:    tt.BucketSize,
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
				partitionManager: newPartitionManager(),
				clock:            clock,
				producer:         newProducer(&kafkaClient, "test", tt.numPartitions, "", log.NewNopLogger(), reg),
			}

			// Assign the Partition IDs.
			s.partitionManager.assign(context.Background(), tt.assignedPartitions)

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

			require.Equal(t, tt.expectedNumRecords, len(kafkaClient.produced))
		})
	}
}

func TestIngestLimits_ExceedsLimits_Concurrent(t *testing.T) {
	clock := quartz.NewMock(t)
	now := clock.Now()

	limits := &MockLimits{
		MaxGlobalStreams: 5,
	}

	reg := prometheus.NewRegistry()
	kafkaClient := mockKafka{}

	// Setup test data with a mix of active and expired streams>
	usage := &usageStore{
		numPartitions: 1,
		stripes: []map[string]tenantUsage{
			{
				"tenant1": {
					0: {
						1: {hash: 1, lastSeenAt: now.UnixNano(), totalSize: 1000, rateBuckets: []rateBucket{{timestamp: now.UnixNano(), size: 1000}}},                        // active
						2: {hash: 2, lastSeenAt: now.Add(-30 * time.Minute).UnixNano(), totalSize: 2000, rateBuckets: []rateBucket{{timestamp: now.UnixNano(), size: 2000}}}, // active
						3: {hash: 3, lastSeenAt: now.Add(-2 * time.Hour).UnixNano(), totalSize: 3000},                                                                        // expired
						4: {hash: 4, lastSeenAt: now.Add(-45 * time.Minute).UnixNano(), totalSize: 4000, rateBuckets: []rateBucket{{timestamp: now.UnixNano(), size: 4000}}}, // active
						5: {hash: 5, lastSeenAt: now.Add(-3 * time.Hour).UnixNano(), totalSize: 5000},                                                                        // expired
					},
				},
			},
		},
		locks: make([]stripeLock, 1),
	}

	s := &Service{
		cfg: Config{
			NumPartitions: 1,
			ActiveWindow:  time.Hour,
			RateWindow:    5 * time.Minute,
			BucketSize:    time.Minute,
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
		partitionManager: newPartitionManager(),
		metrics:          newMetrics(reg),
		limits:           limits,
		clock:            clock,
		producer:         newProducer(&kafkaClient, "test", 1, "", log.NewNopLogger(), reg),
	}

	// Assign the Partition IDs.
	s.partitionManager.assign(context.Background(), []int32{0})

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
	require.Equal(t, 50, len(kafkaClient.produced))
}

func TestNew(t *testing.T) {
	cfg := Config{
		KafkaConfig: kafka.Config{
			Topic:        "test-topic",
			WriteTimeout: 10 * time.Second,
		},
		ActiveWindow: time.Hour,
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

	s, err := New(cfg, limits, log.NewNopLogger(), prometheus.NewRegistry())
	require.NoError(t, err)
	require.NotNil(t, s)
	require.NotNil(t, s.clientReader)
	require.NotNil(t, s.clientWriter)

	require.Equal(t, cfg, s.cfg)

	require.NotNil(t, s.usage)
	require.NotNil(t, s.lifecycler)
}
