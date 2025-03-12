package openshift

import (
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	gatewayName = "lokistack-gateway"
	rulerName   = "lokistack-ruler"
)

func BuildRBAC(opts OptionsClusterScope) ([]client.Object, error) {
	objs := make([]client.Object, 0, 4)
	objs = append(objs, BuildGatewayClusterRole(opts))
	objs = append(objs, BuildGatewayClusterRoleBinding(opts))
	objs = append(objs, BuildRulerClusterRole(opts))
	objs = append(objs, BuildRulerClusterRoleBinding(opts))
	return objs, nil
}

// BuildGatewayClusterRole returns a k8s ClusterRole object for the
// lokistack gateway serviceaccount to allow creating:
//   - TokenReviews to authenticate the user by bearer token.
//   - SubjectAccessReview to authorize the user by bearer token.
//     if having access to read/create logs.
func BuildGatewayClusterRole(opts OptionsClusterScope) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRole",
			APIVersion: rbacv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   authorizerRbacName(gatewayName),
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
func BuildGatewayClusterRoleBinding(opts OptionsClusterScope) *rbacv1.ClusterRoleBinding {
	return &rbacv1.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRoleBinding",
			APIVersion: rbacv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   authorizerRbacName(gatewayName),
			Labels: opts.BuildOpts.Labels,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     authorizerRbacName(gatewayName),
		},
		Subjects: opts.BuildOpts.GatewaySubjects,
	}
}

// BuildRulerClusterRole returns a k8s ClusterRole object for the
// lokistack ruler serviceaccount to allow patching sending alerts to alertmanagers.
func BuildRulerClusterRole(opts OptionsClusterScope) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRole",
			APIVersion: rbacv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   authorizerRbacName(rulerName),
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
func BuildRulerClusterRoleBinding(opts OptionsClusterScope) *rbacv1.ClusterRoleBinding {
	return &rbacv1.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRoleBinding",
			APIVersion: rbacv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   authorizerRbacName(rulerName),
			Labels: opts.BuildOpts.Labels,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     authorizerRbacName(rulerName),
		},
		Subjects: opts.BuildOpts.GatewaySubjects,
	}
}
