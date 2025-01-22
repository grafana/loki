package frontend

import (
	"flag"
)

type Config struct {
	ClientConfig ClientConfig `yaml:"client_config"`
}

func (cfg Config) RegisterFlags(f *flag.FlagSet) {
	cfg.ClientConfig.RegisterFlags(f)
}

// TODO: Validate configuration. This is called during initialization.
func (cfg Config) Validate() error {
	return nil
}
