// package bloomshipperconfig resides in its own package to prevent circular imports with storage package
package config

import (
	"errors"
	"flag"
	"strings"
)

type Config struct {
	WorkingDirectory string `yaml:"working_directory"`
}

func (c *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.StringVar(&c.WorkingDirectory, prefix+"shipper.working-directory", "bloom-shipper", "Working directory to store downloaded Bloom Blocks.")
}

func (c *Config) Validate() error {
	if strings.TrimSpace(c.WorkingDirectory) == "" {
		return errors.New("working directory must be specified")
	}
	return nil
}
