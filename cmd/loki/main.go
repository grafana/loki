package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/go-kit/kit/log/level"
	"github.com/grafana/loki/pkg/helpers"
	"github.com/grafana/loki/pkg/loki"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/version"
	"github.com/weaveworks/common/tracing"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/flagext"
)

func init() {
	prometheus.MustRegister(version.NewCollector("loki"))
}

func main() {
	var (
		cfg        loki.Config
		configFile = ""
	)
	flag.StringVar(&configFile, "config.file", "", "Configuration file to load.")
	flagext.RegisterFlags(&cfg)
	flag.Parse()

	// The flags set the EnforceMetricName to be true, but in loki it _should_ be false.
	cfg.LimitsConfig.EnforceMetricName = false

	util.InitLogger(&cfg.Server)

	if configFile != "" {
		if err := helpers.LoadConfig(configFile, &cfg); err != nil {
			level.Error(util.Logger).Log("msg", "error loading config", "filename", configFile, "err", err)
			os.Exit(1)
		}
	}

	// Setting the environment variable JAEGER_AGENT_HOST enables tracing
	trace := tracing.NewFromEnv(fmt.Sprintf("loki-%s", cfg.Target))
	defer func() {
		if err := trace.Close(); err != nil {
			level.Error(util.Logger).Log("msg", "error closing tracing", "err", err)
			os.Exit(1)
		}
	}()

	t, err := loki.New(cfg)
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
