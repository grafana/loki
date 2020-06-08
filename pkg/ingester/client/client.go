package client

import (
	"flag"
	"io"
	"time"

	"github.com/cortexproject/cortex/pkg/distributor"
	"github.com/cortexproject/cortex/pkg/util/grpcclient"
	"github.com/grpc-ecosystem/grpc-opentracing/go/otgrpc"
	"github.com/opentracing/opentracing-go"
	"github.com/weaveworks/common/middleware"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/loki/pkg/logproto"
)

type HealthAndIngesterClient interface {
	logproto.IngesterClient
	grpc_health_v1.HealthClient
	Close() error
}

type ClosableHealthAndIngesterClient struct {
	logproto.PusherClient
	logproto.QuerierClient
	logproto.IngesterClient
	grpc_health_v1.HealthClient
	io.Closer
}

// Config for an ingester client.
type Config struct {
	PoolConfig       distributor.PoolConfig `yaml:"pool_config,omitempty"`
	RemoteTimeout    time.Duration          `yaml:"remote_timeout,omitempty"`
	GRPCClientConfig grpcclient.Config      `yaml:"grpc_client_config"`
}

// RegisterFlags registers flags.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.GRPCClientConfig.RegisterFlagsWithPrefix("ingester.client", f)
	cfg.PoolConfig.RegisterFlags(f)

	f.DurationVar(&cfg.PoolConfig.RemoteTimeout, "ingester.client.healthcheck-timeout", 1*time.Second, "Timeout for healthcheck rpcs.")
	f.DurationVar(&cfg.RemoteTimeout, "ingester.client.timeout", 5*time.Second, "Timeout for ingester client RPCs.")
}

// New returns a new ingester client.
func New(cfg Config, addr string) (HealthAndIngesterClient, error) {
	opts := []grpc.DialOption{
		grpc.WithInsecure(),
		grpc.WithDefaultCallOptions(cfg.GRPCClientConfig.CallOptions()...),
	}
	opts = append(opts, cfg.GRPCClientConfig.DialOption(instrumentation())...)
	conn, err := grpc.Dial(addr, opts...)
	if err != nil {
		return nil, err
	}
	return ClosableHealthAndIngesterClient{
		PusherClient:   logproto.NewPusherClient(conn),
		QuerierClient:  logproto.NewQuerierClient(conn),
		IngesterClient: logproto.NewIngesterClient(conn),
		HealthClient:   grpc_health_v1.NewHealthClient(conn),
		Closer:         conn,
	}, nil
}

func instrumentation() ([]grpc.UnaryClientInterceptor, []grpc.StreamClientInterceptor) {
	return []grpc.UnaryClientInterceptor{
			otgrpc.OpenTracingClientInterceptor(opentracing.GlobalTracer()),
			middleware.ClientUserHeaderInterceptor,
		}, []grpc.StreamClientInterceptor{
			otgrpc.OpenTracingStreamClientInterceptor(opentracing.GlobalTracer()),
			middleware.StreamClientUserHeaderInterceptor,
		}
}
