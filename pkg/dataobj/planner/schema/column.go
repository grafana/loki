package schema

import (
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
)

// ColumnSchema describes a column.
type ColumnSchema struct {
	Name string
	Type datasetmd.ValueType
}

// Schema describes the schema of a dataset.
type Schema struct {
	Columns []ColumnSchema
}
