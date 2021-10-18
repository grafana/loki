package openshift

import "sigs.k8s.io/controller-runtime/pkg/client"

// Build returns a list of auxiliary openshift/k8s objects
// for lokistack gateway deployments on OpenShift.
func Build(opts Options) []client.Object {
	return []client.Object{
		BuildRoute(opts),
		BuildServiceAccount(opts),
		BuildClusterRole(opts),
		BuildClusterRoleBinding(opts),
	}
}
