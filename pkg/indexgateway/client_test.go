package indexgateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/grpcclient"
	"github.com/grafana/dskit/httpgrpc"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/kv/consul"
	"github.com/grafana/dskit/ring"
	dskitclient "github.com/grafana/dskit/ring/client"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/loki/v3/pkg/util/discovery"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/validation"
)

type mockDNSProvider struct {
	addrs []string
}

func newMockDNSProvider(addrs []string) discovery.DNS {
	return &mockDNSProvider{
		addrs: addrs,
	}
}

func (m *mockDNSProvider) Addresses() []string {
	return m.addrs
}

func (m *mockDNSProvider) Stop() {}

type mockIndexGatewayServer struct {
	logproto.IndexGatewayServer
}

func (m mockIndexGatewayServer) GetChunkRef(context.Context, *logproto.GetChunkRefRequest) (*logproto.GetChunkRefResponse, error) {
	return &logproto.GetChunkRefResponse{}, nil
}

type mockGatewayConn struct {
	logproto.IndexGatewayClient
	grpc_health_v1.HealthClient
	returnErrors bool
}

func (m *mockGatewayConn) GetChunkRef(context.Context, *logproto.GetChunkRefRequest, ...grpc.CallOption) (*logproto.GetChunkRefResponse, error) {
	if m.returnErrors {
		return nil, errors.New("mock error")
	}
	return &logproto.GetChunkRefResponse{}, nil
}

func (m *mockGatewayConn) GetShards(_ context.Context, _ *logproto.ShardsRequest, _ ...grpc.CallOption) (logproto.IndexGateway_GetShardsClient, error) {
	if m.returnErrors {
		return nil, errors.New("mock error")
	}
	return &mockShardsClient{}, nil
}

type mockShardsClient struct {
	grpc.ClientStream
	done bool
}

func (m *mockShardsClient) Recv() (*logproto.ShardsResponse, error) {
	if m.done {
		return nil, io.EOF
	}
	m.done = true
	return &logproto.ShardsResponse{}, nil
}

func (m *mockGatewayConn) Check(context.Context, *grpc_health_v1.HealthCheckRequest, ...grpc.CallOption) (*grpc_health_v1.HealthCheckResponse, error) {
	return &grpc_health_v1.HealthCheckResponse{Status: grpc_health_v1.HealthCheckResponse_SERVING}, nil
}

func (m *mockGatewayConn) Close() error { return nil }

type mockTenantLimits map[string]*validation.Limits

func (tl mockTenantLimits) TenantLimits(userID string) *validation.Limits {
	return tl[userID]
}

func (tl mockTenantLimits) AllByUserID() map[string]*validation.Limits {
	return tl
}

