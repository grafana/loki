package config

import (
	"flag"
	"fmt"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"gopkg.in/yaml.v2"

	"github.com/grafana/loki/v3/clients/pkg/promtail/client"
	"github.com/grafana/loki/v3/clients/pkg/promtail/limit"
	"github.com/grafana/loki/v3/clients/pkg/promtail/positions"
	"github.com/grafana/loki/v3/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/v3/clients/pkg/promtail/server"
	"github.com/grafana/loki/v3/clients/pkg/promtail/targets/file"
	"github.com/grafana/loki/v3/clients/pkg/promtail/wal"

	"github.com/grafana/loki/v3/pkg/tracing"
	"github.com/grafana/loki/v3/pkg/util/flagext"
)

// Options contains cross-cutting promtail configurations
type Options struct {
}

// Config for promtail, describing what files to watch.
type Config struct {
	Global       GlobalConfig  `yaml:"global,omitempty"`
	ServerConfig server.Config `yaml:"server,omitempty"`
	// deprecated use ClientConfigs instead
	ClientConfig    client.Config         `yaml:"client,omitempty"`
	ClientConfigs   []client.Config       `yaml:"clients,omitempty"`
	PositionsConfig positions.Config      `yaml:"positions,omitempty"`
	ScrapeConfig    []scrapeconfig.Config `yaml:"scrape_configs,omitempty"`
	TargetConfig    file.Config           `yaml:"target_config,omitempty"`
	LimitsConfig    limit.Config          `yaml:"limits_config,omitempty"`
	Options         Options               `yaml:"options,omitempty"`
	Tracing         tracing.Config        `yaml:"tracing"`
	WAL             wal.Config            `yaml:"wal"`
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (c *Config) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// We want to set c to the defaults and then overwrite it with the input.
	// To make unmarshal fill the plain data struct rather than calling UnmarshalYAML
	// again, we have to hide it using a type indirection.
	type plain Config
	if err := unmarshal((*plain)(c)); err != nil {
		return err
	}

	// Validate unique names.
	jobNames := map[string]struct{}{}
	for _, j := range c.ScrapeConfig {
		if _, ok := jobNames[j.JobName]; ok {
			return fmt.Errorf("found multiple scrape configs with job name %q", j.JobName)
		}
		jobNames[j.JobName] = struct{}{}
	}
	return nil
}

// RegisterFlags with prefix registers flags where every name is prefixed by
// prefix. If prefix is a non-empty string, prefix should end with a period.
func (c *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	c.Global.RegisterFlagsWithPrefix(prefix, f)
	c.ServerConfig.RegisterFlagsWithPrefix(prefix, f)
	c.ClientConfig.RegisterFlagsWithPrefix(prefix, f)
	c.PositionsConfig.RegisterFlagsWithPrefix(prefix, f)
	c.TargetConfig.RegisterFlagsWithPrefix(prefix, f)
	c.LimitsConfig.RegisterFlagsWithPrefix(prefix, f)
	c.Tracing.RegisterFlagsWithPrefix(prefix, f)
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

func (c *Config) Setup(l log.Logger) {
	if c.ClientConfig.URL.URL != nil {
		level.Warn(l).Log("msg", "use of CLI client.* and config file Client block are both deprecated in favour of the config file Clients block and will be removed in a future release")
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

// GlobalConfig holds configuration settings which apply to all targets.
// Individual scrape jobs can override the defaults.
type GlobalConfig struct {
	FileWatch file.WatchConfig `mapstructure:"file_watch_config" yaml:"file_watch_config"`
}

// RegisterFlags with prefix registers flags where every name is prefixed by
// prefix. If prefix is a non-empty string, prefix should end with a period.
func (cfg *GlobalConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	cfg.FileWatch.RegisterFlagsWithPrefix(prefix+"file-watch.", f)
}

// RegisterFlags register flags.
func (cfg *GlobalConfig) RegisterFlags(flags *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("", flags)
}
