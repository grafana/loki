package main

import (
	"flag"
	"fmt"
	"os"
	"reflect"
	"runtime"
	"time"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/log"
	"github.com/grafana/dskit/tracing"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/version"

	"github.com/grafana/loki/pkg/loki"
	"github.com/grafana/loki/pkg/util"
	_ "github.com/grafana/loki/pkg/util/build"
	"github.com/grafana/loki/pkg/util/cfg"
	util_log "github.com/grafana/loki/pkg/util/log"
	"github.com/grafana/loki/pkg/validation"
)

func exit(code int) {
	util_log.Flush()
	os.Exit(code)
}

func main() {
	startTime := time.Now()

	var config loki.ConfigWrapper

	if loki.PrintVersion(os.Args[1:]) {
		fmt.Println(version.Print("loki"))
		os.Exit(0)
	}
	if err := cfg.DynamicUnmarshal(&config, os.Args[1:], flag.CommandLine); err != nil {
		fmt.Fprintf(os.Stderr, "failed parsing config: %v\n", err)
		os.Exit(1)
	}

	// This global is set to the config passed into the last call to `NewOverrides`. If we don't
	// call it atleast once, the defaults are set to an empty struct.
	// We call it with the flag values so that the config file unmarshalling only overrides the values set in the config.
	validation.SetDefaultLimitsForYAMLUnmarshalling(config.LimitsConfig)

	// Init the logger which will honor the log level set in config.Server
	if reflect.DeepEqual(&config.Server.LogLevel, &log.Level{}) {
		level.Error(util_log.Logger).Log("msg", "invalid log level")
		exit(1)
	}
	serverCfg := &config.Server
	serverCfg.Log = util_log.InitLogger(serverCfg, prometheus.DefaultRegisterer, config.UseBufferedLogger, config.UseSyncLogger)

	// Validate the config once both the config file has been loaded
	// and CLI flags parsed.
	if err := config.Validate(); err != nil {
		level.Error(util_log.Logger).Log("msg", "validating config", "err", err.Error())
		exit(1)
	}

	if config.PrintConfig {
		if err := util.PrintConfig(os.Stderr, &config); err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to print config to stderr", "err", err.Error())
		}
	}

	if config.LogConfig {
		if err := util.LogConfig(&config); err != nil {
			level.Error(util_log.Logger).Log("msg", "failed to log config object", "err", err.Error())
		}
	}

	if config.VerifyConfig {
		level.Info(util_log.Logger).Log("msg", "config is valid")
		exit(0)
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

	// Allocate a block of memory to reduce the frequency of garbage collection.
	// The larger the ballast, the lower the garbage collection frequency.
	// https://github.com/grafana/loki/issues/781
	ballast := make([]byte, config.BallastBytes)
	runtime.KeepAlive(ballast)

	// Start Loki
	t, err := loki.New(config.Config)
	util_log.CheckFatal("initializing loki", err, util_log.Logger)

	if config.ListTargets {
		t.ListTargets()
		exit(0)
	}

	level.Info(util_log.Logger).Log("msg", "Starting Loki", "version", version.Info())

	err = t.Run(loki.RunOpts{StartTime: startTime})
	util_log.CheckFatal("running loki", err, util_log.Logger)
}
