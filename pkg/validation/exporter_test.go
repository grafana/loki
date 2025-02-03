package validation

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockTenantLimits struct {
	limits map[string]*Limits
}

func newMockTenantLimits(limits map[string]*Limits) *mockTenantLimits {
	return &mockTenantLimits{
		limits: limits,
	}
}

func (l *mockTenantLimits) TenantLimits(userID string) *Limits {
	return l.limits[userID]
}

func (l *mockTenantLimits) AllByUserID() map[string]*Limits { return l.limits }

func TestOverridesExporter_noConfig(t *testing.T) {
	overrides, _ := NewOverrides(Limits{}, newMockTenantLimits(nil))
	exporter := NewOverridesExporter(overrides)
	count := testutil.CollectAndCount(exporter, "loki_overrides")
	assert.Equal(t, 0, count)
	require.Greater(t, testutil.CollectAndCount(exporter, "loki_overrides_defaults"), 0)
}

func TestOverridesExporter_withConfig(t *testing.T) {
	tenantLimits := map[string]*Limits{
		"tenant-a": {
			MaxQueriersPerTenant: 5,
			BloomCreationEnabled: true,
		},
	}
	overrides, _ := NewOverrides(Limits{}, newMockTenantLimits(tenantLimits))
	exporter := NewOverridesExporter(overrides)
	count := testutil.CollectAndCount(exporter, "loki_overrides")
	assert.Equal(t, 2, count)
	require.Greater(t, testutil.CollectAndCount(exporter, "loki_overrides_defaults"), 0)
}
