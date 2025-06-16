// Package client provides gRPC client implementation for distributor service.
package client

import (
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/grafana/dskit/grpcclient"
	"github.com/grafana/dskit/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util/server"
)

const (
	GRPCLoadBalancingPolicyRoundRobin = "round_robin"

	grpcServiceConfigTemplate = `{"loadBalancingPolicy":"%s"}`
)

var (
	requestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "loki_distributor_client_request_duration_seconds",
		Help:    "Time spent doing distributor requests.",
		Buckets: prometheus.ExponentialBuckets(0.001, 4, 6),
	}, []string{"operation", "status_code"})
)

// Config contains the config for an ingest-limits client.
type Config struct {
	Addr                         string                         `yaml:"addr"`
	GRPCClientConfig             grpcclient.Config              `yaml:"grpc_client_config"`
	GRPCUnaryClientInterceptors  []grpc.UnaryClientInterceptor  `yaml:"-"`
	GRCPStreamClientInterceptors []grpc.StreamClientInterceptor `yaml:"-"`

	// Internal is used to indicate that this client communicates on behalf of
	// a machine and not a user. When Internal = true, the client won't attempt
	// to inject an userid into the context.
	Internal bool `yaml:"-"`
}

func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&cfg.Addr, prefix+".addr", "", "The address of the distributor. Preferred 'dns:///distributor.namespace.svc.cluster.local:3100'")
	cfg.GRPCClientConfig.RegisterFlagsWithPrefix(prefix, f)
}

// Client is a gRPC client for the distributor.
type Client struct {
	logproto.PusherClient
	grpc_health_v1.HealthClient
	io.Closer
}

// New returns a new Client for the specified ingest-limits.
func New(cfg Config) (*Client, error) {
	opts := []grpc.DialOption{
		grpc.WithDefaultCallOptions(cfg.GRPCClientConfig.CallOptions()...),
	}
	unaryInterceptors, streamInterceptors := getGRPCInterceptors(&cfg)
	dialOpts, err := cfg.GRPCClientConfig.DialOption(unaryInterceptors, streamInterceptors, middleware.NoOpInvalidClusterValidationReporter)
	if err != nil {
		return nil, err
	}
	dialOpts = append(dialOpts, grpc.WithStatsHandler(otelgrpc.NewClientHandler()))

	serviceConfig := fmt.Sprintf(grpcServiceConfigTemplate, GRPCLoadBalancingPolicyRoundRobin)

	dialOpts = append(dialOpts,
		grpc.WithBlock(), // nolint:staticcheck // grpc.WithBlock() has been deprecated; we'll address it before upgrading to gRPC 2
		grpc.WithKeepaliveParams(
			keepalive.ClientParameters{
				Time:                time.Second * 10,
				Timeout:             time.Second * 5,
				PermitWithoutStream: true,
			},
		),
		grpc.WithDefaultServiceConfig(serviceConfig),
	)

	opts = append(opts, dialOpts...)
	// nolint:staticcheck // grpc.Dial() has been deprecated; we'll address it before upgrading to gRPC 2.
	conn, err := grpc.Dial(cfg.Addr, opts...)
	if err != nil {
		return nil, err
	}
	return &Client{
		PusherClient: logproto.NewPusherClient(conn),
		HealthClient: grpc_health_v1.NewHealthClient(conn),
		Closer:       conn,
	}, nil
}

// getInterceptors returns the gRPC interceptors for the given ClientConfig.
func getGRPCInterceptors(cfg *Config) ([]grpc.UnaryClientInterceptor, []grpc.StreamClientInterceptor) {
	var (
		unaryInterceptors  []grpc.UnaryClientInterceptor
		streamInterceptors []grpc.StreamClientInterceptor
	)

	unaryInterceptors = append(unaryInterceptors, cfg.GRPCUnaryClientInterceptors...)
	unaryInterceptors = append(unaryInterceptors, server.UnaryClientQueryTagsInterceptor)
	unaryInterceptors = append(unaryInterceptors, server.UnaryClientHTTPHeadersInterceptor)
	if !cfg.Internal {
		unaryInterceptors = append(unaryInterceptors, middleware.ClientUserHeaderInterceptor)
	}
	unaryInterceptors = append(unaryInterceptors, middleware.UnaryClientInstrumentInterceptor(requestDuration))

	streamInterceptors = append(streamInterceptors, cfg.GRCPStreamClientInterceptors...)
	streamInterceptors = append(streamInterceptors, server.StreamClientQueryTagsInterceptor)
	streamInterceptors = append(streamInterceptors, server.StreamClientHTTPHeadersInterceptor)
	if !cfg.Internal {
		streamInterceptors = append(streamInterceptors, middleware.StreamClientUserHeaderInterceptor)
	}
	streamInterceptors = append(streamInterceptors, middleware.StreamClientInstrumentInterceptor(requestDuration))

	return unaryInterceptors, streamInterceptors
}
