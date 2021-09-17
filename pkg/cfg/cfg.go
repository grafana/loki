package cfg

import (
	"flag"
	"os"

	"github.com/grafana/dskit/flagext"
	"github.com/pkg/errors"

	"github.com/grafana/loki/pkg/loki"
	"github.com/grafana/loki/pkg/util/cfg"
)

// ConfigWrapper is a struct containing the Loki config along with other values that can be set on the command line
// for interacting with the config file or the application directly.
type ConfigWrapper struct {
	loki.Config     `yaml:",inline"`
	PrintVersion    bool
	VerifyConfig    bool
	PrintConfig     bool
	LogConfig       bool
	ConfigFile      string
	ConfigExpandEnv bool
}

func (c *ConfigWrapper) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&c.PrintVersion, "version", false, "Print this builds version information")
	f.BoolVar(&c.VerifyConfig, "verify-config", false, "Verify config file and exits")
	f.BoolVar(&c.PrintConfig, "print-config-stderr", false, "Dump the entire Loki config object to stderr")
	f.BoolVar(&c.LogConfig, "log-config-reverse-order", false, "Dump the entire Loki config object at Info log "+
		"level with the order reversed, reversing the order makes viewing the entries easier in Grafana.")
	f.StringVar(&c.ConfigFile, "config.file", "", "yaml file to load")
	f.BoolVar(&c.ConfigExpandEnv, "config.expand-env", false, "Expands ${var} in config according to the values of the environment variables.")
	c.Config.RegisterFlags(f)
}

// Clone takes advantage of pass-by-value semantics to return a distinct *Config.
// This is primarily used to parse a different flag set without mutating the original *Config.
func (c *ConfigWrapper) Clone() flagext.Registerer {
	return func(c ConfigWrapper) *ConfigWrapper {
		return &c
	}(*c)
}

// Unmarshal handles populating Loki's config based on the following precedence:
// 1. Defaults provided by the `RegisterFlags` interface
// 2. Sections populated by the `common` config section of the Loki config
// 3. Any config options specified directly in the loki config file
// 4. Any config options specified on the command line.
func Unmarshal(dst cfg.Cloneable) error {
	return cfg.Unmarshal(dst,
		// First populate the config with defaults including flags from the command line
		cfg.Defaults(flag.CommandLine),
		// Next populate the config from the config file, we do this to populate the `common`
		// section of the config file by taking advantage of the code in YAMLFlag which will load
		// and process the config file.
		cfg.YAMLFlag(os.Args[1:], "config.file"),
		// Apply our logic to use values from the common section to set values throughout the Loki config.
		CommonConfig(),
		// Load configs from the config file a second time, this will supersede anything set by the common
		// config with values specified in the config file.
		cfg.YAMLFlag(os.Args[1:], "config.file"),
		// Load the flags again, this will supersede anything set from config file with flags from the command line.
		cfg.Flags(),
	)
}

// CommonConfig applies all rules for setting Loki config values from the common section of the Loki config file.
// This entire method's purpose is to simplify Loki's config in an opinionated way so that Loki can be run
// with the minimal amount of config options for most use cases. It also aims to reduce redundancy where
// some values are set multiple times through the Loki config.
func CommonConfig() cfg.Source {
	return func(dst cfg.Cloneable) error {
		r, ok := dst.(*ConfigWrapper)
		if !ok {
			return errors.New("dst is not a Loki ConfigWrapper")
		}

		// Apply all our custom logic here to set values in the Loki config from values in the common config
		// FIXME this is just an example showing how we can use values from the common section to set values on the Loki config object
		r.StorageConfig.BoltDBShipperConfig.SharedStoreType = r.Common.Store

		return nil
	}
}
