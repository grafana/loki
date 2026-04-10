package loki

import "flag"

// GCConfig configures the Go garbage collector.
type GCConfig struct {
	AutoMemLimitEnabled bool    `yaml:"automemlimit_enabled"`
	AutoMemLimitRatio   float64 `yaml:"automemlimit_ratio"`
}

func (c *GCConfig) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&c.AutoMemLimitEnabled, "gc.automemlimit_enabled", false,
		"Set to true to enable automatic adjustment of GOMEMLIMIT based on available memory.")
	f.Float64Var(&c.AutoMemLimitRatio, "gc.automemlimit_ratio", 0.8,
		"The proportion of available memory to use for GOMEMLIMIT. Must be between 0 and 1.")
}
