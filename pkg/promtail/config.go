package promtail

import (
	"flag"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"

	yaml "gopkg.in/yaml.v2"

	"github.com/prometheus/prometheus/config"
	sd_config "github.com/prometheus/prometheus/discovery/config"
	"github.com/weaveworks/common/server"
)

// Config for promtail, describing what files to watch.
type Config struct {
	ServerConfig    server.Config   `yaml:"server,omitempty"`
	ClientConfig    ClientConfig    `yaml:"client,omitempty"`
	PositionsConfig PositionsConfig `yaml:"positions,omitempty"`
	ScrapeConfig    []ScrapeConfig  `yaml:"scrape_configs,omitempty"`
}

func (c *Config) RegisterFlags(f *flag.FlagSet) {
	c.ServerConfig.RegisterFlags(f)
	c.ClientConfig.RegisterFlags(f)
	c.PositionsConfig.RegisterFlags(f)
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

// ScrapeConfig describes a job to scrape.
type ScrapeConfig struct {
	JobName                string                           `yaml:"job_name,omitempty"`
	EntryParser            EntryParser                      `yaml:"entry_parser"`
	RelabelConfigs         []*config.RelabelConfig          `yaml:"relabel_configs,omitempty"`
	ServiceDiscoveryConfig sd_config.ServiceDiscoveryConfig `yaml:",inline"`
}

var DefaultScrapeConfig = ScrapeConfig{
	EntryParser: Docker,
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (c *ScrapeConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	*c = DefaultScrapeConfig
	type plain ScrapeConfig
	if err := unmarshal((*plain)(c)); err != nil {
		return err
	}
	if len(c.JobName) == 0 {
		return fmt.Errorf("job_name is empty")
	}
	return nil
}

type EntryParser int

const (
	Docker EntryParser = iota
	Raw
)

func (e EntryParser) String() string {
	switch e {
	case Docker:
		return "docker"
	case Raw:
		return "raw"
	default:
		panic(e)
	}
}

func (e *EntryParser) Set(s string) error {
	switch strings.ToLower(s) {
	case "docker":
		*e = Docker
		return nil
	case "raw":
		*e = Raw
		return nil
	default:
		return fmt.Errorf("unrecognised EntryParser: %v", s)
	}
}

func (e *EntryParser) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	return e.Set(s)
}
