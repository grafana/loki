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

	"github.com/grafana/loki/v3/pkg/logproto"
)

// mockIngestLimitsClient mocks logproto.IngestLimitsClient.
type mockIngestLimitsClient struct {
	logproto.IngestLimitsClient
	t *testing.T

	// The requests expected to be received.
	expectedAssignedPartitionsRequest *logproto.GetAssignedPartitionsRequest
	expectedStreamUsageRequest        *logproto.GetStreamUsageRequest

	// The mocked responses.
	getAssignedPartitionsResponse *logproto.GetAssignedPartitionsResponse
	getStreamUsageResponse        *logproto.GetStreamUsageResponse
}

func (m *mockIngestLimitsClient) GetAssignedPartitions(_ context.Context, r *logproto.GetAssignedPartitionsRequest, _ ...grpc.CallOption) (*logproto.GetAssignedPartitionsResponse, error) {
	if expected := m.expectedAssignedPartitionsRequest; expected != nil {
		require.Equal(m.t, expected, r)
	}
	return m.getAssignedPartitionsResponse, nil
}

func (m *mockIngestLimitsClient) GetStreamUsage(_ context.Context, r *logproto.GetStreamUsageRequest, _ ...grpc.CallOption) (*logproto.GetStreamUsageResponse, error) {
	if expected := m.expectedStreamUsageRequest; expected != nil {
		require.Equal(m.t, expected, r)
	}
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

// mockFactory mocks ring_client.PoolFactory. It returns a mocked
// logproto.IngestLimitsClient.
type mockFactory struct {
	clients map[string]logproto.IngestLimitsClient
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

func newMockRingWithClientPool(_ *testing.T, name string, clients []logproto.IngestLimitsClient, instances []ring.InstanceDesc) (ring.ReadRing, *ring_client.Pool) {
	// Set up the mock ring.
	ring := &mockReadRing{
		rs: ring.ReplicationSet{
			Instances: instances,
		},
	}
	// Set up the factory that is used to create clients on demand.
	factory := &mockFactory{
		clients: make(map[string]logproto.IngestLimitsClient),
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
