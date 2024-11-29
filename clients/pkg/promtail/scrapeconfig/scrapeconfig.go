package scrapeconfig

import (
	"fmt"
	"reflect"
	"time"

	"github.com/IBM/sarama"
	"github.com/grafana/dskit/flagext"

	"github.com/grafana/dskit/server"
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
	"github.com/prometheus/prometheus/model/relabel"

	"github.com/grafana/loki/v3/clients/pkg/logentry/stages"
	"github.com/grafana/loki/v3/clients/pkg/promtail/discovery/consulagent"
)

// Config describes a job to scrape.
type Config struct {
	JobName              string                      `mapstructure:"job_name,omitempty" yaml:"job_name,omitempty"`
	PipelineStages       stages.PipelineStages       `mapstructure:"pipeline_stages,omitempty" yaml:"pipeline_stages,omitempty"`
	JournalConfig        *JournalTargetConfig        `mapstructure:"journal,omitempty" yaml:"journal,omitempty"`
	SyslogConfig         *SyslogTargetConfig         `mapstructure:"syslog,omitempty" yaml:"syslog,omitempty"`
	GcplogConfig         *GcplogTargetConfig         `mapstructure:"gcplog,omitempty" yaml:"gcplog,omitempty"`
	PushConfig           *PushTargetConfig           `mapstructure:"loki_push_api,omitempty" yaml:"loki_push_api,omitempty"`
	WindowsConfig        *WindowsEventsTargetConfig  `mapstructure:"windows_events,omitempty" yaml:"windows_events,omitempty"`
	KafkaConfig          *KafkaTargetConfig          `mapstructure:"kafka,omitempty" yaml:"kafka,omitempty"`
	AzureEventHubsConfig *AzureEventHubsTargetConfig `mapstructure:"azure_event_hubs,omitempty" yaml:"azure_event_hubs,omitempty"`
	GelfConfig           *GelfTargetConfig           `mapstructure:"gelf,omitempty" yaml:"gelf,omitempty"`
	CloudflareConfig     *CloudflareConfig           `mapstructure:"cloudflare,omitempty" yaml:"cloudflare,omitempty"`
	HerokuDrainConfig    *HerokuDrainTargetConfig    `mapstructure:"heroku_drain,omitempty" yaml:"heroku_drain,omitempty"`
	RelabelConfigs       []*relabel.Config           `mapstructure:"relabel_configs,omitempty" yaml:"relabel_configs,omitempty"`
	// List of Docker service discovery configurations.
	DockerSDConfigs        []*moby.DockerSDConfig `mapstructure:"docker_sd_configs,omitempty" yaml:"docker_sd_configs,omitempty"`
	ServiceDiscoveryConfig ServiceDiscoveryConfig `mapstructure:",squash" yaml:",inline"`
	Encoding               string                 `mapstructure:"encoding,omitempty" yaml:"encoding,omitempty"`
	DecompressionCfg       *DecompressionConfig   `yaml:"decompression,omitempty"`
}

type DecompressionConfig struct {
	Enabled      bool
	InitialDelay time.Duration `yaml:"initial_delay"`
	Format       string
}

type ServiceDiscoveryConfig struct {
	// List of labeled target groups for this job.
	StaticConfigs discovery.StaticConfig `mapstructure:"static_configs" yaml:"static_configs"`
	// List of DNS service discovery configurations.
	DNSSDConfigs []*dns.SDConfig `mapstructure:"dns_sd_configs,omitempty" yaml:"dns_sd_configs,omitempty"`
	// List of file service discovery configurations.
	FileSDConfigs []*file.SDConfig `mapstructure:"file_sd_configs,omitempty" yaml:"file_sd_configs,omitempty"`
	// List of Consul service discovery configurations.
	ConsulSDConfigs []*consul.SDConfig `mapstructure:"consul_sd_configs,omitempty" yaml:"consul_sd_configs,omitempty"`
	// List of Consul agent service discovery configurations.
	ConsulAgentSDConfigs []*consulagent.SDConfig `mapstructure:"consulagent_sd_configs,omitempty" yaml:"consulagent_sd_configs,omitempty"`
	// List of DigitalOcean service discovery configurations.
	DigitalOceanSDConfigs []*digitalocean.SDConfig `mapstructure:"digitalocean_sd_configs,omitempty" yaml:"digitalocean_sd_configs,omitempty"`
	// List of Docker Swarm service discovery configurations.
	DockerSwarmSDConfigs []*moby.DockerSwarmSDConfig `mapstructure:"dockerswarm_sd_configs,omitempty" yaml:"dockerswarm_sd_configs,omitempty"`
	// List of Serverset service discovery configurations.
	ServersetSDConfigs []*zookeeper.ServersetSDConfig `mapstructure:"serverset_sd_configs,omitempty" yaml:"serverset_sd_configs,omitempty"`
	// NerveSDConfigs is a list of Nerve service discovery configurations.
	NerveSDConfigs []*zookeeper.NerveSDConfig `mapstructure:"nerve_sd_configs,omitempty" yaml:"nerve_sd_configs,omitempty"`
	// MarathonSDConfigs is a list of Marathon service discovery configurations.
	MarathonSDConfigs []*marathon.SDConfig `mapstructure:"marathon_sd_configs,omitempty" yaml:"marathon_sd_configs,omitempty"`
	// List of Kubernetes service discovery configurations.
	KubernetesSDConfigs []*kubernetes.SDConfig `mapstructure:"kubernetes_sd_configs,omitempty" yaml:"kubernetes_sd_configs,omitempty"`
	// List of GCE service discovery configurations.
	GCESDConfigs []*gce.SDConfig `mapstructure:"gce_sd_configs,omitempty" yaml:"gce_sd_configs,omitempty"`
	// List of EC2 service discovery configurations.
	EC2SDConfigs []*aws.EC2SDConfig `mapstructure:"ec2_sd_configs,omitempty" yaml:"ec2_sd_configs,omitempty"`
	// List of OpenStack service discovery configurations.
	OpenstackSDConfigs []*openstack.SDConfig `mapstructure:"openstack_sd_configs,omitempty" yaml:"openstack_sd_configs,omitempty"`
	// List of Azure service discovery configurations.
	AzureSDConfigs []*azure.SDConfig `mapstructure:"azure_sd_configs,omitempty" yaml:"azure_sd_configs,omitempty"`
	// List of Triton service discovery configurations.
	TritonSDConfigs []*triton.SDConfig `mapstructure:"triton_sd_configs,omitempty" yaml:"triton_sd_configs,omitempty"`
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

	// Journal matches to filter. Character (+) is not supported, only logical AND
	// matches will be added.
	Matches string `yaml:"matches"`
}

