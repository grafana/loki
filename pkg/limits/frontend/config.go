package frontend

import (
	"flag"

	"github.com/grafana/dskit/grpcclient"
)

type Config struct {
	GRPCClientConfig grpcclient.Config `yaml:"grpc_client_config"`
}

func (cfg Config) RegisterFlags(f *flag.FlagSet) {
	cfg.GRPCClientConfig.RegisterFlagsWithPrefix("limits-frontend.grpc-client-config", f)
}

func (cfg Config) Validate() error {
	return nil
}
