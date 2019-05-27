package main

import (
	"flag"
	"os"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/flagext"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/version"

	"github.com/grafana/loki/pkg/promtail"
	"github.com/grafana/loki/pkg/promtail/config"
)

var (
	configFile = "cmd/promtail/promtail-local-config.yaml"
	cfg        config.Config
)

func init() {
	prometheus.MustRegister(version.NewCollector("promtail"))
}

func main() {
	flag.StringVar(&configFile, "config.file", "promtail.yml", "The config file.")
	flagext.RegisterFlags(&cfg)
	flag.Parse()

	util.InitLogger(&cfg.ServerConfig.Config)

	if configFile != "" {
		errChan := make(chan error, 1)

		m, err := promtail.InitMaster(configFile, cfg)
		if err != nil {
			errChan <- err
		} else {
			// Re-init the logger which will now honor a different log level set in ServerConfig.Config
			util.InitLogger(&m.Promtail.Cfg.ServerConfig.Config)
			if err := m.Promtail.Run(); err != nil {
				level.Error(util.Logger).Log("msg", "error running promtail", "error", err)
				errChan <- err
			}
			m.Promtail.Shutdown()
			m.Cancel <- struct{}{}
		}

		<-errChan
		m.Promtail.Shutdown()
		os.Exit(1)
	} else {
		level.Error(util.Logger).Log("msg", "config file not found", "error", nil)
		os.Exit(1)
	}
}
