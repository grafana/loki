package client

import (
	fmt "fmt"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/ring"
	"github.com/go-kit/kit/log"
	"github.com/gogo/status"
	context "golang.org/x/net/context"
	grpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health/grpc_health_v1"
)

type mockIngester struct {
	IngesterClient
	happy  bool
	status grpc_health_v1.HealthCheckResponse_ServingStatus
}

func (i mockIngester) Check(ctx context.Context, in *grpc_health_v1.HealthCheckRequest, opts ...grpc.CallOption) (*grpc_health_v1.HealthCheckResponse, error) {
	if !i.happy {
		return nil, fmt.Errorf("Fail")
	}
	return &grpc_health_v1.HealthCheckResponse{Status: i.status}, nil
}

func (i mockIngester) Close() error {
	return nil
}

func (i mockIngester) Watch(ctx context.Context, in *grpc_health_v1.HealthCheckRequest, opts ...grpc.CallOption) (grpc_health_v1.Health_WatchClient, error) {
	return nil, status.Error(codes.Unimplemented, "Watching is not supported")
}

type mockReadRing struct {
	ring.ReadRing
}

func (mockReadRing) GetAll() (ring.ReplicationSet, error) {
	return ring.ReplicationSet{}, nil
}

func TestHealthCheck(t *testing.T) {
	tcs := []struct {
		ingester mockIngester
		hasError bool
	}{
		{mockIngester{happy: true, status: grpc_health_v1.HealthCheckResponse_UNKNOWN}, true},
		{mockIngester{happy: true, status: grpc_health_v1.HealthCheckResponse_SERVING}, false},
		{mockIngester{happy: true, status: grpc_health_v1.HealthCheckResponse_NOT_SERVING}, true},
		{mockIngester{happy: false, status: grpc_health_v1.HealthCheckResponse_UNKNOWN}, true},
		{mockIngester{happy: false, status: grpc_health_v1.HealthCheckResponse_SERVING}, true},
		{mockIngester{happy: false, status: grpc_health_v1.HealthCheckResponse_NOT_SERVING}, true},
	}
	for _, tc := range tcs {
		err := healthCheck(tc.ingester, 50*time.Millisecond)
		hasError := err != nil
		if hasError != tc.hasError {
			t.Errorf("Expected error: %t, error: %v", tc.hasError, err)
		}
	}
}

func TestIngesterCache(t *testing.T) {
	buildCount := 0
	factory := func(addr string) (grpc_health_v1.HealthClient, error) {
		if addr == "bad" {
			return nil, fmt.Errorf("Fail")
		}
		buildCount++
		return mockIngester{happy: true, status: grpc_health_v1.HealthCheckResponse_SERVING}, nil
	}

	pool := NewPool(PoolConfig{
		RemoteTimeout:       50 * time.Millisecond,
		ClientCleanupPeriod: 10 * time.Second,
	}, mockReadRing{}, factory, log.NewNopLogger())
	defer pool.Stop()

	pool.GetClientFor("1")
	if buildCount != 1 {
		t.Errorf("Did not create client")
	}

	pool.GetClientFor("1")
	if buildCount != 1 {
		t.Errorf("Created client that should have been cached")
	}

	pool.GetClientFor("2")
	if pool.Count() != 2 {
		t.Errorf("Expected Count() = 2, got %d", pool.Count())
	}

	pool.RemoveClientFor("1")
	if pool.Count() != 1 {
		t.Errorf("Expected Count() = 1, got %d", pool.Count())
	}

	pool.GetClientFor("1")
	if buildCount != 3 || pool.Count() != 2 {
		t.Errorf("Did not re-create client correctly")
	}

	_, err := pool.GetClientFor("bad")
	if err == nil {
		t.Errorf("Bad create should have thrown an error")
	}
	if pool.Count() != 2 {
		t.Errorf("Bad create should not have been added to cache")
	}

	addrs := pool.RegisteredAddresses()
	if len(addrs) != pool.Count() {
		t.Errorf("Lengths of registered addresses and cache.Count() do not match")
	}
}

func TestCleanUnhealthy(t *testing.T) {
	goodAddrs := []string{"good1", "good2"}
	badAddrs := []string{"bad1", "bad2"}
	clients := map[string]grpc_health_v1.HealthClient{}
	for _, addr := range goodAddrs {
		clients[addr] = mockIngester{happy: true, status: grpc_health_v1.HealthCheckResponse_SERVING}
	}
	for _, addr := range badAddrs {
		clients[addr] = mockIngester{happy: false, status: grpc_health_v1.HealthCheckResponse_NOT_SERVING}
	}
	pool := &Pool{
		clients: clients,
		logger:  log.NewNopLogger(),
	}
	pool.cleanUnhealthy()
	for _, addr := range badAddrs {
		if _, ok := pool.clients[addr]; ok {
			t.Errorf("Found bad ingester after clean: %s\n", addr)
		}
	}
	for _, addr := range goodAddrs {
		if _, ok := pool.clients[addr]; !ok {
			t.Errorf("Could not find good ingester after clean: %s\n", addr)
		}
	}
}
