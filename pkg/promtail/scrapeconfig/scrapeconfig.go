package scrapeconfig

import (
	"fmt"
	"reflect"
	"time"

	"github.com/prometheus/common/model"
	"github.com/weaveworks/common/server"

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
	SyslogConfig           *SyslogTargetConfig              `yaml:"syslog,omitempty"`
	PushConfig             *PushTargetConfig                `yaml:"loki_push_api,omitempty"`
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

	// JSON forces the output message of entries read from the journal to be
	// JSON. The message will contain all original fields from the source
	// journal entry.
	JSON bool `yaml:"json"`

	// Labels optionally holds labels to associate with each record coming out
	// of the journal.
	Labels model.LabelSet `yaml:"labels"`

	// Path to a directory to read journal entries from. Defaults to system path
	// if empty.
	Path string `yaml:"path"`
}

// SyslogTargetConfig describes a scrape config that listens for log lines over syslog.
type SyslogTargetConfig struct {
	// ListenAddress is the address to listen on for syslog messages.
	ListenAddress string `yaml:"listen_address"`

	// IdleTimeout is the idle timeout for tcp connections.
	IdleTimeout time.Duration `yaml:"idle_timeout"`

	// LabelStructuredData sets if the structured data part of a syslog message
	// is translated to a label.
	// [example@99999 test="yes"] => {__syslog_message_sd_example_99999_test="yes"}
	LabelStructuredData bool `yaml:"label_structured_data"`

	// Labels optionally holds labels to associate with each record read from syslog.
	Labels model.LabelSet `yaml:"labels"`
}

// PushTargetConfig describes a scrape config that listens for Loki push messages.
type PushTargetConfig struct {
	// Server is the weaveworks server config for listening connections
	Server server.Config `yaml:"server"`

	// Labels optionally holds labels to associate with each record received on the push api.
	Labels model.LabelSet `yaml:"labels"`

	// If promtail should maintain the incoming log timestamp or replace it with the current time.
	KeepTimestamp bool `yaml:"use_incoming_timestamp"`
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
