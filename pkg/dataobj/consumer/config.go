package consumer

import (
	"errors"
	"flag"

	"github.com/grafana/loki/v3/pkg/dataobj"
)

type Config struct {
	dataobj.BuilderConfig
	TenantID string `yaml:"tenant_id"`
}

func (cfg *Config) Validate() error {
	if cfg.TenantID == "" {
		return errors.New("tenantID is required")
	}
	return cfg.BuilderConfig.Validate()
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("dataobj-consumer", f)
}

func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	cfg.BuilderConfig.RegisterFlagsWithPrefix(prefix, f)
	f.StringVar(&cfg.TenantID, prefix+".tenant-id", "fake", "The tenant ID to use for the data object builder.")
}
