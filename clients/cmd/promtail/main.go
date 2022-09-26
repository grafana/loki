package main

import (
	"flag"
	"fmt"
	"os"
	"reflect"
	"sync"

	// embed time zone data
	_ "time/tzdata"

	"k8s.io/klog"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/version"
	"github.com/weaveworks/common/logging"

	"github.com/grafana/loki/clients/pkg/logentry/stages"
	"github.com/grafana/loki/clients/pkg/promtail"
	"github.com/grafana/loki/clients/pkg/promtail/client"
	"github.com/grafana/loki/clients/pkg/promtail/config"
	"github.com/grafana/loki/pkg/util"
	_ "github.com/grafana/loki/pkg/util/build"
	"github.com/grafana/loki/pkg/util/cfg"
	util_log "github.com/grafana/loki/pkg/util/log"
)

func init() {
	prometheus.MustRegister(version.NewCollector("promtail"))
}

var mtx sync.Mutex

type Config struct {
	config.Config   `yaml:",inline"`
	printVersion    bool
	printConfig     bool
	logConfig       bool
	dryRun          bool
	configFile      string
	configExpandEnv bool
	inspect         bool

	cnt int
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
	// Load configWrap, merging configWrap file and CLI flags
	var configWrap Config
	args := os.Args[1:]
	if err := cfg.DefaultUnmarshal(&configWrap, args, flag.CommandLine); err != nil {
		fmt.Println("Unable to parse configWrap:", err)
		os.Exit(1)
	}
	// Handle -version CLI flag
	if configWrap.printVersion {
		fmt.Println(version.Print("promtail"))
		os.Exit(0)
	}

	// Init the logger which will honor the log level set in cfg.Server
	if reflect.DeepEqual(&configWrap.Config.ServerConfig.Config.LogLevel, &logging.Level{}) {
		fmt.Println("Invalid log level")
		os.Exit(1)
	}
	util_log.InitLogger(&configWrap.Config.ServerConfig.Config, prometheus.DefaultRegisterer)

	// Use Stderr instead of files for the klog.
	klog.SetOutput(os.Stderr)

	if configWrap.inspect {
		stages.Inspect = true
	}

	// Set the global debug variable in the stages package which is used to conditionally log
	// debug messages which otherwise cause huge allocations processing log lines for log messages never printed
	if configWrap.Config.ServerConfig.Config.LogLevel.String() == "debug" {
		stages.Debug = true
	}

	if configWrap.printConfig {
		err := util.PrintConfig(os.Stderr, &configWrap)
		if err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to print configWrap to stderr", "err", err.Error())
		}
	}

	if configWrap.logConfig {
		err := util.LogConfig(&configWrap)
		if err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to log configWrap object", "err", err.Error())
		}
	}

	clientMetrics := client.NewMetrics(prometheus.DefaultRegisterer, configWrap.Config.Options.StreamLagLabels)
	newConfigFunc := func() config.Config {
		mtx.Lock()
		defer mtx.Unlock()
		var config Config
		fmt.Println("msg", "reload config file", "os.Args[1:]", os.Args[1:], "flag.CommandLine", flag.CommandLine)
		if err := cfg.DefaultUnmarshal(&config, args, flag.NewFlagSet(os.Args[0], flag.ExitOnError)); err != nil {
			fmt.Println("Unable to parse configWrap:", err)
			os.Exit(1)
		}
		return config.Config
	}
	p, err := promtail.New(configWrap.Config, newConfigFunc, clientMetrics, configWrap.dryRun)
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
