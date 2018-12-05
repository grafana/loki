package client

import (
	"flag"
	"io"
	"time"

	cortex_client "github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grpc-ecosystem/grpc-opentracing/go/otgrpc"
	"github.com/mwitkow/go-grpc-middleware"
	opentracing "github.com/opentracing/opentracing-go"
	"github.com/weaveworks/common/middleware"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"
)

// Config for an ingester client.
type Config struct {
	PoolConfig     cortex_client.PoolConfig `yaml:"pool_config,omitempty"`
	MaxRecvMsgSize int                      `yaml:"max_recv_msg_size,omitempty"`
	RemoteTimeout  time.Duration            `yaml:"remote_timeout,omitempty"`
}

// RegisterFlags registers flags.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.PoolConfig.RegisterFlags(f)

	f.IntVar(&cfg.MaxRecvMsgSize, "ingester.client.max-recv-message-size", 64*1024*1024, "Maximum message size, in bytes, this client will receive.")
	f.DurationVar(&cfg.PoolConfig.RemoteTimeout, "ingester.client.healthcheck-timeout", 1*time.Second, "Timeout for healthcheck rpcs.")
	f.DurationVar(&cfg.RemoteTimeout, "ingester.client.timeout", 5*time.Second, "Timeout for ingester client RPCs.")
}

// New returns a new ingester client.
func New(cfg Config, addr string) (grpc_health_v1.HealthClient, error) {
	opts := []grpc.DialOption{
		grpc.WithInsecure(),
		grpc.WithUnaryInterceptor(grpc_middleware.ChainUnaryClient(
			otgrpc.OpenTracingClientInterceptor(opentracing.GlobalTracer()),
			middleware.ClientUserHeaderInterceptor,
		)),
		grpc.WithStreamInterceptor(grpc_middleware.ChainStreamClient(
			otgrpc.OpenTracingStreamClientInterceptor(opentracing.GlobalTracer()),
			middleware.StreamClientUserHeaderInterceptor,
		)),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(cfg.MaxRecvMsgSize),
			grpc.UseCompressor("gzip"),
		),
	}
	conn, err := grpc.Dial(addr, opts...)
	if err != nil {
		return nil, err
	}
	return struct {
		logproto.PusherClient
		logproto.QuerierClient
		grpc_health_v1.HealthClient
		io.Closer
	}{
		PusherClient:  logproto.NewPusherClient(conn),
		QuerierClient: logproto.NewQuerierClient(conn),
		Closer:        conn,
	}, nil
}