func TestGatewayClient_RingMode(t *testing.T) {
	// prepare servers and ring
	logger := log.NewNopLogger()
	ringKey := "test"
	n := 6  // nuber of index gateway instances
	rf := 1 // replication factor
	s := 3  // shard size

	nodes := make([]*mockIndexGatewayServer, n)
	for i := 0; i < n; i++ {
		nodes[i] = &mockIndexGatewayServer{}
	}

	nodeDescs := map[string]ring.InstanceDesc{}

	for i := range nodes {
		addr := fmt.Sprintf("index-gateway-%d", i)
		nodeDescs[addr] = ring.InstanceDesc{
			Addr:                addr,
			State:               ring.ACTIVE,
			Timestamp:           time.Now().Unix(),
			RegisteredTimestamp: time.Now().Add(-10 * time.Minute).Unix(),
			Tokens:              []uint32{uint32((math.MaxUint32 / n) * i)},
		}
	}

	kvStore, closer := consul.NewInMemoryClient(ring.GetCodec(), logger, nil)
	t.Cleanup(func() { closer.Close() })

	err := kvStore.CAS(context.Background(), ringKey,
		func(_ interface{}) (interface{}, bool, error) {
			return &ring.Desc{
				Ingesters: nodeDescs,
			}, true, nil
		},
	)
	require.NoError(t, err)

	ringCfg := ring.Config{
		KVStore: kv.Config{
			Mock: kvStore,
		},
		HeartbeatTimeout:     time.Hour,
		ZoneAwarenessEnabled: false,
		ReplicationFactor:    rf,
	}

	igwRing, err := ring.New(ringCfg, "indexgateway", ringKey, logger, nil)
	require.NoError(t, err)
	require.NoError(t, services.StartAndAwaitRunning(context.Background(), igwRing))
	require.Eventually(t, func() bool {
		return igwRing.InstancesCount() == n
	}, time.Minute, time.Second)

	t.Cleanup(func() {
		igwRing.StopAsync()
	})

	t.Run("global shard size", func(t *testing.T) {
		o, err := validation.NewOverrides(validation.Limits{IndexGatewayShardSize: s}, nil)
		require.NoError(t, err)

		cfg := ClientConfig{}
		flagext.DefaultValues(&cfg)
		cfg.Mode = RingMode
		cfg.Ring = igwRing

		c, err := NewGatewayClient("test", cfg, nil, o, logger, constants.Loki)
		require.NoError(t, err)
		require.NotNil(t, c)

		// Shuffle sharding is deterministic
		// The same tenant ID gets the same servers assigned every time

		addrs, err := c.getServerAddresses("12345")
		require.NoError(t, err)
		require.Len(t, addrs, s)
		require.ElementsMatch(t, addrs, []string{"index-gateway-0", "index-gateway-3", "index-gateway-5"})

		addrs, err = c.getServerAddresses("67890")
		require.NoError(t, err)
		require.Len(t, addrs, s)
		require.ElementsMatch(t, addrs, []string{"index-gateway-2", "index-gateway-3", "index-gateway-5"})
	})

	t.Run("per tenant shard size", func(t *testing.T) {
		tl := mockTenantLimits{
			"12345": &validation.Limits{IndexGatewayShardSize: 1},
			// tenant 67890 has not tenant specific overrides
		}
		o, err := validation.NewOverrides(validation.Limits{IndexGatewayShardSize: s}, tl)
		require.NoError(t, err)

		cfg := ClientConfig{}
		flagext.DefaultValues(&cfg)
		cfg.Mode = RingMode
		cfg.Ring = igwRing

		c, err := NewGatewayClient("test", cfg, nil, o, logger, constants.Loki)
		require.NoError(t, err)
		require.NotNil(t, c)

		// Shuffle sharding is deterministic
		// The same tenant ID gets the same servers assigned every time

		addrs, err := c.getServerAddresses("12345")
		require.NoError(t, err)
		require.Len(t, addrs, 1)
		require.ElementsMatch(t, addrs, []string{"index-gateway-3"})

		addrs, err = c.getServerAddresses("67890")
		require.NoError(t, err)
		require.Len(t, addrs, s)
		require.ElementsMatch(t, addrs, []string{"index-gateway-2", "index-gateway-3", "index-gateway-5"})
	})
}

func createSimpleGatewayClient(t *testing.T, addrs []string, maxRetries int) (log.Logger, *dskitclient.Pool, *GatewayClient) {
	logger := log.NewNopLogger()
	r := prometheus.NewRegistry()
	o, _ := validation.NewOverrides(validation.Limits{}, nil)
	client, err := NewGatewayClient(
		"test",
		ClientConfig{
			Mode:             "simple",
			GRPCClientConfig: grpcclient.Config{},
			Address:          "1.1.1.1",
			MaxRetries:       maxRetries,
		},
		r, o, logger, constants.Loki)
	require.NoError(t, err)
	defer client.Stop()
	require.NoError(t, services.StopAndAwaitTerminated(context.Background(), client.pool))
	client.dnsProvider = newMockDNSProvider(addrs)
	pool := configurePool(t, client, logger, 0)
	return logger, pool, client
}

func configurePool(t *testing.T, client *GatewayClient, logger log.Logger, numErrorsToReturn int) *dskitclient.Pool {
	pool := dskitclient.NewPool(
		"test",
		dskitclient.PoolConfig{CheckInterval: time.Hour},
		func() ([]string, error) { return client.dnsProvider.Addresses(), nil },
		dskitclient.PoolAddrFunc(func(string) (dskitclient.PoolClient, error) {
			var returnErrors bool
			if numErrorsToReturn > 0 {
				numErrorsToReturn--
				returnErrors = true
			} else {
				returnErrors = false
			}
			return &mockGatewayConn{returnErrors: returnErrors}, nil
		}),
		nil, logger,
	)
	require.NoError(t, services.StartAndAwaitRunning(context.Background(), pool))
	client.pool = pool
	return pool
}

