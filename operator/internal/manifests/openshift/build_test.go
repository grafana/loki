package openshift

import (
	"fmt"
	"testing"
	"time"

	routev1 "github.com/openshift/api/route/v1"
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
	cr := objs[1].(*rbacv1.Role)
	rb := objs[2].(*rbacv1.RoleBinding)

	require.Equal(t, cr.Kind, rb.RoleRef.Kind)
	require.Equal(t, cr.Name, rb.RoleRef.Name)
}

func TestBuildGatewayObjets_RouteWithTimeoutAnnotation(t *testing.T) {
	gwWriteTimeout := 1 * time.Minute
	opts := NewOptions("abc", "ns", "abc", "abc", "abc", gwWriteTimeout, map[string]string{}, "abc")
	opts.BuildOpts.ExternalAccessEnabled = true // Enable external access so Route is created

	objs := BuildGatewayObjects(*opts)
	a := objs[3].GetAnnotations() // Route is at index 3 when ExternalAccessEnabled is true

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

func TestBuildGatewayObjects_ExternalAccessEnabled(t *testing.T) {
	testCases := []struct {
		name                  string
		externalAccessEnabled bool
		expectedObjectCount   int
		shouldIncludeRoute    bool
	}{
		{
			name:                  "should include Route when ExternalAccessEnabled is true",
			externalAccessEnabled: true,
			expectedObjectCount:   4,
			shouldIncludeRoute:    true,
		},
		{
			name:                  "should not include Route when ExternalAccessEnabled is false",
			externalAccessEnabled: false,
			expectedObjectCount:   3,
			shouldIncludeRoute:    false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			opts := NewOptions("abc", "ns", "abc", "abc", "abc", 1*time.Minute, map[string]string{}, "abc")
			opts.BuildOpts.ExternalAccessEnabled = tc.externalAccessEnabled

			objs := BuildGatewayObjects(*opts)
			require.Len(t, objs, tc.expectedObjectCount)
			if !tc.shouldIncludeRoute {
				for i, obj := range objs {
					_, isRoute := obj.(*routev1.Route)
					require.False(t, isRoute, "No Route should be present at index %d when ExternalAccessEnabled is false", i)
				}
			}
		})
	}
}
