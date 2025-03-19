package logical

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/planner/schema"
)

// The Limit instruction limits the number of rows from a table relation. Limit
// implements [Instruction] and [Value].
type Limit struct {
	id string

	// Input is the child plan node which provides the data to limit.
	Input TreeNode

	// Skip is the number of rows to skip before returning results. A value of 0
	// means no rows are skipped.
	Skip uint64

	// Fetch is the maximum number of rows to return. A value of 0 means all rows
	// are returned (after applying Skip).
	Fetch uint64
}

var (
	_ Value       = (*Limit)(nil)
	_ Instruction = (*Limit)(nil)
)

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
func newLimit(input TreeNode, skip uint64, fetch uint64) *Limit {
	return &Limit{
		Input: input,
		Skip:  skip,
		Fetch: fetch,
	}
}

// Name returns an identifier for the Limit operation.
func (l *Limit) Name() string {
	if l.id != "" {
		return l.id
	}
	return fmt.Sprintf("<%p>", l)
}

// String returns the disassembled SSA form of the Limit instruction.
func (l *Limit) String() string {
	// TODO(rfratto): change the type of l.Input to [Value] so we can use
	// s.Value.Name here.
	return fmt.Sprintf("limit %v [skip=%d, fetch=%d]", l.Input, l.Skip, l.Fetch)
}

// Schema returns the schema of the limit operation.
// The schema is the same as the input plan's schema since limiting
// only affects the number of rows, not their structure.
func (l *Limit) Schema() *schema.Schema {
	res := l.Input.Schema()
	return &res
}

func (l *Limit) isInstruction() {}
func (l *Limit) isValue()       {}