// The streaming callback wraps errors before returning them to poolDo.
func TestGatewayClient_SimpleMode_RetriesGetShards(t *testing.T) {
	logger, _, client := createSimpleGatewayClient(t, []string{
		"0.0.0.0", "1.1.1.1", "2.2.2.2", "3.3.3.3", "4.4.4.4",
		"5.5.5.5", "6.6.6.6", "7.7.7.7", "8.8.8.8", "9.9.9.9",
	}, 2)
	ctx := user.InjectOrgID(context.Background(), "tenant-123")

	// Retry up to 2 errors
	for numErrorsToReturn := 0; numErrorsToReturn <= 2; numErrorsToReturn++ {
		configurePool(t, client, logger, numErrorsToReturn)
		_, err := client.GetShards(ctx, &logproto.ShardsRequest{})
		require.NoError(t, err)
	}

	// Fail after 3 errors
	configurePool(t, client, logger, 3)
	_, err := client.GetShards(ctx, &logproto.ShardsRequest{})
	require.Error(t, err)
}

func TestGatewayClient_SimpleMode_ShuffleSharding(t *testing.T) {
	_, pool, client := createSimpleGatewayClient(t, []string{
		"0.0.0.0", "1.1.1.1", "2.2.2.2", "3.3.3.3", "4.4.4.4",
		"5.5.5.5", "6.6.6.6", "7.7.7.7", "8.8.8.8", "9.9.9.9",
	}, 2)
	client.limits = mockLimits{maxCapacity: 0.5}
	for i := 0; i < 1000; i++ {
		ctx := user.InjectOrgID(context.Background(), "tenant-01")
		_, err := client.GetChunkRef(ctx, &logproto.GetChunkRefRequest{})
		require.NoError(t, err)
	}
	require.Len(t, pool.RegisteredAddresses(), 5)
}

func TestDoubleRegistration(t *testing.T) {
	logger := log.NewNopLogger()
	r := prometheus.NewRegistry()
	o, _ := validation.NewOverrides(validation.Limits{}, nil)

	clientCfg := ClientConfig{
		Address: "my-store-address:1234",
	}

	client, err := NewGatewayClient("primary", clientCfg, r, o, logger, constants.Loki)
	require.NoError(t, err)
	defer client.Stop()

	client, err = NewGatewayClient("primary", clientCfg, r, o, logger, constants.Loki)
	require.NoError(t, err)
	defer client.Stop()
}

// gate.NewInstrumented panics when two clients register indistinguishable metrics.
func TestGatewayClient_SharedRegisterer(t *testing.T) {
	logger := log.NewNopLogger()
	reg := prometheus.NewRegistry()
	o, _ := validation.NewOverrides(validation.Limits{}, nil)

	cfg := ClientConfig{Address: "my-store-address:1234", MaxInFlightRequests: 7}

	require.NotPanics(t, func() {
		for _, name := range []string{"primary", "secondary"} {
			client, err := NewGatewayClient(name, cfg, reg, o, logger, constants.Loki)
			require.NoError(t, err)
			t.Cleanup(client.Stop)
		}
	})

	for _, name := range []string{"primary", "secondary"} {
		m := findMetric(t, reg, "loki_index_gateway_client_gate_queries_concurrent_max", map[string]string{"client": name})
		require.NotNil(t, m, "no gate metrics registered for the %s client", name)
		require.Equal(t, float64(7), m.GetGauge().GetValue())
	}
}

type fakeGateways struct {
	dialErr map[string]error
	rpcErr  map[string]error

	mu       sync.Mutex
	attempts []string
}

func (f *fakeGateways) record(addr string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attempts = append(f.attempts, addr)
}

func (f *fakeGateways) tried() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.attempts)
}

func (f *fakeGateways) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attempts = nil
}

type scriptedGatewayConn struct {
	logproto.IndexGatewayClient
	grpc_health_v1.HealthClient
	gateways *fakeGateways
	addr     string
}

