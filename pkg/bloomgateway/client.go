package bloomgateway

import (
	"context"
	"flag"
	"io"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/grpcclient"
	"github.com/grafana/dskit/instrument"
	"github.com/grafana/dskit/ring"
	ringclient "github.com/grafana/dskit/ring/client"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/loki/pkg/distributor/clientpool"
	"github.com/grafana/loki/pkg/logproto"
)

// BloomGatewayGRPCPool represents a pool of gRPC connections to different index gateway instances.
type BloomGatewayGRPCPool struct {
	grpc_health_v1.HealthClient
	logproto.BloomGatewayClient
	io.Closer
}

// NewBloomGatewayGRPCPool instantiates a new pool of GRPC connections for the Bloom Gateway
// Internally, it also instantiates a protobuf bloom gateway client and a health client.
func NewBloomGatewayGRPCPool(address string, opts []grpc.DialOption) (*BloomGatewayGRPCPool, error) {
	conn, err := grpc.Dial(address, opts...)
	if err != nil {
		return nil, errors.Wrap(err, "new grpc pool dial")
	}

	return &BloomGatewayGRPCPool{
		Closer:             conn,
		HealthClient:       grpc_health_v1.NewHealthClient(conn),
		BloomGatewayClient: logproto.NewBloomGatewayClient(conn),
	}, nil
}

// IndexGatewayClientConfig configures the Index Gateway client used to
// communicate with the Index Gateway server.
type ClientConfig struct {
	// PoolConfig defines the behavior of the gRPC connection pool used to communicate
	// with the Bloom Gateway.
	// It is defined at the distributors YAML section and reused here.
	PoolConfig clientpool.PoolConfig `yaml:"-"`

	// GRPCClientConfig configures the gRPC connection between the Bloom Gateway client and the server.
	GRPCClientConfig grpcclient.Config `yaml:"grpc_client_config"`

	// LogGatewayRequests configures if requests sent to the gateway should be logged or not.
	// The log messages are of type debug and contain the address of the gateway and the relevant tenant.
	LogGatewayRequests bool `yaml:"log_gateway_requests"`

	// Ring is the Bloom Gateway ring used to find the appropriate Bloom Gateway instance
	// this client should talk to.
	Ring ring.ReadRing `yaml:"-"`
}

// RegisterFlags registers flags for the Bloom Gateway client configuration.
func (i *ClientConfig) RegisterFlags(f *flag.FlagSet) {
	i.RegisterFlagsWithPrefix("bloom-gateway-client.", f)
}

// RegisterFlagsWithPrefix registers flags for the Bloom Gateway client configuration with a common prefix.
func (i *ClientConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	i.GRPCClientConfig.RegisterFlagsWithPrefix(prefix+"grpc", f)
	f.BoolVar(&i.LogGatewayRequests, prefix+"log-gateway-requests", false, "Flag to control whether requests sent to the gateway should be logged or not.")
}

type GatwayClient struct {
	cfg    ClientConfig
	limits Limits
	logger log.Logger
	pool   *ringclient.Pool
	ring   ring.ReadRing
}

func NewGatewayClient(cfg ClientConfig, limits Limits, registerer prometheus.Registerer, logger log.Logger) (*GatwayClient, error) {
	latency := promauto.With(registerer).NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "loki",
		Name:      "bloom_gateway_request_duration_seconds",
		Help:      "Time (in seconds) spent serving requests when using the bloom gateway",
		Buckets:   instrument.DefBuckets,
	}, []string{"operation", "status_code"})

	dialOpts, err := cfg.GRPCClientConfig.DialOption(grpcclient.Instrument(latency))
	if err != nil {
		return nil, err
	}

	poolFactory := func(addr string) (ringclient.PoolClient, error) {
		pool, err := NewBloomGatewayGRPCPool(addr, dialOpts)
		if err != nil {
			return nil, errors.Wrap(err, "new bloom gateway grpc pool")
		}
		return pool, nil
	}

	c := &GatwayClient{
		cfg:    cfg,
		logger: logger,
		limits: limits,
		pool:   clientpool.NewPool(cfg.PoolConfig, cfg.Ring, poolFactory, logger),
	}

	return c, nil
}

// FilterChunkRefs implements Client
func (c *GatwayClient) FilterChunkRefs(ctx context.Context, streams []uint64, chunkRefs [][]logproto.ChunkRef) ([]uint64, [][]logproto.ChunkRef, error) {
	return nil, nil, nil
}
