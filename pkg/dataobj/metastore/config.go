package metastore

import (
	"flag"
	"time"
)

// Config is the configuration block for the metastore settings.
type Config struct {
	TOCResolver TOCResolverConfig `yaml:"toc_resolver" category:"experimental"`
}

// RegisterFlags registers the flags for the metastore settings.
func (c *Config) RegisterFlags(f *flag.FlagSet) {
	c.TOCResolver.RegisterFlagsWithPrefix("dataobj-metastore.toc-resolver.", f)
}

// Validate returns an error when the settings are invalid. storageLag is the delay after which data objects hold all the
// data.
func (c *Config) Validate(storageLag time.Duration) error {
	return c.TOCResolver.Validate(storageLag)
}
