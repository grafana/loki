package manifests

import (
	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests/openshift"
)

type FeatureGate struct {
	Stack       lokiv1.LokiStack
	IsOpenShift bool
	Version     *openshift.OpenShiftVersion
}

func NewFeatureGate(isOpenShift bool, version *openshift.OpenShiftVersion) FeatureGate {
	return FeatureGate{
		IsOpenShift: isOpenShift,
		Version:     version,
	}
}

func (f FeatureGate) NetworkPoliciesEnabled(np lokiv1.NetworkPoliciesType) bool {
	switch np {
	case lokiv1.NetworkPoliciesDisabled:
		return false
	case lokiv1.NetworkPoliciesEnabled:
		return true
	case lokiv1.NetworkPoliciesDefault:
		if f.IsOpenShift {
			return f.Version.IsAtLeast(4, 20)
		}
		return false
	default:
		return false
	}
}
