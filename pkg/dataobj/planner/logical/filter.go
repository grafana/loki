// Package logical implements logical query plan operations and expressions
package logical

import (
	"github.com/grafana/loki/v3/pkg/dataobj/planner/logical/format"
	"github.com/grafana/loki/v3/pkg/dataobj/planner/schema"
)

// Compile-time check to ensure Filter implements Plan
var (
	_ Plan = &Filter{}
)

// Filter represents a plan node that filters rows based on a boolean expression.
type Filter struct {
	// input is the child plan node providing data to filter
	input Plan
	// expr is the boolean expression used to filter rows
	expr Expr
}

// NewFilter creates a new Filter plan node.
// The Filter logical plan applies a filter expression to determine which rows
// should be selected (included) in its output. This is represented by the WHERE
// clause in SQL. A simple example would be SELECT * FROM foo WHERE a > 5.
// The filter expression needs to evaluate to a Boolean result.
func NewFilter(input Plan, expr Expr) *Filter {
	return &Filter{
		input: input,
		expr:  expr,
	}
}

// Schema returns the schema of the data produced by this filter.
// Filter does not change the schema of its input.
func (f *Filter) Schema() schema.Schema {
	return f.input.Schema()
}

// Children returns the child plan nodes
func (f *Filter) Children() []Plan {
	return []Plan{f.input}
}

// Format implements format.Format
func (f *Filter) Format(fm format.Formatter) {
	n := format.Node{
		Singletons: []string{"Filter"},
		Tuples: []format.ContentTuple{{
			Key:   "expr",
			Value: format.SingleContent(f.expr.ToField(f.input).Name),
		}},
	}

	nextFM := fm.WriteNode(n)
	f.expr.Format(nextFM)
	f.input.Format(nextFM)
}
