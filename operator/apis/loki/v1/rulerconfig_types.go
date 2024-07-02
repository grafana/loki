package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AlertManagerDiscoverySpec defines the configuration to use DNS resolution for AlertManager hosts.
type AlertManagerDiscoverySpec struct {
	// Use DNS SRV records to discover Alertmanager hosts.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Enable SRV"
	EnableSRV bool `json:"enableSRV"`

	// How long to wait between refreshing DNS resolutions of Alertmanager hosts.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +kubebuilder:default:="1m"
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Refresh Interval"
	RefreshInterval PrometheusDuration `json:"refreshInterval,omitempty"`
}

// AlertManagerNotificationQueueSpec defines the configuration for AlertManager notification settings.
type AlertManagerNotificationQueueSpec struct {
	// Capacity of the queue for notifications to be sent to the Alertmanager.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +kubebuilder:default:=10000
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number",displayName="Notification Queue Capacity"
	Capacity int32 `json:"capacity,omitempty"`

	// HTTP timeout duration when sending notifications to the Alertmanager.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +kubebuilder:default:="10s"
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Timeout"
	Timeout PrometheusDuration `json:"timeout,omitempty"`

	// Max time to tolerate outage for restoring "for" state of alert.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +kubebuilder:default:="1h"
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Outage Tolerance"
	ForOutageTolerance PrometheusDuration `json:"forOutageTolerance,omitempty"`

	// Minimum duration between alert and restored "for" state. This is maintained
	// only for alerts with configured "for" time greater than the grace period.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +kubebuilder:default:="10m"
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Firing Grace Period"
	ForGracePeriod PrometheusDuration `json:"forGracePeriod,omitempty"`

	// Minimum amount of time to wait before resending an alert to Alertmanager.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +kubebuilder:default:="1m"
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Resend Delay"
	ResendDelay PrometheusDuration `json:"resendDelay,omitempty"`
}

// AlertManagerSpec defines the configuration for ruler's alertmanager connectivity.
type AlertManagerSpec struct {
	// URL for alerts return path.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Alert External URL"
	ExternalURL string `json:"externalUrl,omitempty"`

	// Additional labels to add to all alerts.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Extra Alert Labels"
	ExternalLabels map[string]string `json:"externalLabels,omitempty"`

	// If enabled, then requests to Alertmanager use the v2 API.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch",displayName="Enable AlertManager V2 API"
	EnableV2 bool `json:"enableV2"`

	// List of AlertManager URLs to send notifications to. Each Alertmanager URL is treated as
	// a separate group in the configuration. Multiple Alertmanagers in HA per group can be
	// supported by using DNS resolution (See EnableDNSDiscovery).
	//
	// +required
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="AlertManager Endpoints"
	Endpoints []string `json:"endpoints"`

	// Defines the configuration for DNS-based discovery of AlertManager hosts.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:advanced",displayName="DNS Discovery"
	DiscoverySpec *AlertManagerDiscoverySpec `json:"discovery,omitempty"`

	// Defines the configuration for the notification queue to AlertManager hosts.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:advanced",displayName="Notification Queue"
	NotificationQueueSpec *AlertManagerNotificationQueueSpec `json:"notificationQueue,omitempty"`

	// List of alert relabel configurations.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Alert Relabel Configuration"
	RelabelConfigs []RelabelConfig `json:"relabelConfigs,omitempty"`

	// Client configuration for reaching the alertmanager endpoint.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="TLS Config"
	Client *AlertManagerClientConfig `json:"client,omitempty"`
}

// AlertManagerClientConfig defines the client configuration for reaching alertmanager endpoints.
type AlertManagerClientConfig struct {
	// TLS configuration for reaching the alertmanager endpoints.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:advanced",displayName="TLS"
	TLS *AlertManagerClientTLSConfig `json:"tls,omitempty"`

	// Header authentication configuration for reaching the alertmanager endpoints.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:advanced",displayName="Header Authentication"
	HeaderAuth *AlertManagerClientHeaderAuth `json:"headerAuth,omitempty"`

	// Basic authentication configuration for reaching the alertmanager endpoints.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:advanced",displayName="Basic Authentication"
	BasicAuth *AlertManagerClientBasicAuth `json:"basicAuth,omitempty"`
}

// AlertManagerClientBasicAuth defines the basic authentication configuration for reaching alertmanager endpoints.
type AlertManagerClientBasicAuth struct {
	// The subject's username for the basic authentication configuration.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Username"
	Username *string `json:"username,omitempty"`

	// The subject's password for the basic authentication configuration.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Password"
	Password *string `json:"password,omitempty"`
}

// AlertManagerClientHeaderAuth defines the header configuration reaching alertmanager endpoints.
type AlertManagerClientHeaderAuth struct {
	// The authentication type for the header authentication configuration.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Type"
	Type *string `json:"type,omitempty"`

	// The credentials for the header authentication configuration.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Credentials"
	Credentials *string `json:"credentials,omitempty"`

	// The credentials file for the Header authentication configuration. It is mutually exclusive with `credentials`.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Credentials File"
	CredentialsFile *string `json:"credentialsFile,omitempty"`
}

