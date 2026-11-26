package limits

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/validation"
)

// fakeTenantLimits implements validation.TenantLimits for tests.
type fakeTenantLimits struct {
	tenants map[string]*validation.Limits
}

func (f *fakeTenantLimits) TenantLimits(userID string) *validation.Limits {
	return f.tenants[userID]
}

func (f *fakeTenantLimits) AllByUserID() map[string]*validation.Limits {
	return f.tenants
}

func newTestMaxLimits(defaults validation.Limits, tenants map[string]*validation.Limits) *maxLimits {
	tl := &fakeTenantLimits{tenants: tenants}
	ov, err := validation.NewOverrides(defaults, tl)
	if err != nil {
		panic(err)
	}
	return NewMaxLimits(ov, prometheus.NewRegistry()).(*maxLimits)
}

func TestMaxLimits_RetentionPeriod_FollowsTenantChanges(t *testing.T) {
	defaults := validation.Limits{}
	defaults.RetentionPeriod = model.Duration(7 * 24 * time.Hour)

	tenants := map[string]*validation.Limits{
		"a": {RetentionPeriod: model.Duration(14 * 24 * time.Hour)},
	}
	m := newTestMaxLimits(defaults, tenants)
	require.Equal(t, 14*24*time.Hour, m.RetentionPeriod())

	// A runtime config reload changes the tenant limits in place.
	tenants["b"] = &validation.Limits{RetentionPeriod: model.Duration(30 * 24 * time.Hour)}
	require.Equal(t, 30*24*time.Hour, m.RetentionPeriod())
}

func TestMaxLimits_RetentionPeriod_MaxAcrossTenants(t *testing.T) {
	defaults := validation.Limits{}
	defaults.RetentionPeriod = model.Duration(7 * 24 * time.Hour)

	tenants := map[string]*validation.Limits{
		"short": {RetentionPeriod: model.Duration(3 * 24 * time.Hour)},
		"long":  {RetentionPeriod: model.Duration(30 * 24 * time.Hour)},
	}

	m := newTestMaxLimits(defaults, tenants)

	// Should return the longest tenant retention (30d), not the default (7d).
	got := m.RetentionPeriod()
	require.Equal(t, 30*24*time.Hour, got)
}

func TestMaxLimits_RetentionPeriod_ZeroTenantSkipped(t *testing.T) {
	defaults := validation.Limits{}
	defaults.RetentionPeriod = model.Duration(7 * 24 * time.Hour)

	tenants := map[string]*validation.Limits{
		"limited":   {RetentionPeriod: model.Duration(30 * 24 * time.Hour)},
		"unlimited": {RetentionPeriod: 0}, // zero = "use default", not "keep forever"
	}

	m := newTestMaxLimits(defaults, tenants)

	// Zero tenant is skipped; max of default (7d) and limited (30d) = 30d.
	got := m.RetentionPeriod()
	require.Equal(t, 30*24*time.Hour, got)
}

func TestMaxLimits_RetentionPeriod_DefaultZero(t *testing.T) {
	defaults := validation.Limits{}
	// Zero default = 0 (no retention configured).

	tenants := map[string]*validation.Limits{
		"a": {RetentionPeriod: model.Duration(30 * 24 * time.Hour)},
	}

	m := newTestMaxLimits(defaults, tenants)
	// Tenant overrides the zero default.
	got := m.RetentionPeriod()
	require.Equal(t, 30*24*time.Hour, got)
}

func TestMaxLimits_RetentionPeriod_NoTenants(t *testing.T) {
	defaults := validation.Limits{}
	defaults.RetentionPeriod = model.Duration(14 * 24 * time.Hour)

	m := newTestMaxLimits(defaults, nil)
	got := m.RetentionPeriod()
	require.Equal(t, 14*24*time.Hour, got)
}

func TestMaxLimits_NilTenantLimits(t *testing.T) {
	defaults := validation.Limits{}
	defaults.RetentionPeriod = model.Duration(14 * 24 * time.Hour)

	ov, err := validation.NewOverrides(defaults, nil)
	require.NoError(t, err)

	m := NewMaxLimits(ov, prometheus.NewRegistry())

	require.Equal(t, 14*24*time.Hour, m.RetentionPeriod())
}
