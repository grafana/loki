package queryfrontend

import (
	loglinelimits "github.com/grafana/loki/v3/pkg/logline/queryfrontend/limits"
)

// Per-tenant mode values, as written in the runtime overrides file.
const (
	ModeStringOff    = "off"
	ModeStringDryRun = "dry_run"
	ModeStringLive   = "live"
)

// limitsTenantSettings adapts Loki's per-tenant limits to TenantSettings.
type limitsTenantSettings struct {
	limits loglinelimits.Limits
}

// NewTenantSettings returns TenantSettings backed by Loki's per-tenant limits.
//
// A nil limits yields unset for every tenant, which leaves the deployment-wide
// configuration in charge.
func NewTenantSettings(limits loglinelimits.Limits) TenantSettings {
	if limits == nil {
		return staticTenantSettings{}
	}
	return &limitsTenantSettings{limits: limits}
}

func (s *limitsTenantSettings) Mode(tenant string) Mode {
	if s == nil || s.limits == nil {
		return ModeUnset
	}
	switch s.limits.LoglineMode(tenant) {
	case ModeStringOff:
		return ModeOff
	case ModeStringDryRun:
		return ModeDryRun
	case ModeStringLive:
		return ModeLive
	default:
		// Anything else, including "", leaves the tenant on the
		// deployment-wide setting rather than guessing.
		return ModeUnset
	}
}

func (s *limitsTenantSettings) MinQueryBytesForIndex(tenant string) (int64, bool) {
	if s == nil || s.limits == nil {
		return 0, false
	}
	return s.limits.LoglineMinQueryBytesForIndex(tenant)
}
