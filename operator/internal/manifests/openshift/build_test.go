package openshift

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	routev1 "github.com/openshift/api/route/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
)

func TestBuildGatewayObjects_ServiceAccountRefMatches(t *testing.T) {
	opts := NewOptions(lokiv1.OpenshiftLogging, "abc", "ns", "abc", "example.com", "abc", "abc", map[string]string{}, map[string]TenantData{}, "abc")

	objs := BuildGatewayObjects(opts)
	sa := objs[1].(*corev1.ServiceAccount)
	rb := objs[3].(*rbacv1.ClusterRoleBinding)

	require.Equal(t, sa.Kind, rb.Subjects[0].Kind)
	require.Equal(t, sa.Name, rb.Subjects[0].Name)
	require.Equal(t, sa.Namespace, rb.Subjects[0].Namespace)
}

func TestBuildGatewayObjects_ClusterRoleRefMatches(t *testing.T) {
	opts := NewOptions(lokiv1.OpenshiftLogging, "abc", "ns", "abc", "example.com", "abc", "abc", map[string]string{}, map[string]TenantData{}, "abc")

	objs := BuildGatewayObjects(opts)
	cr := objs[2].(*rbacv1.ClusterRole)
	rb := objs[3].(*rbacv1.ClusterRoleBinding)

	require.Equal(t, cr.Kind, rb.RoleRef.Kind)
	require.Equal(t, cr.Name, rb.RoleRef.Name)
}

func TestBuildGatewayObjects_MonitoringClusterRoleRefMatches(t *testing.T) {
	opts := NewOptions(lokiv1.OpenshiftLogging, "abc", "ns", "abc", "example.com", "abc", "abc", map[string]string{}, map[string]TenantData{}, "abc")

	objs := BuildGatewayObjects(opts)
	cr := objs[4].(*rbacv1.Role)
	rb := objs[5].(*rbacv1.RoleBinding)

	require.Equal(t, cr.Kind, rb.RoleRef.Kind)
	require.Equal(t, cr.Name, rb.RoleRef.Name)
}

func TestBuildGatewayObjects_ServiceAccountAnnotationsRouteRefMatches(t *testing.T) {
	opts := NewOptions(lokiv1.OpenshiftLogging, "abc", "ns", "abc", "example.com", "abc", "abc", map[string]string{}, map[string]TenantData{}, "abc")

	objs := BuildGatewayObjects(opts)
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

func TestBuildRulerObjects(t *testing.T) {
	opts := NewOptions(lokiv1.OpenshiftLogging, "abc", "ns", "abc", "example.com", "abc", "abc", map[string]string{}, map[string]TenantData{}, "abc")

	objs := BuildRulerObjects(opts)
	sa := objs[0].(*corev1.ServiceAccount)
	cr := objs[1].(*rbacv1.ClusterRole)
	rb := objs[2].(*rbacv1.ClusterRoleBinding)

	require.Equal(t, sa.Kind, rb.Subjects[0].Kind)
	require.Equal(t, sa.Name, rb.Subjects[0].Name)
	require.Equal(t, sa.Namespace, rb.Subjects[0].Namespace)
	require.Equal(t, cr.Kind, rb.RoleRef.Kind)
	require.Equal(t, cr.Name, rb.RoleRef.Name)
}
