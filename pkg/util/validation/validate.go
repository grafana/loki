package validation

const (
	// ErrQueryTooLong is used in chunk store, querier and query frontend.
	ErrQueryTooLong = "the query time range exceeds the limit (query length: %s, limit: %s)"

	ErrQueryTooOld = "this data is no longer available, it is past now - max_query_lookback (%s)"

	// ErrMaxEntriesLimit is used by the querier, the query frontend and the v2
	// engine, and matched by pkg/util/server, so they cannot drift apart.
	ErrMaxEntriesLimit = "max entries limit per query exceeded, limit > max_entries_limit_per_query (%d > %d)"
)
