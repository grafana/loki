package scrape

import (
	"fmt"

	"github.com/grafana/loki/pkg/promtail/api"
	sd_config "github.com/prometheus/prometheus/discovery/config"
	"github.com/prometheus/prometheus/pkg/relabel"
)

// Config describes a job to scrape.
type Config struct {
	JobName                string                           `yaml:"job_name,omitempty"`
	EntryParser            api.EntryParser                  `yaml:"entry_parser"`
	RelabelConfigs         []*relabel.Config                `yaml:"relabel_configs,omitempty"`
	ServiceDiscoveryConfig sd_config.ServiceDiscoveryConfig `yaml:",inline"`
}

// DefaultScrapeConfig is the default Config.
var DefaultScrapeConfig = Config{
	EntryParser: api.Docker,
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (c *Config) UnmarshalYAML(unmarshal func(interface{}) error) error {
	*c = DefaultScrapeConfig
	type plain Config
	if err := unmarshal((*plain)(c)); err != nil {
		return err
	}
	if len(c.JobName) == 0 {
		return fmt.Errorf("job_name is empty")
	}
	return nil
}
