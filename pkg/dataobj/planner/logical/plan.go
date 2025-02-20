// Package logical implements logical query plan operations and expressions
package logical

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj/planner/schema"
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

// ColumnExpr represents a reference to a column in the input data
type ColumnExpr struct {
	// name is the identifier of the referenced column
	name string
}

// ToField looks up and returns the schema for the referenced column
func (c ColumnExpr) ToField(p Plan) schema.ColumnSchema {
	for _, col := range p.Schema().Columns {
		if col.Name == c.name {
			return col
		}
	}
	panic(fmt.Sprintf("column %s not found", c.name))
}

// Scan represents a plan node that scans input data
type Scan struct {
	// schema defines the structure of the scanned data
	schema schema.Schema
	// projection specifies which columns to include
	projection []string
}

// Projection represents a plan node that projects specific columns
type Projection struct {
	// input is the child plan node providing data to project
	input Plan
}
