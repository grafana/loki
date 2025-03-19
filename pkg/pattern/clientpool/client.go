package clientpool

import (
	"flag"
	"io"
	"time"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util/server"

	"github.com/grafana/dskit/grpcclient"
	"github.com/grafana/dskit/middleware"
	"github.com/grpc-ecosystem/grpc-opentracing/go/otgrpc"
	"github.com/opentracing/opentracing-go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"
)

var ingesterClientRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "loki_pattern_ingester_client_request_duration_seconds",
	Help:    "Time spent doing pattern Ingester requests.",
	Buckets: prometheus.ExponentialBuckets(0.001, 4, 6),
}, []string{"operation", "status_code"})

type HealthAndIngesterClient interface {
	grpc_health_v1.HealthClient
	Close() error
}

type ClosableHealthAndIngesterClient struct {
	logproto.PatternClient
	grpc_health_v1.HealthClient
	io.Closer
}

// Config for an ingester client.
type Config struct {
	PoolConfig                   PoolConfig                     `yaml:"pool_config,omitempty" doc:"description=Configures how connections are pooled."`
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
	cfg.GRPCClientConfig.RegisterFlagsWithPrefix("pattern-ingester.client", f)
	cfg.PoolConfig.RegisterFlagsWithPrefix("pattern-ingester.", f)

	f.DurationVar(&cfg.PoolConfig.RemoteTimeout, "pattern-ingester.client.healthcheck-timeout", 1*time.Second, "How quickly a dead client will be removed after it has been detected to disappear. Set this to a value to allow time for a secondary health check to recover the missing client.")
	f.DurationVar(&cfg.RemoteTimeout, "pattern-ingester.client.timeout", 5*time.Second, "The remote request timeout on the client side.")
}

// New returns a new ingester client.
func NewClient(cfg Config, addr string) (HealthAndIngesterClient, error) {
	opts := []grpc.DialOption{
		grpc.WithDefaultCallOptions(cfg.GRPCClientConfig.CallOptions()...),
	}
	unaryInterceptors, streamInterceptors := instrumentation(&cfg)
	dialOpts, err := cfg.GRPCClientConfig.DialOption(unaryInterceptors, streamInterceptors, middleware.NoOpInvalidClusterValidationReporter)
	if err != nil {
		return nil, err
	}

	opts = append(opts, dialOpts...)

	// nolint:staticcheck // grpc.Dial() has been deprecated; we'll address it before upgrading to gRPC 2.
	conn, err := grpc.Dial(addr, opts...)
	if err != nil {
		return nil, err
	}
	return ClosableHealthAndIngesterClient{
		PatternClient: logproto.NewPatternClient(conn),
		HealthClient:  grpc_health_v1.NewHealthClient(conn),
		Closer:        conn,
	}, nil
}

func instrumentation(cfg *Config) ([]grpc.UnaryClientInterceptor, []grpc.StreamClientInterceptor) {
	var unaryInterceptors []grpc.UnaryClientInterceptor
	unaryInterceptors = append(unaryInterceptors, cfg.GRPCUnaryClientInterceptors...)
	unaryInterceptors = append(unaryInterceptors, server.UnaryClientQueryTagsInterceptor)
	unaryInterceptors = append(unaryInterceptors, otgrpc.OpenTracingClientInterceptor(opentracing.GlobalTracer()))
	if !cfg.Internal {
		unaryInterceptors = append(unaryInterceptors, middleware.ClientUserHeaderInterceptor)
	}
	unaryInterceptors = append(unaryInterceptors, middleware.UnaryClientInstrumentInterceptor(ingesterClientRequestDuration))

	var streamInterceptors []grpc.StreamClientInterceptor
	streamInterceptors = append(streamInterceptors, cfg.GRCPStreamClientInterceptors...)
	streamInterceptors = append(streamInterceptors, server.StreamClientQueryTagsInterceptor)
	streamInterceptors = append(streamInterceptors, otgrpc.OpenTracingStreamClientInterceptor(opentracing.GlobalTracer()))
	if !cfg.Internal {
		streamInterceptors = append(streamInterceptors, middleware.StreamClientUserHeaderInterceptor)
	}
	streamInterceptors = append(streamInterceptors, middleware.StreamClientInstrumentInterceptor(ingesterClientRequestDuration))

	return unaryInterceptors, streamInterceptors
}
