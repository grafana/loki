package downloads

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTenantDays_Flag(t *testing.T) {
	var d TenantDays
	require.NoError(t, d.Set("tenant-a=7, *=1,"))
	require.Equal(t, TenantDays{"tenant-a": 7, "*": 1}, d)
	require.Equal(t, "*=1,tenant-a=7", d.String())

	require.NoError(t, d.Set(""))
	require.Empty(t, d)

	for _, bad := range []string{"tenant-a", "=7", "tenant-a=x"} {
		require.Error(t, d.Set(bad), bad)
	}
}

func TestQueryReadyOverrides_Validate(t *testing.T) {
	require.NoError(t, (&QueryReadyOverrides{}).Validate())
	require.NoError(t, (&QueryReadyOverrides{NumDays: TenantDays{"tenant-a": 7}, TenantsInclude: []string{"tenant-a"}}).Validate())
	require.Error(t, (&QueryReadyOverrides{NumDays: TenantDays{"tenant-a": -1}}).Validate())
	require.Error(t, (&QueryReadyOverrides{TenantsInclude: []string{"tenant-a"}, TenantsExclude: []string{"tenant-a"}}).Validate())
}
