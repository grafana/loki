package config

import (
	"fmt"
	"math"
	"strings"

	configv1 "github.com/grafana/loki/operator/apis/config/v1"
	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
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
	GossipRing            Address
	Querier               Address
	IndexGateway          Address
	Ruler                 Ruler
	StorageDirectory      string
	MaxConcurrent         MaxConcurrent
	WriteAheadLog         WriteAheadLog
	EnableRemoteReporting bool

	ObjectStorage storage.Options

	Retention RetentionOptions
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

	RelabelConfigs []RelabelConfig
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

// ReplayMemoryCeiling calculates 50% of the ingester memory
// for the ingester to use for the write-ahead-log capbability.
func (w WriteAheadLog) ReplayMemoryCeiling() string {
	value := int64(math.Ceil(float64(w.IngesterMemoryRequest) * float64(0.5)))
	return fmt.Sprintf("%d", value)
}

// RetentionOptions configures global retention options on the compactor.
type RetentionOptions struct {
	Enabled           bool
	DeleteWorkerCount uint
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
	IndexGateway  string
	Ingester      string
	QueryFrontend string
	Ruler         string
}

type HTTPServerNames struct {
	Compactor string
	Querier   string
}
