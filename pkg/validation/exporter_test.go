package validation

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

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
