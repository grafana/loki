package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"reflect"

	"github.com/go-kit/kit/log/level"
	"github.com/grafana/loki/pkg/cfg"
	"github.com/grafana/loki/pkg/loki"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/version"
	"github.com/weaveworks/common/logging"
	"github.com/weaveworks/common/tracing"

	"github.com/cortexproject/cortex/pkg/util"
)

func init() {
	prometheus.MustRegister(version.NewCollector("loki"))
}

func main() {
	printVersion := flag.Bool("version", false, "Print this builds version information")
	cfg := loadConfig()
	if *printVersion {
		fmt.Print(version.Print("loki"))
		os.Exit(0)
	}

	// Init the logger which will honor the log level set in cfg.Server
	if reflect.DeepEqual(&cfg.Server.LogLevel, &logging.Level{}) {
		level.Error(util.Logger).Log("msg", "invalid log level")
		os.Exit(1)
	}
	util.InitLogger(&cfg.Server)

	// Setting the environment variable JAEGER_AGENT_HOST enables tracing
	trace := tracing.NewFromEnv(fmt.Sprintf("loki-%s", cfg.Target))
	defer func() {
		if err := trace.Close(); err != nil {
			level.Error(util.Logger).Log("msg", "error closing tracing", "err", err)
			os.Exit(1)
		}
	}()

	// Start Loki
	t, err := loki.New(*cfg)
	if err != nil {
		level.Error(util.Logger).Log("msg", "error initialising loki", "err", err)
		os.Exit(1)
	}

	level.Info(util.Logger).Log("msg", "Starting Loki", "version", version.Info())

	if err := t.Run(); err != nil {
		level.Error(util.Logger).Log("msg", "error running loki", "err", err)
	}

	if err := t.Stop(); err != nil {
		level.Error(util.Logger).Log("msg", "error stopping loki", "err", err)
		os.Exit(1)
	}
}

// loadConfig loads the config from various sources which take precedence over each other
func loadConfig() *loki.Config {
	var config loki.Config

	// defaults shared by FlagDefaultsDangerous and Flags
	var defaults []byte

	// unmarshal config
	if err := cfg.Unmarshal(&config,
		cfg.FlagDefaultsDangerous(&loki.Config{}, &defaults),
		cfg.YAMLFlag(),
		cfg.Flags(&loki.Config{}, defaults),
	); err != nil {
		log.Fatalln(err)
	}

	return &config
}
