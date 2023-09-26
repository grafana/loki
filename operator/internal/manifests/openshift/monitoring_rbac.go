package openshift

import (
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	monitoringServiceAccountName = "prometheus-k8s"
	monitoringNamespace          = "openshift-monitoring"
)

// BuildMonitoringRole returns a Role resource that defines
// list and watch access on pods, services and endpoints.
func BuildMonitoringRole(opts Options) *rbacv1.Role {
	return &rbacv1.Role{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Role",
			APIVersion: rbacv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      monitoringRbacName(opts.BuildOpts.LokiStackName),
			Namespace: opts.BuildOpts.LokiStackNamespace,
			Labels:    opts.BuildOpts.Labels,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"pods", "services", "endpoints"},
				Verbs:     []string{"get", "list", "watch"},
			},
		},
	}
}

// BuildMonitoringRoleBinding returns a RoleBinding resource that binds the
// OpenShift Cluster Monitoring Prometheus service account `prometheus-k8s` to the
// LokiStack namespace to allow discovering LokiStack owned pods, services and endpoints.
func BuildMonitoringRoleBinding(opts Options) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		TypeMeta: metav1.TypeMeta{
			Kind:       "RoleBinding",
			APIVersion: rbacv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      monitoringRbacName(opts.BuildOpts.LokiStackName),
			Namespace: opts.BuildOpts.LokiStackNamespace,
			Labels:    opts.BuildOpts.Labels,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     monitoringRbacName(opts.BuildOpts.LokiStackName),
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      monitoringServiceAccountName,
				Namespace: monitoringNamespace,
			},
		},
	}
}
