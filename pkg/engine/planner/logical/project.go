package logical

import (
	"github.com/grafana/loki/v3/pkg/engine/planner/schema"
)

// Projection represents a plan node that projects expressions from its input.
// It corresponds to the SELECT clause in SQL and is used to select, transform,
// or compute columns from the input plan.
type Projection struct {
	// input is the child plan node providing data to project
	input Plan
	// exprs is the list of expressions to project
	exprs []Expr
}

// newProjection creates a new Projection plan node.
// The Projection logical plan applies a list of expressions to the input data,
// producing a new set of columns. This is represented by the SELECT clause in SQL.
// Sometimes this is as simple as a list of columns, such as SELECT a, b, c FROM foo,
// but it could also include any other type of expression that is supported.
// A more complex example would be SELECT (CAST(a AS float) * 3.141592)) AS my_float FROM foo.
func newProjection(input Plan, exprs []Expr) *Projection {
	return &Projection{
		input: input,
		exprs: exprs,
	}
}

// Schema returns the schema of the projection plan.
// The schema is derived from the projected expressions.
func (p *Projection) Schema() schema.Schema {
	cols := make([]schema.ColumnSchema, len(p.exprs))
	for i, expr := range p.exprs {
		cols[i] = expr.ToField(p.input)
	}
	return schema.Schema{Columns: cols}
}

// Child returns the input plan.
// This is a convenience method for accessing the child plan.
func (p *Projection) Child() Plan {
	return p.input
}

// ProjectExprs returns the list of projection expressions.
// These are the expressions that define the output columns.
func (p *Projection) ProjectExprs() []Expr {
	return p.exprs
}
