package bloomworker

import "flag"

// Config configures the bloom-planner component.
type Config struct {
	// TODO: Add config
}

// RegisterFlagsWithPrefix registers flags for the bloom-planner configuration.
func (cfg *Config) RegisterFlagsWithPrefix(_ string, _ *flag.FlagSet) {
	// TODO: Register flags with flagsPrefix
}

func (cfg *Config) Validate() error {
	return nil
}

type Limits interface {
	// TODO: Add limits
}