// AlertManagerClientTLSConfig defines the TLS configuration for reaching alertmanager endpoints.
type AlertManagerClientTLSConfig struct {
	// The CA certificate file path for the TLS configuration.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="CA Path"
	CAPath *string `json:"caPath,omitempty"`

	// The server name to validate in the alertmanager server certificates.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Server Name"
	ServerName *string `json:"serverName,omitempty"`

	// The client-side certificate file path for the TLS configuration.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Cert Path"
	CertPath *string `json:"certPath,omitempty"`

	// The client-side key file path for the TLS configuration.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Key Path"
	KeyPath *string `json:"keyPath,omitempty"`

	// Skip validating server certificate.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Skip validating server certificate"
	InsecureSkipVerify *bool `json:"insecureSkipVerify,omitempty"`
}

// RemoteWriteAuthType defines the type of authorization to use to access the remote write endpoint.
//
// +kubebuilder:validation:Enum=basic;header
type RemoteWriteAuthType string

const (
	// BasicAuthorization defines the remote write client to use HTTP basic authorization.
	BasicAuthorization RemoteWriteAuthType = "basic"
	// BearerAuthorization defines the remote write client to use HTTP bearer authorization.
	BearerAuthorization RemoteWriteAuthType = "bearer"
)

// RemoteWriteClientSpec defines the configuration of the remote write client.
type RemoteWriteClientSpec struct {
	// Name of the remote write config, which if specified must be unique among remote write configs.
	//
	// +required
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Name"
	Name string `json:"name"`

	// The URL of the endpoint to send samples to.
	//
	// +required
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Endpoint"
	URL string `json:"url"`

	// Timeout for requests to the remote write endpoint.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +kubebuilder:default:="30s"
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Remote Write Timeout"
	Timeout PrometheusDuration `json:"timeout,omitempty"`

	// Type of authorzation to use to access the remote write endpoint
	//
	// +required
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:select:basic","urn:alm:descriptor:com.tectonic.ui:select:header"},displayName="Authorization Type"
	AuthorizationType RemoteWriteAuthType `json:"authorization"`

	// Name of a secret in the namespace configured for authorization secrets.
	//
	// +required
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:io.kubernetes:Secret",displayName="Authorization Secret Name"
	AuthorizationSecretName string `json:"authorizationSecretName"`

	// Additional HTTP headers to be sent along with each remote write request.
	//
	// +optional
	// +kubebuilder:validation:Optional
	AdditionalHeaders map[string]string `json:"additionalHeaders,omitempty"`

	// List of remote write relabel configurations.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Metric Relabel Configuration"
	RelabelConfigs []RelabelConfig `json:"relabelConfigs,omitempty"`

	// Optional proxy URL.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="HTTP Proxy URL"
	ProxyURL string `json:"proxyUrl,omitempty"`

	// Configure whether HTTP requests follow HTTP 3xx redirects.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +kubebuilder:default:=true
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch",displayName="Follow HTTP Redirects"
	FollowRedirects bool `json:"followRedirects"`
}

// RelabelActionType defines the enumeration type for RelabelConfig actions.
//
// +kubebuilder:validation:Enum=drop;hashmod;keep;labeldrop;labelkeep;labelmap;replace
type RelabelActionType string

// RelabelConfig allows dynamic rewriting of the label set, being applied to samples before ingestion.
// It defines `<metric_relabel_configs>` and `<alert_relabel_configs>` sections of Prometheus configuration.
// More info: https://prometheus.io/docs/prometheus/latest/configuration/configuration/#metric_relabel_configs
type RelabelConfig struct {
	// The source labels select values from existing labels. Their content is concatenated
	// using the configured separator and matched against the configured regular expression
	// for the replace, keep, and drop actions.
	//
	// +required
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Source Labels"
	SourceLabels []string `json:"sourceLabels"`

	// Separator placed between concatenated source label values. default is ';'.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +kubebuilder:default:=";"
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Separator"
	Separator string `json:"separator,omitempty"`

	// Label to which the resulting value is written in a replace action.
	// It is mandatory for replace actions. Regex capture groups are available.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Target Label"
	TargetLabel string `json:"targetLabel,omitempty"`

	// Regular expression against which the extracted value is matched. Default is '(.*)'
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +kubebuilder:default:="(.*)"
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Regex"
	Regex string `json:"regex,omitempty"`

	// Modulus to take of the hash of the source label values.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Modulus"
	Modulus uint64 `json:"modulus,omitempty"`

	// Replacement value against which a regex replace is performed if the
	// regular expression matches. Regex capture groups are available. Default is '$1'
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +kubebuilder:default:="$1"
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Replacement"
	Replacement string `json:"replacement,omitempty"`

	// Action to perform based on regex matching. Default is 'replace'
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +kubebuilder:default:="replace"
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Action"
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:select:drop","urn:alm:descriptor:com.tectonic.ui:select:hashmod","urn:alm:descriptor:com.tectonic.ui:select:keep","urn:alm:descriptor:com.tectonic.ui:select:labeldrop","urn:alm:descriptor:com.tectonic.ui:select:labelkeep","urn:alm:descriptor:com.tectonic.ui:select:labelmap","urn:alm:descriptor:com.tectonic.ui:select:replace"},displayName="Action"
	Action RelabelActionType `json:"action,omitempty"`
}

