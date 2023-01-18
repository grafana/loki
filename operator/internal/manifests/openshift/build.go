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
		BuildGatewayClusterRole(opts),
		BuildGatewayClusterRoleBinding(opts),
		BuildMonitoringRole(opts),
		BuildMonitoringRoleBinding(opts),
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