type SyslogFormat string

const (
	// A modern Syslog RFC
	SyslogFormatRFC5424 = "rfc5424"
	// A legacy Syslog RFC also known as BSD-syslog
	SyslogFormatRFC3164 = "rfc3164"
)

// SyslogTargetConfig describes a scrape config that listens for log lines over syslog.
type SyslogTargetConfig struct {
	// ListenAddress is the address to listen on for syslog messages.
	ListenAddress string `yaml:"listen_address"`

	// ListenProtocol is the protocol used to listen for syslog messages.
	// Must be either `tcp` (default) or `udp`
	ListenProtocol string `yaml:"listen_protocol"`

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

	// UseRFC5424Message defines whether the full RFC5424 formatted syslog
	// message should be pushed to Loki
	UseRFC5424Message bool `yaml:"use_rfc5424_message"`

	// Syslog format used at the target. Acceptable value is rfc5424 or rfc3164.
	// Default is rfc5424.
	SyslogFormat SyslogFormat `yaml:"syslog_format"`

	// MaxMessageLength sets the maximum limit to the length of syslog messages
	MaxMessageLength int `yaml:"max_message_length"`

	TLSConfig promconfig.TLSConfig `yaml:"tls_config,omitempty"`
}

func (config SyslogTargetConfig) IsRFC3164Message() bool {
	return config.SyslogFormat == SyslogFormatRFC3164
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

	// ExcludeEventMessage allows to exclude the human-friendly message contained in each windows event.
	ExcludeEventMessage bool `yaml:"exclude_event_message"`

	// ExcludeUserData allows to exclude the user data of each windows event.
	ExcludeUserData bool `yaml:"exclude_user_data"`

	// Labels optionally holds labels to associate with each log line.
	Labels model.LabelSet `yaml:"labels"`
}

type AzureEventHubsTargetConfig struct {
	// Labels optionally holds labels to associate with each log line.
	Labels model.LabelSet `yaml:"labels"`

	// UseIncomingTimestamp sets the timestamp to the incoming messages
	// timestamp if it's set.
	UseIncomingTimestamp bool `yaml:"use_incoming_timestamp"`

	// Event Hubs to consume (Required).
	EventHubs []string `yaml:"event_hubs"`

	// Event Hubs ConnectionString for authentication on Azure Cloud (Required).
	ConnectionString string `yaml:"connection_string"`

	// Event Hubs namespace host name (Required). Typically, it looks like <your-namespace>.servicebus.windows.net:9093.
	FullyQualifiedNamespace string `yaml:"fully_qualified_namespace"`

	// The consumer group id.
	GroupID string `yaml:"group_id"`

	// Ignore messages that doesn't match schema for Azure resource logs
	DisallowCustomMessages bool `yaml:"disallow_custom_messages"`
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

	// Authentication strategy with Kafka brokers
	Authentication KafkaAuthentication `yaml:"authentication"`
}

// KafkaAuthenticationType specifies method to authenticate with Kafka brokers
type KafkaAuthenticationType string

const (
	// KafkaAuthenticationTypeNone represents using no authentication
	KafkaAuthenticationTypeNone = "none"
	// KafkaAuthenticationTypeSSL represents using SSL/TLS to authenticate
	KafkaAuthenticationTypeSSL = "ssl"
	// KafkaAuthenticationTypeSASL represents using SASL to authenticate
	KafkaAuthenticationTypeSASL = "sasl"
)

