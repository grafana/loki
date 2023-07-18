package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// LokiStackConfigSpec defines the desired state of LokiStackConfig
type LokiStackConfigSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Foo is an example field of LokiStackConfig. Edit lokistackconfig_types.go to remove/update
	Foo string `json:"foo,omitempty"`
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
