package main

import (
	"flag"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/go-kit/kit/log/level"
	"github.com/grafana/tempo/pkg/helpers"
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
		level.Error(util.Logger).Log("msg", "error initialising tempo", "err", err)
		os.Exit(1)
	}

	if err := t.Run(); err != nil {
		level.Error(util.Logger).Log("msg", "error running tempo", "err", err)
	}

	if err := t.Stop(); err != nil {
		level.Error(util.Logger).Log("msg", "error stopping tempo", "err", err)
		os.Exit(1)
	}
}

func readConfig(filename string, cfg *tempo.Config) error {
	f, err := os.Open(filepath.Clean(filename))
	if err != nil {
		return errors.Wrap(err, "error opening config file")
	}
	defer helpers.LogError("closing config", f.Close)

	buf, err := ioutil.ReadAll(f)
	if err != nil {
		return errors.Wrap(err, "Error reading config file: %v")
	}

	if err := yaml.Unmarshal(buf, &cfg); err != nil {
		return errors.Wrap(err, "Error reading config file: %v")
	}
	return nil
}
