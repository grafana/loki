package main

import (
	"flag"
	"fmt"
	"os"
	"reflect"

	"k8s.io/klog"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/flagext"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/version"
	"github.com/weaveworks/common/logging"

	_ "github.com/grafana/loki/pkg/build"
	"github.com/grafana/loki/pkg/cfg"
	"github.com/grafana/loki/pkg/logentry/stages"
	"github.com/grafana/loki/pkg/promtail"
	"github.com/grafana/loki/pkg/promtail/config"
	logutil "github.com/grafana/loki/pkg/util"
)

func init() {
	prometheus.MustRegister(version.NewCollector("promtail"))
}

type Config struct {
	config.Config `yaml:",inline"`
	printVersion  bool
	printConfig   bool
	logConfig     bool
	dryRun        bool
	configFile    string
}

func (c *Config) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&c.printVersion, "version", false, "Print this builds version information")
	f.BoolVar(&c.printConfig, "print-config-stderr", false, "Dump the entire Loki config object to stderr")
	f.BoolVar(&c.logConfig, "log-config-reverse-order", false, "Dump the entire Loki config object at Info log "+
		"level with the order reversed, reversing the order makes viewing the entries easier in Grafana.")
	f.BoolVar(&c.dryRun, "dry-run", false, "Start Promtail but print entries instead of sending them to Loki.")
	f.StringVar(&c.configFile, "config.file", "", "yaml file to load")
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
	util.InitLogger(&config.ServerConfig.Config)

	// Use Stderr instead of files for the klog.
	klog.SetOutput(os.Stderr)

	// Set the global debug variable in the stages package which is used to conditionally log
	// debug messages which otherwise cause huge allocations processing log lines for log messages never printed
	if config.ServerConfig.Config.LogLevel.String() == "debug" {
		stages.Debug = true
	}

	if config.printConfig {
		err := logutil.PrintConfig(os.Stderr, &config)
		if err != nil {
			level.Error(util.Logger).Log("msg", "failed to print config to stderr", "err", err.Error())
		}
	}

	if config.logConfig {
		err := logutil.LogConfig(&config)
		if err != nil {
			level.Error(util.Logger).Log("msg", "failed to log config object", "err", err.Error())
		}
	}

	p, err := promtail.New(config.Config, config.dryRun)
	if err != nil {
		level.Error(util.Logger).Log("msg", "error creating promtail", "error", err)
		os.Exit(1)
	}

	level.Info(util.Logger).Log("msg", "Starting Promtail", "version", version.Info())
	defer p.Shutdown()

	if err := p.Run(); err != nil {
		level.Error(util.Logger).Log("msg", "error starting promtail", "error", err)
		os.Exit(1)
	}
}