func (c *scriptedGatewayConn) GetChunkRef(context.Context, *logproto.GetChunkRefRequest, ...grpc.CallOption) (*logproto.GetChunkRefResponse, error) {
	c.gateways.record(c.addr)
	if err := c.gateways.rpcErr[c.addr]; err != nil {
		return nil, err
	}
	return &logproto.GetChunkRefResponse{}, nil
}

func (c *scriptedGatewayConn) Check(context.Context, *grpc_health_v1.HealthCheckRequest, ...grpc.CallOption) (*grpc_health_v1.HealthCheckResponse, error) {
	return &grpc_health_v1.HealthCheckResponse{Status: grpc_health_v1.HealthCheckResponse_SERVING}, nil
}

func (c *scriptedGatewayConn) Close() error { return nil }

func newScriptedGatewayClient(t *testing.T, cfg ClientConfig, reg prometheus.Registerer, gateways *fakeGateways, addrs []string) *GatewayClient {
	t.Helper()

	logger := log.NewNopLogger()
	o, err := validation.NewOverrides(validation.Limits{}, nil)
	require.NoError(t, err)

	cfg.Mode = SimpleMode
	cfg.Address = "index-gateway"

	client, err := NewGatewayClient("test", cfg, reg, o, logger, constants.Loki)
	require.NoError(t, err)
	require.NoError(t, services.StopAndAwaitTerminated(context.Background(), client.pool))
	t.Cleanup(client.Stop)

	client.dnsProvider = newMockDNSProvider(addrs)
	pool := dskitclient.NewPool(
		"test",
		dskitclient.PoolConfig{CheckInterval: time.Hour},
		func() ([]string, error) { return client.dnsProvider.Addresses(), nil },
		dskitclient.PoolAddrFunc(func(addr string) (dskitclient.PoolClient, error) {
			if err := gateways.dialErr[addr]; err != nil {
				gateways.record(addr)
				return nil, err
			}
			return &scriptedGatewayConn{gateways: gateways, addr: addr}, nil
		}),
		nil, logger,
	)
	require.NoError(t, services.StartAndAwaitRunning(context.Background(), pool))
	client.pool = pool

	return client
}

func tenGateways() []string {
	addrs := make([]string, 10)
	for i := range addrs {
		addrs[i] = fmt.Sprintf("%d.%d.%d.%d", i, i, i, i)
	}
	return addrs
}

func failEvery(addrs []string, err error) map[string]error {
	failures := make(map[string]error, len(addrs))
	for _, addr := range addrs {
		failures[addr] = err
	}
	return failures
}

func TestGatewayClient_RetryBudget(t *testing.T) {
	addrs := tenGateways()
	shed := httpgrpc.Error(http.StatusServiceUnavailable, "shed")

	for _, tc := range []struct {
		name         string
		maxRetries   int
		gateways     *fakeGateways
		wantAttempts int
	}{
		{
			name:         "zero retries means a single attempt",
			maxRetries:   0,
			gateways:     &fakeGateways{rpcErr: failEvery(addrs, errors.New("boom"))},
			wantAttempts: 1,
		},
		{
			name:         "budget bounds the number of instances tried",
			maxRetries:   2,
			gateways:     &fakeGateways{rpcErr: failEvery(addrs, errors.New("boom"))},
			wantAttempts: 3,
		},
		{
			name:         "budget larger than the pool stops at the pool",
			maxRetries:   100,
			gateways:     &fakeGateways{rpcErr: failEvery(addrs, errors.New("boom"))},
			wantAttempts: len(addrs),
		},
		{
			name:         "connection failures consume the budget",
			maxRetries:   2,
			gateways:     &fakeGateways{dialErr: failEvery(addrs, errors.New("no route to host"))},
			wantAttempts: 3,
		},
		{
			name:         "shed requests consume the budget",
			maxRetries:   2,
			gateways:     &fakeGateways{rpcErr: failEvery(addrs, shed)},
			wantAttempts: 3,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := newScriptedGatewayClient(t, ClientConfig{MaxRetries: tc.maxRetries}, nil, tc.gateways, addrs)

			ctx := user.InjectOrgID(context.Background(), "tenant-123")
			_, err := client.GetChunkRef(ctx, &logproto.GetChunkRefRequest{})

			require.Error(t, err)
			require.Len(t, tc.gateways.tried(), tc.wantAttempts)
		})
	}
}

