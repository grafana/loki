package bloombuild

import (
	"flag"
	"fmt"
	bloomplanner "github.com/grafana/loki/v3/pkg/bloombuild/planner"
)

// Config configures the bloom-planner component.
type Config struct {
	Enabled bool `yaml:"enabled"`

	Planner bloomplanner.Config `yaml:"planner"`
}

// RegisterFlags registers flags for the bloom building configuration.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&cfg.Enabled, "bloom-build.enabled", false, "Flag to enable or disable the usage of the bloom-build-planner and bloom-builder components.")
	cfg.Planner.RegisterFlagsWithPrefix("bloom-build.enabled.planner", f)
}

func (cfg *Config) Validate() error {
	if !cfg.Enabled {
		return nil
	}

	if err := cfg.Planner.Validate(); err != nil {
		return fmt.Errorf("invalid bloom planner configuration: %w", err)
	}

	return nil
}