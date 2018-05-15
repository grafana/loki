package promtail

import (
	"fmt"
	"io/ioutil"
	"strings"

	yaml "gopkg.in/yaml.v2"

	"github.com/prometheus/prometheus/config"
	sd_config "github.com/prometheus/prometheus/discovery/config"
)

type Config struct {
	ScrapeConfig []ScrapeConfig `yaml:"scrape_configs,omitempty"`

	// Catches all undefined fields and must be empty after parsing.
	XXX map[string]interface{} `yaml:",inline"`
}

func LoadConfig(filename string) (*Config, error) {
	buf, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(buf, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (c *Config) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type plain Config
	err := unmarshal((*plain)(c))
	if err != nil {
		return err
	}
	if err = checkOverflow(c.XXX, "config"); err != nil {
		return err
	}
	return nil
}

type ScrapeConfig struct {
	JobName                string                           `yaml:"job_name,omitempty"`
	ServiceDiscoveryConfig sd_config.ServiceDiscoveryConfig `yaml:",inline"`
	RelabelConfigs         []*config.RelabelConfig          `yaml:"relabel_configs,omitempty"`

	// Catches all undefined fields and must be empty after parsing.
	XXX map[string]interface{} `yaml:",inline"`
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (c *ScrapeConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type plain ScrapeConfig
	err := unmarshal((*plain)(c))
	if err != nil {
		return err
	}
	if err = checkOverflow(c.XXX, "scrape_config"); err != nil {
		return err
	}
	if len(c.JobName) == 0 {
		return fmt.Errorf("job_name is empty")
	}
	return nil
}

func checkOverflow(m map[string]interface{}, ctx string) error {
	if len(m) > 0 {
		var keys []string
		for k := range m {
			keys = append(keys, k)
		}
		return fmt.Errorf("unknown fields in %s: %s", ctx, strings.Join(keys, ", "))
	}
	return nil
}