func TestGatewayClient_NeverTriesTheSameInstanceTwice(t *testing.T) {
	// SRV resolution can return the same host:port more than once.
	addrs := []string{"1.1.1.1", "2.2.2.2", "1.1.1.1", "2.2.2.2", "1.1.1.1"}
	gateways := &fakeGateways{rpcErr: failEvery(addrs, errors.New("boom"))}

	client := newScriptedGatewayClient(t, ClientConfig{MaxRetries: 100}, nil, gateways, addrs)

	ctx := user.InjectOrgID(context.Background(), "tenant-123")
	_, err := client.GetChunkRef(ctx, &logproto.GetChunkRefRequest{})

	require.Error(t, err)
	require.ElementsMatch(t, []string{"1.1.1.1", "2.2.2.2"}, gateways.tried())
}

func TestGatewayClient_ShedInstanceFallsThroughToAnotherOne(t *testing.T) {
	gateways := &fakeGateways{rpcErr: map[string]error{
		"1.1.1.1": httpgrpc.Error(http.StatusServiceUnavailable, "shed"),
	}}
	client := newScriptedGatewayClient(t, ClientConfig{MaxRetries: 1}, nil, gateways, []string{"1.1.1.1", "2.2.2.2"})

	ctx := user.InjectOrgID(context.Background(), "tenant-123")

	shedFirst := 0
	for range 50 {
		gateways.reset()
		_, err := client.GetChunkRef(ctx, &logproto.GetChunkRefRequest{})
		require.NoError(t, err)

		if tried := gateways.tried(); tried[0] == "1.1.1.1" {
			shedFirst++
			require.Equal(t, []string{"1.1.1.1", "2.2.2.2"}, tried)
		}
	}
	require.Positive(t, shedFirst, "the shedding instance was never tried first, so the fallthrough was not exercised")
}

func TestGatewayClient_AllInstancesShedReturns503(t *testing.T) {
	addrs := tenGateways()
	gateways := &fakeGateways{rpcErr: failEvery(addrs, httpgrpc.Error(http.StatusServiceUnavailable, "shed"))}

	client := newScriptedGatewayClient(t, ClientConfig{MaxRetries: 2}, nil, gateways, addrs)

	ctx := user.InjectOrgID(context.Background(), "tenant-123")
	_, err := client.GetChunkRef(ctx, &logproto.GetChunkRefRequest{})

	requireShedError(t, err)
}

func TestGatewayClient_InFlightCap(t *testing.T) {
	addrs := []string{"1.1.1.1"}
	ctx := user.InjectOrgID(context.Background(), "tenant-123")

	t.Run("rejects at capacity with a 503 without contacting any instance", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		gateways := &fakeGateways{}
		client := newScriptedGatewayClient(t, ClientConfig{MaxInFlightRequests: 1}, reg, gateways, addrs)

		require.NoError(t, client.inFlight.Start(ctx))
		defer client.inFlight.Done()

		_, err := client.GetChunkRef(ctx, &logproto.GetChunkRefRequest{})

		requireShedError(t, err)
		require.Empty(t, gateways.tried())

		m := findMetric(t, reg, "loki_index_gateway_client_gate_duration_seconds", map[string]string{
			"client": "test", "outcome": "rejected_other",
		})
		require.NotNil(t, m)
		require.Equal(t, uint64(1), m.GetHistogram().GetSampleCount())
	})

	t.Run("in-flight gauge tracks reality", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		client := newScriptedGatewayClient(t, ClientConfig{MaxInFlightRequests: 4}, reg, &fakeGateways{}, addrs)

		inFlight := func() float64 {
			m := findMetric(t, reg, "loki_index_gateway_client_gate_queries_in_flight", map[string]string{"client": "test"})
			require.NotNil(t, m)
			return m.GetGauge().GetValue()
		}

		require.Zero(t, inFlight())

		require.NoError(t, client.inFlight.Start(ctx))
		require.Equal(t, float64(1), inFlight())

		_, err := client.GetChunkRef(ctx, &logproto.GetChunkRefRequest{})
		require.NoError(t, err)
		require.Equal(t, float64(1), inFlight())

		client.inFlight.Done()
		require.Zero(t, inFlight())
	})

	t.Run("zero disables the cap", func(t *testing.T) {
		reg := prometheus.NewRegistry()
		client := newScriptedGatewayClient(t, ClientConfig{MaxInFlightRequests: 0}, reg, &fakeGateways{}, addrs)

		for range 100 {
			require.NoError(t, client.inFlight.Start(ctx))
		}

		_, err := client.GetChunkRef(ctx, &logproto.GetChunkRefRequest{})
		require.NoError(t, err)

		require.Nil(t, findMetric(t, reg, "loki_index_gateway_client_gate_queries_in_flight", nil))
	})
}

