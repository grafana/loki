package main

import (
	"flag"
	"fmt"
	"os"
	"reflect"

	"github.com/go-kit/kit/log/level"
	"github.com/grafana/dskit/dslog"
	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/version"
	"github.com/weaveworks/common/logging"
	"github.com/weaveworks/common/tracing"

	"github.com/grafana/loki/pkg/loki"
	"github.com/grafana/loki/pkg/util"
	_ "github.com/grafana/loki/pkg/util/build"
	"github.com/grafana/loki/pkg/util/cfg"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/validation"
)

func init() {
	prometheus.MustRegister(version.NewCollector("loki"))
}

type Config struct {
	loki.Config     `yaml:",inline"`
	printVersion    bool
	verifyConfig    bool
	printConfig     bool
	logConfig       bool
	configFile      string
	configExpandEnv bool
}

func (c *Config) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&c.printVersion, "version", false, "Print this builds version information")
	f.BoolVar(&c.verifyConfig, "verify-config", false, "Verify config file and exits")
	f.BoolVar(&c.printConfig, "print-config-stderr", false, "Dump the entire Loki config object to stderr")
	f.BoolVar(&c.logConfig, "log-config-reverse-order", false, "Dump the entire Loki config object at Info log "+
		"level with the order reversed, reversing the order makes viewing the entries easier in Grafana.")
	f.StringVar(&c.configFile, "config.file", "", "yaml file to load")
	f.BoolVar(&c.configExpandEnv, "config.expand-env", false, "Expands ${var} in config according to the values of the environment variables.")
	c.Config.RegisterFlags(f)
}

// Clone takes advantage of pass-by-value semantics to return a distinct *Config.
// This is primarily used to parse a different flag set without mutating the original *Config.
func (c *Config) Clone() flagext.Registerer {
	return func(c Config) *Config {
		return &c
	}(*c)
}

func main() {
	var config Config

	if err := cfg.Parse(&config); err != nil {
		fmt.Fprintf(os.Stderr, "failed parsing config: %v\n", err)
		os.Exit(1)
	}
	if config.printVersion {
		fmt.Println(version.Print("loki"))
		os.Exit(0)
	}

	// This global is set to the config passed into the last call to `NewOverrides`. If we don't
	// call it atleast once, the defaults are set to an empty struct.
	// We call it with the flag values so that the config file unmarshalling only overrides the values set in the config.
	validation.SetDefaultLimitsForYAMLUnmarshalling(config.LimitsConfig)

	// Init the logger which will honor the log level set in config.Server
	if reflect.DeepEqual(&config.Server.LogLevel, &logging.Level{}) {
		level.Error(util_log.Logger).Log("msg", "invalid log level")
		os.Exit(1)
	}
	util_log.InitLogger(&config.Server, prometheus.DefaultRegisterer)

	// Validate the config once both the config file has been loaded
	// and CLI flags parsed.
	err := config.Validate()
	if err != nil {
		level.Error(util_log.Logger).Log("msg", "validating config", "err", err.Error())
		os.Exit(1)
	}

	if config.verifyConfig {
		level.Info(util_log.Logger).Log("msg", "config is valid")
		os.Exit(0)
	}

	if config.printConfig {
		err := util.PrintConfig(os.Stderr, &config)
		if err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to print config to stderr", "err", err.Error())
		}
	}

	if config.logConfig {
		err := util.LogConfig(&config)
		if err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to log config object", "err", err.Error())
		}
	}

	if config.Tracing.Enabled {
		// Setting the environment variable JAEGER_AGENT_HOST enables tracing
		trace, err := tracing.NewFromEnv(fmt.Sprintf("loki-%s", config.Target))
		if err != nil {
			level.Error(util_log.Logger).Log("msg", "error in initializing tracing. tracing will not be enabled", "err", err)
		}
		defer func() {
			if trace != nil {
				if err := trace.Close(); err != nil {
					level.Error(util_log.Logger).Log("msg", "error closing tracing", "err", err)
				}
			}

		}()
	}

	// Start Loki
	t, err := loki.New(config.Config)
	dslog.CheckFatal("initialising loki", err, util_log.Logger)

	level.Info(util_log.Logger).Log("msg", "Starting Loki", "version", version.Info())

	err = t.Run()
	dslog.CheckFatal("running loki", err, util_log.Logger)
}