// KafkaAuthentication describe the configuration for authentication with Kafka brokers
type KafkaAuthentication struct {
	// Type is authentication type
	// Possible values: none, sasl and ssl (defaults to none).
	Type KafkaAuthenticationType `yaml:"type"`

	// TLSConfig is used for TLS encryption and authentication with Kafka brokers
	TLSConfig promconfig.TLSConfig `yaml:"tls_config,omitempty"`

	// SASLConfig is used for SASL authentication with Kafka brokers
	SASLConfig KafkaSASLConfig `yaml:"sasl_config,omitempty"`
}

// KafkaSASLConfig describe the SASL configuration for authentication with Kafka brokers
type KafkaSASLConfig struct {
	// SASL mechanism. Supports PLAIN, SCRAM-SHA-256 and SCRAM-SHA-512
	Mechanism sarama.SASLMechanism `yaml:"mechanism"`

	// SASL Username
	User string `yaml:"user"`

	// SASL Password for the User
	Password flagext.Secret `yaml:"password"`

	// UseTLS sets whether TLS is used with SASL
	UseTLS bool `yaml:"use_tls"`

	// TLSConfig is used for SASL over TLS. It is used only when UseTLS is true
	TLSConfig promconfig.TLSConfig `yaml:",inline"`
}

// GelfTargetConfig describes a scrape config that read GELF messages on UDP.
type GelfTargetConfig struct {
	// ListenAddress is the address to listen on UDP for gelf messages. (Default to `:12201`)
	ListenAddress string `yaml:"listen_address"`

	// Labels optionally holds labels to associate with each record read from gelf messages.
	Labels model.LabelSet `yaml:"labels"`

	// UseIncomingTimestamp sets the timestamp to the incoming gelf messages
	// timestamp if it's set.
	UseIncomingTimestamp bool `yaml:"use_incoming_timestamp"`
}

type CloudflareConfig struct {
	// APIToken is the API key for the Cloudflare account.
	APIToken string `yaml:"api_token"`
	// ZoneID is the ID of the zone to use.
	ZoneID string `yaml:"zone_id"`
	// Labels optionally holds labels to associate with each record read from cloudflare logs.
	Labels model.LabelSet `yaml:"labels"`
	// The amount of workers to use for parsing cloudflare logs. Default to 3.
	Workers int `yaml:"workers"`
	// The timerange to fetch for each pull request that will be spread across workers. Default 1m.
	PullRange model.Duration `yaml:"pull_range"`
	// Fields to fetch from cloudflare logs.
	// Default to default fields.
	// Available fields type:
	// - default
	// - minimal
	// - extended
	// - all
	// - custom
	FieldsType string `yaml:"fields_type"`
	// The additional list of fields to supplement those provided via fields_type.
	AdditionalFields []string `yaml:"additional_fields"`
}

// GcplogTargetConfig describes a scrape config to pull logs from any pubsub topic.
type GcplogTargetConfig struct {
	// ProjectID is the Cloud project id
	ProjectID string `yaml:"project_id"`

	// Subscription is the subscription name we use to pull logs from a pubsub topic.
	Subscription string `yaml:"subscription"`

	// Labels are the additional labels to be added to log entry while pushing it to Loki server.
	Labels model.LabelSet `yaml:"labels"`

	// UseIncomingTimestamp represents whether to keep the timestamp same as actual log entry coming in or replace it with
	// current timestamp at the time of processing.
	// Its default value(`false`) denotes, replace it with current timestamp at the time of processing.
	UseIncomingTimestamp bool `yaml:"use_incoming_timestamp"`

	// SubscriptionType decides if the target works with a `pull` or `push` subscription type.
	// Defaults to `pull` for backwards compatibility reasons.
	SubscriptionType string `yaml:"subscription_type"`

	// PushTimeout is used to set a maximum processing time for each incoming GCP Logs entry. Used just for `push` subscription type.
	PushTimeout time.Duration `yaml:"push_timeout"`

	// Server is the weaveworks server config for listening connections. Used just for `push` subscription type.
	Server server.Config `yaml:"server"`

	// UseFullLine force Promtail to send the full line from Cloud Logging even if `textPayload` is available.
	// By default, if `textPayload` is present in the line, then it's used as log line.
	UseFullLine bool `yaml:"use_full_line"`
}

// HerokuDrainTargetConfig describes a scrape config to listen and consume heroku logs, in the HTTPS drain manner.
type HerokuDrainTargetConfig struct {
	// Server is the weaveworks server config for listening connections
	Server server.Config `yaml:"server"`

	// Labels optionally holds labels to associate with each record received on the push api.
	Labels model.LabelSet `yaml:"labels"`

	// UseIncomingTimestamp sets the timestamp to the incoming heroku log entry timestamp. If false,
	// promtail will assign the current timestamp to the log entry when it was processed.
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
