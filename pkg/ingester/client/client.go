package client

import (
	"flag"
	"io"
	"time"

	cortex_client "github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/util/grpcclient"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grpc-ecosystem/grpc-opentracing/go/otgrpc"
	opentracing "github.com/opentracing/opentracing-go"
	"github.com/weaveworks/common/middleware"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"
)

// Config for an ingester client.
type Config struct {
	PoolConfig       cortex_client.PoolConfig `yaml:"pool_config,omitempty"`
	RemoteTimeout    time.Duration            `yaml:"remote_timeout,omitempty"`
	GRPCClientConfig grpcclient.Config        `yaml:"grpc_client_config"`
}

// RegisterFlags registers flags.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.GRPCClientConfig.RegisterFlags("ingester.client", f)
	cfg.PoolConfig.RegisterFlags(f)

	f.DurationVar(&cfg.PoolConfig.RemoteTimeout, "ingester.client.healthcheck-timeout", 1*time.Second, "Timeout for healthcheck rpcs.")
	f.DurationVar(&cfg.RemoteTimeout, "ingester.client.timeout", 5*time.Second, "Timeout for ingester client RPCs.")
}

// New returns a new ingester client.
func New(cfg Config, addr string) (grpc_health_v1.HealthClient, error) {
	opts := []grpc.DialOption{
		grpc.WithInsecure(),
		grpc.WithDefaultCallOptions(
			grpc.UseCompressor("gzip"),
		),
	}
	opts = append(opts, cfg.GRPCClientConfig.DialOption(instrumentation())...)
	conn, err := grpc.Dial(addr, opts...)
	if err != nil {
		return nil, err
	}
	return struct {
		logproto.PusherClient
		logproto.QuerierClient
		logproto.IngesterClient
		grpc_health_v1.HealthClient
		io.Closer
	}{
		PusherClient:   logproto.NewPusherClient(conn),
		QuerierClient:  logproto.NewQuerierClient(conn),
		IngesterClient: logproto.NewIngesterClient(conn),
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
