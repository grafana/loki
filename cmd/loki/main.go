package main

import (
	"flag"
	"fmt"
	"os"
	"reflect"

	"github.com/go-kit/log/level"
	"github.com/prometheus/common/version"
	"github.com/weaveworks/common/logging"
	"github.com/weaveworks/common/tracing"

	"github.com/grafana/loki/pkg/loki"
	logutil "github.com/grafana/loki/pkg/util"
	_ "github.com/grafana/loki/pkg/util/build"
	"github.com/grafana/loki/pkg/util/cfg"
	"github.com/grafana/loki/pkg/validation"

	util_log "github.com/cortexproject/cortex/pkg/util/log"
)

func main() {
	var config loki.ConfigWrapper

	if err := cfg.DynamicUnmarshal(&config, os.Args[1:], flag.CommandLine); err != nil {
		fmt.Fprintf(os.Stderr, "failed parsing config: %v\n", err)
		os.Exit(1)
	}
	if config.PrintVersion {
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
	util_log.InitLogger(&config.Server)

	// Validate the config once both the config file has been loaded
	// and CLI flags parsed.
	err := config.Validate()
	if err != nil {
		level.Error(util_log.Logger).Log("msg", "validating config", "err", err.Error())
		os.Exit(1)
	}

	if config.VerifyConfig {
		level.Info(util_log.Logger).Log("msg", "config is valid")
		os.Exit(0)
	}

	if config.PrintConfig {
		err := logutil.PrintConfig(os.Stderr, &config)
		if err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to print config to stderr", "err", err.Error())
		}
	}

	if config.LogConfig {
		err := logutil.LogConfig(&config)
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
	util_log.CheckFatal("initialising loki", err)

	level.Info(util_log.Logger).Log("msg", "Starting Loki", "version", version.Info())

	err = t.Run(loki.RunOpts{})
	util_log.CheckFatal("running loki", err)
}
