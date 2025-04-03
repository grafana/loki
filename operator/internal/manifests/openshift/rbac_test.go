package openshift

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	rbacv1 "k8s.io/api/rbac/v1"
)

func TestBuildRBAC(t *testing.T) {
	subjects := []rbacv1.Subject{{
		Kind:      "ServiceAccount",
		Name:      "test-sa",
		Namespace: "test-ns",
	}}
	opts := NewOptionsClusterScope("test", map[string]string{"app": "loki"}, subjects)

	objs := BuildRBAC(opts)

	require.Len(t, objs, 2, "Should only return the ClusterRole and ClusterRoleBinding for the ruler")

	cr, ok := objs[0].(*rbacv1.ClusterRole)
	require.True(t, ok, "First object should be a ClusterRole")
	crb, ok := objs[1].(*rbacv1.ClusterRoleBinding)
	require.True(t, ok, "Second object should be a ClusterRoleBinding")

	require.Equal(t, cr.Name, crb.RoleRef.Name)
	require.Equal(t, subjects, crb.Subjects)
}

func TestLegacyRBAC(t *testing.T) {
	// Define test gateway and ruler names
	testGatewayName := "test-gateway"
	testRulerName := "test-ruler"
	expectedGatewayName := fmt.Sprintf("%s-%s", testGatewayName, "authorizer")
	expectedRulerName := fmt.Sprintf("%s-%s", testRulerName, "authorizer")

	// Call the function under test
	objs := LegacyRBAC(testGatewayName, testRulerName)

	// Verify the number of returned objects
	require.Len(t, objs, 4, "Should return exactly 4 objects")

	// Check types and extract objects
	gatewayCR, ok := objs[0].(*rbacv1.ClusterRole)
	require.True(t, ok, "First object should be a ClusterRole for gateway")
	gatewayCRB, ok := objs[1].(*rbacv1.ClusterRoleBinding)
	require.True(t, ok, "Second object should be a ClusterRoleBinding for gateway")
	rulerCR, ok := objs[2].(*rbacv1.ClusterRole)
	require.True(t, ok, "Third object should be a ClusterRole for ruler")
	rulerCRB, ok := objs[3].(*rbacv1.ClusterRoleBinding)
	require.True(t, ok, "Fourth object should be a ClusterRoleBinding for ruler")

	// Verify gateway we only care about the name since these resources will only
	// deleted
	require.Equal(t, gatewayCR.Name, gatewayCRB.Name)
	require.Equal(t, gatewayCR.Name, expectedGatewayName)

	// Verify ruler we only care about the name since these resources will only
	// deleted
	require.Equal(t, rulerCR.Name, rulerCRB.Name)
	require.Equal(t, rulerCR.Name, expectedRulerName)
}
