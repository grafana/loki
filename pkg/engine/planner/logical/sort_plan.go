package logical

import (
	"github.com/grafana/loki/v3/pkg/engine/planner/schema"
)

// Sort represents a plan node that sorts rows based on sort expressions.
// It corresponds to the ORDER BY clause in SQL and is used to order
// the results of a query based on one or more sort expressions.
type Sort struct {
	// input is the child plan node providing data to sort
	input Plan
	// expr is the sort expression to apply
	expr SortExpr
}

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
		input: input,
		expr:  expr,
	}
}

// Schema returns the schema of the sort plan.
// The schema is the same as the input plan's schema since sorting
// only affects the order of rows, not their structure.
func (s *Sort) Schema() schema.Schema {
	return s.input.Schema()
}

// Type returns the plan type for this node.
func (s *Sort) Type() PlanType {
	return PlanTypeSort
}

// Child returns the input plan.
func (s *Sort) Child() Plan {
	return s.input
}

// SortExpr returns the sort expression.
func (s *Sort) Expr() SortExpr {
	return s.expr
}
