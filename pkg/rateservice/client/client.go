package client

import (
	"flag"
	"io"

	"github.com/grafana/dskit/grpcclient"
	"github.com/grafana/dskit/middleware"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/loki/v3/pkg/rateservice/proto"
)

type Config struct {
	Address          string            `yaml:"address"`
	GRPCClientConfig grpcclient.Config `yaml:"grpc_client_config" doc:"description=Configures client gRPC connections to limits service."`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("rate-service-client", f)
}

func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	cfg.GRPCClientConfig.RegisterFlagsWithPrefix(prefix, f)
	f.StringVar(&cfg.Address, prefix+".address", "", "The address of the rate service.")
}

type Client struct {
	proto.RateServiceClient
	grpc_health_v1.HealthClient
	io.Closer
}

func NewClient(cfg Config) (*Client, error) {
	var opts []grpc.DialOption
	opts = append(opts, grpc.WithDefaultCallOptions(cfg.GRPCClientConfig.CallOptions()...))
	dialOpts, err := cfg.GRPCClientConfig.DialOption(nil, nil, middleware.NoOpInvalidClusterValidationReporter)
	if err != nil {
		return nil, err
	}
	opts = append(opts, dialOpts...)
	conn, err := grpc.NewClient(cfg.Address, opts...)
	if err != nil {
		return nil, err
	}
	return &Client{
		RateServiceClient: proto.NewRateServiceClient(conn),
		HealthClient:      grpc_health_v1.NewHealthClient(conn),
		Closer:            conn,
	}, nil
}
