package main

import (
	"flag"
	"fmt"
	"os"
	"reflect"

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

	var config config.Config
	if err := cfg.Parse(&config); err != nil {
		level.Error(util.Logger).Log("msg", "parsing config", "error", err)
		os.Exit(1)
	}
	if *printVersion {
		fmt.Print(version.Print("promtail"))
		os.Exit(0)
	}

	// Init the logger which will honor the log level set in cfg.Server
	if reflect.DeepEqual(&config.ServerConfig.Config.LogLevel, &logging.Level{}) {
		level.Error(util.Logger).Log("msg", "invalid log level")
		os.Exit(1)
	}
	util.InitLogger(&config.ServerConfig.Config)

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
