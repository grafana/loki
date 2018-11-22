package main

import (
	"flag"
	"io/ioutil"
	"os"

	"github.com/go-kit/kit/log/level"
	"github.com/grafana/tempo/pkg/tempo"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/flagext"
)

func main() {
	var (
		cfg        tempo.Config
		configFile = ""
	)
	flag.StringVar(&configFile, "config.file", "", "Configuration file to load.")
	flagext.RegisterFlags(&cfg)
	flag.Parse()

	util.InitLogger(&cfg.Server)

	if configFile != "" {
		if err := readConfig(configFile, &cfg); err != nil {
			level.Error(util.Logger).Log("msg", "error loading config", "filename", configFile, "err", err)
			os.Exit(1)
		}
	}

	t, err := tempo.New(cfg)
	if err != nil {
		level.Error(util.Logger).Log("msg", "error initialising module", "err", err)
		os.Exit(1)
	}

	t.Run()
	t.Stop()
}

func readConfig(filename string, cfg *tempo.Config) error {
	f, err := os.Open(filename)
	if err != nil {
		return errors.Wrap(err, "error opening config file")
	}
	defer f.Close()

	buf, err := ioutil.ReadAll(f)
	if err != nil {
		return errors.Wrap(err, "Error reading config file: %v")
	}

	if err := yaml.Unmarshal(buf, &cfg); err != nil {
		return errors.Wrap(err, "Error reading config file: %v")
	}
	return nil
}
