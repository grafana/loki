package main

import (
	"flag"
	"fmt"
	"os"
	"reflect"

	"k8s.io/klog"

	"github.com/cortexproject/cortex/pkg/util"
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

func main() {
	printVersion := flag.Bool("version", false, "Print this builds version information")
	dryRun := flag.Bool("dry-run", false, "Start Promtail but print entries instead of sending them to Loki.")
	printConfig := flag.Bool("print-config-stderr", false, "Dump the entire Loki config object to stderr")
	logConfig := flag.Bool("log-config-reverse-order", false, "Dump the entire Loki config object at Info log "+
		"level with the order reversed, reversing the order makes viewing the entries easier in Grafana.")

	// Load config, merging config file and CLI flags
	var config config.Config
	if err := cfg.Parse(&config); err != nil {
		fmt.Println("Unable to parse config:", err)
		os.Exit(1)
	}

	// Handle -version CLI flag
	if *printVersion {
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

	if *printConfig {
		err := logutil.PrintConfig(os.Stderr, &config)
		if err != nil {
			level.Error(util.Logger).Log("msg", "failed to print config to stderr", "err", err.Error())
		}
	}

	if *logConfig {
		err := logutil.LogConfig(&config)
		if err != nil {
			level.Error(util.Logger).Log("msg", "failed to log config object", "err", err.Error())
		}
	}

	p, err := promtail.New(config, *dryRun)
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
