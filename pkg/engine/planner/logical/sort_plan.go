package logical

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/planner/schema"
)

// Sort represents a plan node that sorts rows based on sort expressions.
// It corresponds to the ORDER BY clause in SQL and is used to order
// the results of a query based on one or more sort expressions.

// The Sort instruction sorts rows from a table relation. Sort implements both
// [Instruction] and [Value].
type Sort struct {
	id string

	Table Value // The table relation to sort.

	Column     ColumnRef // The column to sort by.
	Ascending  bool      // Whether to sort in ascending order.
	NullsFirst bool      // Controls whether NULLs appear first (true) or last (false).
}

var (
	_ Value       = (*Sort)(nil)
	_ Instruction = (*Sort)(nil)
)

// newSort creates a new Sort plan node.
// It takes an input plan and a vector of sort expressions to apply.
//
// Example usage:
//
//	// Sort by age in ascending order, NULLs last, then by name in descending order, NULLs first
//	sort := newSort(inputPlan, []SortExpr{
//		NewSortExpr("sort_by_age", Col("age"), true, false),
//		NewSortExpr("sort_by_name", Col("name"), false, true),
//	})
func newSort(table Value, column ColumnRef, ascending, nullsFirst bool) *Sort {
	return &Sort{
		Table: table,

		Column:     column,
		Ascending:  ascending,
		NullsFirst: nullsFirst,
	}
}

// Name returns an identifier for the Sort operation.
func (s *Sort) Name() string {
	if s.id != "" {
		return s.id
	}
	return fmt.Sprintf("<%p>", s)
}

// String returns the disassembled SSA form of the Sort instruction.
func (s *Sort) String() string {
	// TODO(rfratto): change the type of s.Input to [Value] so we can use
	// s.Value.Name here.
	return fmt.Sprintf(
		"SORT %s [column=%s, asc=%t, nulls_first=%t]",
		s.Table.Name(),
		s.Column.String(),
		s.Ascending,
		s.NullsFirst,
	)
}

// Schema returns the schema of the sort plan.
func (s *Sort) Schema() *schema.Schema {
	// The schema is the same as the input plan's schema since sorting only
	// affects the order of rows, not their structure.
	return s.Table.Schema()
}

func (s *Sort) isInstruction() {}
func (s *Sort) isValue()       {}
