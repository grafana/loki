package logline

import (
	"flag"
	"fmt"

	"github.com/grafana/loki/v3/pkg/logline/store"
)

// Config is the Loki-level logline configuration shared by querier (object
// reads) and query-frontend (catalog + middleware).
type Config struct {
	Enabled bool         `yaml:"enabled"`
	Store   store.Config `yaml:"store"`
}

// RegisterFlags registers logline.enabled and store flags.
func (c *Config) RegisterFlags(f *flag.FlagSet) {
	if f == nil {
		f = flag.CommandLine
	}
	f.BoolVar(&c.Enabled, "logline.enabled", false, "Enable logline index reads on queriers")
	c.Store.RegisterFlags(f)
}

// Validate checks store settings when logline is enabled.
func (c *Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	if err := c.Store.Validate(); err != nil {
		return fmt.Errorf("invalid store config: %w", err)
	}
	return nil
}
