// Package schema provides types and utilities for working with dataset schemas
package schema

import (
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
)

// ColumnSchema describes a column in a dataset, including its name and data type.
type ColumnSchema struct {
	// Name is the identifier of the column
	Name string
	// Type represents the data type of values in this column
	Type datasetmd.ValueType
}

// Schema represents the complete structure of a dataset, containing all column definitions.
type Schema struct {
	// Columns is a slice of ColumnSchema defining all columns in the dataset
	Columns []ColumnSchema
}
