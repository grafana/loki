package util

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAllowedTenants_NoConfig(t *testing.T) {
	a := NewAllowedTenants(nil, nil)
	require.True(t, a.IsAllowed("all"))
	require.True(t, a.IsAllowed("tenants"))
	require.True(t, a.IsAllowed("allowed"))
}

func TestAllowedTenants_Enabled(t *testing.T) {
	a := NewAllowedTenants([]string{"A", "B"}, nil)
	require.True(t, a.IsAllowed("A"))
	require.True(t, a.IsAllowed("B"))
	require.False(t, a.IsAllowed("C"))
	require.False(t, a.IsAllowed("D"))
}

func TestAllowedTenants_Disabled(t *testing.T) {
	a := NewAllowedTenants(nil, []string{"A", "B"})
	require.False(t, a.IsAllowed("A"))
	require.False(t, a.IsAllowed("B"))
	require.True(t, a.IsAllowed("C"))
	require.True(t, a.IsAllowed("D"))
}

func TestAllowedTenants_Combination(t *testing.T) {
	a := NewAllowedTenants([]string{"A", "B"}, []string{"B", "C"})
	require.True(t, a.IsAllowed("A"))  // enabled, and not disabled
	require.False(t, a.IsAllowed("B")) // enabled, but also disabled
	require.False(t, a.IsAllowed("C")) // disabled
	require.False(t, a.IsAllowed("D")) // not enabled
}

func TestAllowedTenants_Nil(t *testing.T) {
	var a *AllowedTenants

	// All tenants are allowed when using nil as allowed tenants.
	require.True(t, a.IsAllowed("A"))
	require.True(t, a.IsAllowed("B"))
	require.True(t, a.IsAllowed("C"))
}
