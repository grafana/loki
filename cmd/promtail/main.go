package main

import (
	"flag"
	"os"
	"path"
	"reflect"
	"strings"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/flagext"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/version"
	"github.com/weaveworks/common/logging"

	"github.com/grafana/loki/pkg/promtail"
	"github.com/grafana/loki/pkg/promtail/config"

	"github.com/mitchellh/mapstructure"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

func init() {
	prometheus.MustRegister(version.NewCollector("promtail"))
}

func main() {
	var (
		configFile = "cmd/promtail/promtail-local-config.yaml"
		config     config.Config
	)
	flag.StringVar(&configFile, "config.file", "promtail.yml", "The config file.")
	flagext.RegisterFlags(&config)
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
	pflag.Parse()

	util.InitLogger(&config.ServerConfig.Config)

	if err := viper.BindPFlags(pflag.CommandLine); err != nil {
		level.Error(util.Logger).Log("msg", "error parsing flags", "err", err)
		os.Exit(1)
	}

	configFile = viper.GetString("config.file")
	viper.SetConfigType("yaml")
	viper.SetConfigName(strings.TrimSuffix(path.Base(configFile), path.Ext(configFile)))
	viper.AddConfigPath(path.Dir(configFile))
	if err := viper.ReadInConfig(); err != nil {
		level.Error(util.Logger).Log("msg", "error reading config", "filename", configFile, "err", err)
		os.Exit(1)
	}

	if err := viper.Unmarshal(&config, func(decoderConfig *mapstructure.DecoderConfig) {
		decoderConfig.TagName = "yaml"
	}); err != nil {
		level.Error(util.Logger).Log("msg", "error decoding config", "filename", configFile, "err", err)
		os.Exit(1)
	}

	// Re-init the logger which will now honor a different log level set in ServerConfig.Config
	if reflect.DeepEqual(&config.ServerConfig.Config.LogLevel, &logging.Level{}) {
		level.Error(util.Logger).Log("msg", "invalid log level")
		os.Exit(1)
	}
	util.InitLogger(&config.ServerConfig.Config)

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
