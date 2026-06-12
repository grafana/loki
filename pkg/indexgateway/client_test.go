package indexgateway

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/grpcclient"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/kv/consul"
	"github.com/grafana/dskit/ring"
	dskitclient "github.com/grafana/dskit/ring/client"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/user"
	"github.com/grafana/loki/v3/pkg/util/discovery"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/grafana/loki/v3/pkg/validation"
)

type mockDnsProvider struct {
	addrs []string
}

func newMockDnsProvider(addrs []string) discovery.DNS {
	return &mockDnsProvider{
		addrs: addrs,
	}
}

func (m *mockDnsProvider) Addresses() []string {
	return m.addrs
}

func (m *mockDnsProvider) Stop() {}

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

		c, err := NewGatewayClient(cfg, nil, o, logger, constants.Loki)
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

		c, err := NewGatewayClient(cfg, nil, o, logger, constants.Loki)
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

func createSimpleGatewayClient(t *testing.T, addrs []string) (log.Logger, *dskitclient.Pool, *GatewayClient) {
	logger := log.NewNopLogger()
	r := prometheus.NewRegistry()
	o, _ := validation.NewOverrides(validation.Limits{}, nil)
	client, err := NewGatewayClient(
		ClientConfig{
			Mode:             "simple",
			GRPCClientConfig: grpcclient.Config{},
			Address:          "1.1.1.1",
		},
		r, o, logger, constants.Loki)
	require.NoError(t, err)
	defer client.Stop()
	require.NoError(t, services.StopAndAwaitTerminated(context.Background(), client.pool))
	client.dnsProvider = newMockDnsProvider(addrs)
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

func TestGatewayClient_SimpleMode_Retries(t *testing.T) {
	logger, _, client := createSimpleGatewayClient(t, []string{"1.1.1.1", "2.2.2.2", "3.3.3.3", "4.4.4.4"})
	ctx := user.InjectOrgID(context.Background(), "tenant-123")

	// Retry up to 2 errors
	for numErrorsToReturn := 0; numErrorsToReturn <= 2; numErrorsToReturn++ {
		configurePool(t, client, logger, numErrorsToReturn)
		_, err := client.GetChunkRef(ctx, &logproto.GetChunkRefRequest{})
		require.NoError(t, err)
	}

	// Fail after 3 errors
	configurePool(t, client, logger, 3)
	_, err := client.GetChunkRef(ctx, &logproto.GetChunkRefRequest{})
	require.Error(t, err)
}

func TestGatewayClient_SimpleMode_ShuffleSharding(t *testing.T) {
	_, pool, client := createSimpleGatewayClient(t, []string{
		"0.0.0.0", "1.1.1.1", "2.2.2.2", "3.3.3.3", "4.4.4.4",
		"5.5.5.5", "6.6.6.6", "7.7.7.7", "8.8.8.8", "9.9.9.9",
	})
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

	client, err := NewGatewayClient(clientCfg, r, o, logger, constants.Loki)
	require.NoError(t, err)
	defer client.Stop()

	client, err = NewGatewayClient(clientCfg, r, o, logger, constants.Loki)
	require.NoError(t, err)
	defer client.Stop()
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
			client := &GatewayClient{limits: mockLimits}

			result := client.jumpHashShuffleSharding("tenant1", tt.input)
			require.Equal(t, tt.expected, result)
		})
	}

	t.Run("same tenant gets same subset", func(t *testing.T) {
		mockLimits := &mockLimits{maxCapacity: 0.5}
		client := &GatewayClient{limits: mockLimits}

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
		client := &GatewayClient{limits: mockLimits}

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
