package errors

var ErrQueryMustContainMetricName = QueryError("query must contain metric name")

// Query errors are to be treated as user errors, rather than storage errors.
type QueryError string

func (e QueryError) Error() string {
	return string(e)
}
