package config

import (
	"flag"

	"github.com/grafana/loki/pkg/promtail/client"
	"github.com/grafana/loki/pkg/promtail/positions"
	"github.com/grafana/loki/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/pkg/promtail/server"
	"github.com/grafana/loki/pkg/promtail/targets/file"
)

// Config for promtail, describing what files to watch.
type Config struct {
	ServerConfig server.Config `yaml:"server,omitempty"`
	// deprecated use ClientConfigs instead
	ClientConfig    client.Config         `yaml:"client,omitempty"`
	ClientConfigs   []client.Config       `yaml:"clients,omitempty"`
	PositionsConfig positions.Config      `yaml:"positions,omitempty"`
	ScrapeConfig    []scrapeconfig.Config `yaml:"scrape_configs,omitempty"`
	TargetConfig    file.Config           `yaml:"target_config,omitempty"`
}

// RegisterFlags registers flags.
func (c *Config) RegisterFlags(f *flag.FlagSet) {
	c.ServerConfig.RegisterFlags(f)
	c.ClientConfig.RegisterFlags(f)
	c.PositionsConfig.RegisterFlags(f)
	c.TargetConfig.RegisterFlags(f)
}
