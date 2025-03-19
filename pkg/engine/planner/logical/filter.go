package logical

import (
	"github.com/grafana/loki/v3/pkg/engine/planner/schema"
)

// Filter represents a plan node that filters rows based on a boolean expression.
// It corresponds to the WHERE clause in SQL and is used to select a subset of rows
// from the input plan based on a predicate expression.
type Filter struct {
	// input is the child plan node providing data to filter
	input Plan
	// expr is the boolean expression used to filter rows
	expr Expr
}

// newFilter creates a new Filter plan node.
// It takes an input plan and a boolean expression that determines which rows
// should be selected (included) in its output. This is represented by the WHERE
// clause in SQL. A simple example would be SELECT * FROM foo WHERE a > 5.
// The filter expression needs to evaluate to a Boolean result.
func newFilter(input Plan, expr Expr) *Filter {
	return &Filter{
		input: input,
		expr:  expr,
	}
}

// Schema returns the schema of the filter plan.
// The schema of a filter is the same as the schema of its input,
// as filtering only removes rows and doesn't modify the structure.
func (f *Filter) Schema() schema.Schema {
	return f.input.Schema()
}

// Child returns the input plan.
// This is a convenience method for accessing the child plan.
func (f *Filter) Child() Plan {
	return f.input
}

// FilterExpr returns the filter expression.
// This is the boolean expression used to determine which rows to include.
func (f *Filter) FilterExpr() Expr {
	return f.expr
}

// Type implements the Plan interface
func (f *Filter) Type() PlanType {
	return PlanTypeFilter
}
