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
	"github.com/grafana/dskit/log"
	"github.com/grafana/dskit/tracing"
	"github.com/prometheus/client_golang/prometheus"
	collectors_version "github.com/prometheus/client_golang/prometheus/collectors/version"
	"github.com/prometheus/common/version"

	"github.com/grafana/loki/v3/clients/pkg/logentry/stages"
	"github.com/grafana/loki/v3/clients/pkg/promtail"
	"github.com/grafana/loki/v3/clients/pkg/promtail/client"
	promtail_config "github.com/grafana/loki/v3/clients/pkg/promtail/config"

	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/cfg"

	_ "github.com/grafana/loki/v3/pkg/util/build"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

func init() {
	prometheus.MustRegister(collectors_version.NewCollector("promtail"))
}

var mtx sync.Mutex

type Config struct {
	promtail_config.Config `yaml:",inline"`
	printVersion           bool
	printConfig            bool
	logConfig              bool
	dryRun                 bool
	checkSyntax            bool
	configFile             string
	configExpandEnv        bool
	inspect                bool
}

func (c *Config) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&c.printVersion, "version", false, "Print this builds version information")
	f.BoolVar(&c.printConfig, "print-config-stderr", false, "Dump the entire Loki config object to stderr")
	f.BoolVar(&c.logConfig, "log-config-reverse-order", false, "Dump the entire Loki config object at Info log "+
		"level with the order reversed, reversing the order makes viewing the entries easier in Grafana.")
	f.BoolVar(&c.dryRun, "dry-run", false, "Start Promtail but print entries instead of sending them to Loki.")
	f.BoolVar(&c.checkSyntax, "check-syntax", false, "Validate the config file of its syntax")
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

// wrap os.Exit so that deferred functions execute before the process exits
func exit(code int) {
	// flush all logs that may be buffered in memory
	util_log.Flush()

	os.Exit(code)
}

func main() {
	// Load config, merging config file and CLI flags
	var config Config
	args := os.Args[1:]
	if err := cfg.DefaultUnmarshal(&config, args, flag.CommandLine); err != nil {
		fmt.Println("Unable to parse config:", err)
		exit(1)
	}
	if config.checkSyntax {
		if config.configFile == "" {
			fmt.Println("Invalid config file")
			exit(1)
		}
		fmt.Println("Valid config file! No syntax issues found")
		exit(0)
	}

	// Handle -version CLI flag
	if config.printVersion {
		fmt.Println(version.Print("promtail"))
		exit(0)
	}

	// Init the logger which will honor the log level set in cfg.Server
	if reflect.DeepEqual(&config.Config.ServerConfig.Config.LogLevel, &log.Level{}) {
		fmt.Println("Invalid log level")
		exit(1)
	}
	serverCfg := &config.Config.ServerConfig.Config
	serverCfg.Log = util_log.InitLogger(serverCfg, prometheus.DefaultRegisterer, false)

	// Use Stderr instead of files for the klog.
	klog.SetOutput(os.Stderr)

	if config.inspect {
		stages.Inspect = true
	}

	// Set the global debug variable in the stages package which is used to conditionally log
	// debug messages which otherwise cause huge allocations processing log lines for log messages never printed
	if config.Config.ServerConfig.Config.LogLevel.String() == "debug" {
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

	if config.Tracing.Enabled {
		// Setting the environment variable JAEGER_AGENT_HOST enables tracing
		trace, err := tracing.NewFromEnv("promtail")
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

	clientMetrics := client.NewMetrics(prometheus.DefaultRegisterer)
	newConfigFunc := func() (*promtail_config.Config, error) {
		mtx.Lock()
		defer mtx.Unlock()
		var config Config
		if err := cfg.DefaultUnmarshal(&config, args, flag.NewFlagSet(os.Args[0], flag.ExitOnError)); err != nil {
			fmt.Println("Unable to parse config:", err)
			return nil, fmt.Errorf("unable to parse config: %w", err)
		}
		return &config.Config, nil
	}
	p, err := promtail.New(config.Config, newConfigFunc, clientMetrics, config.dryRun)
	if err != nil {
		level.Error(util_log.Logger).Log("msg", "error creating promtail", "error", err)
		exit(1)
	}

	level.Info(util_log.Logger).Log("msg", "Starting Promtail", "version", version.Info())
	defer p.Shutdown()

	if err := p.Run(); err != nil {
		level.Error(util_log.Logger).Log("msg", "error starting promtail", "error", err)
		exit(1)
	}
}
