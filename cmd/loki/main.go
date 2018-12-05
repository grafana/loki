package main

import (
	"flag"
	"os"

	"github.com/go-kit/kit/log/level"
	"github.com/grafana/loki/pkg/helpers"
	"github.com/grafana/loki/pkg/loki"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/flagext"
)

func main() {
	var (
		cfg        loki.Config
		configFile = ""
	)
	flag.StringVar(&configFile, "config.file", "", "Configuration file to load.")
	flagext.RegisterFlags(&cfg)
	flag.Parse()

	util.InitLogger(&cfg.Server)

	if configFile != "" {
		if err := helpers.LoadConfig(configFile, &cfg); err != nil {
			level.Error(util.Logger).Log("msg", "error loading config", "filename", configFile, "err", err)
			os.Exit(1)
		}
	}

	t, err := loki.New(cfg)
	if err != nil {
		level.Error(util.Logger).Log("msg", "error initialising loki", "err", err)
		os.Exit(1)
	}

	if err := t.Run(); err != nil {
		level.Error(util.Logger).Log("msg", "error running loki", "err", err)
	}

	if err := t.Stop(); err != nil {
		level.Error(util.Logger).Log("msg", "error stopping loki", "err", err)
		os.Exit(1)
	}
}
