package openshift

import (
	rbacv1 "k8s.io/api/rbac/v1"
)

type OptionsClusterScope struct {
	BuildOpts BuildOptionsClusterScope
}

type BuildOptionsClusterScope struct {
	OperatorNs      string
	GatewaySubjects []rbacv1.Subject
	RulerSubjects   []rbacv1.Subject
	Labels          map[string]string
}

func NewOptionsClusterScope(operatorNs string, labels map[string]string, gatewaySubjects []rbacv1.Subject, rulerSubjects []rbacv1.Subject) OptionsClusterScope {
	return OptionsClusterScope{
		BuildOpts: BuildOptionsClusterScope{
			OperatorNs:      operatorNs,
			GatewaySubjects: gatewaySubjects,
			RulerSubjects:   rulerSubjects,
			Labels:          labels,
		},
	}
}
