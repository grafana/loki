package frontend

import (
	"context"
	"testing"

	"github.com/go-kit/log"
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
}

func (m *mockLimits) MaxGlobalStreamsPerUser(_ string) int {
	return m.maxGlobalStreams
}

type mockReadRing struct {
	ring.ReadRing
	replicationSet ring.ReplicationSet
}

func (m *mockReadRing) GetAllHealthy(_ ring.Operation) (ring.ReplicationSet, error) {
	return m.replicationSet, nil
}

type mockFactory struct {
	clients []logproto.IngestLimitsClient
}

func (f *mockFactory) FromInstance(_ ring.InstanceDesc) (ring_client.PoolClient, error) {
	for _, c := range f.clients {
		return c.(ring_client.PoolClient), nil
	}
	return nil, nil
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
	return m.getStreamUsageResponse, nil
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
		streams                    []*logproto.StreamMetadataWithSize
		getStreamUsageResps        []*logproto.GetStreamUsageResponse
		getAssignedPartitionsResps []*logproto.GetAssignedPartitionsResponse
		expectedRejections         []*logproto.RejectedStream
	}{
		{
			name:             "no streams",
			tenant:           "test",
			maxGlobalStreams: 10,
			streams:          []*logproto.StreamMetadataWithSize{},
			getStreamUsageResps: []*logproto.GetStreamUsageResponse{
				{
					Tenant:        "test",
					ActiveStreams: 0,
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
			streams: []*logproto.StreamMetadataWithSize{
				{StreamHash: 1},
				{StreamHash: 2},
			},
			getStreamUsageResps: []*logproto.GetStreamUsageResponse{
				{
					Tenant:        "test",
					ActiveStreams: 2,
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
			streams: []*logproto.StreamMetadataWithSize{
				{StreamHash: 6}, // Exceeds limit
				{StreamHash: 7}, // Exceeds limit
			},
			getStreamUsageResps: []*logproto.GetStreamUsageResponse{
				{
					Tenant:         "test",
					ActiveStreams:  5,
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
				{StreamHash: 6, Reason: RejectedStreamReasonExceedsGlobalLimit},
				{StreamHash: 7, Reason: RejectedStreamReasonExceedsGlobalLimit},
			},
		},
		{
			name:             "exceeds limit but reject only new streams",
			tenant:           "test",
			maxGlobalStreams: 5,
			streams: []*logproto.StreamMetadataWithSize{
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
				{StreamHash: 6, Reason: RejectedStreamReasonExceedsGlobalLimit},
				{StreamHash: 7, Reason: RejectedStreamReasonExceedsGlobalLimit},
			},
		},
		{
			name:             "empty response from backend",
			tenant:           "test",
			maxGlobalStreams: 10,
			streams: []*logproto.StreamMetadataWithSize{
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock clients that return the test responses
			mockClients := make([]logproto.IngestLimitsClient, len(tt.getStreamUsageResps))
			mockInstances := make([]ring.InstanceDesc, len(tt.getStreamUsageResps))

			for i, resp := range tt.getStreamUsageResps {
				mockClients[i] = &mockIngestLimitsClient{
					getStreamUsageResponse:        resp,
					getAssignedPartitionsResponse: tt.getAssignedPartitionsResps[i],
				}
				mockInstances[i] = ring.InstanceDesc{
					Addr: "mock-instance",
				}
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
				&mockFactory{clients: mockClients},
				prometheus.NewGauge(prometheus.GaugeOpts{}),
				log.NewNopLogger(),
			)

			mockLimits := &mockLimits{
				maxGlobalStreams: tt.maxGlobalStreams,
			}

			service := NewRingIngestLimitsService(mockRing, mockPool, mockLimits, log.NewNopLogger(), prometheus.NewRegistry())

			req := &logproto.ExceedsLimitsRequest{
				Tenant:  tt.tenant,
				Streams: tt.streams,
			}

			resp, err := service.ExceedsLimits(context.Background(), req)
			require.NoError(t, err)
			require.Equal(t, tt.tenant, resp.Tenant)
			require.Equal(t, tt.expectedRejections, resp.RejectedStreams)
		})
	}
}
