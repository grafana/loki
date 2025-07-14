package metastore

import (
	"flag"

	"github.com/pkg/errors"
)

type Config struct {
	UpdaterConfig UpdaterConfig `yaml:"updater" category:"experimental"`
}

func (c *Config) RegisterFlags(f *flag.FlagSet) {
	c.UpdaterConfig.RegisterFlagsWithPrefix("dataobj-metastore.", f)
}

func (c *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	c.UpdaterConfig.RegisterFlagsWithPrefix(prefix, f)
}

func (c *Config) Validate() error {
	if err := c.UpdaterConfig.Validate(); err != nil {
		return err
	}

	return nil
}

type UpdaterConfig struct {
	FormatRaw string             `yaml:"format"`
	Format    TopLevelObjectType `yaml:"-"`
}

func (c *UpdaterConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&c.FormatRaw, prefix+"format", "v1", "Top level object format to store pointers to index objects")
}

func (c *UpdaterConfig) Validate() error {
	switch c.FormatRaw {
	case "v1", "":
		c.Format = TopLevelObjectTypeV1
	case "v2":
		c.Format = TopLevelObjectTypeV2
	default:
		return errors.Errorf("invalid top level object format: %s", c.FormatRaw)
	}

	return nil
}
