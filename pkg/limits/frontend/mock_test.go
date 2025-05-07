package frontend

import (
	"context"
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

// mockExceedsLimitsGatherer mocks an ExeceedsLimitsGatherer. It avoids having to
// set up a mock ring to test the frontend.
type mockExceedsLimitsGatherer struct {
	t *testing.T

	expectedExceedsLimitsRequest *proto.ExceedsLimitsRequest
	exceedsLimitsResponses       []*proto.ExceedsLimitsResponse
}

func (g *mockExceedsLimitsGatherer) ExceedsLimits(_ context.Context, req *proto.ExceedsLimitsRequest) ([]*proto.ExceedsLimitsResponse, error) {
	if expected := g.expectedExceedsLimitsRequest; expected != nil {
		require.Equal(g.t, expected, req)
	}
	return g.exceedsLimitsResponses, nil
}

// mockIngestLimitsClient mocks proto.IngestLimitsClient.
type mockIngestLimitsClient struct {
	proto.IngestLimitsClient
	t *testing.T

	// The requests expected to be received.
	expectedAssignedPartitionsRequest *proto.GetAssignedPartitionsRequest
	expectedExceedsLimitsRequest      *proto.ExceedsLimitsRequest

	// The mocked responses.
	getAssignedPartitionsResponse    *proto.GetAssignedPartitionsResponse
	getAssignedPartitionsResponseErr error
	exceedsLimitsResponse            *proto.ExceedsLimitsResponse
	exceedsLimitsResponseErr         error

	// The expected request counts.
	expectedNumAssignedPartitionsRequests int
	expectedNumExceedsLimitsRequests      int

	// The actual request counts.
	numAssignedPartitionsRequests int
	numExceedsLimitsRequests      int
}

func (m *mockIngestLimitsClient) GetAssignedPartitions(_ context.Context, r *proto.GetAssignedPartitionsRequest, _ ...grpc.CallOption) (*proto.GetAssignedPartitionsResponse, error) {
	if expected := m.expectedAssignedPartitionsRequest; expected != nil {
		require.Equal(m.t, expected, r)
	}
	m.numAssignedPartitionsRequests++
	if err := m.getAssignedPartitionsResponseErr; err != nil {
		return nil, err
	}
	return m.getAssignedPartitionsResponse, nil
}

func (m *mockIngestLimitsClient) ExceedsLimits(_ context.Context, r *proto.ExceedsLimitsRequest, _ ...grpc.CallOption) (*proto.ExceedsLimitsResponse, error) {
	if expected := m.expectedExceedsLimitsRequest; expected != nil {
		require.Equal(m.t, expected, r)
	}
	m.numExceedsLimitsRequests++
	if err := m.exceedsLimitsResponseErr; err != nil {
		return nil, err
	}
	return m.exceedsLimitsResponse, nil
}

func (m *mockIngestLimitsClient) AssertExpectedNumRequests() {
	require.Equal(m.t, m.expectedNumAssignedPartitionsRequests, m.numAssignedPartitionsRequests)
	require.Equal(m.t, m.expectedNumExceedsLimitsRequests, m.numExceedsLimitsRequests)
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

func newMockRingWithClientPool(_ *testing.T, name string, clients []*mockIngestLimitsClient, instances []ring.InstanceDesc) (ring.ReadRing, *ring_client.Pool) {
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
