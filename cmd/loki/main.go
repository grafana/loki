package main

import (
	"flag"
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/version"
	"github.com/weaveworks/common/logging"
	"github.com/weaveworks/common/tracing"

	_ "github.com/grafana/loki/pkg/build"
	"github.com/grafana/loki/pkg/cfg"
	"github.com/grafana/loki/pkg/loki"
	logutil "github.com/grafana/loki/pkg/util"

	"github.com/cortexproject/cortex/pkg/util"

	"github.com/grafana/loki/pkg/util/validation"
)

func init() {
	prometheus.MustRegister(version.NewCollector("loki"))
}

var lineReplacer = strings.NewReplacer("\n", "\\n  ")

func main() {
	printVersion := flag.Bool("version", false, "Print this builds version information")
	printConfig := flag.Bool("print-config-stderr", false, "Dump the entire Loki config object to stderr")
	logConfig := flag.Bool("log-config-reverse-order", false, "Dump the entire Loki config object at Info log "+
		"level with the order reversed, reversing the order makes viewing the entries easier in Grafana.")

	var config loki.Config
	if err := cfg.Parse(&config); err != nil {
		fmt.Fprintf(os.Stderr, "failed parsing config: %v\n", err)
		os.Exit(1)
	}
	if *printVersion {
		fmt.Println(version.Print("loki"))
		os.Exit(0)
	}

	// This global is set to the config passed into the last call to `NewOverrides`. If we don't
	// call it atleast once, the defaults are set to an empty struct.
	// We call it with the flag values so that the config file unmarshalling only overrides the values set in the config.
	validation.SetDefaultLimitsForYAMLUnmarshalling(config.LimitsConfig)

	// Init the logger which will honor the log level set in config.Server
	if reflect.DeepEqual(&config.Server.LogLevel, &logging.Level{}) {
		level.Error(util.Logger).Log("msg", "invalid log level")
		os.Exit(1)
	}
	util.InitLogger(&config.Server)

	// Validate the config once both the config file has been loaded
	// and CLI flags parsed.
	err := config.Validate(util.Logger)
	if err != nil {
		level.Error(util.Logger).Log("msg", "validating config", "err", err.Error())
		os.Exit(1)
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

	if config.Tracing.Enabled {
		// Setting the environment variable JAEGER_AGENT_HOST enables tracing
		trace, err := tracing.NewFromEnv(fmt.Sprintf("loki-%s", config.Target))
		if err != nil {
			level.Error(util.Logger).Log("msg", "error in initializing tracing. tracing will not be enabled", "err", err)
		}
		defer func() {
			if trace != nil {
				if err := trace.Close(); err != nil {
					level.Error(util.Logger).Log("msg", "error closing tracing", "err", err)
				}
			}

		}()
	}

	// Start Loki
	t, err := loki.New(config)
	util.CheckFatal("initialising loki", err)

	level.Info(util.Logger).Log("msg", "Starting Loki", "version", version.Info())

	err = t.Run()
	util.CheckFatal("running loki", err)
}
