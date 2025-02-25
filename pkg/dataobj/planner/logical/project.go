// Package logical implements logical query plan operations and expressions
package logical

import (
	"github.com/grafana/loki/v3/pkg/dataobj/planner/schema"
)

// Compile-time check to ensure Projection implements Plan and projectionNode
var (
	_ Plan           = &Projection{}
	_ projectionNode = &Projection{}
)

// Projection represents a plan node that projects expressions from its input
type Projection struct {
	// input is the child plan node providing data to project
	input Plan
	// exprs are the expressions to evaluate against the input
	exprs []Expr
}

// NewProjection creates a new Projection plan node.
// The Projection logical plan applies a projection to its input.
// A projection is a list of expressions to be evaluated against the input data.
// Sometimes this is as simple as a list of columns, such as SELECT a, b, c FROM foo,
// but it could also include any other type of expression that is supported.
// A more complex example would be SELECT (CAST(a AS float) * 3.141592)) AS my_float FROM foo.
func NewProjection(input Plan, exprs []Expr) *Projection {
	return &Projection{
		input: input,
		exprs: exprs,
	}
}

// Schema returns the schema of the data produced by this projection.
func (p *Projection) Schema() schema.Schema {
	var columns []schema.ColumnSchema
	for _, expr := range p.exprs {
		columns = append(columns, expr.ToField(p.input))
	}
	return schema.FromColumns(columns)
}

// Type implements the ast interface
func (p *Projection) Type() nodeType {
	return nodeTypeProjection
}

// ASTChildren implements the ast interface
func (p *Projection) ASTChildren() []ast {
	// Convert the Plan interface to ast interface
	return []ast{p.input.(ast)}
}

// ProjectExprs implements the projectionNode interface
func (p *Projection) ProjectExprs() []Expr {
	return p.exprs
}
