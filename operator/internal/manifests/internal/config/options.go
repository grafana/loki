package config

import (
	"fmt"
	"math"
	"strings"
	"time"

	configv1 "github.com/grafana/loki/operator/api/config/v1"
	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests/storage"
)

// Options is used to render the loki-config.yaml file template
type Options struct {
	Stack lokiv1.LokiStackSpec
	Gates configv1.FeatureGates
	TLS   TLSOptions

	Namespace             string
	Name                  string
	Compactor             Address
	FrontendWorker        Address
	GossipRing            GossipRing
	Querier               Address
	IndexGateway          Address
	Ruler                 Ruler
	StorageDirectory      string
	MaxConcurrent         MaxConcurrent
	WriteAheadLog         WriteAheadLog
	EnableRemoteReporting bool
	DiscoverLogLevels     bool
	Shippers              []string

	ObjectStorage storage.Options

	HTTPTimeouts HTTPTimeoutConfig

	Retention RetentionOptions

	OTLPAttributes OTLPAttributeConfig

	Overrides map[string]LokiOverrides
}

type LokiOverrides struct {
	Limits lokiv1.PerTenantLimitsTemplateSpec
	Ruler  RulerOverrides
}

type RulerOverrides struct {
	AlertManager *AlertManagerConfig
}

// Address FQDN and port for a k8s service.
type Address struct {
	// Protocol is optional
	Protocol string
	// FQDN is required
	FQDN string
	// Port is required
	Port int
}

// GossipRing defines the memberlist configuration
type GossipRing struct {
	// EnableIPv6 is optional, memberlist IPv6 support
	EnableIPv6 bool
	// InstanceAddr is optional, defaults to private networks
	InstanceAddr string
	// InstancePort is required
	InstancePort int
	// BindPort is the port for listening to gossip messages
	BindPort int
	// MembersDiscoveryAddr is required
	MembersDiscoveryAddr           string
	EnableInstanceAvailabilityZone bool
}

