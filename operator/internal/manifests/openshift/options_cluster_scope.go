package openshift

import (
	rbacv1 "k8s.io/api/rbac/v1"
)

type ClusterScopeOptions struct {
	OperatorNs    string
	RulerSubjects []rbacv1.Subject
	Labels        map[string]string
}

func NewOptionsClusterScope(operatorNs string, labels map[string]string, rulerSubjects []rbacv1.Subject) *ClusterScopeOptions {
	return &ClusterScopeOptions{
		OperatorNs:    operatorNs,
		RulerSubjects: rulerSubjects,
		Labels:        labels,
	}
}
