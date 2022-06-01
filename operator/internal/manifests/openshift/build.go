package openshift

import "sigs.k8s.io/controller-runtime/pkg/client"

// BuildGatewayObjects returns a list of auxiliary openshift/k8s objects
// for lokistack gateway deployments on OpenShift.
func BuildGatewayObjects(opts Options) []client.Object {
	objs := []client.Object{
		BuildRoute(opts),
		BuildServiceAccount(opts),
		BuildClusterRole(opts),
		BuildClusterRoleBinding(opts),
	}

	if opts.BuildOpts.EnableServiceMonitors {
		objs = append(
			objs,
			BuildMonitoringRole(opts),
			BuildMonitoringRoleBinding(opts),
		)
	}

	return objs
}

// BuildGatewayObjects returns a list of auxiliary openshift/k8s objects
// for loki deployments on OpenShift.
func BuildLokiStackObjects(opts Options) []client.Object {
	objs := []client.Object{}
	if opts.BuildOpts.EnableCertificateSigningService {
		objs = append(objs, BuildServiceCAConfigMap(opts))
	}

	return objs
}
