package logical

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj/planner/schema"
)

type Plan interface {
	Schema() schema.Schema
	Children() []Plan
}

type Expr interface {
	ToField(Plan) schema.ColumnSchema
}

type ColumnExpr struct {
	name string
}

func (c ColumnExpr) ToField(p Plan) schema.ColumnSchema {
	for _, col := range p.Schema().Columns {
		if col.Name == c.name {
			return col
		}
	}
	panic(fmt.Sprintf("column %s not found", c.name))
}

type Scan struct {
	schema     schema.Schema
	projection []string
}

type Projection struct {
	input Plan
}
