package frontend

import (
	"flag"
	"fmt"

	"github.com/grafana/dskit/ring"

	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

type Config struct {
	ClientConfig     BackendClientConfig   `yaml:"client_config"`
	LifecyclerConfig ring.LifecyclerConfig `yaml:"lifecycler,omitempty"`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.ClientConfig.RegisterFlagsWithPrefix("ingest-limits-frontend", f)
	cfg.LifecyclerConfig.RegisterFlagsWithPrefix("ingest-limits-frontend.", f, util_log.Logger)
}

func (cfg *Config) Validate() error {
	if err := cfg.ClientConfig.GRPCClientConfig.Validate(); err != nil {
		return fmt.Errorf("invalid gRPC client config: %w", err)
	}
	return nil
}
