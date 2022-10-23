package openshift

import (
	"fmt"
	"testing"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"

	"github.com/stretchr/testify/require"
)

func TestBuildServiceAccount_AnnotationsMatchLoggingTenants(t *testing.T) {
	opts := NewOptions(lokiv1.OpenshiftLogging, "abc", "ns", "abc", "example.com", "abc", "abc", map[string]string{}, map[string]TenantData{}, "abc")

	sa := BuildGatewayServiceAccount(opts)
	require.Len(t, sa.GetAnnotations(), len(loggingTenants))

	var keys []string
	for key := range sa.GetAnnotations() {
		keys = append(keys, key)
	}

	for _, name := range loggingTenants {
		v := fmt.Sprintf("serviceaccounts.openshift.io/oauth-redirectreference.%s", name)
		require.Contains(t, keys, v)
	}
}

func TestBuildServiceAccount_AnnotationsMatchNetworkTenants(t *testing.T) {
	opts := NewOptions(lokiv1.OpenshiftNetwork, "def", "ns2", "def", "example2.com", "def", "def", map[string]string{}, map[string]TenantData{}, "abc")

	sa := BuildGatewayServiceAccount(opts)
	require.Len(t, sa.GetAnnotations(), len(networkTenants))

	var keys []string
	for key := range sa.GetAnnotations() {
		keys = append(keys, key)
	}

	for _, name := range networkTenants {
		v := fmt.Sprintf("serviceaccounts.openshift.io/oauth-redirectreference.%s", name)
		require.Contains(t, keys, v)
	}
}
