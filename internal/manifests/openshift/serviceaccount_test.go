package openshift

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildServiceAccount_AnnotationsMatchDefaultTenants(t *testing.T) {
	opts := NewOptions("abc", "abc", "efgh", "example.com", "abc", "abc", map[string]string{})

	sa := BuildServiceAccount(opts)
	require.Len(t, sa.GetAnnotations(), len(defaultTenants))

	var keys []string
	for key := range sa.GetAnnotations() {
		keys = append(keys, key)
	}

	for _, name := range defaultTenants {
		v := fmt.Sprintf("serviceaccounts.openshift.io/oauth-redirectreference.%s", name)
		require.Contains(t, keys, v)
	}
}
