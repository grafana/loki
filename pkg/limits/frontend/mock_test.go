package frontend

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/ring"
	ring_client "github.com/grafana/dskit/ring/client"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/loki/v3/pkg/limits/proto"
)

// mockLimitsClient mocks a limitsClient. It avoids having to set up a mock
// ring to test the frontend.
type mockLimitsClient struct {
	t *testing.T

	expectedExceedsLimitsRequest *proto.ExceedsLimitsRequest
	exceedsLimitsResponses       []*proto.ExceedsLimitsResponse
	err                          error
}

func (m *mockLimitsClient) ExceedsLimits(_ context.Context, req *proto.ExceedsLimitsRequest) ([]*proto.ExceedsLimitsResponse, error) {
	if expected := m.expectedExceedsLimitsRequest; expected != nil {
		require.Equal(m.t, expected, req)
	}
	return m.exceedsLimitsResponses, m.err
}

// mockLimitsProtoClient mocks proto.IngestLimitsClient.
type mockLimitsProtoClient struct {
	proto.IngestLimitsClient
	t *testing.T

	// The complete set of expected requests over the lifetime of the client.
	// We don't check the expected requests for GetAssignedPartitions as it
	// has no fields. Instead, tests should check the number of requests
	// received with [Finished].
	expectedExceedsLimitsRequests []*proto.ExceedsLimitsRequest

	// The complete set of mocked responses over the lifetime of the client.
	// When a request is received, it consumes the next response (or error)
	// until there are no more left. Aadditional requests fail with an error.
	getAssignedPartitionsResponses    []*proto.GetAssignedPartitionsResponse
	getAssignedPartitionsResponseErrs []error
	exceedsLimitsResponses            []*proto.ExceedsLimitsResponse
	exceedsLimitsResponseErrs         []error

	// The actual request counts.
	numAssignedPartitionsRequests int
	numExceedsLimitsRequests      int
}

func (m *mockLimitsProtoClient) GetAssignedPartitions(_ context.Context, _ *proto.GetAssignedPartitionsRequest, _ ...grpc.CallOption) (*proto.GetAssignedPartitionsResponse, error) {
	idx := m.numAssignedPartitionsRequests
	// Check that we haven't received more requests than we have mocked
	// responses.
	if idx >= len(m.getAssignedPartitionsResponses) {
		return nil, errors.New("unexpected GetAssignedPartitionsRequest")
	}
	m.numAssignedPartitionsRequests++
	if err := m.getAssignedPartitionsResponseErrs[idx]; err != nil {
		return nil, err
	}
	return m.getAssignedPartitionsResponses[idx], nil
}

func (m *mockLimitsProtoClient) ExceedsLimits(_ context.Context, req *proto.ExceedsLimitsRequest, _ ...grpc.CallOption) (*proto.ExceedsLimitsResponse, error) {
	idx := m.numExceedsLimitsRequests
	// Check that we haven't received more requests than we have mocked
	// responses.
	if idx >= len(m.exceedsLimitsResponses) {
		return nil, errors.New("unexpected ExceedsLimitsRequest")
	}
	m.numExceedsLimitsRequests++
	if len(m.expectedExceedsLimitsRequests) > 0 {
		require.Equal(m.t, m.expectedExceedsLimitsRequests[idx], req)
	}
	if err := m.exceedsLimitsResponseErrs[idx]; err != nil {
		return nil, err
	}
	return m.exceedsLimitsResponses[idx], nil
}

func (m *mockLimitsProtoClient) Finished() {
	require.Equal(m.t, len(m.getAssignedPartitionsResponses), m.numAssignedPartitionsRequests)
	require.Equal(m.t, len(m.exceedsLimitsResponses), m.numExceedsLimitsRequests)
}

func (m *mockLimitsProtoClient) Close() error {
	return nil
}

func (m *mockLimitsProtoClient) Check(_ context.Context, _ *grpc_health_v1.HealthCheckRequest, _ ...grpc.CallOption) (*grpc_health_v1.HealthCheckResponse, error) {
	return &grpc_health_v1.HealthCheckResponse{
		Status: grpc_health_v1.HealthCheckResponse_SERVING,
	}, nil
}

func (m *mockLimitsProtoClient) List(_ context.Context, _ *grpc_health_v1.HealthListRequest, _ ...grpc.CallOption) (*grpc_health_v1.HealthListResponse, error) {
	return &grpc_health_v1.HealthListResponse{}, nil
}

func (m *mockLimitsProtoClient) Watch(_ context.Context, _ *grpc_health_v1.HealthCheckRequest, _ ...grpc.CallOption) (grpc_health_v1.Health_WatchClient, error) {
	return nil, nil
}

// mockFactory mocks ring_client.PoolFactory. It returns a mocked
// proto.IngestLimitsClient.
type mockFactory struct {
	clients map[string]proto.IngestLimitsClient
}

func (f *mockFactory) FromInstance(inst ring.InstanceDesc) (ring_client.PoolClient, error) {
	client, ok := f.clients[inst.Addr]
	if !ok {
		return nil, fmt.Errorf("no client for address %s", inst.Addr)
	}
	return client.(ring_client.PoolClient), nil
}

// mockReadRing mocks ring.ReadRing.
type mockReadRing struct {
	ring.ReadRing
	rs ring.ReplicationSet
}

func (m *mockReadRing) GetAllHealthy(_ ring.Operation) (ring.ReplicationSet, error) {
	return m.rs, nil
}

func newMockRingWithClientPool(_ *testing.T, name string, clients []*mockLimitsProtoClient, instances []ring.InstanceDesc) (ring.ReadRing, *ring_client.Pool) {
	// Set up the mock ring.
	ring := &mockReadRing{
		rs: ring.ReplicationSet{
			Instances: instances,
		},
	}
	// Set up the factory that is used to create clients on demand.
	factory := &mockFactory{
		clients: make(map[string]proto.IngestLimitsClient),
	}
	for i := 0; i < len(clients); i++ {
		factory.clients[instances[i].Addr] = clients[i]
	}
	// Set up the client pool for the mock clients.
	pool := ring_client.NewPool(
		name,
		ring_client.PoolConfig{
			CheckInterval:      0,
			HealthCheckEnabled: false,
		},
		ring_client.NewRingServiceDiscovery(ring),
		factory,
		prometheus.NewGauge(prometheus.GaugeOpts{}),
		log.NewNopLogger(),
	)
	return ring, pool
}
