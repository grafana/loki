package logical

import (
	"github.com/grafana/loki/v3/pkg/engine/planner/schema"
)

// Select represents a plan node that filters rows based on a boolean
// expression. It corresponds to the WHERE clause in SQL and is used to select
// a subset of rows from the input plan based on a predicate expression.
type Select struct {
	Input Plan // Child plan node providing data to filter.
	Expr  Expr // Boolean expression used to filter nodes.
}

// newSelect creates a new Select plan node.
// It takes an input plan and a boolean expression that determines which rows
// should be selected (included) in its output. This is represented by the WHERE
// clause in SQL. A simple example would be SELECT * FROM foo WHERE a > 5.
// The filter expression needs to evaluate to a Boolean result.
func newSelect(input Plan, expr Expr) *Select {
	return &Select{
		Input: input,
		Expr:  expr,
	}
}

// Schema returns the schema of the Select plan.
// The schema of a Select is the same as the schema of its input,
// as selection only removes rows and doesn't modify the structure.
func (f *Select) Schema() schema.Schema {
	return f.Input.Schema()
}
