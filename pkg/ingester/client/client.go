package client

import (
	"flag"
	"io"
	"time"

	"github.com/grafana/dskit/grpcclient"
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
	logproto.StreamDataClient
	grpc_health_v1.HealthClient
	io.Closer
}

// Config for an ingester client.
type Config struct {
	PoolConfig                   clientpool.PoolConfig          `yaml:"pool_config,omitempty" doc:"description=Configures how connections are pooled."`
	RemoteTimeout                time.Duration                  `yaml:"remote_timeout,omitempty"`
	GRPCClientConfig             grpcclient.Config              `yaml:"grpc_client_config" doc:"description=Configures how the gRPC connection to ingesters work as a client."`
	GRPCUnaryClientInterceptors  []grpc.UnaryClientInterceptor  `yaml:"-"`
	GRCPStreamClientInterceptors []grpc.StreamClientInterceptor `yaml:"-"`

	// Internal is used to indicate that this client communicates on behalf of
	// a machine and not a user. When Internal = true, the client won't attempt
	// to inject an userid into the context.
	Internal bool `yaml:"-"`
}

// RegisterFlags registers flags.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.GRPCClientConfig.RegisterFlagsWithPrefix("ingester.client", f)
	cfg.PoolConfig.RegisterFlags(f)

	f.DurationVar(&cfg.PoolConfig.RemoteTimeout, "ingester.client.healthcheck-timeout", 1*time.Second, "How quickly a dead client will be removed after it has been detected to disappear. Set this to a value to allow time for a secondary health check to recover the missing client.")
	f.DurationVar(&cfg.RemoteTimeout, "ingester.client.timeout", 5*time.Second, "The remote request timeout on the client side.")
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
		PusherClient:     logproto.NewPusherClient(conn),
		QuerierClient:    logproto.NewQuerierClient(conn),
		IngesterClient:   logproto.NewIngesterClient(conn),
		StreamDataClient: logproto.NewStreamDataClient(conn),
		HealthClient:     grpc_health_v1.NewHealthClient(conn),
		Closer:           conn,
	}, nil
}

func instrumentation(cfg *Config) ([]grpc.UnaryClientInterceptor, []grpc.StreamClientInterceptor) {
	var unaryInterceptors []grpc.UnaryClientInterceptor
	unaryInterceptors = append(unaryInterceptors, cfg.GRPCUnaryClientInterceptors...)
	unaryInterceptors = append(unaryInterceptors, otgrpc.OpenTracingClientInterceptor(opentracing.GlobalTracer()))
	if !cfg.Internal {
		unaryInterceptors = append(unaryInterceptors, middleware.ClientUserHeaderInterceptor)
	}
	unaryInterceptors = append(unaryInterceptors, middleware.UnaryClientInstrumentInterceptor(ingesterClientRequestDuration))

	var streamInterceptors []grpc.StreamClientInterceptor
	streamInterceptors = append(streamInterceptors, cfg.GRCPStreamClientInterceptors...)
	streamInterceptors = append(streamInterceptors, otgrpc.OpenTracingStreamClientInterceptor(opentracing.GlobalTracer()))
	if !cfg.Internal {
		streamInterceptors = append(streamInterceptors, middleware.StreamClientUserHeaderInterceptor)
	}
	streamInterceptors = append(streamInterceptors, middleware.StreamClientInstrumentInterceptor(ingesterClientRequestDuration))

	return unaryInterceptors, streamInterceptors
}
