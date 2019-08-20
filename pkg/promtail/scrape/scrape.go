package scrape

import (
	"fmt"
	"reflect"

	"github.com/prometheus/common/model"

	sd_config "github.com/prometheus/prometheus/discovery/config"
	"github.com/prometheus/prometheus/pkg/relabel"

	"github.com/grafana/loki/pkg/logentry/stages"
	"github.com/grafana/loki/pkg/promtail/api"
)

// Config describes a job to scrape.
type Config struct {
	JobName                string                           `yaml:"job_name,omitempty"`
	EntryParser            api.EntryParser                  `yaml:"entry_parser"`
	PipelineStages         stages.PipelineStages            `yaml:"pipeline_stages,omitempty"`
	JournalConfig          *JournalTargetConfig             `yaml:"journal,omitempty"`
	RelabelConfigs         []*relabel.Config                `yaml:"relabel_configs,omitempty"`
	ServiceDiscoveryConfig sd_config.ServiceDiscoveryConfig `yaml:",inline"`
}

// JournalTargetConfig describes systemd journal records to scrape.
type JournalTargetConfig struct {
	// MaxAge determines the oldest relative time from process start that will
	// be read and sent to Loki. Values like 14h means no entry older than
	// 14h will be read. If unspecified, defaults to 7h.
	//
	// A relative time specified here takes precedence over the saved position;
	// if the cursor is older than the MaxAge value, it will not be used.
	MaxAge string `yaml:"max_age"`

	// Labels optionally holds labels to associate with each record coming out
	// of the journal.
	Labels model.LabelSet `yaml:"labels"`

	// Path to a directory to read journal entries from. Defaults to system path
	// if empty.
	Path string `yaml:"path"`
}

// DefaultScrapeConfig is the default Config.
var DefaultScrapeConfig = Config{
	EntryParser: api.Docker,
}

// HasServiceDiscoveryConfig checks to see if the service discovery used for
// file targets is non-zero.
func (c *Config) HasServiceDiscoveryConfig() bool {
	return !reflect.DeepEqual(c.ServiceDiscoveryConfig, sd_config.ServiceDiscoveryConfig{})
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
