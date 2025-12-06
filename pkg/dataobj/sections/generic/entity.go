package generic

import (
	"github.com/apache/arrow-go/v18/arrow"
)

// Schema defines the structure of entities in a generic section.
// It contains a list of Arrow fields that define the columns.
type Schema struct {
	inner *arrow.Schema
	// Optional sort information
	sortIndex     []int // Indices of columns to sort by
	sortDirection []int // 1 for ASC, -1 for DESC
}

func (s *Schema) AddField(field arrow.Field) error {
	newSchema, err := s.inner.AddField(s.inner.NumFields(), field)
	if err != nil {
		return err
	}
	s.inner = newSchema
	return nil
}

func (s *Schema) Copy() *Schema {
	return &Schema{
		inner:         arrow.NewSchema(s.inner.Fields(), nil),
		sortIndex:     s.sortIndex,
		sortDirection: s.sortDirection,
	}
}

// NewSchema creates a new Schema with the given fields.
func NewSchema(fields []arrow.Field) *Schema {
	return &Schema{
		inner: arrow.NewSchema(fields, nil),
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
	indices, exists := s.inner.FieldsByName(name)
	if !exists {
		// Should this panic instead?
		return arrow.Field{}, false
	}
	return indices[0], true
}

// Fields returns the list of fields in the schema.
func (s *Schema) Fields() []arrow.Field {
	return s.inner.Fields()
}

// NumFields returns the number of fields in the schema.
func (s *Schema) NumFields() int {
	return s.inner.NumFields()
}
