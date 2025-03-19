package logical

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/planner/schema"
)

// ColumnExpr represents a reference to a column in the input data
type ColumnExpr struct {
	Name string // Name of the referenced column.
}

// Col creates a column reference expression
func Col(name string) Expr {
	return NewColumnExpr(ColumnExpr{Name: name})
}

// ToField looks up and returns the schema for the referenced column
func (c ColumnExpr) ToField(p Plan) schema.ColumnSchema {
	for _, col := range p.Schema().Columns {
		if col.Name == c.Name {
			return col
		}
	}
	panic(fmt.Sprintf("column %s not found", c.Name))
}
