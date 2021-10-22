package validation

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
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
	exporter := NewOverridesExporter(newMockTenantLimits(nil))
	count := testutil.CollectAndCount(exporter, "loki_overrides")
	assert.Equal(t, 0, count)
}

func TestOverridesExporter_withConfig(t *testing.T) {
	tenantLimits := map[string]*Limits{
		"tenant-a": {
			MaxQueriersPerTenant: 5,
		},
	}
	exporter := NewOverridesExporter(newMockTenantLimits(tenantLimits))
	count := testutil.CollectAndCount(exporter, "loki_overrides")
	assert.Greater(t, count, 0)
}
