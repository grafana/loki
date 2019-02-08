package config

import (
	"flag"
	"io/ioutil"
	"path/filepath"

	"github.com/grafana/loki/pkg/promtail/scrape"
	"github.com/grafana/loki/pkg/promtail/targets"
	"gopkg.in/yaml.v2"

	"github.com/weaveworks/common/server"

	"github.com/grafana/loki/pkg/promtail/client"
	"github.com/grafana/loki/pkg/promtail/positions"
)

// Config for promtail, describing what files to watch.
type Config struct {
	ServerConfig    server.Config    `yaml:"server,omitempty"`
	ClientConfig    client.Config    `yaml:"client,omitempty"`
	PositionsConfig positions.Config `yaml:"positions,omitempty"`
	ScrapeConfig    []scrape.Config  `yaml:"scrape_configs,omitempty"`
	TargetConfig    targets.Config   `yaml:"target_config,omitempty"`
}

// RegisterFlags registers flags.
func (c *Config) RegisterFlags(f *flag.FlagSet) {
	c.ServerConfig.RegisterFlags(f)
	c.ClientConfig.RegisterFlags(f)
	c.PositionsConfig.RegisterFlags(f)
	c.TargetConfig.RegisterFlags(f)
}

// LoadConfig loads config from a file.
func LoadConfig(filename string) (*Config, error) {
	buf, err := ioutil.ReadFile(filepath.Clean(filename))
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.UnmarshalStrict(buf, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
