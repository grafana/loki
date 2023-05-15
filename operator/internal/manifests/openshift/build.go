package openshift

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// BuildGatewayObjects returns a list of auxiliary openshift/k8s objects
// for lokistack gateway deployments on OpenShift.
func BuildGatewayObjects(opts Options) []client.Object {
	return []client.Object{
		BuildRoute(opts),
		BuildGatewayCAConfigMap(opts),
		BuildMonitoringRole(opts),
		BuildMonitoringRoleBinding(opts),
	}
}

// BuildGatewayTenantModeObjects returns a list of auxiliary openshift/k8s objects
// for lokistack gateway deployments on OpenShift for tenant modes:
// - openshift-logging
// - openshift-network
func BuildGatewayTenantModeObjects(opts Options) []client.Object {
	return []client.Object{
		BuildGatewayClusterRole(opts),
		BuildGatewayClusterRoleBinding(opts),
	}
}

// BuildRulerObjects returns a list of auxiliary openshift/k8s objects
// for lokistack ruler deployments on OpenShift.
func BuildRulerObjects(opts Options) []client.Object {
	return []client.Object{
		BuildAlertManagerCAConfigMap(opts),
		BuildRulerServiceAccount(opts),
		BuildRulerClusterRole(opts),
		BuildRulerClusterRoleBinding(opts),
	}
}
