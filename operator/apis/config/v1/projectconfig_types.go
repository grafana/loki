package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	cfg "sigs.k8s.io/controller-runtime/pkg/config/v1alpha1"
)

type BundleType string

const (
	BundleTypeCommunity          BundleType = "community"
	BundleTypeCommunityOpenShift BundleType = "community-openshift"
	BundleTypeOpenShift          BundleType = "openshift"
)

//+kubebuilder:object:root=true

// ProjectConfig is the Schema for the projectconfigs API
type ProjectConfig struct {
	metav1.TypeMeta `json:",inline"`

	// ControllerManagerConfigurationSpec returns the contfigurations for controllers
	cfg.ControllerManagerConfigurationSpec `json:",inline"`

	BundleType BundleType `json:"bundleType"`
}

func (p *ProjectConfig) IsOpenShiftBundle() bool {
	return p.BundleType == BundleTypeCommunityOpenShift || p.BundleType == BundleTypeOpenShift
}

func init() {
	SchemeBuilder.Register(&ProjectConfig{})
}
