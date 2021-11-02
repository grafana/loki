package config

import (
	"flag"
	"fmt"

	yaml "gopkg.in/yaml.v2"

	"github.com/grafana/loki/clients/pkg/promtail/client"
	"github.com/grafana/loki/clients/pkg/promtail/positions"
	"github.com/grafana/loki/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/clients/pkg/promtail/server"
	"github.com/grafana/loki/clients/pkg/promtail/targets/file"

	"github.com/grafana/loki/pkg/util/flagext"
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

// RegisterFlags with prefix registers flags where every name is prefixed by
// prefix. If prefix is a non-empty string, prefix should end with a period.
func (c *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	c.ServerConfig.RegisterFlagsWithPrefix(prefix, f)
	c.ClientConfig.RegisterFlagsWithPrefix(prefix, f)
	c.PositionsConfig.RegisterFlagsWithPrefix(prefix, f)
	c.TargetConfig.RegisterFlagsWithPrefix(prefix, f)
}

// RegisterFlags registers flags.
func (c *Config) RegisterFlags(f *flag.FlagSet) {
	c.RegisterFlagsWithPrefix("", f)
}

func (c Config) String() string {
	b, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Sprintf("<error creating config string: %s>", err)
	}
	return string(b)
}

func (c *Config) Setup() {
	if c.ClientConfig.URL.URL != nil {
		// if a single client config is used we add it to the multiple client config for backward compatibility
		c.ClientConfigs = append(c.ClientConfigs, c.ClientConfig)
	}

	// This is a bit crude but if the Loki Push API target is specified,
	// force the log level to match the promtail log level
	for i := range c.ScrapeConfig {
		if c.ScrapeConfig[i].PushConfig != nil {
			c.ScrapeConfig[i].PushConfig.Server.LogLevel = c.ServerConfig.LogLevel
			c.ScrapeConfig[i].PushConfig.Server.LogFormat = c.ServerConfig.LogFormat
		}
	}

	// Merge the provided external labels from the single client config/command line with each client config from
	// `clients`. This is done to allow --client.external-labels=key=value passed at command line to apply to all clients
	// The order here is specified to allow the yaml to override the command line flag if there are any labels
	// which exist in both the command line arguments as well as the yaml, and while this is
	// not typically the order of precedence, the assumption here is someone providing a specific config in
	// yaml is doing so explicitly to make a key specific to a client.
	if len(c.ClientConfig.ExternalLabels.LabelSet) > 0 {
		for i := range c.ClientConfigs {
			c.ClientConfigs[i].ExternalLabels = flagext.LabelSet{LabelSet: c.ClientConfig.ExternalLabels.LabelSet.Merge(c.ClientConfigs[i].ExternalLabels.LabelSet)}
		}
	}
}
