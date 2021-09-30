package loki

import (
	"flag"
	"fmt"

	"github.com/grafana/dskit/flagext"
	"github.com/pkg/errors"

	"github.com/grafana/loki/pkg/util/cfg"
)

// ConfigWrapper is a struct containing the Loki config along with other values that can be set on the command line
// for interacting with the config file or the application directly.
// ConfigWrapper implements cfg.DynamicCloneable, allowing configuration to be dynamically set based
// on the logic in ApplyDynamicConfig, which receives values set in config file
type ConfigWrapper struct {
	Config          `yaml:",inline"`
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

// ApplyDynamicConfig satisfies WithCommonCloneable interface, and applies all rules for setting Loki
// config values from the common section of the Loki config file.
// This method's purpose is to simplify Loki's config in an opinionated way so that Loki can be run
// with the minimal amount of config options for most use cases. It also aims to reduce redundancy where
// some values are set multiple times through the Loki config.
func (c *ConfigWrapper) ApplyDynamicConfig() cfg.Source {
	defaults := ConfigWrapper{}
	flagext.DefaultValues(&defaults)

	return func(dst cfg.Cloneable) error {
		r, ok := dst.(*ConfigWrapper)
		if !ok {
			return errors.New("dst is not a Loki ConfigWrapper")
		}

		if defaultsUnmarshalError != nil {
			return defaultsUnmarshalError
		}

		// Apply all our custom logic here to set values in the Loki config from values in the common config
		if r.Common.PathPrefix != "" {
			if r.Ruler.RulePath == defaults.Ruler.RulePath {
				r.Ruler.RulePath = fmt.Sprintf("%s/rules", r.Common.PathPrefix)
			}

			if r.Ingester.WAL.Dir == defaults.Ingester.WAL.Dir {
				r.Ingester.WAL.Dir = fmt.Sprintf("%s/wal", r.Common.PathPrefix)
			}
		}

		return nil
	}
}
