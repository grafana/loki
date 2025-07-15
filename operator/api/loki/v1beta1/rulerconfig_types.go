package v1beta1

import (
	v1 "github.com/grafana/loki/operator/api/loki/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
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

// +kubebuilder:object:root=true
// +kubebuilder:unservedversion
// +kubebuilder:subresource:status

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

// ConvertTo RulerConfig this RulerConfig (v1beta1) to the Hub version (v1).
func (src *RulerConfig) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*v1.RulerConfig)

	dst.ObjectMeta = src.ObjectMeta
	dst.Status.Conditions = src.Status.Conditions
	dst.Spec.EvalutionInterval = v1.PrometheusDuration(src.Spec.EvalutionInterval)
	dst.Spec.PollInterval = v1.PrometheusDuration(src.Spec.PollInterval)

	if src.Spec.RemoteWriteSpec != nil {
		dst.Spec.RemoteWriteSpec = &v1.RemoteWriteSpec{
			Enabled:       src.Spec.RemoteWriteSpec.Enabled,
			RefreshPeriod: v1.PrometheusDuration(src.Spec.RemoteWriteSpec.RefreshPeriod),
		}

		if src.Spec.RemoteWriteSpec.QueueSpec != nil {
			dst.Spec.RemoteWriteSpec.QueueSpec = &v1.RemoteWriteClientQueueSpec{
				Capacity:          src.Spec.RemoteWriteSpec.QueueSpec.Capacity,
				MaxShards:         src.Spec.RemoteWriteSpec.QueueSpec.MaxShards,
				MinShards:         src.Spec.RemoteWriteSpec.QueueSpec.MinShards,
				MaxSamplesPerSend: src.Spec.RemoteWriteSpec.QueueSpec.MaxSamplesPerSend,
				BatchSendDeadline: v1.PrometheusDuration(src.Spec.RemoteWriteSpec.QueueSpec.BatchSendDeadline),
				MinBackOffPeriod:  v1.PrometheusDuration(src.Spec.RemoteWriteSpec.QueueSpec.MinBackOffPeriod),
				MaxBackOffPeriod:  v1.PrometheusDuration(src.Spec.RemoteWriteSpec.QueueSpec.MaxBackOffPeriod),
			}
		}

		if src.Spec.RemoteWriteSpec.ClientSpec != nil {
			dst.Spec.RemoteWriteSpec.ClientSpec = &v1.RemoteWriteClientSpec{
				Name:                    src.Spec.RemoteWriteSpec.ClientSpec.Name,
				URL:                     src.Spec.RemoteWriteSpec.ClientSpec.URL,
				Timeout:                 v1.PrometheusDuration(src.Spec.RemoteWriteSpec.ClientSpec.Timeout),
				AuthorizationType:       v1.RemoteWriteAuthType(src.Spec.RemoteWriteSpec.ClientSpec.AuthorizationType),
				AuthorizationSecretName: src.Spec.RemoteWriteSpec.ClientSpec.AuthorizationSecretName,
				AdditionalHeaders:       src.Spec.RemoteWriteSpec.ClientSpec.AdditionalHeaders,
				ProxyURL:                src.Spec.RemoteWriteSpec.ClientSpec.ProxyURL,
				FollowRedirects:         src.Spec.RemoteWriteSpec.ClientSpec.FollowRedirects,
			}

			if src.Spec.RemoteWriteSpec.ClientSpec.RelabelConfigs != nil {
				dst.Spec.RemoteWriteSpec.ClientSpec.RelabelConfigs = make([]v1.RelabelConfig, len(src.Spec.RemoteWriteSpec.ClientSpec.RelabelConfigs))
				for i, c := range src.Spec.RemoteWriteSpec.ClientSpec.RelabelConfigs {
					dst.Spec.RemoteWriteSpec.ClientSpec.RelabelConfigs[i] = v1.RelabelConfig{
						SourceLabels: c.SourceLabels,
						Separator:    c.Separator,
						TargetLabel:  c.TargetLabel,
						Regex:        c.Regex,
						Modulus:      c.Modulus,
						Replacement:  c.Replacement,
						Action:       v1.RelabelActionType(c.Action),
					}
				}
			}
		}
	}

	if src.Spec.AlertManagerSpec != nil {
		dst.Spec.AlertManagerSpec = &v1.AlertManagerSpec{
			ExternalURL:    src.Spec.AlertManagerSpec.ExternalURL,
			ExternalLabels: src.Spec.AlertManagerSpec.ExternalLabels,
			EnableV2:       src.Spec.AlertManagerSpec.EnableV2,
			Endpoints:      src.Spec.AlertManagerSpec.Endpoints,
		}

		if src.Spec.AlertManagerSpec.NotificationQueueSpec != nil {
			dst.Spec.AlertManagerSpec.NotificationQueueSpec = &v1.AlertManagerNotificationQueueSpec{
				Capacity:           src.Spec.AlertManagerSpec.NotificationQueueSpec.Capacity,
				Timeout:            v1.PrometheusDuration(src.Spec.AlertManagerSpec.NotificationQueueSpec.Timeout),
				ForOutageTolerance: v1.PrometheusDuration(src.Spec.AlertManagerSpec.NotificationQueueSpec.ForOutageTolerance),
				ForGracePeriod:     v1.PrometheusDuration(src.Spec.AlertManagerSpec.NotificationQueueSpec.ForGracePeriod),
				ResendDelay:        v1.PrometheusDuration(src.Spec.AlertManagerSpec.NotificationQueueSpec.ResendDelay),
			}
		}

		if src.Spec.AlertManagerSpec.RelabelConfigs != nil {
			dst.Spec.AlertManagerSpec.RelabelConfigs = make([]v1.RelabelConfig, len(src.Spec.AlertManagerSpec.RelabelConfigs))
			for i, rc := range src.Spec.AlertManagerSpec.RelabelConfigs {
				dst.Spec.AlertManagerSpec.RelabelConfigs[i] = v1.RelabelConfig{
					SourceLabels: rc.SourceLabels,
					Separator:    rc.Separator,
					TargetLabel:  rc.TargetLabel,
					Regex:        rc.Regex,
					Modulus:      rc.Modulus,
					Replacement:  rc.Replacement,
					Action:       v1.RelabelActionType(rc.Action),
				}
			}
		}

		if src.Spec.AlertManagerSpec.Client != nil {
			dst.Spec.AlertManagerSpec.Client = &v1.AlertManagerClientConfig{}
			if src.Spec.AlertManagerSpec.Client.BasicAuth != nil {
				dst.Spec.AlertManagerSpec.Client.BasicAuth = &v1.AlertManagerClientBasicAuth{
					Username: src.Spec.AlertManagerSpec.Client.BasicAuth.Username,
					Password: src.Spec.AlertManagerSpec.Client.BasicAuth.Password,
				}
			}
			if src.Spec.AlertManagerSpec.Client.HeaderAuth != nil {
				dst.Spec.AlertManagerSpec.Client.HeaderAuth = &v1.AlertManagerClientHeaderAuth{
					Type:            src.Spec.AlertManagerSpec.Client.HeaderAuth.Type,
					Credentials:     src.Spec.AlertManagerSpec.Client.HeaderAuth.Credentials,
					CredentialsFile: src.Spec.AlertManagerSpec.Client.HeaderAuth.CredentialsFile,
				}
			}

			if src.Spec.AlertManagerSpec.Client.TLS != nil {
				dst.Spec.AlertManagerSpec.Client.TLS = &v1.AlertManagerClientTLSConfig{
					CAPath:     src.Spec.AlertManagerSpec.Client.TLS.CAPath,
					ServerName: src.Spec.AlertManagerSpec.Client.TLS.ServerName,
					CertPath:   src.Spec.AlertManagerSpec.Client.TLS.CertPath,
					KeyPath:    src.Spec.AlertManagerSpec.Client.TLS.KeyPath,
				}
			}
		}

		if src.Spec.AlertManagerSpec.DiscoverySpec != nil {
			dst.Spec.AlertManagerSpec.DiscoverySpec = &v1.AlertManagerDiscoverySpec{
				EnableSRV:       src.Spec.AlertManagerSpec.DiscoverySpec.EnableSRV,
				RefreshInterval: v1.PrometheusDuration(src.Spec.AlertManagerSpec.DiscoverySpec.RefreshInterval),
			}
		}

		if src.Spec.AlertManagerSpec.NotificationQueueSpec != nil {
			dst.Spec.AlertManagerSpec.NotificationQueueSpec = &v1.AlertManagerNotificationQueueSpec{
				Capacity:           src.Spec.AlertManagerSpec.NotificationQueueSpec.Capacity,
				Timeout:            v1.PrometheusDuration(src.Spec.AlertManagerSpec.NotificationQueueSpec.Timeout),
				ForOutageTolerance: v1.PrometheusDuration(src.Spec.AlertManagerSpec.NotificationQueueSpec.ForOutageTolerance),
				ForGracePeriod:     v1.PrometheusDuration(src.Spec.AlertManagerSpec.NotificationQueueSpec.ForGracePeriod),
				ResendDelay:        v1.PrometheusDuration(src.Spec.AlertManagerSpec.NotificationQueueSpec.ResendDelay),
			}
		}

		if src.Spec.AlertManagerSpec.RelabelConfigs != nil {
			dst.Spec.AlertManagerSpec.RelabelConfigs = make([]v1.RelabelConfig, len(src.Spec.AlertManagerSpec.RelabelConfigs))
			for i, rc := range src.Spec.AlertManagerSpec.RelabelConfigs {
				dst.Spec.AlertManagerSpec.RelabelConfigs[i] = v1.RelabelConfig{
					SourceLabels: rc.SourceLabels,
					Separator:    rc.Separator,
					TargetLabel:  rc.TargetLabel,
					Regex:        rc.Regex,
					Modulus:      rc.Modulus,
					Replacement:  rc.Replacement,
					Action:       v1.RelabelActionType(rc.Action),
				}
			}
		}
	}

	if src.Spec.Overrides != nil {
		dst.Spec.Overrides = make(map[string]v1.RulerOverrides)
		for k, v := range src.Spec.Overrides {
			if v.AlertManagerOverrides != nil {
				dst.Spec.Overrides[k] = v1.RulerOverrides{
					AlertManagerOverrides: &v1.AlertManagerSpec{
						ExternalURL:    v.AlertManagerOverrides.ExternalURL,
						ExternalLabels: v.AlertManagerOverrides.ExternalLabels,
						EnableV2:       v.AlertManagerOverrides.EnableV2,
						Endpoints:      v.AlertManagerOverrides.Endpoints,
					},
				}

				if v.AlertManagerOverrides.NotificationQueueSpec != nil {
					dst.Spec.Overrides[k].AlertManagerOverrides.NotificationQueueSpec = &v1.AlertManagerNotificationQueueSpec{
						Capacity:           v.AlertManagerOverrides.NotificationQueueSpec.Capacity,
						Timeout:            v1.PrometheusDuration(v.AlertManagerOverrides.NotificationQueueSpec.Timeout),
						ForOutageTolerance: v1.PrometheusDuration(v.AlertManagerOverrides.NotificationQueueSpec.ForOutageTolerance),
						ForGracePeriod:     v1.PrometheusDuration(v.AlertManagerOverrides.NotificationQueueSpec.ForGracePeriod),
						ResendDelay:        v1.PrometheusDuration(v.AlertManagerOverrides.NotificationQueueSpec.ResendDelay),
					}
				}

				if v.AlertManagerOverrides.RelabelConfigs != nil {
					dst.Spec.Overrides[k].AlertManagerOverrides.RelabelConfigs = make([]v1.RelabelConfig, len(v.AlertManagerOverrides.RelabelConfigs))
					for i, rc := range v.AlertManagerOverrides.RelabelConfigs {
						dst.Spec.Overrides[k].AlertManagerOverrides.RelabelConfigs[i] = v1.RelabelConfig{
							SourceLabels: rc.SourceLabels,
							Separator:    rc.Separator,
							TargetLabel:  rc.TargetLabel,
							Regex:        rc.Regex,
							Modulus:      rc.Modulus,
							Replacement:  rc.Replacement,
							Action:       v1.RelabelActionType(rc.Action),
						}
					}
				}

				if v.AlertManagerOverrides.DiscoverySpec != nil {
					dst.Spec.Overrides[k].AlertManagerOverrides.DiscoverySpec = &v1.AlertManagerDiscoverySpec{
						EnableSRV:       v.AlertManagerOverrides.EnableV2,
						RefreshInterval: v1.PrometheusDuration(v.AlertManagerOverrides.DiscoverySpec.RefreshInterval),
					}
				}

				if v.AlertManagerOverrides.Client != nil {
					dst.Spec.Overrides[k].AlertManagerOverrides.Client = &v1.AlertManagerClientConfig{}
					if v.AlertManagerOverrides.Client.TLS != nil {
						dst.Spec.Overrides[k].AlertManagerOverrides.Client.TLS = &v1.AlertManagerClientTLSConfig{
							CAPath:     v.AlertManagerOverrides.Client.TLS.CAPath,
							ServerName: v.AlertManagerOverrides.Client.TLS.ServerName,
							CertPath:   v.AlertManagerOverrides.Client.TLS.CertPath,
							KeyPath:    v.AlertManagerOverrides.Client.TLS.KeyPath,
						}
					}
					if v.AlertManagerOverrides.Client.BasicAuth != nil {
						dst.Spec.Overrides[k].AlertManagerOverrides.Client.BasicAuth = &v1.AlertManagerClientBasicAuth{
							Username: v.AlertManagerOverrides.Client.BasicAuth.Username,
							Password: v.AlertManagerOverrides.Client.BasicAuth.Password,
						}
					}
					if v.AlertManagerOverrides.Client.HeaderAuth != nil {
						dst.Spec.Overrides[k].AlertManagerOverrides.Client.HeaderAuth = &v1.AlertManagerClientHeaderAuth{
							Type:            v.AlertManagerOverrides.Client.HeaderAuth.Type,
							Credentials:     v.AlertManagerOverrides.Client.HeaderAuth.Credentials,
							CredentialsFile: v.AlertManagerOverrides.Client.HeaderAuth.CredentialsFile,
						}
					}
				}
			}
		}

	}

	return nil
}

