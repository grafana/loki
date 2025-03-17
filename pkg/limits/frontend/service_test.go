package frontend

import (
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/limiter"
	"github.com/grafana/dskit/ring"
	ring_client "github.com/grafana/dskit/ring/client"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/loki/v3/pkg/logproto"
)

type mockLimits struct {
	maxGlobalStreams int
	ingestionRate    float64
}

func (m *mockLimits) MaxGlobalStreamsPerUser(_ string) int {
	return m.maxGlobalStreams
}

func (m *mockLimits) IngestionRateBytes(_ string) float64 {
	return m.ingestionRate
}

func (m *mockLimits) IngestionBurstSizeBytes(_ string) int {
	return 1000
}

type mockReadRing struct {
	ring.ReadRing
	replicationSet ring.ReplicationSet
}

func (m *mockReadRing) GetAllHealthy(_ ring.Operation) (ring.ReplicationSet, error) {
	return m.replicationSet, nil
}

type mockFactory struct {
	clientsByAddr map[string]logproto.IngestLimitsClient
}

func (f *mockFactory) FromInstance(inst ring.InstanceDesc) (ring_client.PoolClient, error) {
	client, ok := f.clientsByAddr[inst.Addr]
	if !ok {
		return nil, fmt.Errorf("no client for address %s", inst.Addr)
	}
	return client.(ring_client.PoolClient), nil
}

type mockIngestLimitsClient struct {
	logproto.IngestLimitsClient
	getStreamUsageResponse        *logproto.GetStreamUsageResponse
	getAssignedPartitionsResponse *logproto.GetAssignedPartitionsResponse
}

func (m *mockIngestLimitsClient) GetAssignedPartitions(_ context.Context, _ *logproto.GetAssignedPartitionsRequest, _ ...grpc.CallOption) (*logproto.GetAssignedPartitionsResponse, error) {
	return m.getAssignedPartitionsResponse, nil
}

func (m *mockIngestLimitsClient) GetStreamUsage(_ context.Context, _ *logproto.GetStreamUsageRequest, _ ...grpc.CallOption) (*logproto.GetStreamUsageResponse, error) {
	// Create a copy of the response to avoid modifying the original
	resp := &logproto.GetStreamUsageResponse{
		Tenant:         m.getStreamUsageResponse.Tenant,
		ActiveStreams:  m.getStreamUsageResponse.ActiveStreams,
		Rate:           m.getStreamUsageResponse.Rate,
		UnknownStreams: m.getStreamUsageResponse.UnknownStreams,
	}
	return resp, nil
}

func (m *mockIngestLimitsClient) Close() error {
	return nil
}

func (m *mockIngestLimitsClient) Check(_ context.Context, _ *grpc_health_v1.HealthCheckRequest, _ ...grpc.CallOption) (*grpc_health_v1.HealthCheckResponse, error) {
	return &grpc_health_v1.HealthCheckResponse{
		Status: grpc_health_v1.HealthCheckResponse_SERVING,
	}, nil
}

func (m *mockIngestLimitsClient) Watch(_ context.Context, _ *grpc_health_v1.HealthCheckRequest, _ ...grpc.CallOption) (grpc_health_v1.Health_WatchClient, error) {
	return nil, nil
}

