package arrowagg

import (
	"github.com/apache/arrow-go/v18/arrow"
)

// Mapper is a utility for quickly finding the index of common fields in a
// schema.
//
// Mapper caches mappings to speed up repeated lookups. Caches are cleared as
// [Mapper.RemoveSchema] is called, or when calling [Mapping.Reset].
type Mapper struct {
	target   []arrow.Field
	mappings map[*arrow.Schema]*mapping
}

// NewMapper creates a new Mapper that locates the target fields in a schema.
func NewMapper(target []arrow.Field) *Mapper {
	return &Mapper{
		target:   target,
		mappings: make(map[*arrow.Schema]*mapping),
	}
}

// FieldIndex returns the index of the targetIndex'd field in the schema, or -1
// if it doesn't exist. targetIndex corresponds to the index of the field in
// the target slice passed to [NewMapper].
//
// FieldIndex returns -1 if targetIndex is out of bounds for the target slice
// passed to [NewMapper].
func (m *Mapper) FieldIndex(schema *arrow.Schema, targetIndex int) int {
	if targetIndex < 0 || targetIndex >= len(m.target) {
		return -1
	}

	mapping := m.getOrMakeMapping(schema)
	if len(mapping.lookups) <= targetIndex {
		return -1
	}
	return mapping.lookups[targetIndex]
}

func (m *Mapper) getOrMakeMapping(schema *arrow.Schema) *mapping {
	if cached, ok := m.mappings[schema]; ok {
		return cached
	}

	res := newMapping(schema, m.target)
	m.mappings[schema] = res
	return res
}

// RemoveSchema removes an individual schema from the mapper.
func (m *Mapper) RemoveSchema(schema *arrow.Schema) {
	delete(m.mappings, schema)
}

// Reset resets the mapper, immediately clearing any cached mappings.
func (m *Mapper) Reset() {
	clear(m.mappings)
}

type mapping struct {
	schema  *arrow.Schema
	checked map[*arrow.Schema]struct{} // schemas that have been checked for equality against this mapping.
	lookups []int                      // lookups[i] -> index of the i'th "to" field in schema.
}

func newMapping(schema *arrow.Schema, to []arrow.Field) *mapping {
	// Create a new mapping for the schema, and store it in the mappings map.
	mapping := &mapping{
		schema:  schema,
		lookups: make([]int, len(to)),
		checked: make(map[*arrow.Schema]struct{}),
	}

	for i, field := range to {
		// Default to -1 for fields that are not found in the schema.
		mapping.lookups[i] = -1

		for j, schemaField := range schema.Fields() {
			if field.Equal(schemaField) {
				mapping.lookups[i] = j
				break
			}
		}
	}

	return mapping
}
