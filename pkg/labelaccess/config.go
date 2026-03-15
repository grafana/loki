package labelaccess

import (
	"flag"
)

// Config holds the configuration for label-based access control.
type Config struct {
	Enabled bool `yaml:"enabled" category:"experimental"`
}

// RegisterFlags adds the flags for LBAC to the given FlagSet.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&cfg.Enabled, "lbac.enabled", false,
		"Enables label based access control through the X-Prom-Label-Policy header.",
	)
}
