package metastore

import (
	"flag"

	"github.com/pkg/errors"
)

// Config is the configuration block for the metastore settings.
type Config struct {
	Updater UpdaterConfig `yaml:"updater" experimental:"true"`
}

// RegisterFlags registers the flags for the metastore settings.
func (c *Config) RegisterFlags(f *flag.FlagSet) {
	c.Updater.RegisterFlagsWithPrefix("dataobj-metastore.", f)
}

// RegisterFlagsWithPrefix registers the flags for the metastore settings with a prefix.
func (c *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	c.Updater.RegisterFlagsWithPrefix(prefix, f)
}

// Validate validates the metastore settings.
func (c *Config) Validate() error {
	if err := c.Updater.Validate(); err != nil {
		return err
	}

	return nil
}

// UpdaterConfig is the configuration block for the metastore updater settings.
type UpdaterConfig struct {
	StorageFormatRaw string            `yaml:"storage_format" experimental:"true"`
	StorageFormat    StorageFormatType `yaml:"-"`
}

// RegisterFlagsWithPrefix registers the flags for the metastore updater settings with a prefix.
func (c *UpdaterConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&c.StorageFormatRaw, prefix+"storage-format", "v1", "The format to use for the metastore top-level index objects.")
}

// Validate validates the metastore updater settings.
func (c *UpdaterConfig) Validate() error {
	switch c.StorageFormatRaw {
	case "v1", "":
		c.StorageFormat = StorageFormatTypeV1
	case "v2":
		c.StorageFormat = StorageFormatTypeV2
	default:
		return errors.Errorf("invalid metastore storage format for top-level index objects: %s", c.StorageFormatRaw)
	}

	return nil
}
