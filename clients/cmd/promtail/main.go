package main

import (
	"flag"
	"fmt"
	"os"
	"reflect"

	// embed time zone data
	_ "time/tzdata"

	"k8s.io/klog"

	"github.com/go-kit/kit/log/level"
	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/version"
	"github.com/weaveworks/common/logging"

	"github.com/grafana/loki/clients/pkg/logentry/stages"
	"github.com/grafana/loki/clients/pkg/promtail"
	"github.com/grafana/loki/clients/pkg/promtail/config"

	"github.com/grafana/loki/pkg/util"
	_ "github.com/grafana/loki/pkg/util/build"
	"github.com/grafana/loki/pkg/util/cfg"
	util_log "github.com/grafana/loki/pkg/util/log"
)

func init() {
	prometheus.MustRegister(version.NewCollector("promtail"))
}

type Config struct {
	config.Config   `yaml:",inline"`
	printVersion    bool
	printConfig     bool
	logConfig       bool
	dryRun          bool
	configFile      string
	configExpandEnv bool
	inspect         bool
}

func (c *Config) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&c.printVersion, "version", false, "Print this builds version information")
	f.BoolVar(&c.printConfig, "print-config-stderr", false, "Dump the entire Loki config object to stderr")
	f.BoolVar(&c.logConfig, "log-config-reverse-order", false, "Dump the entire Loki config object at Info log "+
		"level with the order reversed, reversing the order makes viewing the entries easier in Grafana.")
	f.BoolVar(&c.dryRun, "dry-run", false, "Start Promtail but print entries instead of sending them to Loki.")
	f.BoolVar(&c.inspect, "inspect", false, "Allows for detailed inspection of pipeline stages")
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
	// Load config, merging config file and CLI flags
	var config Config
	if err := cfg.Parse(&config); err != nil {
		fmt.Println("Unable to parse config:", err)
		os.Exit(1)
	}

	// Handle -version CLI flag
	if config.printVersion {
		fmt.Println(version.Print("promtail"))
		os.Exit(0)
	}

	// Init the logger which will honor the log level set in cfg.Server
	if reflect.DeepEqual(&config.ServerConfig.Config.LogLevel, &logging.Level{}) {
		fmt.Println("Invalid log level")
		os.Exit(1)
	}
	util_log.InitLogger(&config.ServerConfig.Config, prometheus.DefaultRegisterer)

	// Use Stderr instead of files for the klog.
	klog.SetOutput(os.Stderr)

	if config.inspect {
		stages.Inspect = true
	}

	// Set the global debug variable in the stages package which is used to conditionally log
	// debug messages which otherwise cause huge allocations processing log lines for log messages never printed
	if config.ServerConfig.Config.LogLevel.String() == "debug" {
		stages.Debug = true
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

	p, err := promtail.New(config.Config, config.dryRun, prometheus.DefaultRegisterer)
	if err != nil {
		level.Error(util_log.Logger).Log("msg", "error creating promtail", "error", err)
		os.Exit(1)
	}

	level.Info(util_log.Logger).Log("msg", "Starting Promtail", "version", version.Info())
	defer p.Shutdown()

	if err := p.Run(); err != nil {
		level.Error(util_log.Logger).Log("msg", "error starting promtail", "error", err)
		os.Exit(1)
	}
}
