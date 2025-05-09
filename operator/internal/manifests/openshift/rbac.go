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

func BuildRBAC(opts *ClusterScopeOptions) []client.Object {
	objs := make([]client.Object, 0, 2)
	objs = append(objs, buildRulerClusterRole(opts.Labels))
	objs = append(objs, buildRulerClusterRoleBinding(opts.Labels, opts.RulerSubjects))
	return objs
}

func LegacyRBAC(gatewayName, rulerName string) []client.Object {
	objs := make([]client.Object, 0, 4)

	clusterrole := func(name string) *rbacv1.ClusterRole {
		return &rbacv1.ClusterRole{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ClusterRole",
				APIVersion: rbacv1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
		}
	}
	clusterrolebinding := func(name string) *rbacv1.ClusterRoleBinding {
		return &rbacv1.ClusterRoleBinding{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ClusterRoleBinding",
				APIVersion: rbacv1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
		}
	}

	objs = append(objs, clusterrole(authorizerRbacName(gatewayName)))
	objs = append(objs, clusterrolebinding(authorizerRbacName(gatewayName)))
	objs = append(objs, clusterrole(authorizerRbacName(rulerName)))
	objs = append(objs, clusterrolebinding(authorizerRbacName(rulerName)))

	return objs
}

// buildRulerClusterRole returns a k8s ClusterRole object for the
// lokistack ruler serviceaccount to allow patching sending alerts to alertmanagers.
func buildRulerClusterRole(labels map[string]string) *rbacv1.ClusterRole {
	return &rbacv1.ClusterRole{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRole",
			APIVersion: rbacv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   authorizerRbacName(rulerName),
			Labels: labels,
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

// buildRulerClusterRoleBinding returns a k8s ClusterRoleBinding object for
// the lokistack ruler serviceaccount to grant access to alertmanagers.
func buildRulerClusterRoleBinding(labels map[string]string, subjects []rbacv1.Subject) *rbacv1.ClusterRoleBinding {
	return &rbacv1.ClusterRoleBinding{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ClusterRoleBinding",
			APIVersion: rbacv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   authorizerRbacName(rulerName),
			Labels: labels,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     authorizerRbacName(rulerName),
		},
		Subjects: subjects,
	}
}
