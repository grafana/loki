package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RecordingRuleSpec defines the desired state of RecordingRule
type RecordingRuleSpec struct {
	// TenantID of tenant where the recording rules are evaluated in.
	//
	// +required
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Tenant ID"
	TenantID string `json:"tenantID"`

	// List of groups for recording rules.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Groups"
	Groups []*RecordingRuleGroup `json:"groups"`
}

// RecordingRuleGroup defines a group of Loki  recording rules.
type RecordingRuleGroup struct {
	// Name of the recording rule group. Must be unique within all recording rules.
	//
	// +required
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Name"
	Name string `json:"name"`

	// Interval defines the time interval between evaluation of the given
	// recoding rule.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +kubebuilder:default:="1m"
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Evaluation Interval"
	Interval PrometheusDuration `json:"interval"`

	// Limit defines the number of series a recording rule can produce. 0 is no limit.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number",displayName="Limit of produced series"
	Limit int32 `json:"limit,omitempty"`

	// Rules defines a list of recording rules
	//
	// +required
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Rules"
	Rules []*RecordingRuleGroupSpec `json:"rules"`
}

// RecordingRuleGroupSpec defines the spec for a Loki recording rule.
type RecordingRuleGroupSpec struct {
	// The name of the time series to output to. Must be a valid metric name.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Metric Name"
	Record string `json:"record,omitempty"`

	// The LogQL expression to evaluate. Every evaluation cycle this is
	// evaluated at the current time, and all resultant time series become
	// pending/firing alerts.
	//
	// +required
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="LogQL Expression"
	Expr string `json:"expr"`

	// Labels to add to each recording rule.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Labels"
	Labels map[string]string `json:"labels,omitempty"`
}

// RecordingRuleStatus defines the observed state of RecordingRule
type RecordingRuleStatus struct {
	// Conditions of the RecordingRule generation health.
	//
	// +optional
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=status,xDescriptors="urn:alm:descriptor:io.kubernetes.conditions"
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:storageversion
//+kubebuilder:webhook:path=/validate-loki-grafana-com-v1-recordingrule,mutating=false,failurePolicy=fail,sideEffects=None,groups=loki.grafana.com,resources=recordingrules,verbs=create;update,versions=v1,name=vrecordingrule.loki.grafana.com,admissionReviewVersions=v1

// RecordingRule is the Schema for the recordingrules API
//
// +operator-sdk:csv:customresourcedefinitions:displayName="RecordingRule",resources={{LokiStack,v1}}
type RecordingRule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RecordingRuleSpec   `json:"spec,omitempty"`
	Status RecordingRuleStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// RecordingRuleList contains a list of RecordingRule
type RecordingRuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RecordingRule `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RecordingRule{}, &RecordingRuleList{})
}

// Hub declares the v1.RecordingRule as the hub CRD version.
func (*RecordingRule) Hub() {}
