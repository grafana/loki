package frontend

import (
	"flag"
	"fmt"
)

type Config struct {
	ClientConfig BackendClientConfig `yaml:"client_config"`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.ClientConfig.RegisterFlagsWithPrefix("ingest-limits-frontend", f)
}

func (cfg *Config) Validate() error {
	if err := cfg.ClientConfig.GRPCClientConfig.Validate(); err != nil {
		return fmt.Errorf("invalid gRPC client config: %w", err)
	}
	return nil
}
