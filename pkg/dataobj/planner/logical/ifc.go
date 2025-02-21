// Package logical implements logical query plan operations and expressions
package logical

import (
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
}

// Expr represents an expression that can be evaluated to produce a column
type Expr interface {
	// ToField converts the expression to a column schema
	ToField(Plan) schema.ColumnSchema
}

// DataSource represents a source of data that can be scanned
type DataSource interface {
	// Schema returns the schema of the data source
	Schema() schema.Schema
}
