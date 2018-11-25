package promtail

import (
	"fmt"
	"io/ioutil"
	"path/filepath"

	yaml "gopkg.in/yaml.v2"

	"github.com/prometheus/prometheus/config"
	sd_config "github.com/prometheus/prometheus/discovery/config"
	"github.com/prometheus/prometheus/discovery/targetgroup"
)

// Config for promtail, describing what files to watch.
type Config struct {
	ScrapeConfig []ScrapeConfig `yaml:"scrape_configs,omitempty"`
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
	RelabelConfigs         []*config.RelabelConfig          `yaml:"relabel_configs,omitempty"`
	ServiceDiscoveryConfig sd_config.ServiceDiscoveryConfig `yaml:",inline"`
	StaticConfig           targetgroup.Group                `yaml:"static_config"`
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (c *ScrapeConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type plain ScrapeConfig
	err := unmarshal((*plain)(c))
	if err != nil {
		return err
	}
	if len(c.JobName) == 0 {
		return fmt.Errorf("job_name is empty")
	}
	return nil
}
