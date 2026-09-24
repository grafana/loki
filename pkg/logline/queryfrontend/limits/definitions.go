package limits

// Limits are the per-tenant logline settings for the query path. They live in
// Loki's limits_config, so a tenant overrides them in the runtime config like
// any other limit.
type Limits interface {
	// LoglineQueryMode is off, dry_run, live, or empty for unset.
	LoglineQueryMode(userID string) string
	// LoglineQueryMinQueryBytesForIndex is the minimum index-stats bytes a
	// query must cover before a hint lookup. 0 disables the check.
	LoglineQueryMinQueryBytesForIndex(userID string) int64
}
