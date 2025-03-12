package openshift

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	rbacv1 "k8s.io/api/rbac/v1"
)

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
