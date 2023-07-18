package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BuiltInCertManagement is the configuration for the built-in facility to generate and rotate
// TLS client and serving certificates for all LokiStack services and internal clients except
// for the lokistack-gateway.
type BuiltInCertManagement struct {
	// Enabled defines to flag to enable/disable built-in certificate management feature gate.
	Enabled bool `json:"enabled,omitempty"`
	// CACertValidity defines the total duration of the CA certificate validity.
	CACertValidity string `json:"caValidity,omitempty"`
	// CACertRefresh defines the duration of the CA certificate validity until a rotation
	// should happen. It can be set up to 80% of CA certificate validity or equal to the
	// CA certificate validity. Latter should be used only for rotating only when expired.
	CACertRefresh string `json:"caRefresh,omitempty"`
	// CertValidity defines the total duration of the validity for all LokiStack certificates.
	CertValidity string `json:"certValidity,omitempty"`
	// CertRefresh defines the duration of the certificate validity until a rotation
	// should happen. It can be set up to 80% of certificate validity or equal to the
	// certificate validity. Latter should be used only for rotating only when expired.
	// The refresh is applied to all LokiStack certificates at once.
	CertRefresh string `json:"certRefresh,omitempty"`
}

// OpenShiftFeatureGates is the supported set of all operator features gates on OpenShift.
type OpenShiftFeatureGates struct {
	// Enabled defines the flag to enable that these feature gates are used against OpenShift Container Platform releases.
	Enabled bool `json:"enabled,omitempty"`

	// ServingCertsService enables OpenShift service-ca annotations on the lokistack-gateway service only
	// to use the in-platform CA and generate a TLS cert/key pair per service for
	// in-cluster data-in-transit encryption.
	// More details: https://docs.openshift.com/container-platform/latest/security/certificate_types_descriptions/service-ca-certificates.html
	ServingCertsService bool `json:"servingCertsService,omitempty"`

	// ClusterTLSPolicy enables usage of TLS policies set in the API Server.
	// More details: https://docs.openshift.com/container-platform/4.11/security/tls-security-profiles.html
	ClusterTLSPolicy bool `json:"clusterTLSPolicy,omitempty"`
}

