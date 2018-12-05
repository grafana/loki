package main

import (
	"flag"
	"os"

	"github.com/go-kit/kit/log/level"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/flagext"

	"github.com/grafana/loki/pkg/helpers"
	"github.com/grafana/loki/pkg/promtail"
)

func main() {
	var (
		configFile = "docs/promtail-local-config.yaml"
		config     promtail.Config
	)
	flag.StringVar(&configFile, "config.file", "promtail.yml", "The config file.")
	flagext.RegisterFlags(&config)
	flag.Parse()

	util.InitLogger(&config.ServerConfig)

	if configFile != "" {
		if err := helpers.LoadConfig(configFile, &config); err != nil {
			level.Error(util.Logger).Log("msg", "error loading config", "filename", configFile, "err", err)
			os.Exit(1)
		}
	}

	p, err := promtail.New(config)
	if err != nil {
		level.Error(util.Logger).Log("msg", "error creating loki", "error", err)
		os.Exit(1)
	}

	if err := p.Run(); err != nil {
		level.Error(util.Logger).Log("msg", "error starting loki", "error", err)
		os.Exit(1)
	}

	p.Shutdown()
}
