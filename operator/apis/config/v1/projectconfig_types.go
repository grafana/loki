package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	cfg "sigs.k8s.io/controller-runtime/pkg/config/v1alpha1"
)

// FeatureFlags is a set of operator feature flags.
type FeatureFlags struct {
	EnableCertificateSigningService bool `json:"enableCertSigningService,omitempty"`
	EnableServiceMonitors           bool `json:"enableServiceMonitors,omitempty"`
	EnableTLSHTTPServices           bool `json:"enableTlsHttpServices,omitempty"`
	EnableTLSServiceMonitorConfig   bool `json:"enableTlsServiceMonitorConfig,omitempty"`
	EnableTLSGRPCServices           bool `json:"enableTlsGrpcServices,omitempty"`
	EnablePrometheusAlerts          bool `json:"enableLokiStackAlerts,omitempty"`
	EnableGateway                   bool `json:"enableLokiStackGateway,omitempty"`
	EnableGatewayRoute              bool `json:"enableLokiStackGatewayRoute,omitempty"`
	EnableGrafanaLabsStats          bool `json:"enableGrafanaLabsStats,omitempty"`
	EnableLokiStackWebhook          bool `json:"enableLokiStackWebhook,omitempty"`
	EnableAlertingRuleWebhook       bool `json:"enableAlertingRuleWebhook,omitempty"`
	EnableRecordingRuleWebhook      bool `json:"enableRecordingRuleWebhook,omitempty"`
	EnableRuntimeSeccompProfile     bool `json:"enableRuntimeSeccompProfile,omitempty"`
}

//+kubebuilder:object:root=true

// ProjectConfig is the Schema for the projectconfigs API
type ProjectConfig struct {
	metav1.TypeMeta `json:",inline"`

	// ControllerManagerConfigurationSpec returns the contfigurations for controllers
	cfg.ControllerManagerConfigurationSpec `json:",inline"`

	Flags FeatureFlags `json:"featureFlags,omitempty"`
}

func init() {
	SchemeBuilder.Register(&ProjectConfig{})
}
