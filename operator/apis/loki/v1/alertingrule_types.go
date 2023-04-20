package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AlertingRuleSpec defines the desired state of AlertingRule
type AlertingRuleSpec struct {
	// TenantID of tenant where the alerting rules are evaluated in.
	//
	// +required
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Tenant ID"
	TenantID string `json:"tenantID"`

	// List of groups for alerting rules.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Groups"
	Groups []*AlertingRuleGroup `json:"groups"`
}

// AlertingRuleGroup defines a group of Loki alerting rules.
type AlertingRuleGroup struct {
	// Name of the alerting rule group. Must be unique within all alerting rules.
	//
	// +required
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Name"
	Name string `json:"name"`

	// Interval defines the time interval between evaluation of the given
	// alerting rule.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +kubebuilder:default:="1m"
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Evaluation Interval"
	Interval PrometheusDuration `json:"interval"`

	// Limit defines the number of alerts an alerting rule can produce. 0 is no limit.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number",displayName="Limit of firing alerts"
	Limit int32 `json:"limit,omitempty"`

	// Rules defines a list of alerting rules
	//
	// +required
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Rules"
	Rules []*AlertingRuleGroupSpec `json:"rules"`
}

// AlertingRuleGroupSpec defines the spec for a Loki alerting rule.
type AlertingRuleGroupSpec struct {
	// The name of the alert. Must be a valid label value.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Name"
	Alert string `json:"alert,omitempty"`

	// The LogQL expression to evaluate. Every evaluation cycle this is
	// evaluated at the current time, and all resultant time series become
	// pending/firing alerts.
	//
	// +required
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="LogQL Expression"
	Expr string `json:"expr"`

	// Alerts are considered firing once they have been returned for this long.
	// Alerts which have not yet fired for long enough are considered pending.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Firing Threshold"
	For PrometheusDuration `json:"for,omitempty"`

	// Annotations to add to each alert.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Annotations"
	Annotations map[string]string `json:"annotations,omitempty"`

	// Labels to add to each alert.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Labels"
	Labels map[string]string `json:"labels,omitempty"`
}

// AlertingRuleStatus defines the observed state of AlertingRule
type AlertingRuleStatus struct {
	// Conditions of the AlertingRule generation health.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=status,xDescriptors="urn:alm:descriptor:io.kubernetes.conditions"
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:storageversion
//+kubebuilder:webhook:path=/validate-loki-grafana-com-v1-alertingrule,mutating=false,failurePolicy=fail,sideEffects=None,groups=loki.grafana.com,resources=alertingrules,verbs=create;update,versions=v1,name=valertingrule.loki.grafana.com,admissionReviewVersions=v1

// AlertingRule is the Schema for the alertingrules API
//
// +operator-sdk:csv:customresourcedefinitions:displayName="AlertingRule",resources={{LokiStack,v1}}
type AlertingRule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AlertingRuleSpec   `json:"spec,omitempty"`
	Status AlertingRuleStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// AlertingRuleList contains a list of AlertingRule
type AlertingRuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AlertingRule `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AlertingRule{}, &AlertingRuleList{})
}

// Hub declares the v1.AlertingRule as the hub CRD version.
func (*AlertingRule) Hub() {}