// RemoteWriteClientQueueSpec defines the configuration of the remote write client queue.
type RemoteWriteClientQueueSpec struct {
	// Number of samples to buffer per shard before we block reading of more
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +kubebuilder:default:=2500
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number",displayName="Queue Capacity"
	Capacity int32 `json:"capacity,omitempty"`

	// Maximum number of shards, i.e. amount of concurrency.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +kubebuilder:default:=200
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number",displayName="Maximum Shards"
	MaxShards int32 `json:"maxShards,omitempty"`

	// Minimum number of shards, i.e. amount of concurrency.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +kubebuilder:default:=200
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number",displayName="Minimum Shards"
	MinShards int32 `json:"minShards,omitempty"`

	// Maximum number of samples per send.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +kubebuilder:default:=500
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number",displayName="Maximum Shards per Send"
	MaxSamplesPerSend int32 `json:"maxSamplesPerSend,omitempty"`

	// Maximum time a sample will wait in buffer.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +kubebuilder:default:="5s"
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Batch Send Deadline"
	BatchSendDeadline PrometheusDuration `json:"batchSendDeadline,omitempty"`

	// Initial retry delay. Gets doubled for every retry.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +kubebuilder:default:="30ms"
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Min BackOff Period"
	MinBackOffPeriod PrometheusDuration `json:"minBackOffPeriod,omitempty"`

	// Maximum retry delay.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +kubebuilder:default:="100ms"
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Max BackOff Period"
	MaxBackOffPeriod PrometheusDuration `json:"maxBackOffPeriod,omitempty"`
}

// RemoteWriteSpec defines the configuration for ruler's remote_write connectivity.
type RemoteWriteSpec struct {
	// Enable remote-write functionality.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch",displayName="Enabled"
	Enabled bool `json:"enabled,omitempty"`

	// Minimum period to wait between refreshing remote-write reconfigurations.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +kubebuilder:default:="10s"
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Min Refresh Period"
	RefreshPeriod PrometheusDuration `json:"refreshPeriod,omitempty"`

	// Defines the configuration for remote write client.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:advanced",displayName="Client"
	ClientSpec *RemoteWriteClientSpec `json:"client,omitempty"`

	// Defines the configuration for remote write client queue.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:advanced",displayName="Client Queue"
	QueueSpec *RemoteWriteClientQueueSpec `json:"queue,omitempty"`
}

// RulerConfigSpec defines the desired state of Ruler
type RulerConfigSpec struct {
	// Interval on how frequently to evaluate rules.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +kubebuilder:default:="1m"
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Evaluation Interval"
	EvalutionInterval PrometheusDuration `json:"evaluationInterval,omitempty"`

	// Interval on how frequently to poll for new rule definitions.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +kubebuilder:default:="1m"
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Poll Interval"
	PollInterval PrometheusDuration `json:"pollInterval,omitempty"`

	// Defines alert manager configuration to notify on firing alerts.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:advanced",displayName="Alert Manager Configuration"
	AlertManagerSpec *AlertManagerSpec `json:"alertmanager,omitempty"`

	// Defines a remote write endpoint to write recording rule metrics.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:advanced",displayName="Remote Write Configuration"
	RemoteWriteSpec *RemoteWriteSpec `json:"remoteWrite,omitempty"`

	// Overrides defines the config overrides to be applied per-tenant.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:advanced",displayName="Rate Limiting"
	Overrides map[string]RulerOverrides `json:"overrides,omitempty"`
}

// RulerOverrides defines the overrides applied per-tenant.
type RulerOverrides struct {
	// AlertManagerOverrides defines the overrides to apply to the alertmanager config.
	//
	// +optional
	// +kubebuilder:validation:Optional
	AlertManagerOverrides *AlertManagerSpec `json:"alertmanager,omitempty"`
}

// RulerConfigStatus defines the observed state of RulerConfig
type RulerConfigStatus struct {
	// Conditions of the RulerConfig health.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=status,xDescriptors="urn:alm:descriptor:io.kubernetes.conditions"
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:storageversion
//+kubebuilder:webhook:path=/validate-loki-grafana-com-v1-rulerconfig,mutating=false,failurePolicy=fail,sideEffects=None,groups=loki.grafana.com,resources=rulerconfigs,verbs=create;update,versions=v1,name=vrulerconfig.loki.grafana.com,admissionReviewVersions=v1

// RulerConfig is the Schema for the rulerconfigs API
//
// +operator-sdk:csv:customresourcedefinitions:displayName="RulerConfig",resources={{LokiStack,v1}}
type RulerConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RulerConfigSpec   `json:"spec,omitempty"`
	Status RulerConfigStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// RulerConfigList contains a list of RuleConfig
type RulerConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RulerConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RulerConfig{}, &RulerConfigList{})
}

// Hub declares the v1.RulerConfig as the hub CRD version.
func (*RulerConfig) Hub() {}
