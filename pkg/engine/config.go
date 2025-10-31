package engine

import "flag"

// Config holds the configuration options to use with the next generation Loki
// Query Engine.
type Config struct {
	// Enable the next generation Loki Query Engine for supported queries.
	Enable bool `yaml:"enable" category:"experimental"`

	Executor ExecutorConfig `yaml:",inline"`
	Worker   WorkerConfig   `yaml:",inline"`
}

func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.BoolVar(&cfg.Enable, prefix+"enable", false, "Experimental: Enable next generation query engine for supported queries.")

	cfg.Executor.RegisterFlagsWithPrefix(prefix, f)
	cfg.Worker.RegisterFlagsWithPrefix(prefix, f)
}
