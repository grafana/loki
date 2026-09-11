package queryfrontend

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

// maxQueryBytesReader is an optional TenantSettings extension. GEL can
// implement this (wrapping Loki overrides) without changing
// WrapMiddleware's signature. When absent, prefetch uses
// MiddlewareConfig.MaxQueryBytesRead.
type maxQueryBytesReader interface {
	MaxQueryBytesRead(tenant string) int
}

type staticTenantSettings struct{}

func (staticTenantSettings) Mode(string) Mode { return ModeUnset }

func (staticTenantSettings) MinQueryBytesForIndex(string) (int64, bool) { return 0, false }
