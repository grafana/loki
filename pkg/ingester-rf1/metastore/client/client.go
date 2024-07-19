package metastoreclient

import (
	"flag"
	"fmt"

	"github.com/grafana/dskit/grpcclient"
	"github.com/grafana/dskit/services"
	"google.golang.org/grpc"

	metastorepb "github.com/grafana/loki/v3/pkg/ingester-rf1/metastore/metastorepb"
)

type Config struct {
	MetastoreAddress string            `yaml:"address"`
	GRPCClientConfig grpcclient.Config `yaml:"grpc_client_config" doc:"description=Configures the gRPC client used to communicate with the metastore."`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.MetastoreAddress, "metastore.address", "localhost:9095", "")
	cfg.GRPCClientConfig.RegisterFlagsWithPrefix("metastore.grpc-client-config", f)
}

func (cfg *Config) Validate() error {
	if cfg.MetastoreAddress == "" {
		return fmt.Errorf("metastore.address is required")
	}
	return cfg.GRPCClientConfig.Validate()
}

type Client struct {
	metastorepb.MetastoreServiceClient
	service services.Service
	conn    *grpc.ClientConn
	config  Config
}

func New(config Config) (c *Client, err error) {
	c = &Client{config: config}
	c.conn, err = dial(c.config)
	if err != nil {
		return nil, err
	}
	c.MetastoreServiceClient = metastorepb.NewMetastoreServiceClient(c.conn)
	c.service = services.NewIdleService(nil, c.stopping)
	return c, nil
}

func (c *Client) stopping(error) error { return c.conn.Close() }

func (c *Client) Service() services.Service { return c.service }

func dial(cfg Config) (*grpc.ClientConn, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	options, err := cfg.GRPCClientConfig.DialOption(nil, nil)
	if err != nil {
		return nil, err
	}
	// TODO: https://github.com/grpc/grpc-proto/blob/master/grpc/service_config/service_config.proto
	options = append(options, grpc.WithDefaultServiceConfig(grpcServiceConfig))
	return grpc.Dial(cfg.MetastoreAddress, options...)
}

const grpcServiceConfig = `{
	"healthCheckConfig": {
		"serviceName": "metastorepb.MetastoreService.RaftLeader"
	},
	"loadBalancingPolicy":"round_robin",
    "methodConfig": [{
        "name": [{"service": "metastorepb.MetastoreService"}],
        "waitForReady": true,
        "retryPolicy": {
            "MaxAttempts": 4,
            "InitialBackoff": ".01s",
            "MaxBackoff": ".01s",
            "BackoffMultiplier": 1.0,
            "RetryableStatusCodes": [ "UNAVAILABLE" ]
        }
    }]
}`
