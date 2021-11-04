package scrapeconfig

import (
	"fmt"
	"reflect"
	"time"

	promconfig "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/discovery"
	"github.com/prometheus/prometheus/discovery/aws"
	"github.com/prometheus/prometheus/discovery/azure"
	"github.com/prometheus/prometheus/discovery/consul"
	"github.com/prometheus/prometheus/discovery/digitalocean"
	"github.com/prometheus/prometheus/discovery/dns"
	"github.com/prometheus/prometheus/discovery/file"
	"github.com/prometheus/prometheus/discovery/gce"
	"github.com/prometheus/prometheus/discovery/kubernetes"
	"github.com/prometheus/prometheus/discovery/marathon"
	"github.com/prometheus/prometheus/discovery/moby"
	"github.com/prometheus/prometheus/discovery/openstack"
	"github.com/prometheus/prometheus/discovery/triton"
	"github.com/prometheus/prometheus/discovery/zookeeper"
	"github.com/prometheus/prometheus/pkg/relabel"
	"github.com/weaveworks/common/server"

	"github.com/grafana/loki/clients/pkg/logentry/stages"
	"github.com/grafana/loki/clients/pkg/promtail/discovery/consulagent"
)

// Config describes a job to scrape.
type Config struct {
	JobName                string                     `yaml:"job_name,omitempty"`
	PipelineStages         stages.PipelineStages      `yaml:"pipeline_stages,omitempty"`
	JournalConfig          *JournalTargetConfig       `yaml:"journal,omitempty"`
	SyslogConfig           *SyslogTargetConfig        `yaml:"syslog,omitempty"`
	GcplogConfig           *GcplogTargetConfig        `yaml:"gcplog,omitempty"`
	PushConfig             *PushTargetConfig          `yaml:"loki_push_api,omitempty"`
	WindowsConfig          *WindowsEventsTargetConfig `yaml:"windows_events,omitempty"`
	KafkaConfig            *KafkaTargetConfig         `yaml:"kafka,omitempty"`
	RelabelConfigs         []*relabel.Config          `yaml:"relabel_configs,omitempty"`
	ServiceDiscoveryConfig ServiceDiscoveryConfig     `yaml:",inline"`
}

type ServiceDiscoveryConfig struct {
	// List of labeled target groups for this job.
	StaticConfigs discovery.StaticConfig `yaml:"static_configs"`
	// List of DNS service discovery configurations.
	DNSSDConfigs []*dns.SDConfig `yaml:"dns_sd_configs,omitempty"`
	// List of file service discovery configurations.
	FileSDConfigs []*file.SDConfig `yaml:"file_sd_configs,omitempty"`
	// List of Consul service discovery configurations.
	ConsulSDConfigs []*consul.SDConfig `yaml:"consul_sd_configs,omitempty"`
	// List of Consul agent service discovery configurations.
	ConsulAgentSDConfigs []*consulagent.SDConfig `yaml:"consulagent_sd_configs,omitempty"`
	// List of DigitalOcean service discovery configurations.
	DigitalOceanSDConfigs []*digitalocean.SDConfig `yaml:"digitalocean_sd_configs,omitempty"`
	// List of Docker Swarm service discovery configurations.
	DockerSwarmSDConfigs []*moby.DockerSwarmSDConfig `yaml:"dockerswarm_sd_configs,omitempty"`
	// List of Serverset service discovery configurations.
	ServersetSDConfigs []*zookeeper.ServersetSDConfig `yaml:"serverset_sd_configs,omitempty"`
	// NerveSDConfigs is a list of Nerve service discovery configurations.
	NerveSDConfigs []*zookeeper.NerveSDConfig `yaml:"nerve_sd_configs,omitempty"`
	// MarathonSDConfigs is a list of Marathon service discovery configurations.
	MarathonSDConfigs []*marathon.SDConfig `yaml:"marathon_sd_configs,omitempty"`
	// List of Kubernetes service discovery configurations.
	KubernetesSDConfigs []*kubernetes.SDConfig `yaml:"kubernetes_sd_configs,omitempty"`
	// List of GCE service discovery configurations.
	GCESDConfigs []*gce.SDConfig `yaml:"gce_sd_configs,omitempty"`
	// List of EC2 service discovery configurations.
	EC2SDConfigs []*aws.EC2SDConfig `yaml:"ec2_sd_configs,omitempty"`
	// List of OpenStack service discovery configurations.
	OpenstackSDConfigs []*openstack.SDConfig `yaml:"openstack_sd_configs,omitempty"`
	// List of Azure service discovery configurations.
	AzureSDConfigs []*azure.SDConfig `yaml:"azure_sd_configs,omitempty"`
	// List of Triton service discovery configurations.
	TritonSDConfigs []*triton.SDConfig `yaml:"triton_sd_configs,omitempty"`
}

