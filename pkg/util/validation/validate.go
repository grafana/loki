package validation

const (
	// ErrQueryTooLong is used in chunk store, querier and query frontend.
	ErrQueryTooLong = "the query time range exceeds the limit (query length: %s, limit: %s)"

	ErrQueryTooOld = "this data is no longer available, it is past now - max_query_lookback (%s)"
)
