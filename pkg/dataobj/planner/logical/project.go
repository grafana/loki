// Package logical implements logical query plan operations and expressions
package logical

import (
	"github.com/grafana/loki/v3/pkg/dataobj/planner/logical/format"
	"github.com/grafana/loki/v3/pkg/dataobj/planner/schema"
)

// Compile-time check to ensure Projection implements Plan
var (
	_ Plan = &Projection{}
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
	return schema.SchemaFromColumns(columns)
}

// Children returns the child plan nodes
func (p *Projection) Children() []Plan {
	return []Plan{p.input}
}

// Format implements format.Format
func (p *Projection) Format(fm format.Formatter) {
	var tuples []format.ContentTuple
	for _, expr := range p.exprs {
		field := expr.ToField(p.input)
		tuples = append(tuples, format.ContentTuple{
			Key:   field.Name,
			Value: format.SingleContent(field.Type.String()),
		})
	}

	n := format.Node{
		Singletons: []string{"Projection"},
		Tuples:     tuples,
	}

	nextFM := fm.WriteNode(n)
	for _, expr := range p.exprs {
		expr.Format(nextFM)
	}
	p.input.Format(nextFM)
}