// HTTPTimeoutConfig defines the HTTP server config options.
type HTTPTimeoutConfig struct {
	IdleTimeout  time.Duration
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

// Ruler configuration
type Ruler struct {
	Enabled               bool
	RulesStorageDirectory string
	EvaluationInterval    string
	PollInterval          string
	AlertManager          *AlertManagerConfig
	RemoteWrite           *RemoteWriteConfig
}

// AlertManagerConfig for ruler alertmanager config
type AlertManagerConfig struct {
	Hosts          string
	ExternalURL    string
	ExternalLabels map[string]string
	EnableV2       bool

	// DNS Discovery
	EnableDiscovery bool
	RefreshInterval string

	// Notification config
	QueueCapacity      int32
	Timeout            string
	ForOutageTolerance string
	ForGracePeriod     string
	ResendDelay        string

	Notifier *NotifierConfig

	RelabelConfigs []RelabelConfig
}

type NotifierConfig struct {
	TLS        TLSConfig
	BasicAuth  BasicAuth
	HeaderAuth HeaderAuth
}

type BasicAuth struct {
	Username *string
	Password *string
}

type HeaderAuth struct {
	Type            *string
	Credentials     *string
	CredentialsFile *string
}

type TLSConfig struct {
	CertPath           *string
	KeyPath            *string
	CAPath             *string
	ServerName         *string
	InsecureSkipVerify *bool
	CipherSuites       *string
	MinVersion         *string
}

// RemoteWriteConfig for ruler remote write config
type RemoteWriteConfig struct {
	Enabled        bool
	RefreshPeriod  string
	Client         *RemoteWriteClientConfig
	Queue          *RemoteWriteQueueConfig
	RelabelConfigs []RelabelConfig
}

// RemoteWriteClientConfig for ruler remote write client config
type RemoteWriteClientConfig struct {
	Name            string
	URL             string
	RemoteTimeout   string
	Headers         map[string]string
	ProxyURL        string
	FollowRedirects bool

	// Authentication
	BasicAuthUsername string
	BasicAuthPassword string
	BearerToken       string
}

// RemoteWriteQueueConfig for ruler remote write queue config
type RemoteWriteQueueConfig struct {
	Capacity          int32
	MaxShards         int32
	MinShards         int32
	MaxSamplesPerSend int32
	BatchSendDeadline string
	MinBackOffPeriod  string
	MaxBackOffPeriod  string
}

// RelabelConfig for ruler remote write relabel configs.
type RelabelConfig struct {
	SourceLabels []string
	Separator    string
	TargetLabel  string
	Regex        string
	Modulus      uint64
	Replacement  string
	Action       string
}

// SourceLabelsString returns a string array of source labels.
func (r RelabelConfig) SourceLabelsString() string {
	var sb strings.Builder
	sb.WriteString("[")
	for i, labelname := range r.SourceLabels {
		sb.WriteString(fmt.Sprintf(`"%s"`, labelname))

		if i != len(r.SourceLabels)-1 {
			sb.WriteString(",")
		}
	}
	sb.WriteString("]")

	return sb.String()
}

// SeparatorString returns the user-defined separator or per default semicolon.
func (r RelabelConfig) SeparatorString() string {
	if r.Separator == "" {
		return `""`
	}

	return r.Separator
}

// MaxConcurrent for concurrent query processing.
type MaxConcurrent struct {
	AvailableQuerierCPUCores int32
}

// WriteAheadLog for ingester processing
type WriteAheadLog struct {
	Directory             string
	IngesterMemoryRequest int64
}

const (
	// minimumReplayCeiling contains the minimum value that will be used for the replay_memory_ceiling.
	// It is set, so that even when the ingester has a low memory request, the replay will not flush each block
	// on its own.
	minimumReplayCeiling = 512 * 1024 * 1024
)

// ReplayMemoryCeiling calculates 50% of the ingester memory
// for the ingester to use for the write-ahead-log capbability.
func (w WriteAheadLog) ReplayMemoryCeiling() string {
	value := int64(math.Ceil(float64(w.IngesterMemoryRequest) * float64(0.5)))
	if value < minimumReplayCeiling {
		value = minimumReplayCeiling
	}
	return fmt.Sprintf("%d", value)
}

// RetentionOptions configures global retention options on the compactor.
type RetentionOptions struct {
	Enabled           bool
	DeleteWorkerCount uint
}

// OTLPAttributeConfig contains the rendered OTLP label configuration.
// This is both influenced by the tenancy mode and the custom OTLP configuration on the LokiStack and might
// contain more labels than the user has configured if some labels are deemed "required".
type OTLPAttributeConfig struct {
	RemoveDefaultLabels bool
	Global              *OTLPTenantAttributeConfig
	Tenants             map[string]*OTLPTenantAttributeConfig
}

type OTLPTenantAttributeConfig struct {
	ResourceAttributes []OTLPAttribute
	ScopeAttributes    []OTLPAttribute
	LogAttributes      []OTLPAttribute
}

type OTLPAttributeAction string

const (
	OTLPAttributeActionStreamLabel OTLPAttributeAction = "index_label"
	OTLPAttributeActionMetadata    OTLPAttributeAction = "structured_metadata"
)

type OTLPAttribute struct {
	Action OTLPAttributeAction
	Names  []string
	Regex  string
}

type TLSOptions struct {
	Ciphers       []string
	MinTLSVersion string
	Paths         TLSFilePaths
	ServerNames   TLSServerNames
}

func (o TLSOptions) CipherSuitesString() string {
	return strings.Join(o.Ciphers, ",")
}

type TLSFilePaths struct {
	CA   string
	GRPC TLSCertPath
	HTTP TLSCertPath
}

type TLSCertPath struct {
	Certificate string
	Key         string
}

type TLSServerNames struct {
	GRPC GRPCServerNames
	HTTP HTTPServerNames
}

type GRPCServerNames struct {
	Compactor     string
	IndexGateway  string
	Ingester      string
	QueryFrontend string
	Ruler         string
}

type HTTPServerNames struct {
	Querier string
}