// FeatureGates is the supported set of all operator feature gates.
type FeatureGates struct {
	// ServiceMonitors enables creating a Prometheus-Operator managed ServiceMonitor
	// resource per LokiStack component.
	ServiceMonitors bool `json:"serviceMonitors,omitempty"`
	// ServiceMonitorTLSEndpoints enables TLS for the ServiceMonitor endpoints.
	ServiceMonitorTLSEndpoints bool `json:"serviceMonitorTlsEndpoints,omitempty"`
	// LokiStackAlerts enables creating Prometheus-Operator managed PrometheusRules
	// for common Loki alerts.
	LokiStackAlerts bool `json:"lokiStackAlerts,omitempty"`

	// HTTPEncryption enables TLS encryption for all HTTP LokiStack services.
	// Each HTTP service requires a secret named as the service with the following data:
	// - `tls.crt`: The TLS server side certificate.
	// - `tls.key`: The TLS key for server-side encryption.
	// In addition each service requires a configmap named as the LokiStack CR with the
	// suffix `-ca-bundle`, e.g. `lokistack-dev-ca-bundle` and the following data:
	// - `service-ca.crt`: The CA signing the service certificate in `tls.crt`.
	HTTPEncryption bool `json:"httpEncryption,omitempty"`
	// GRPCEncryption enables TLS encryption for all GRPC LokiStack services.
	// Each GRPC service requires a secret named as the service with the following data:
	// - `tls.crt`: The TLS server side certificate.
	// - `tls.key`: The TLS key for server-side encryption.
	// In addition each service requires a configmap named as the LokiStack CR with the
	// suffix `-ca-bundle`, e.g. `lokistack-dev-ca-bundle` and the following data:
	// - `service-ca.crt`: The CA signing the service certificate in `tls.crt`.
	GRPCEncryption bool `json:"grpcEncryption,omitempty"`
	// BuiltInCertManagement enables the built-in facility for generating and rotating
	// TLS client and serving certificates for all LokiStack services and internal clients except
	// for the lokistack-gateway, In detail all internal Loki HTTP and GRPC communication is lifted
	// to require mTLS. For the lokistack-gateay you need to provide a secret with or use the `ServingCertsService`
	// on OpenShift:
	// - `tls.crt`: The TLS server side certificate.
	// - `tls.key`: The TLS key for server-side encryption.
	// In addition each service requires a configmap named as the LokiStack CR with the
	// suffix `-ca-bundle`, e.g. `lokistack-dev-ca-bundle` and the following data:
	// - `service-ca.crt`: The CA signing the service certificate in `tls.crt`.
	BuiltInCertManagement BuiltInCertManagement `json:"builtInCertManagement,omitempty"`

	// LokiStackGateway enables reconciling the reverse-proxy lokistack-gateway
	// component for multi-tenant authentication/authorization traffic control
	// to Loki.
	LokiStackGateway bool `json:"lokiStackGateway,omitempty"`

	// GrafanaLabsUsageReport enables the Grafana Labs usage report for Loki.
	// More details: https://grafana.com/docs/loki/latest/release-notes/v2-5/#usage-reporting
	GrafanaLabsUsageReport bool `json:"grafanaLabsUsageReport,omitempty"`

	// RestrictedPodSecurityStandard enables compliance with the restrictive pod security standard.
	// More details: https://kubernetes.io/docs/concepts/security/pod-security-standards/#restricted
	RestrictedPodSecurityStandard bool `json:"restrictedPodSecurityStandard,omitempty"`

	// When DefaultNodeAffinity is enabled the operator will set a default node affinity on all pods.
	// This will limit scheduling of the pods to Nodes with Linux.
	DefaultNodeAffinity bool `json:"defaultNodeAffinity,omitempty"`

	// OpenShift contains a set of feature gates supported only on OpenShift.
	OpenShift OpenShiftFeatureGates `json:"openshift,omitempty"`

	// TLSProfile allows to chose a TLS security profile. Enforced
	// when using HTTPEncryption or GRPCEncryption.
	TLSProfile string `json:"tlsProfile,omitempty"`
}

// TLSProfileType is a TLS security profile based on the Mozilla definitions:
// https://wiki.mozilla.org/Security/Server_Side_TLS
type TLSProfileType string

const (
	// TLSProfileOldType is a TLS security profile based on:
	// https://wiki.mozilla.org/Security/Server_Side_TLS#Old_backward_compatibility
	TLSProfileOldType TLSProfileType = "Old"
	// TLSProfileIntermediateType is a TLS security profile based on:
	// https://wiki.mozilla.org/Security/Server_Side_TLS#Intermediate_compatibility_.28default.29
	TLSProfileIntermediateType TLSProfileType = "Intermediate"
	// TLSProfileModernType is a TLS security profile based on:
	// https://wiki.mozilla.org/Security/Server_Side_TLS#Modern_compatibility
	TLSProfileModernType TLSProfileType = "Modern"
)

// LokiStackConfigSpec defines the desired state of LokiStackConfig
type LokiStackConfigSpec struct {
	Gates FeatureGates `json:"featureGates,omitempty"`
}

// LokiStackConfigStatus defines the observed state of LokiStackConfig
type LokiStackConfigStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster

// LokiStackConfig is the Schema for the lokistackconfigs API
type LokiStackConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   LokiStackConfigSpec   `json:"spec,omitempty"`
	Status LokiStackConfigStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// LokiStackConfigList contains a list of LokiStackConfig
type LokiStackConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LokiStackConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&LokiStackConfig{}, &LokiStackConfigList{})
}