// findMetric returns the first matching metric or nil.
func findMetric(t *testing.T, reg *prometheus.Registry, name string, want map[string]string) *dto.Metric {
	t.Helper()

	families, err := reg.Gather()
	require.NoError(t, err)

	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, m := range family.GetMetric() {
			if hasLabels(m, want) {
				return m
			}
		}
	}
	return nil
}

func hasLabels(m *dto.Metric, want map[string]string) bool {
	got := make(map[string]string, len(m.GetLabel()))
	for _, l := range m.GetLabel() {
		got[l.GetName()] = l.GetValue()
	}
	for name, value := range want {
		if got[name] != value {
			return false
		}
	}
	return true
}

func TestClientConfig_Validate(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(*ClientConfig)
		wantErr string
	}{
		{
			name:   "defaults",
			mutate: func(*ClientConfig) {},
		},
		{
			name:   "in-flight cap disabled",
			mutate: func(cfg *ClientConfig) { cfg.MaxInFlightRequests = 0 },
		},
		{
			name:   "retries disabled",
			mutate: func(cfg *ClientConfig) { cfg.MaxRetries = 0 },
		},
		{
			name:    "negative in-flight cap",
			mutate:  func(cfg *ClientConfig) { cfg.MaxInFlightRequests = -1 },
			wantErr: "max-in-flight-requests",
		},
		{
			name:    "negative retries",
			mutate:  func(cfg *ClientConfig) { cfg.MaxRetries = -1 },
			wantErr: "max-retries",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := ClientConfig{}
			flagext.DefaultValues(&cfg)
			tc.mutate(&cfg)

			err := cfg.Validate()
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestClientConfig_Defaults(t *testing.T) {
	cfg := ClientConfig{}
	flagext.DefaultValues(&cfg)

	require.Equal(t, 2048, cfg.MaxInFlightRequests)
	require.Equal(t, 2, cfg.MaxRetries)
}

func Test_jumpHashShuffleSharding(t *testing.T) {

	tests := []struct {
		description string
		input       []string
		factor      float64
		expected    []string
	}{
		{
			description: "empty address list",
			input:       []string{},
			factor:      0.5,
			expected:    []string{},
		},
		{
			description: "single address",
			input:       []string{"gateway-1"},
			factor:      0.5,
			expected:    []string{"gateway-1"},
		},
		{
			description: "max capacity 1.0 returns all addresses",
			input:       []string{"gateway-1", "gateway-2", "gateway-3"},
			factor:      1.0,
			expected:    []string{"gateway-1", "gateway-2", "gateway-3"},
		},
		{
			description: "max capacity 0.0 returns all addresses",
			input:       []string{"gateway-1", "gateway-2", "gateway-3"},
			factor:      0.0,
			expected:    []string{"gateway-1", "gateway-2", "gateway-3"},
		},
		{
			description: "max capacity rounds up",
			input:       []string{"gateway-1", "gateway-2", "gateway-3"},
			factor:      0.5,
			expected:    []string{"gateway-2", "gateway-3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			mockLimits := &mockLimits{maxCapacity: tt.factor}
			// MinShuffleShardSize=0 so the floor does not interfere with these cases
			client := &GatewayClient{limits: mockLimits, cfg: ClientConfig{MinShuffleShardSize: 0}}

			result := client.jumpHashShuffleSharding("tenant1", tt.input)
			require.Equal(t, tt.expected, result)
		})
	}

	t.Run("min shard size", func(t *testing.T) {
		tenGateways := func() []string {
			addrs := make([]string, 10)
			for i := range addrs {
				addrs[i] = fmt.Sprintf("gateway-%d", i)
			}
			return addrs
		}()

		tests := []struct {
			description  string
			addrs        []string
			factor       float64
			minShardSize int
			expectedLen  int
		}{
			{
				description:  "floors the computed size",
				addrs:        tenGateways,
				factor:       0.1, // ceil(10*0.1)=1, raised to min=3
				minShardSize: 3,
				expectedLen:  3,
			},
			{
				description:  "does not reduce a larger computed size",
				addrs:        tenGateways,
				factor:       0.5, // ceil(10*0.5)=5, already > min=3
				minShardSize: 3,
				expectedLen:  5,
			},
			{
				description:  "0 disables the floor",
				addrs:        tenGateways,
				factor:       0.1, // ceil(10*0.1)=1, min=0 leaves it at 1
				minShardSize: 0,
				expectedLen:  1,
			},
			{
				description:  "capped at total available gateways",
				addrs:        []string{"gateway-0", "gateway-1", "gateway-2", "gateway-3"},
				factor:       0.1, // ceil(4*0.1)=1, min=10 exceeds total so returns all 4
				minShardSize: 10,
				expectedLen:  4,
			},
		}

		for _, tt := range tests {
			t.Run(tt.description, func(t *testing.T) {
				client := &GatewayClient{
					limits: &mockLimits{maxCapacity: tt.factor},
					cfg:    ClientConfig{MinShuffleShardSize: tt.minShardSize},
				}
				result := client.jumpHashShuffleSharding("tenant1", tt.addrs)
				require.Len(t, result, tt.expectedLen)
			})
		}
	})

	t.Run("same tenant gets same subset", func(t *testing.T) {
		mockLimits := &mockLimits{maxCapacity: 0.5}
		client := &GatewayClient{limits: mockLimits, cfg: ClientConfig{MinShuffleShardSize: 0}}

		addrs := []string{"gateway-1", "gateway-2", "gateway-3"}

		// Call multiple times with the same tenant
		result1 := client.jumpHashShuffleSharding("tenant1", addrs)
		result2 := client.jumpHashShuffleSharding("tenant1", addrs)
		result3 := client.jumpHashShuffleSharding("tenant1", addrs)

		require.Equal(t, result1, result2)
		require.Equal(t, result2, result3)
	})

	t.Run("different tenants get different subsets", func(t *testing.T) {
		mockLimits := &mockLimits{maxCapacity: 0.3}
		client := &GatewayClient{limits: mockLimits, cfg: ClientConfig{MinShuffleShardSize: 0}}

		addrs := make([]string, 9)
		for i := range len(addrs) {
			addrs[i] = fmt.Sprintf("gateway-%d", i)
		}

		result1 := client.jumpHashShuffleSharding("tenant1", addrs)
		result2 := client.jumpHashShuffleSharding("tenant2", addrs)
		result3 := client.jumpHashShuffleSharding("tenant3", addrs)

		require.Equal(t, []string{"gateway-3", "gateway-4", "gateway-5"}, result1)
		require.Equal(t, []string{"gateway-5", "gateway-6", "gateway-7"}, result2)
		require.Equal(t, []string{"gateway-7", "gateway-8", "gateway-0"}, result3)
	})

}

func Test_addressesForQueryEndTime(t *testing.T) {
	// Use the current time as reference and create relative times
	now := time.Date(2025, time.September, 11, 0, 0, 0, 0, time.UTC)

	t.Run("empty bucket list", func(t *testing.T) {
		addrs := []string{"127.0.0.1", "127.0.0.2"}
		buckets := []time.Duration{}

		tests := []struct {
			name string
			t    time.Time
			want []string
		}{
			{
				name: "any timestamp",
				t:    now.Add(-300 * time.Hour),
				want: []string{"127.0.0.1", "127.0.0.2"},
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got := addressesForQueryEndTime(addrs, tt.t, buckets, now)
				require.Equal(t, tt.want, got)
			})
		}
	})

	t.Run("empty address list", func(t *testing.T) {
		addrs := []string{}
		buckets := []time.Duration{-168 * time.Hour, -336 * time.Hour, -504 * time.Hour}

		tests := []struct {
			name string
			t    time.Time
			want []string
		}{
			{
				name: "first bucket",
				t:    now.Add(-1 * time.Hour),
				want: []string{},
			},
			{
				name: "third bucket",
				t:    now.Add(-400 * time.Hour),
				want: []string{},
			},
			{
				name: "inf bucket",
				t:    now.Add(-600 * time.Hour),
				want: []string{},
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got := addressesForQueryEndTime(addrs, tt.t, buckets, now)
				require.Equal(t, tt.want, got)
			})
		}
	})

	t.Run("address list smaller than pow(2, len(buckets))", func(t *testing.T) {
		addrs := []string{"127.0.0.1", "127.0.0.2"}
		buckets := []time.Duration{-168 * time.Hour, -336 * time.Hour, -504 * time.Hour}

		tests := []struct {
			name string
			t    time.Time
			want []string
		}{
			{
				name: "first bucket",
				t:    now.Add(-1 * time.Hour),
				want: []string{"127.0.0.1", "127.0.0.2"},
			},
			{
				name: "third bucket",
				t:    now.Add(-400 * time.Hour),
				want: []string{"127.0.0.1", "127.0.0.2"},
			},
			{
				name: "inf bucket",
				t:    now.Add(-600 * time.Hour),
				want: []string{"127.0.0.1", "127.0.0.2"},
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got := addressesForQueryEndTime(addrs, tt.t, buckets, now)
				require.Equal(t, tt.want, got)
			})
		}
	})

	t.Run("address list equal to pow(2, len(buckets))", func(t *testing.T) {
		addrs := []string{"127.0.0.1", "127.0.0.2", "127.0.0.3", "127.0.0.4", "127.0.0.5", "127.0.0.6", "127.0.0.7", "127.0.0.8"}
		buckets := []time.Duration{-168 * time.Hour, -336 * time.Hour, -504 * time.Hour}

		tests := []struct {
			name string
			t    time.Time
			want []string
		}{
			{
				name: "first bucket",
				t:    now.Add(-1 * time.Hour),
				want: []string{"127.0.0.1", "127.0.0.2", "127.0.0.3", "127.0.0.4"},
			},
			{
				name: "second bucket",
				t:    now.Add(-335 * time.Hour),
				want: []string{"127.0.0.5", "127.0.0.6"},
			},
			{
				name: "third bucket",
				t:    now.Add(-400 * time.Hour),
				want: []string{"127.0.0.7"},
			},
			{
				name: "inf bucket",
				t:    now.Add(-600 * time.Hour),
				want: []string{"127.0.0.8"},
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got := addressesForQueryEndTime(addrs, tt.t, buckets, now)
				require.Equal(t, tt.want, got)
			})
		}
	})

	t.Run("address list greather than pow(2, len(buckets))", func(t *testing.T) {
		addrs := []string{"127.0.0.1", "127.0.0.2", "127.0.0.3", "127.0.0.4", "127.0.0.5", "127.0.0.6", "127.0.0.7", "127.0.0.8", "127.0.0.9", "127.0.0.10", "127.0.0.11"}
		buckets := []time.Duration{-168 * time.Hour, -336 * time.Hour, -504 * time.Hour}

		tests := []struct {
			name string
			t    time.Time
			want []string
		}{
			{
				name: "first bucket",
				t:    now.Add(-1 * time.Hour),
				want: []string{"127.0.0.1", "127.0.0.2", "127.0.0.3", "127.0.0.4", "127.0.0.5"},
			},
			{
				name: "second bucket",
				t:    now.Add(-335 * time.Hour),
				want: []string{"127.0.0.6", "127.0.0.7"},
			},
			{
				name: "third bucket",
				t:    now.Add(-400 * time.Hour),
				want: []string{"127.0.0.8"},
			},
			{
				name: "inf bucket",
				t:    now.Add(-600 * time.Hour),
				want: []string{"127.0.0.9", "127.0.0.10", "127.0.0.11"},
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got := addressesForQueryEndTime(addrs, tt.t, buckets, now)
				require.Equal(t, tt.want, got)
			})
		}
	})
}
