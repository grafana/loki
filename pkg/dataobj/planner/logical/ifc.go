// Package logical implements logical query plan operations and expressions
package logical

import (
	"github.com/grafana/loki/v3/pkg/dataobj/planner/logical/format"
	"github.com/grafana/loki/v3/pkg/dataobj/planner/schema"
)

// Compile-time checks to ensure types implement Expr interface
var (
	_ Expr = ColumnExpr{}
)

// Plan represents a logical query plan node that can provide its schema and children
type Plan interface {
	// Schema returns the schema of the data produced by this plan node
	Schema() schema.Schema
	// Children returns the child plan nodes
	Children() []Plan
	// Format formats the plan as a string
	Format(format.Formatter)
}

// Expr represents an expression that can be evaluated to produce a column
type Expr interface {
	// ToField converts the expression to a column schema
	ToField(Plan) schema.ColumnSchema
	// Format formats the expression as a string
	Format(format.Formatter)
}
