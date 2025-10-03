// Package schema provides types and utilities for working with dataset schemas
package schema

// ValueType represents the type of a literal in a literal expression.
type ValueType uint32

const (
	_ ValueType = iota // zero-value is an invalid type
	ValueTypeBool
	ValueTypeInt64
	ValueTypeUint64 // Do we need a separate value type for uint64 if we already have int64?
	ValueTypeTimestamp
	ValueTypeString
)

func (t ValueType) String() string {
	switch t {
	case ValueTypeBool:
		return "VALUE_TYPE_BOOL"
	case ValueTypeInt64:
		return "VALUE_TYPE_INT64"
	case ValueTypeUint64:
		return "VALUE_TYPE_UINT64"
	case ValueTypeTimestamp:
		return "VALUE_TYPE_TIMESTAMP"
	case ValueTypeString:
		return "VALUE_TYPE_STRING"
	default:
		return "VALUE_TYPE_UNKNOWN"
	}
}

// ColumnSchema describes a column in a dataset, including its name and data type.
type ColumnSchema struct {
	// Name is the identifier of the column
	Name string
	// Type represents the data type of values in this column
	Type ValueType
}

// Schema represents the complete structure of a dataset, containing all column definitions.
type Schema struct {
	// Columns is a slice of ColumnSchema defining all columns in the dataset
	Columns []ColumnSchema
}

// Filter returns a new Schema with only the columns specified in projection
func (s *Schema) Filter(projection []string) Schema {
	filteredColumns := make([]ColumnSchema, 0, len(projection))
	for _, col := range s.Columns {
		for _, pcol := range projection {
			if col.Name == pcol {
				filteredColumns = append(filteredColumns, col)
			}
		}
	}
	return Schema{
		Columns: filteredColumns,
	}
}

func FromColumns(columns []ColumnSchema) Schema {
	return Schema{
		Columns: columns,
	}
}
