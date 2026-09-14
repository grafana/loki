package queryfrontend

import "context"

// Mode controls how logline query frontend handling behaves for a tenant.
type Mode int

const (
	ModeUnset Mode = iota
	ModeOff
	ModeDryRun
	ModeLive
)

// TenantSettings provides per-tenant overrides for logline query frontend
// behavior. Implementations must be safe for concurrent use.
type TenantSettings interface {
	Mode(tenant string) Mode
	// MinQueryBytesForIndex returns an optional per-tenant stats-gating threshold.
	// A value of 0 disables stats gating; implementations must not return negatives.
	MinQueryBytesForIndex(tenant string) (int64, bool)
}

// queryBytesLimit is Loki's MaxQueryBytesRead (Overrides / CombinedLimits).
// Prefetch uses this to decide whether to wait and inject a plan. Same
// source as the size limiter, including tenant overrides.
type queryBytesLimit interface {
	MaxQueryBytesRead(ctx context.Context, tenant string) int
}

type staticTenantSettings struct{}

func (staticTenantSettings) Mode(string) Mode { return ModeUnset }

func (staticTenantSettings) MinQueryBytesForIndex(string) (int64, bool) { return 0, false }
