package loki

import "flag"

type ProfilingConfig struct {
	BlockProfileRate     int `yaml:"block_profile_rate"`
	CPUProfileRate       int `yaml:"cpu_profile_rate"`
	MutexProfileFraction int `yaml:"mutex_profile_fraction"`
}

// RegisterFlags registers flag.
func (c *ProfilingConfig) RegisterFlags(f *flag.FlagSet) {
	c.RegisterFlagsWithPrefix("profiling.", f)
}

// RegisterFlagsWithPrefix registers flag with a common prefix.
func (c *ProfilingConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.IntVar(&c.BlockProfileRate, prefix+"block-profile-rate", 0, "Sets the value for runtime.SetBlockProfilingRate")
	f.IntVar(&c.CPUProfileRate, prefix+"cpu-profile-rate", 0, "Sets the value for runtime.SetCPUProfileRate")
	f.IntVar(&c.MutexProfileFraction, prefix+"mutex-profile-fraction", 0, "Sets the value for runtime.SetMutexProfileFraction")
}
