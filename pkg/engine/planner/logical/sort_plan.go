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

	Input Plan     // Child plan node providing data to sort.
	Expr  SortExpr // Sort expression to apply.
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
func newSort(input Plan, expr SortExpr) *Sort {
	return &Sort{
		Input: input,
		Expr:  expr,
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
	return fmt.Sprintf("sort %v [asc=%t, nulls_first=%t]", s.Input, s.Expr.Ascending, s.Expr.NullsFirst)
}

// Schema returns the schema of the sort plan.
// The schema is the same as the input plan's schema since sorting
// only affects the order of rows, not their structure.
func (s *Sort) Schema() *schema.Schema {
	res := s.Input.Schema()
	return &res
}

func (s *Sort) isInstruction() {}
func (s *Sort) isValue()       {}
