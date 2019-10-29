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

	"github.com/grafana/loki/pkg/cfg"
	"github.com/grafana/loki/pkg/logentry/stages"
	"github.com/grafana/loki/pkg/promtail"
	"github.com/grafana/loki/pkg/promtail/config"
)

func init() {
	prometheus.MustRegister(version.NewCollector("promtail"))
}

func main() {
	printVersion := flag.Bool("version", false, "Print this builds version information")

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

	p, err := promtail.New(config)
	if err != nil {
		level.Error(util.Logger).Log("msg", "error creating promtail", "error", err)
		os.Exit(1)
	}

	level.Info(util.Logger).Log("msg", "Starting Promtail", "version", version.Info())

	if err := p.Run(); err != nil {
		level.Error(util.Logger).Log("msg", "error starting promtail", "error", err)
		os.Exit(1)
	}

	p.Shutdown()
}
