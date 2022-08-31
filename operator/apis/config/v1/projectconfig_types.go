package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	cfg "sigs.k8s.io/controller-runtime/pkg/config/v1alpha1"
)

// OpenShiftFeatureGates is the supported set of all operator features gates on OpenShift.
type OpenShiftFeatureGates struct {
	// ServingCertsService enables OpenShift service-ca annotations on Services
	// to use the in-platform CA and generate a TLS cert/key pair per service for
	// in-cluster data-in-transit encryption.
	// More details: https://docs.openshift.com/container-platform/latest/security/certificate_types_descriptions/service-ca-certificates.html
	ServingCertsService bool `json:"servingCertsService,omitempty"`

	// GatewayRoute enables creating an OpenShift Route for the LokiStack
	// gateway to expose the service to public internet access.
	// More details: https://docs.openshift.com/container-platform/latest/networking/understanding-networking.html
	GatewayRoute bool `json:"gatewayRoute,omitempty"`
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

	// LokiStackGateway enables reconciling the reverse-proxy lokistack-gateway
	// component for multi-tenant authentication/authorization traffic control
	// to Loki.
	LokiStackGateway bool `json:"lokiStackGateway,omitempty"`

	// GrafanaLabsUsageReport enables the Grafana Labs usage report for Loki.
	// More details: https://grafana.com/docs/loki/latest/release-notes/v2-5/#usage-reporting
	GrafanaLabsUsageReport bool `json:"grafanaLabsUsageReport,omitempty"`

	// RuntimeSeccompProfile enables the restricted seccomp profile on all
	// Lokistack components.
	RuntimeSeccompProfile bool `json:"runtimeSeccompProfile,omitempty"`

	// LokiStackWebhook enables the LokiStack CR validation and conversion webhooks.
	LokiStackWebhook bool `json:"lokiStackWebhook,omitempty"`
	// AlertingRuleWebhook enables the AlertingRule CR validation webhook.
	AlertingRuleWebhook bool `json:"alertingRuleWebhook,omitempty"`
	// RecordingRuleWebhook enables the RecordingRule CR validation webhook.
	RecordingRuleWebhook bool `json:"recordingRuleWebhook,omitempty"`

	// When DefaultNodeAffinity is enabled the operator will set a default node affinity on all pods.
	// This will limit scheduling of the pods to Nodes with Linux.
	DefaultNodeAffinity bool `json:"defaultNodeAffinity,omitempty"`

	// OpenShift contains a set of feature gates supported only on OpenShift.
	OpenShift OpenShiftFeatureGates `json:"openshift,omitempty"`
}

//+kubebuilder:object:root=true

// ProjectConfig is the Schema for the projectconfigs API
type ProjectConfig struct {
	metav1.TypeMeta `json:",inline"`

	// ControllerManagerConfigurationSpec returns the contfigurations for controllers
	cfg.ControllerManagerConfigurationSpec `json:",inline"`

	Gates FeatureGates `json:"featureGates,omitempty"`
}

func init() {
	SchemeBuilder.Register(&ProjectConfig{})
}