func TestRingIngestLimitsService_ExceedsLimits(t *testing.T) {
	tests := []struct {
		name                       string
		tenant                     string
		maxGlobalStreams           int
		ingestionRate              float64
		streams                    []*logproto.StreamMetadata
		getStreamUsageResps        []*logproto.GetStreamUsageResponse
		getAssignedPartitionsResps []*logproto.GetAssignedPartitionsResponse
		expectedRejections         []*logproto.RejectedStream
	}{
		{
			name:             "no streams",
			tenant:           "test",
			maxGlobalStreams: 10,
			ingestionRate:    100,
			streams:          []*logproto.StreamMetadata{},
			getStreamUsageResps: []*logproto.GetStreamUsageResponse{
				{
					Tenant:        "test",
					ActiveStreams: 0,
					Rate:          10,
				},
			},
			getAssignedPartitionsResps: []*logproto.GetAssignedPartitionsResponse{
				{
					AssignedPartitions: map[int32]int64{
						0: 1,
						1: 1,
					},
				},
			},
			expectedRejections: nil,
		},
		{
			name:             "under limit",
			tenant:           "test",
			maxGlobalStreams: 10,
			ingestionRate:    100,
			streams: []*logproto.StreamMetadata{
				{StreamHash: 1},
				{StreamHash: 2},
			},
			getStreamUsageResps: []*logproto.GetStreamUsageResponse{
				{
					Tenant:        "test",
					ActiveStreams: 2,
					Rate:          10,
				},
			},
			getAssignedPartitionsResps: []*logproto.GetAssignedPartitionsResponse{
				{
					AssignedPartitions: map[int32]int64{
						0: 1,
						1: 1,
					},
				},
			},
			expectedRejections: nil,
		},
		{
			name:             "exceeds limit with new streams",
			tenant:           "test",
			maxGlobalStreams: 5,
			ingestionRate:    100,
			streams: []*logproto.StreamMetadata{
				{StreamHash: 6}, // Exceeds limit
				{StreamHash: 7}, // Exceeds limit
			},
			getStreamUsageResps: []*logproto.GetStreamUsageResponse{
				{
					Tenant:         "test",
					ActiveStreams:  5,
					Rate:           10,
					UnknownStreams: []uint64{6, 7},
				},
			},
			getAssignedPartitionsResps: []*logproto.GetAssignedPartitionsResponse{
				{
					AssignedPartitions: map[int32]int64{
						0: 1,
						1: 1,
					},
				},
			},
			expectedRejections: []*logproto.RejectedStream{
				{StreamHash: 6, Reason: ReasonExceedsMaxStreams},
				{StreamHash: 7, Reason: ReasonExceedsMaxStreams},
			},
		},
		{
			name:             "exceeds limit but reject only new streams",
			tenant:           "test",
			maxGlobalStreams: 5,
			ingestionRate:    100,
			streams: []*logproto.StreamMetadata{
				{StreamHash: 1},
				{StreamHash: 2},
				{StreamHash: 3},
				{StreamHash: 4},
				{StreamHash: 5},
				{StreamHash: 6}, // Exceeds limit
				{StreamHash: 7}, // Exceeds limit
			},
			getStreamUsageResps: []*logproto.GetStreamUsageResponse{
				{
					Tenant:         "test",
					ActiveStreams:  5,
					Rate:           10,
					UnknownStreams: []uint64{6, 7},
				},
			},
			getAssignedPartitionsResps: []*logproto.GetAssignedPartitionsResponse{
				{
					AssignedPartitions: map[int32]int64{
						0: 1,
						1: 1,
					},
				},
			},
			expectedRejections: []*logproto.RejectedStream{
				{StreamHash: 6, Reason: ReasonExceedsMaxStreams},
				{StreamHash: 7, Reason: ReasonExceedsMaxStreams},
			},
		},
		{
			name:             "empty response from backend",
			tenant:           "test",
			maxGlobalStreams: 10,
			ingestionRate:    100,
			streams: []*logproto.StreamMetadata{
				{StreamHash: 1},
			},
			getStreamUsageResps: []*logproto.GetStreamUsageResponse{
				{},
			},
			getAssignedPartitionsResps: []*logproto.GetAssignedPartitionsResponse{
				{
					AssignedPartitions: map[int32]int64{
						0: 1,
					},
				},
			},
			expectedRejections: nil, // No rejections because activeStreamsTotal is 0
		},
		{
			name:             "rate limit not exceeded",
			tenant:           "test",
			maxGlobalStreams: 10,
			ingestionRate:    100,
			streams: []*logproto.StreamMetadata{
				{StreamHash: 1},
				{StreamHash: 2},
			},
			getStreamUsageResps: []*logproto.GetStreamUsageResponse{
				{
					Tenant:        "test",
					ActiveStreams: 2,
					Rate:          50, // Below the limit of 100 bytes/sec
				},
			},
			getAssignedPartitionsResps: []*logproto.GetAssignedPartitionsResponse{
				{
					AssignedPartitions: map[int32]int64{
						0: 1,
					},
				},
			},
			expectedRejections: nil,
		},
		{
			name:             "rate limit exceeded",
			tenant:           "test",
			maxGlobalStreams: 10,
			ingestionRate:    100,
			streams: []*logproto.StreamMetadata{
				{StreamHash: 1},
				{StreamHash: 2},
			},
			getStreamUsageResps: []*logproto.GetStreamUsageResponse{
				{
					Tenant:        "test",
					ActiveStreams: 2,
					Rate:          1500, // Above the limit of 100 bytes/sec
				},
			},
			getAssignedPartitionsResps: []*logproto.GetAssignedPartitionsResponse{
				{
					AssignedPartitions: map[int32]int64{
						0: 1,
					},
				},
			},
			expectedRejections: []*logproto.RejectedStream{
				{StreamHash: 1, Reason: ReasonExceedsRateLimit},
				{StreamHash: 2, Reason: ReasonExceedsRateLimit},
			},
		},
		{
			name:             "rate limit exceeded with multiple instances",
			tenant:           "test",
			maxGlobalStreams: 10,
			ingestionRate:    100,
			streams: []*logproto.StreamMetadata{
				{StreamHash: 1},
				{StreamHash: 2},
			},
			getStreamUsageResps: []*logproto.GetStreamUsageResponse{
				{
					Tenant:        "test",
					ActiveStreams: 1,
					Rate:          600,
				},
				{
					Tenant:        "test",
					ActiveStreams: 1,
					Rate:          500,
				},
			},
			getAssignedPartitionsResps: []*logproto.GetAssignedPartitionsResponse{
				{
					AssignedPartitions: map[int32]int64{
						0: 1,
					},
				},
				{
					AssignedPartitions: map[int32]int64{
						1: 1,
					},
				},
			},
			expectedRejections: []*logproto.RejectedStream{
				{StreamHash: 1, Reason: ReasonExceedsRateLimit},
				{StreamHash: 2, Reason: ReasonExceedsRateLimit},
			},
		},
		{
			name:             "both global limit and rate limit exceeded",
			tenant:           "test",
			maxGlobalStreams: 5,
			ingestionRate:    100,
			streams: []*logproto.StreamMetadata{
				{StreamHash: 6},
				{StreamHash: 7},
			},
			getStreamUsageResps: []*logproto.GetStreamUsageResponse{
				{
					Tenant:         "test",
					ActiveStreams:  5,
					UnknownStreams: []uint64{6, 7},
					Rate:           1500, // Above the limit of 100 bytes/sec
				},
			},
			getAssignedPartitionsResps: []*logproto.GetAssignedPartitionsResponse{
				{
					AssignedPartitions: map[int32]int64{
						0: 1,
					},
				},
			},
			expectedRejections: []*logproto.RejectedStream{
				{StreamHash: 6, Reason: ReasonExceedsRateLimit},
				{StreamHash: 7, Reason: ReasonExceedsRateLimit},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock clients that return the test responses
			mockClients := make([]logproto.IngestLimitsClient, len(tt.getStreamUsageResps))
			mockInstances := make([]ring.InstanceDesc, len(tt.getStreamUsageResps))
			clientsByAddr := make(map[string]logproto.IngestLimitsClient)

			for i, resp := range tt.getStreamUsageResps {
				mockClients[i] = &mockIngestLimitsClient{
					getStreamUsageResponse:        resp,
					getAssignedPartitionsResponse: tt.getAssignedPartitionsResps[i],
				}
				addr := fmt.Sprintf("mock-instance-%d", i)
				mockInstances[i] = ring.InstanceDesc{
					Addr: addr,
				}
				clientsByAddr[addr] = mockClients[i]
			}

			mockRing := &mockReadRing{
				replicationSet: ring.ReplicationSet{
					Instances: mockInstances,
				},
			}

			// Create a mock pool that implements ring_client.Pool
			poolCfg := ring_client.PoolConfig{
				CheckInterval:      0,
				HealthCheckEnabled: false,
			}
			mockPool := ring_client.NewPool(
				"test",
				poolCfg,
				ring_client.NewRingServiceDiscovery(mockRing),
				&mockFactory{clientsByAddr: clientsByAddr},
				prometheus.NewGauge(prometheus.GaugeOpts{}),
				log.NewNopLogger(),
			)

			mockLimits := &mockLimits{
				maxGlobalStreams: tt.maxGlobalStreams,
				ingestionRate:    tt.ingestionRate,
			}

			rateLimiter := limiter.NewRateLimiter(newRateLimitsAdapter(mockLimits), 10*time.Second)

			service := NewRingIngestLimitsService(mockRing, mockPool, mockLimits, rateLimiter, log.NewNopLogger(), prometheus.NewRegistry())

			req := &logproto.ExceedsLimitsRequest{
				Tenant:  tt.tenant,
				Streams: tt.streams,
			}

			resp, err := service.ExceedsLimits(context.Background(), req)
			require.NoError(t, err)
			require.Equal(t, tt.tenant, resp.Tenant)

			// Sort the rejected streams for consistent comparison
			if resp.RejectedStreams != nil {
				sort.Slice(resp.RejectedStreams, func(i, j int) bool {
					if resp.RejectedStreams[i].StreamHash == resp.RejectedStreams[j].StreamHash {
						return resp.RejectedStreams[i].Reason < resp.RejectedStreams[j].Reason
					}
					return resp.RejectedStreams[i].StreamHash < resp.RejectedStreams[j].StreamHash
				})
			}

			if tt.expectedRejections != nil {
				sort.Slice(tt.expectedRejections, func(i, j int) bool {
					if tt.expectedRejections[i].StreamHash == tt.expectedRejections[j].StreamHash {
						return tt.expectedRejections[i].Reason < tt.expectedRejections[j].Reason
					}
					return tt.expectedRejections[i].StreamHash < tt.expectedRejections[j].StreamHash
				})
			}

			require.Equal(t, tt.expectedRejections, resp.RejectedStreams)
		})
	}
}
