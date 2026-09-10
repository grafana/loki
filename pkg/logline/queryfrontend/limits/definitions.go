// Package limits declares the per-tenant configuration the logline query
// frontend middleware reads.
//
// It is a leaf package on purpose. The interface has to be referenced from
// util/limiter's CombinedLimits, and the middleware package itself depends on
// pkg/querier/queryrange, which transitively depends on util/limiter. Keeping
// the interface here breaks that cycle, the same way pkg/querier/limits and
// pkg/querier/queryrange/limits do for their packages.
package limits

// Limits is the per-tenant configuration the logline query frontend needs.
type Limits interface {
	// LoglineMode returns the tenant's mode, or "" when unset.
	LoglineMode(userID string) string
	// LoglineMinQueryBytesForIndex returns the tenant's stats-gating
	// threshold and whether it is configured. 0 is a meaningful value that
	// disables the gate, so it cannot double as "unset".
	LoglineMinQueryBytesForIndex(userID string) (int64, bool)
}
