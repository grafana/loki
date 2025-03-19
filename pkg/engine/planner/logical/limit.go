package logical

import (
	"github.com/grafana/loki/v3/pkg/engine/planner/schema"
)

// Limit represents a plan node that limits the number of rows returned.
// It corresponds to the LIMIT clause in SQL and is used to restrict the
// number of rows returned by a query, optionally with an offset.
//
// The Limit plan is typically the final operation in a query plan, applied
// after filtering, projection, and aggregation. It's useful for pagination
// and for reducing the amount of data returned to the client.
type Limit struct {
	// Input is the child plan node which provides the data to limit.
	Input Plan

	// Skip is the number of rows to skip before returning results. A value of 0
	// means no rows are skipped.
	Skip uint64

	// Fetch is the maximum number of rows to return. A value of 0 means all rows
	// are returned (after applying Skip).
	Fetch uint64
}

// Special values for skip and fetch
const (
	// NoSkip indicates that no rows should be skipped (OFFSET 0)
	NoSkip uint64 = 0
	// NoLimit indicates that all rows should be returned (no LIMIT clause)
	NoLimit uint64 = 0
)

// newLimit creates a new Limit plan node.
// The Limit logical plan restricts the number of rows returned by a query.
// It takes an input plan, a skip value (for OFFSET), and a fetch value (for LIMIT).
// If skip is 0, no rows are skipped. If fetch is 0, all rows are returned after applying skip.
//
// Example usage:
//
//	// Return the first 10 rows
//	limit := newLimit(inputPlan, 0, 10)
//
//	// Skip the first 20 rows and return the next 10
//	limit := newLimit(inputPlan, 20, 10)
//
//	// Skip the first 100 rows and return all remaining rows
//	limit := newLimit(inputPlan, 100, 0)
func newLimit(input Plan, skip uint64, fetch uint64) *Limit {
	return &Limit{
		Input: input,
		Skip:  skip,
		Fetch: fetch,
	}
}

// Schema returns the schema of the limit operation.
// The schema is the same as the input plan's schema since limiting
// only affects the number of rows, not their structure.
func (l *Limit) Schema() schema.Schema {
	return l.Input.Schema()
}
