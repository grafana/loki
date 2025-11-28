package generic

import (
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
)

func makeIndex(fields []arrow.Field) map[string]int {
	index := make(map[string]int, len(fields))
	for i, f := range fields {
		index[f.Name] = i
	}
	return index
}

// Schema defines the structure of entities in a generic section.
// It contains a list of Arrow fields that define the columns.
type Schema struct {
	fields []arrow.Field
	index  map[string]int

	// Optional sort information
	sortIndex     []int // Indices of columns to sort by
	sortDirection []int // 1 for ASC, -1 for DESC
}

func (s *Schema) Copy() *Schema {
	// Create a copy of the schema to avoid sharing field slices
	fieldsCopy := make([]arrow.Field, len(s.fields))
	copy(fieldsCopy, s.fields)
	return &Schema{
		fields:        fieldsCopy,
		index:         makeIndex(fieldsCopy), // Initialize the index map
		sortIndex:     s.sortIndex,
		sortDirection: s.sortDirection,
	}
}

// NewSchema creates a new Schema with the given fields.
func NewSchema(fields []arrow.Field) *Schema {
	return &Schema{
		fields: fields,
		index:  makeIndex(fields),
	}
}

// NewSchemaWithSort creates a new Schema with sort information.
// sortIndex contains the indices of columns to sort by.
// sortDirection contains 1 for ascending or -1 for descending for each sort column.
func NewSchemaWithSort(fields []arrow.Field, sortIndex []int, sortDirection []int) *Schema {
	if len(sortIndex) != len(sortDirection) {
		panic("sortIndex and sortDirection must have the same length")
	}
	schema := NewSchema(fields)
	schema.sortIndex = sortIndex
	schema.sortDirection = sortDirection
	return schema
}

func (s *Schema) Field(name string) (arrow.Field, bool) {
	idx, exists := s.index[name]
	if !exists {
		// Should this panic instead?
		return arrow.Field{}, false
	}
	return s.fields[idx], true
}

// Fields returns the list of fields in the schema.
func (s *Schema) Fields() []arrow.Field {
	return s.fields
}

// NumFields returns the number of fields in the schema.
func (s *Schema) NumFields() int {
	return len(s.fields)
}

// Entity represents a single row of data in a generic section.
// It contains values that correspond to the fields defined in its own Schema.
type Entity struct {
	schema *Schema
	values []dataset.Value
}

// NewEntity creates a new Entity with the given schema and values.
// The number of values must match the number of fields in the schema.
func NewEntity(schema *Schema, values []dataset.Value) *Entity {
	if len(values) != schema.NumFields() {
		panic(fmt.Sprintf("entity values count (%d) does not match schema fields count (%d)", len(values), schema.NumFields()))
	}
	return &Entity{
		schema: schema,
		values: values,
	}
}

// Schema returns the schema of the entity.
func (e *Entity) Schema() *Schema {
	return e.schema
}

func (e *Entity) Value(name string) (dataset.Value, bool) {
	idx, exists := e.schema.index[name]
	if !exists {
		// Should this panic instead?
		return dataset.Value{}, false
	}
	return e.values[idx], true
}

// Values returns the values in the entity.
func (e *Entity) Values() []dataset.Value {
	return e.values
}
