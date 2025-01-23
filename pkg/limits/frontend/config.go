package frontend

import (
	"flag"
	"fmt"
)

type Config struct {
	ClientConfig ClientConfig `yaml:"client_config"`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.ClientConfig.RegisterFlagsWithPrefix("ingest-limits-frontend", f)
}

func (cfg *Config) Validate() error {
	if err := cfg.ClientConfig.GRPCClientConfig.Validate(); err != nil {
		return fmt.Errorf("client grpc config is invalid: %w", err)
	}

	return nil
}
