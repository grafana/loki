package tracing

import (
	"flag"
)

type Config struct {
	Enabled          bool `yaml:"enabled"`
	ProfilingEnabled bool `yaml:"profiling_enabled" category:"experimental" doc:"hidden"`
}

func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&cfg.Enabled, "tracing.enabled", true, "Set to false to disable tracing.")
	f.BoolVar(&cfg.ProfilingEnabled, "tracing.profiling-enabled", true, "Set to true to enable profiling integration.")
}

func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.BoolVar(&cfg.Enabled, prefix+"tracing.enabled", true, "Set to false to disable tracing.")
	f.BoolVar(&cfg.ProfilingEnabled, prefix+"tracing.profiling-enabled", true, "Set to true to enable profiling integration.")
}
