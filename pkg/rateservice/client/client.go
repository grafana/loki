package client

import (
	"flag"
	"io"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"

	"github.com/grafana/dskit/grpcclient"
	"github.com/grafana/loki/v3/pkg/rateservice/proto"
)

type Config struct {
	Address                      string                         `yaml:"address"`
	GRPCClientConfig             grpcclient.Config              `yaml:"grpc_client_config" doc:"description=Configures client gRPC connections to limits service."`
	GRPCUnaryClientInterceptors  []grpc.UnaryClientInterceptor  `yaml:"-"`
	GRCPStreamClientInterceptors []grpc.StreamClientInterceptor `yaml:"-"`

	// Internal is used to indicate that this client communicates on behalf of
	// a machine and not a user. When Internal = true, the client won't attempt
	// to inject an userid into the context.
	Internal bool `yaml:"-"`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("rate-service-client", f)
}

func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	cfg.GRPCClientConfig.RegisterFlagsWithPrefix(prefix, f)
}

type Client struct {
	proto.RateServiceClient
	grpc_health_v1.HealthClient
	io.Closer
}

func NewClient(cfg Config) (*Client, error) {
	var opts []grpc.DialOption
	opts = append(opts, grpc.WithDefaultCallOptions(cfg.GRPCClientConfig.CallOptions()...))
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
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
