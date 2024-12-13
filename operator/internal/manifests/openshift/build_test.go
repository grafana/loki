package openshift

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
)

func TestBuildGatewayTenantModeObjects_ClusterRoleRefMatches(t *testing.T) {
	opts := NewOptions("abc", "ns", "abc", "abc", "abc", 1*time.Minute, map[string]string{}, "abc").
		WithTenantsForMode(lokiv1.OpenshiftLogging, "example.com", map[string]TenantData{})

	objs := BuildGatewayTenantModeObjects(*opts)
	cr := objs[0].(*rbacv1.ClusterRole)
	rb := objs[1].(*rbacv1.ClusterRoleBinding)

	require.Equal(t, cr.Kind, rb.RoleRef.Kind)
	require.Equal(t, cr.Name, rb.RoleRef.Name)
}

func TestBuildGatewayObjects_MonitoringClusterRoleRefMatches(t *testing.T) {
	opts := NewOptions("abc", "ns", "abc", "abc", "abc", 1*time.Minute, map[string]string{}, "abc")

	objs := BuildGatewayObjects(*opts)
	cr := objs[2].(*rbacv1.Role)
	rb := objs[3].(*rbacv1.RoleBinding)

	require.Equal(t, cr.Kind, rb.RoleRef.Kind)
	require.Equal(t, cr.Name, rb.RoleRef.Name)
}

func TestBuildGatewayObjets_RouteWithTimeoutAnnotation(t *testing.T) {
	gwWriteTimeout := 1 * time.Minute
	opts := NewOptions("abc", "ns", "abc", "abc", "abc", gwWriteTimeout, map[string]string{}, "abc")

	objs := BuildGatewayObjects(*opts)
	a := objs[0].GetAnnotations()

	got, ok := a[annotationGatewayRouteTimeout]
	require.True(t, ok)

	routeTimeout := gwWriteTimeout + gatewayRouteTimeoutExtension
	want := fmt.Sprintf("%.fs", routeTimeout.Seconds())
	require.Equal(t, want, got)
}

func TestBuildRulerObjects_ClusterRoleRefMatches(t *testing.T) {
	opts := NewOptions("abc", "ns", "abc", "abc", "abc", 1*time.Minute, map[string]string{}, "abc")

	objs := BuildRulerObjects(*opts)
	sa := objs[1].(*corev1.ServiceAccount)
	cr := objs[2].(*rbacv1.ClusterRole)
	rb := objs[3].(*rbacv1.ClusterRoleBinding)

	require.Equal(t, sa.Kind, rb.Subjects[0].Kind)
	require.Equal(t, sa.Name, rb.Subjects[0].Name)
	require.Equal(t, sa.Namespace, rb.Subjects[0].Namespace)
	require.Equal(t, cr.Kind, rb.RoleRef.Kind)
	require.Equal(t, cr.Name, rb.RoleRef.Name)
}
