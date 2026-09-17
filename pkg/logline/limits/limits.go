package limits

import "time"

// Limits defines global settings needed by logline components. Values are
// the maximum across all configured tenants because logline indexes are
// shared (not per-tenant).
type Limits interface {
	// RetentionPeriod returns the retention period for indexes.
	// A zero duration means retention is disabled (keep forever).
	RetentionPeriod() time.Duration
}
