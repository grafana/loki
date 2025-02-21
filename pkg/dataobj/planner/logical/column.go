package logical

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj/planner/schema"
)

var (
	_ Expr = ColumnExpr{}
)

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
