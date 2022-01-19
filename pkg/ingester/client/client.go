package client

import (
	"flag"
	"io"
	"time"

	"github.com/grafana/dskit/grpcclient"
	dsmiddleware "github.com/grafana/dskit/middleware"
	"github.com/grpc-ecosystem/grpc-opentracing/go/otgrpc"
	"github.com/opentracing/opentracing-go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/weaveworks/common/middleware"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/loki/pkg/distributor/clientpool"
	"github.com/grafana/loki/pkg/logproto"
)

var ingesterClientRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "loki_ingester_client_request_duration_seconds",
	Help:    "Time spent doing Ingester requests.",
	Buckets: prometheus.ExponentialBuckets(0.001, 4, 6),
}, []string{"operation", "status_code"})

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
	PoolConfig                   clientpool.PoolConfig          `yaml:"pool_config,omitempty"`
	RemoteTimeout                time.Duration                  `yaml:"remote_timeout,omitempty"`
	GRPCClientConfig             grpcclient.Config              `yaml:"grpc_client_config"`
	GRPCUnaryClientInterceptors  []grpc.UnaryClientInterceptor  `yaml:"-"`
	GRCPStreamClientInterceptors []grpc.StreamClientInterceptor `yaml:"-"`
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
		grpc.WithDefaultCallOptions(cfg.GRPCClientConfig.CallOptions()...),
	}

	dialOpts, err := cfg.GRPCClientConfig.DialOption(instrumentation(&cfg))
	if err != nil {
		return nil, err
	}

	opts = append(opts, dialOpts...)
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

func instrumentation(cfg *Config) ([]grpc.UnaryClientInterceptor, []grpc.StreamClientInterceptor) {
	var unaryInterceptors []grpc.UnaryClientInterceptor
	unaryInterceptors = append(unaryInterceptors, cfg.GRPCUnaryClientInterceptors...)
	unaryInterceptors = append(unaryInterceptors,
		otgrpc.OpenTracingClientInterceptor(opentracing.GlobalTracer()),
		middleware.ClientUserHeaderInterceptor,
		dsmiddleware.PrometheusGRPCUnaryInstrumentation(ingesterClientRequestDuration),
	)
	var streamInterceptors []grpc.StreamClientInterceptor
	streamInterceptors = append(streamInterceptors, cfg.GRCPStreamClientInterceptors...)
	streamInterceptors = append(streamInterceptors,
		otgrpc.OpenTracingStreamClientInterceptor(opentracing.GlobalTracer()),
		middleware.StreamClientUserHeaderInterceptor,
		dsmiddleware.PrometheusGRPCStreamInstrumentation(ingesterClientRequestDuration),
	)

	return unaryInterceptors, streamInterceptors
}
