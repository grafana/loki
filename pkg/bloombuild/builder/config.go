package builder

import (
	"flag"
	"fmt"

	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/grpcclient"
)

// Config configures the bloom-builder component.
type Config struct {
	GrpcConfig     grpcclient.Config `yaml:"grpc_config"`
	PlannerAddress string            `yaml:"planner_address"`
	BackoffConfig  backoff.Config    `yaml:"backoff_config"`
	WorkingDir     string            `yaml:"working_directory" doc:"hidden"`
}

// RegisterFlagsWithPrefix registers flags for the bloom-planner configuration.
func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&cfg.PlannerAddress, prefix+".planner-address", "", "Hostname (and port) of the bloom planner")
	cfg.GrpcConfig.RegisterFlagsWithPrefix(prefix+".grpc", f)
	cfg.BackoffConfig.RegisterFlagsWithPrefix(prefix+".backoff", f)
	f.StringVar(&cfg.WorkingDir, prefix+".working-directory", "", "Working directory to which blocks are temporarily written to. Empty string defaults to the operating system's temp directory.")
}

func (cfg *Config) Validate() error {
	if cfg.PlannerAddress == "" {
		return fmt.Errorf("planner address is required")
	}

	if err := cfg.GrpcConfig.Validate(); err != nil {
		return fmt.Errorf("grpc config is invalid: %w", err)
	}

	return nil
}

type Limits interface {
	BloomBlockEncoding(tenantID string) string
	BloomNGramLength(tenantID string) int
	BloomNGramSkip(tenantID string) int
	BloomMaxBlockSize(tenantID string) int
	BloomMaxBloomSize(tenantID string) int
}
