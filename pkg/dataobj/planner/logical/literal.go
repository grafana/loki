package logical

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/planner/schema"
)

type LiteralString struct {
	str string
}

func (l LiteralString) ToField(_ Plan) schema.ColumnSchema {
	return schema.ColumnSchema{
		Name: l.str,
		Type: datasetmd.VALUE_TYPE_STRING,
	}
}

type LiteralI64 struct {
	n int64
}

func (l LiteralI64) ToField(_ Plan) schema.ColumnSchema {
	return schema.ColumnSchema{
		Name: fmt.Sprint(l.n),
		Type: datasetmd.VALUE_TYPE_INT64,
	}
}