func (cfg ServiceDiscoveryConfig) Configs() (res discovery.Configs) {
	if x := cfg.StaticConfigs; len(x) > 0 {
		res = append(res, x)
	}
	for _, x := range cfg.DNSSDConfigs {
		res = append(res, x)
	}
	for _, x := range cfg.FileSDConfigs {
		res = append(res, x)
	}
	for _, x := range cfg.ConsulSDConfigs {
		res = append(res, x)
	}
	for _, x := range cfg.ConsulAgentSDConfigs {
		res = append(res, x)
	}
	for _, x := range cfg.DigitalOceanSDConfigs {
		res = append(res, x)
	}
	for _, x := range cfg.DockerSwarmSDConfigs {
		res = append(res, x)
	}
	for _, x := range cfg.ServersetSDConfigs {
		res = append(res, x)
	}
	for _, x := range cfg.NerveSDConfigs {
		res = append(res, x)
	}
	for _, x := range cfg.MarathonSDConfigs {
		res = append(res, x)
	}
	for _, x := range cfg.KubernetesSDConfigs {
		res = append(res, x)
	}
	for _, x := range cfg.GCESDConfigs {
		res = append(res, x)
	}
	for _, x := range cfg.EC2SDConfigs {
		res = append(res, x)
	}
	for _, x := range cfg.OpenstackSDConfigs {
		res = append(res, x)
	}
	for _, x := range cfg.AzureSDConfigs {
		res = append(res, x)
	}
	for _, x := range cfg.TritonSDConfigs {
		res = append(res, x)
	}
	return res
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

	// UseIncomingTimestamp sets the timestamp to the incoming syslog messages
	// timestamp if it's set.
	UseIncomingTimestamp bool `yaml:"use_incoming_timestamp"`

	// MaxMessageLength sets the maximum limit to the length of syslog messages
	MaxMessageLength int `yaml:"max_message_length"`

	TLSConfig promconfig.TLSConfig `yaml:"tls_config,omitempty"`
}

// WindowsEventsTargetConfig describes a scrape config that listen for windows event logs.
type WindowsEventsTargetConfig struct {

	// LCID (Locale ID) for event rendering
	// - 1033 to force English language
	// -  0 to use default Windows locale
	Locale uint32 `yaml:"locale"`

	// Name of eventlog, used only if xpath_query is empty
	// Example: "Application"
	EventlogName string `yaml:"eventlog_name"`

	// xpath_query can be in defined short form like "Event/System[EventID=999]"
	// or you can form a XML Query. Refer to the Consuming Events article:
	// https://docs.microsoft.com/en-us/windows/win32/wes/consuming-events
	// XML query is the recommended form, because it is most flexible
	// You can create or debug XML Query by creating Custom View in Windows Event Viewer
	// and then copying resulting XML here
	Query string `yaml:"xpath_query"`

	// UseIncomingTimestamp sets the timestamp to the incoming windows messages
	// timestamp if it's set.
	UseIncomingTimestamp bool `yaml:"use_incoming_timestamp"`

	// BookmarkPath sets the bookmark location on the filesystem.
	// The bookmark contains the current position of the target in XML.
	// When restarting or rollingout promtail, the target will continue to scrape events where it left off based on the bookmark position.
	// The position is updated after each entry processed.
	BookmarkPath string `yaml:"bookmark_path"`

	// PollInterval is the interval at which we're looking if new events are available. By default the target will check every 3seconds.
	PollInterval time.Duration `yaml:"poll_interval"`

	// ExcludeEventData allows to exclude the xml event data.
	ExcludeEventData bool `yaml:"exclude_event_data"`

	// ExcludeUserData allows to exclude the user data of each windows event.
	ExcludeUserData bool `yaml:"exclude_user_data"`

	// Labels optionally holds labels to associate with each log line.
	Labels model.LabelSet `yaml:"labels"`
}

type KafkaTargetConfig struct {
	// Labels optionally holds labels to associate with each log line.
	Labels model.LabelSet `yaml:"labels"`

	// UseIncomingTimestamp sets the timestamp to the incoming kafka messages
	// timestamp if it's set.
	UseIncomingTimestamp bool `yaml:"use_incoming_timestamp"`

	// The list of brokers to connect to kafka (Required).
	Brokers []string `yaml:"brokers"`

	// The consumer group id (Required).
	GroupID string `yaml:"group_id"`

	// Kafka Topics to consume (Required).
	Topics []string `yaml:"topics"`

	// Kafka version. Default to 2.2.1
	Version string `yaml:"version"`

	// Rebalancing strategy to use. (e.g sticky, roundrobin or range)
	Assignor string `yaml:"assignor"`
}

// GcplogTargetConfig describes a scrape config to pull logs from any pubsub topic.
type GcplogTargetConfig struct {
	// ProjectID is the Cloud project id
	ProjectID string `yaml:"project_id"`

	// Subscription is the scription name we use to pull logs from a pubsub topic.
	Subscription string `yaml:"subscription"`

	// Labels are the additional labels to be added to log entry while pushing it to Loki server.
	Labels model.LabelSet `yaml:"labels"`

	// UseIncomingTimestamp represents whether to keep the timestamp same as actual log entry coming in or replace it with
	// current timestamp at the time of processing.
	// Its default value(`false`) denotes, replace it with current timestamp at the time of processing.
	UseIncomingTimestamp bool `yaml:"use_incoming_timestamp"`
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
	PipelineStages: stages.PipelineStages{},
}

// HasServiceDiscoveryConfig checks to see if the service discovery used for
// file targets is non-zero.
func (c *Config) HasServiceDiscoveryConfig() bool {
	return !reflect.DeepEqual(c.ServiceDiscoveryConfig, ServiceDiscoveryConfig{})
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
