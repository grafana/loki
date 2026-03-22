package bloombuild

import (
	"flag"
	"fmt"

	"github.com/grafana/loki/v3/pkg/bloombuild/builder"
	"github.com/grafana/loki/v3/pkg/bloombuild/planner"
)

// Config configures the bloom-planner component.
type Config struct {
	Enabled bool `yaml:"enabled"`

	Planner planner.Config `yaml:"planner"`
	Builder builder.Config `yaml:"builder"`
}

// RegisterFlags registers flags for the bloom building configuration.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&cfg.Enabled, "bloom-build.enabled", false, "Flag to enable or disable the usage of the bloom-planner and bloom-builder components.")
	cfg.Planner.RegisterFlagsWithPrefix("bloom-build.planner", f)
	cfg.Builder.RegisterFlagsWithPrefix("bloom-build.builder", f)
}

func (cfg *Config) Validate() error {
	if !cfg.Enabled {
		return nil
	}

	if err := cfg.Planner.Validate(); err != nil {
		return fmt.Errorf("invalid bloom planner configuration: %w", err)
	}

	if err := cfg.Builder.Validate(); err != nil {
		return fmt.Errorf("invalid bloom builder configuration: %w", err)
	}

	return nil
}
