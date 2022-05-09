package config

import (
	"fmt"
	"math"
	"strings"

	lokiv1beta1 "github.com/grafana/loki/operator/api/v1beta1"
	"github.com/grafana/loki/operator/internal/manifests/storage"
)

// Options is used to render the loki-config.yaml file template
type Options struct {
	Stack lokiv1beta1.LokiStackSpec

	Namespace             string
	Name                  string
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
}

// Address FQDN and port for a k8s service.
type Address struct {
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
}

// RemoteWriteConfig for ruler remote write config
type RemoteWriteConfig struct {
	Enabled        bool
	RefreshPeriod  string
	Client         *RemoteWriteClientConfig
	Queue          *RemoteWriteQueueConfig
	RelabelConfigs []RemoteWriteRelabelConfig
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
	HeaderBearerToken string
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

// RemoteWriteRelabelConfig for ruler remote write relabel configs.
type RemoteWriteRelabelConfig struct {
	SourceLabels []string
	Separator    string
	TargetLabel  string
	Regex        string
	Modulus      uint64
	Replacement  string
	Action       string
}

// SourceLabelsString returns a string array of source labels.
func (r RemoteWriteRelabelConfig) SourceLabelsString() string {
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
func (r RemoteWriteRelabelConfig) SeparatorString() string {
	if r.Separator == "" {
		return ";"
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
