package openshift

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
)

func TestBuild_ServiceAccountRefMatches(t *testing.T) {
	opts, err := NewOptions("abc", "abc", "efgh", "example.com", "abc", "abc", map[string]string{})
	require.NoError(t, err)

	objs := Build(opts)
	sa := objs[1].(*corev1.ServiceAccount)
	rb := objs[3].(*rbacv1.ClusterRoleBinding)

	require.Equal(t, sa.Kind, rb.Subjects[0].Kind)
	require.Equal(t, sa.Name, rb.Subjects[0].Name)
	require.Equal(t, sa.Namespace, rb.Subjects[0].Namespace)
}

func TestBuild_ClusterRoleRefMatches(t *testing.T) {
	opts, err := NewOptions("abc", "abc", "efgh", "example.com", "abc", "abc", map[string]string{})
	require.NoError(t, err)

	objs := Build(opts)
	cr := objs[2].(*rbacv1.ClusterRole)
	rb := objs[3].(*rbacv1.ClusterRoleBinding)

	require.Equal(t, cr.Kind, rb.RoleRef.Kind)
	require.Equal(t, cr.Name, rb.RoleRef.Name)
}

func TestBuild_ServiceAccountAnnotationsRouteRefMatches(t *testing.T) {
	opts, err := NewOptions("abc", "abc", "efgh", "example.com", "abc", "abc", map[string]string{})
	require.NoError(t, err)

	objs := Build(opts)
	rt := objs[0].(*routev1.Route)
	sa := objs[1].(*corev1.ServiceAccount)

	type oauthRedirectReference struct {
		Kind       string `json:"kind"`
		APIVersion string `json:"apiVersion"`
		Ref        *struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"reference"`
	}

	for _, a := range sa.Annotations {
		oauthRef := oauthRedirectReference{}
		err := json.Unmarshal([]byte(a), &oauthRef)
		require.NoError(t, err)

		require.Equal(t, rt.Name, oauthRef.Ref.Name)
		require.Equal(t, rt.Kind, oauthRef.Ref.Kind)
	}
}
