package scrape

import (
	"fmt"
	"github.com/grafana/loki/pkg/promtail/concat"
	sd_config "github.com/prometheus/prometheus/discovery/config"
	"github.com/prometheus/prometheus/pkg/relabel"

	"github.com/grafana/loki/pkg/promtail/api"
)

// Config describes a job to scrape.
type Config struct {
	JobName                string                           `yaml:"job_name,omitempty"`
	EntryParser            api.EntryParser                  `yaml:"entry_parser"`
	RelabelConfigs         []*relabel.Config                `yaml:"relabel_configs,omitempty"`
	ServiceDiscoveryConfig sd_config.ServiceDiscoveryConfig `yaml:",inline"`
	ConcatConfig           concat.Config					`yaml:"concat_config"`
}

// DefaultScrapeConfig is the default Config.
var DefaultScrapeConfig = Config{
	EntryParser:  api.Docker,
	ConcatConfig: concat.Config {
		MultilineStartRegexpString: "",
		Timeout: 0,
	},
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
