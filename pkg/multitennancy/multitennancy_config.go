package multitenancy

import (
	"errors"
	"flag"
)

var (
	errTypeMissingUndefinedOrName = errors.New("multi-tenancy using label must provide both name and undefined")
	errTypeNotDefined             = errors.New("multi-tenancy type must be of either label or auth")
)

// Config for the multi-tenancy side of loki
type Config struct {
	Enabled   bool   `yaml:"enabled"`
	Type      string `yaml:"type"`
	Label     string `yaml:"label,omitempty"`
	Undefined string `yaml:"undefined,omitempty"`
}

// Validate the config and returns an error if it doesnt pass
func (c *Config) Validate() error {
	if !c.Enabled {
		return nil
	}
	if c.Type != "label" && c.Type != "auth" {
		return errTypeNotDefined
	}
	if c.Type == "label" && c.Label == "" && c.Undefined == "" {
		return errTypeMissingUndefinedOrName
	}
	return nil
}

// RegisterFlags adds the
func (c *Config) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&c.Enabled, "multitenancy.enabled", false, "Enable multi-tenancy mode")
	f.StringVar(&c.Type, "multitenancy.type", "auth", "Where to get the Org ID for multi-tenancy ")
	f.StringVar(&c.Label, "multitenancy.label", "", "Specifies the label to use for Org ID (in label mode)")
	f.StringVar(&c.Undefined, "multitenancy.undefined", "undefined", "Sepcifies the name to use when log doesnt contain the label (in label mode)")
}