// ConvertFrom converts from the Hub version (v1) to this version (v1beta1).
func (dst *RulerConfig) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*v1.RulerConfig)

	dst.ObjectMeta = src.ObjectMeta
	dst.Status.Conditions = src.Status.Conditions
	dst.Spec.EvalutionInterval = PrometheusDuration(src.Spec.EvalutionInterval)
	dst.Spec.PollInterval = PrometheusDuration(src.Spec.PollInterval)

	if src.Spec.RemoteWriteSpec != nil {
		dst.Spec.RemoteWriteSpec = &RemoteWriteSpec{
			Enabled:       src.Spec.RemoteWriteSpec.Enabled,
			RefreshPeriod: PrometheusDuration(src.Spec.RemoteWriteSpec.RefreshPeriod),
		}

		if src.Spec.RemoteWriteSpec.QueueSpec != nil {
			dst.Spec.RemoteWriteSpec.QueueSpec = &RemoteWriteClientQueueSpec{
				Capacity:          src.Spec.RemoteWriteSpec.QueueSpec.Capacity,
				MaxShards:         src.Spec.RemoteWriteSpec.QueueSpec.MaxShards,
				MinShards:         src.Spec.RemoteWriteSpec.QueueSpec.MinShards,
				MaxSamplesPerSend: src.Spec.RemoteWriteSpec.QueueSpec.MaxSamplesPerSend,
				BatchSendDeadline: PrometheusDuration(src.Spec.RemoteWriteSpec.QueueSpec.BatchSendDeadline),
				MinBackOffPeriod:  PrometheusDuration(src.Spec.RemoteWriteSpec.QueueSpec.MinBackOffPeriod),
				MaxBackOffPeriod:  PrometheusDuration(src.Spec.RemoteWriteSpec.QueueSpec.MaxBackOffPeriod),
			}
		}

		if src.Spec.RemoteWriteSpec.ClientSpec != nil {
			dst.Spec.RemoteWriteSpec.ClientSpec = &RemoteWriteClientSpec{
				Name:                    src.Spec.RemoteWriteSpec.ClientSpec.Name,
				URL:                     src.Spec.RemoteWriteSpec.ClientSpec.URL,
				Timeout:                 PrometheusDuration(src.Spec.RemoteWriteSpec.ClientSpec.Timeout),
				AuthorizationType:       RemoteWriteAuthType(src.Spec.RemoteWriteSpec.ClientSpec.AuthorizationType),
				AuthorizationSecretName: src.Spec.RemoteWriteSpec.ClientSpec.AuthorizationSecretName,
				AdditionalHeaders:       src.Spec.RemoteWriteSpec.ClientSpec.AdditionalHeaders,
				ProxyURL:                src.Spec.RemoteWriteSpec.ClientSpec.ProxyURL,
				FollowRedirects:         src.Spec.RemoteWriteSpec.ClientSpec.FollowRedirects,
			}

			if src.Spec.RemoteWriteSpec.ClientSpec.RelabelConfigs != nil {
				dst.Spec.RemoteWriteSpec.ClientSpec.RelabelConfigs = make([]RelabelConfig, len(src.Spec.RemoteWriteSpec.ClientSpec.RelabelConfigs))
				for i, c := range src.Spec.RemoteWriteSpec.ClientSpec.RelabelConfigs {
					dst.Spec.RemoteWriteSpec.ClientSpec.RelabelConfigs[i] = RelabelConfig{
						SourceLabels: c.SourceLabels,
						Separator:    c.Separator,
						TargetLabel:  c.TargetLabel,
						Regex:        c.Regex,
						Modulus:      c.Modulus,
						Replacement:  c.Replacement,
						Action:       RelabelActionType(c.Action),
					}
				}
			}
		}
	}

	if src.Spec.AlertManagerSpec != nil {
		dst.Spec.AlertManagerSpec = &AlertManagerSpec{
			ExternalURL:    src.Spec.AlertManagerSpec.ExternalURL,
			ExternalLabels: src.Spec.AlertManagerSpec.ExternalLabels,
			EnableV2:       src.Spec.AlertManagerSpec.EnableV2,
			Endpoints:      src.Spec.AlertManagerSpec.Endpoints,
		}

		if src.Spec.AlertManagerSpec.NotificationQueueSpec != nil {
			dst.Spec.AlertManagerSpec.NotificationQueueSpec = &AlertManagerNotificationQueueSpec{
				Capacity:           src.Spec.AlertManagerSpec.NotificationQueueSpec.Capacity,
				Timeout:            PrometheusDuration(src.Spec.AlertManagerSpec.NotificationQueueSpec.Timeout),
				ForOutageTolerance: PrometheusDuration(src.Spec.AlertManagerSpec.NotificationQueueSpec.ForOutageTolerance),
				ForGracePeriod:     PrometheusDuration(src.Spec.AlertManagerSpec.NotificationQueueSpec.ForGracePeriod),
				ResendDelay:        PrometheusDuration(src.Spec.AlertManagerSpec.NotificationQueueSpec.ResendDelay),
			}
		}

		if src.Spec.AlertManagerSpec.RelabelConfigs != nil {
			dst.Spec.AlertManagerSpec.RelabelConfigs = make([]RelabelConfig, len(src.Spec.AlertManagerSpec.RelabelConfigs))
			for i, rc := range src.Spec.AlertManagerSpec.RelabelConfigs {
				dst.Spec.AlertManagerSpec.RelabelConfigs[i] = RelabelConfig{
					SourceLabels: rc.SourceLabels,
					Separator:    rc.Separator,
					TargetLabel:  rc.TargetLabel,
					Regex:        rc.Regex,
					Modulus:      rc.Modulus,
					Replacement:  rc.Replacement,
					Action:       RelabelActionType(rc.Action),
				}
			}
		}

		if src.Spec.AlertManagerSpec.Client != nil {
			dst.Spec.AlertManagerSpec.Client = &AlertManagerClientConfig{}
			if src.Spec.AlertManagerSpec.Client.BasicAuth != nil {
				dst.Spec.AlertManagerSpec.Client.BasicAuth = &AlertManagerClientBasicAuth{
					Username: src.Spec.AlertManagerSpec.Client.BasicAuth.Username,
					Password: src.Spec.AlertManagerSpec.Client.BasicAuth.Password,
				}
			}
			if src.Spec.AlertManagerSpec.Client.HeaderAuth != nil {
				dst.Spec.AlertManagerSpec.Client.HeaderAuth = &AlertManagerClientHeaderAuth{
					Type:            src.Spec.AlertManagerSpec.Client.HeaderAuth.Type,
					Credentials:     src.Spec.AlertManagerSpec.Client.HeaderAuth.Credentials,
					CredentialsFile: src.Spec.AlertManagerSpec.Client.HeaderAuth.CredentialsFile,
				}
			}

			if src.Spec.AlertManagerSpec.Client.TLS != nil {
				dst.Spec.AlertManagerSpec.Client.TLS = &AlertManagerClientTLSConfig{
					CAPath:     src.Spec.AlertManagerSpec.Client.TLS.CAPath,
					ServerName: src.Spec.AlertManagerSpec.Client.TLS.ServerName,
					CertPath:   src.Spec.AlertManagerSpec.Client.TLS.CertPath,
					KeyPath:    src.Spec.AlertManagerSpec.Client.TLS.KeyPath,
				}
			}
		}

		if src.Spec.AlertManagerSpec.DiscoverySpec != nil {
			dst.Spec.AlertManagerSpec.DiscoverySpec = &AlertManagerDiscoverySpec{
				EnableSRV:       src.Spec.AlertManagerSpec.DiscoverySpec.EnableSRV,
				RefreshInterval: PrometheusDuration(src.Spec.AlertManagerSpec.DiscoverySpec.RefreshInterval),
			}
		}

		if src.Spec.AlertManagerSpec.NotificationQueueSpec != nil {
			dst.Spec.AlertManagerSpec.NotificationQueueSpec = &AlertManagerNotificationQueueSpec{
				Capacity:           src.Spec.AlertManagerSpec.NotificationQueueSpec.Capacity,
				Timeout:            PrometheusDuration(src.Spec.AlertManagerSpec.NotificationQueueSpec.Timeout),
				ForOutageTolerance: PrometheusDuration(src.Spec.AlertManagerSpec.NotificationQueueSpec.ForOutageTolerance),
				ForGracePeriod:     PrometheusDuration(src.Spec.AlertManagerSpec.NotificationQueueSpec.ForGracePeriod),
				ResendDelay:        PrometheusDuration(src.Spec.AlertManagerSpec.NotificationQueueSpec.ResendDelay),
			}
		}

		if src.Spec.AlertManagerSpec.RelabelConfigs != nil {
			dst.Spec.AlertManagerSpec.RelabelConfigs = make([]RelabelConfig, len(src.Spec.AlertManagerSpec.RelabelConfigs))
			for i, rc := range src.Spec.AlertManagerSpec.RelabelConfigs {
				dst.Spec.AlertManagerSpec.RelabelConfigs[i] = RelabelConfig{
					SourceLabels: rc.SourceLabels,
					Separator:    rc.Separator,
					TargetLabel:  rc.TargetLabel,
					Regex:        rc.Regex,
					Modulus:      rc.Modulus,
					Replacement:  rc.Replacement,
					Action:       RelabelActionType(rc.Action),
				}
			}
		}
	}

	if src.Spec.Overrides != nil {
		dst.Spec.Overrides = make(map[string]RulerOverrides)
		for k, v := range src.Spec.Overrides {
			if v.AlertManagerOverrides != nil {
				dst.Spec.Overrides[k] = RulerOverrides{
					AlertManagerOverrides: &AlertManagerSpec{
						ExternalURL:    v.AlertManagerOverrides.ExternalURL,
						ExternalLabels: v.AlertManagerOverrides.ExternalLabels,
						EnableV2:       v.AlertManagerOverrides.EnableV2,
						Endpoints:      v.AlertManagerOverrides.Endpoints,
					},
				}

				if v.AlertManagerOverrides.NotificationQueueSpec != nil {
					dst.Spec.Overrides[k].AlertManagerOverrides.NotificationQueueSpec = &AlertManagerNotificationQueueSpec{
						Capacity:           v.AlertManagerOverrides.NotificationQueueSpec.Capacity,
						Timeout:            PrometheusDuration(v.AlertManagerOverrides.NotificationQueueSpec.Timeout),
						ForOutageTolerance: PrometheusDuration(v.AlertManagerOverrides.NotificationQueueSpec.ForOutageTolerance),
						ForGracePeriod:     PrometheusDuration(v.AlertManagerOverrides.NotificationQueueSpec.ForGracePeriod),
						ResendDelay:        PrometheusDuration(v.AlertManagerOverrides.NotificationQueueSpec.ResendDelay),
					}
				}

				if v.AlertManagerOverrides.RelabelConfigs != nil {
					dst.Spec.Overrides[k].AlertManagerOverrides.RelabelConfigs = make([]RelabelConfig, len(v.AlertManagerOverrides.RelabelConfigs))
					for i, rc := range v.AlertManagerOverrides.RelabelConfigs {
						dst.Spec.Overrides[k].AlertManagerOverrides.RelabelConfigs[i] = RelabelConfig{
							SourceLabels: rc.SourceLabels,
							Separator:    rc.Separator,
							TargetLabel:  rc.TargetLabel,
							Regex:        rc.Regex,
							Modulus:      rc.Modulus,
							Replacement:  rc.Replacement,
							Action:       RelabelActionType(rc.Action),
						}
					}
				}

				if v.AlertManagerOverrides.DiscoverySpec != nil {
					dst.Spec.Overrides[k].AlertManagerOverrides.DiscoverySpec = &AlertManagerDiscoverySpec{
						EnableSRV:       v.AlertManagerOverrides.EnableV2,
						RefreshInterval: PrometheusDuration(v.AlertManagerOverrides.DiscoverySpec.RefreshInterval),
					}
				}

				if v.AlertManagerOverrides.Client != nil {
					dst.Spec.Overrides[k].AlertManagerOverrides.Client = &AlertManagerClientConfig{}
					if v.AlertManagerOverrides.Client.TLS != nil {
						dst.Spec.Overrides[k].AlertManagerOverrides.Client.TLS = &AlertManagerClientTLSConfig{
							CAPath:     v.AlertManagerOverrides.Client.TLS.CAPath,
							ServerName: v.AlertManagerOverrides.Client.TLS.ServerName,
							CertPath:   v.AlertManagerOverrides.Client.TLS.CertPath,
							KeyPath:    v.AlertManagerOverrides.Client.TLS.KeyPath,
						}
					}
					if v.AlertManagerOverrides.Client.BasicAuth != nil {
						dst.Spec.Overrides[k].AlertManagerOverrides.Client.BasicAuth = &AlertManagerClientBasicAuth{
							Username: v.AlertManagerOverrides.Client.BasicAuth.Username,
							Password: v.AlertManagerOverrides.Client.BasicAuth.Password,
						}
					}
					if v.AlertManagerOverrides.Client.HeaderAuth != nil {
						dst.Spec.Overrides[k].AlertManagerOverrides.Client.HeaderAuth = &AlertManagerClientHeaderAuth{
							Type:            v.AlertManagerOverrides.Client.HeaderAuth.Type,
							Credentials:     v.AlertManagerOverrides.Client.HeaderAuth.Credentials,
							CredentialsFile: v.AlertManagerOverrides.Client.HeaderAuth.CredentialsFile,
						}
					}
				}
			}
		}

	}

	return nil
}
