package validation

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/runtime"
)

type mockTenantLimits struct {
	limits map[string]*runtime.Limits
}

func newMockTenantLimits(limits map[string]*runtime.Limits) *mockTenantLimits {
	return &mockTenantLimits{
		limits: limits,
	}
}

func (l *mockTenantLimits) TenantLimits(userID string) *runtime.Limits {
	return l.limits[userID]
}

func (l *mockTenantLimits) AllByUserID() map[string]*runtime.Limits { return l.limits }

func TestOverridesExporter_noConfig(t *testing.T) {
	overrides, _ := runtime.NewOverrides(runtime.Limits{}, newMockTenantLimits(nil))
	exporter := NewOverridesExporter(overrides)
	count := testutil.CollectAndCount(exporter, "loki_overrides")
	assert.Equal(t, 0, count)
	require.Greater(t, testutil.CollectAndCount(exporter, "loki_overrides_defaults"), 0)
}

func TestOverridesExporter_withConfig(t *testing.T) {
	tenantLimits := map[string]*runtime.Limits{
		"tenant-a": {
			MaxQueriersPerTenant: 5,
			BloomCreationEnabled: true,
		},
	}
	overrides, _ := runtime.NewOverrides(runtime.Limits{}, newMockTenantLimits(tenantLimits))
	exporter := NewOverridesExporter(overrides)
	count := testutil.CollectAndCount(exporter, "loki_overrides")
	assert.Equal(t, 2, count)
	require.Greater(t, testutil.CollectAndCount(exporter, "loki_overrides_defaults"), 0)
}
