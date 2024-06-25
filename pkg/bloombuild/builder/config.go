package builder

import (
	"flag"
	"fmt"
	"time"

	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/grpcclient"
)

// Config configures the bloom-builder component.
type Config struct {
	GrpcConfig     grpcclient.Config `yaml:"grpc_config"`
	PlannerAddress string            `yaml:"planner_address"`
	BackoffConfig  backoff.Config    `yaml:"backoff_config"`
}

// RegisterFlagsWithPrefix registers flags for the bloom-planner configuration.
func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&cfg.PlannerAddress, prefix+".planner-address", "", "Hostname (and port) of the bloom planner")
	cfg.GrpcConfig.RegisterFlagsWithPrefix(prefix+".grpc", f)

	f.DurationVar(&cfg.BackoffConfig.MinBackoff, prefix+".backoff.min-backoff", 1*time.Second, "Minimum backoff time")
	f.DurationVar(&cfg.BackoffConfig.MaxBackoff, prefix+".backoff.max-backoff", 10*time.Second, "Maximum backoff time")
	f.IntVar(&cfg.BackoffConfig.MaxRetries, prefix+".backoff.max-retries", 5, "Maximum number of times to retry an operation")
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
	BloomCompactorMaxBlockSize(tenantID string) int
	BloomCompactorMaxBloomSize(tenantID string) int
}
