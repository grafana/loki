package openshift

import (
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BuildGatewayClusterRole returns a k8s ClusterRole object for the
// lokistack gateway serviceaccount to allow creating:
//   - TokenReviews to authenticate the user by bearer token.
//   - SubjectAccessReview to authorize the user by bearer token.
//     if having access to read/create logs.
func BuildGatewayClusterRole(opts Options) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRole",
			APIVersion: rbacv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   authorizerRbacName(opts.BuildOpts.GatewayName),
			Labels: opts.BuildOpts.Labels,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{
					"authentication.k8s.io",
				},
				Resources: []string{
					"tokenreviews",
				},
				Verbs: []string{
					"create",
				},
			},
			{
				APIGroups: []string{
					"authorization.k8s.io",
				},
				Resources: []string{
					"subjectaccessreviews",
				},
				Verbs: []string{
					"create",
				},
			},
		},
	}
}

// BuildGatewayClusterRoleBinding returns a k8s ClusterRoleBinding object for
// the lokistack gateway serviceaccount to grant access to:
// - rbac.authentication.k8s.io/TokenReviews
// - rbac.authorization.k8s.io/SubjectAccessReviews
func BuildGatewayClusterRoleBinding(opts Options) *rbacv1.ClusterRoleBinding {
	return &rbacv1.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRoleBinding",
			APIVersion: rbacv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   authorizerRbacName(opts.BuildOpts.GatewayName),
			Labels: opts.BuildOpts.Labels,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     authorizerRbacName(opts.BuildOpts.GatewayName),
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      gatewayServiceAccountName(opts),
				Namespace: opts.BuildOpts.LokiStackNamespace,
			},
		},
	}
}

// BuildRulerClusterRole returns a k8s ClusterRole object for the
// lokistack ruler serviceaccount to allow patching sending alerts to alertmanagers.
func BuildRulerClusterRole(opts Options) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRole",
			APIVersion: rbacv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   authorizerRbacName(opts.BuildOpts.RulerName),
			Labels: opts.BuildOpts.Labels,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{
					"monitoring.coreos.com",
				},
				Resources: []string{
					"alertmanagers",
				},
				Verbs: []string{
					"patch",
				},
			},
			{
				NonResourceURLs: []string{
					"/api/v2/alerts",
				},
				Verbs: []string{
					"create",
				},
			},
			{
				APIGroups: []string{
					"monitoring.coreos.com",
				},
				Resources: []string{
					"alertmanagers/api",
				},
				Verbs: []string{
					"create",
				},
			},
		},
	}
}

// BuildRulerClusterRoleBinding returns a k8s ClusterRoleBinding object for
// the lokistack ruler serviceaccount to grant access to alertmanagers.
func BuildRulerClusterRoleBinding(opts Options) *rbacv1.ClusterRoleBinding {
	return &rbacv1.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRoleBinding",
			APIVersion: rbacv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   authorizerRbacName(opts.BuildOpts.RulerName),
			Labels: opts.BuildOpts.Labels,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     authorizerRbacName(opts.BuildOpts.RulerName),
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      rulerServiceAccountName(opts),
				Namespace: opts.BuildOpts.LokiStackNamespace,
			},
		},
	}
}
