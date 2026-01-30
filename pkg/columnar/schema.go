package columnar

import "fmt"

// TODO(rfratto): This is a placeholder for a more fleshed out schema
// implementation. It's added to unblock the implementation of pkg/expr.

// A Column describes a single column in a [RecordBatch].
type Column struct {
	Name string // Name of the column.
}

// A Schema describes the set of columns in a [RecordBatch].
type Schema struct {
	columns       []Column
	columnIndices map[string]int
}

// NewSchema creates a new schema from a list of columns. Column names must be
// unique. If column names are not unique, NewSchema panics.
func NewSchema(columns []Column) *Schema {
	indices := make(map[string]int, len(columns))
	for i, col := range columns {
		if _, ok := indices[col.Name]; ok {
			panic(fmt.Sprintf("duplicate column name %s", col.Name))
		}
		indices[col.Name] = i
	}

	return &Schema{
		columns:       columns,
		columnIndices: indices,
	}
}

// NumColumns returns the number of columns in the schema.
func (s *Schema) NumColumns() int { return len(s.columns) }

// Column returns the column at index i. Column panics if i is out of bounds.
func (s *Schema) Column(i int) Column { return s.columns[i] }

// ColumnIndex returns the column with the given name, along with its index.
// ColumnIndex returns the Column{}, -1 if the column doesn't exist.
func (s *Schema) ColumnIndex(name string) (Column, int) {
	if idx, ok := s.columnIndices[name]; ok {
		return s.columns[idx], idx
	}
	return Column{}, -1 // not found
}
