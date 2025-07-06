package arrowagg

import (
	"hash/maphash"
	"runtime"
	"sync"
	"weak"

	"github.com/apache/arrow-go/v18/arrow"
)

// Mapper is a utility for quickly finding the index of common fields in a
// schema.
//
// Mapper caches mappings to speed up repeated lookups. Caches are cleared as
// input [*arrow.Schema]s are garbage collected, or when calling
// [Mapping.Reset].
type Mapper struct {
	target   []arrow.Field
	hash     maphash.Hash
	mappings sync.Map // map[uint64]weak.Pointer[mapping]
}

// NewMapper creates a new Mapper that locates the target fields in a schema.
func NewMapper(target []arrow.Field) *Mapper {
	return &Mapper{target: target}
}

// FieldIndex returns the index of the targetIndex'd field in the schema, or -1
// if it doesn't exist. targetIndex corresponds to the index of the field in
// the target slice passed to [NewMapper].
//
// FieldIndex returns -1 if targetIndex is out of bounds for the target slice
// passed to [NewMapper].
//
// FieldIndex panics if encountering an unlikely hash collision between two
// different schemas.
func (m *Mapper) FieldIndex(schema *arrow.Schema, targetIndex int) int {
	if targetIndex < 0 || targetIndex >= len(m.target) {
		return -1
	}

	mapping := m.getMapper(schema)
	if len(mapping.lookups) <= targetIndex {
		return -1
	}
	return mapping.lookups[targetIndex]
}

func (m *Mapper) getMapper(schema *arrow.Schema) *mapping {
	hash := m.hashSchema(schema)

	var created *mapping

Retry:
	cached, ok := m.mappings.Load(hash)
	if ok {
		found := cached.(weak.Pointer[mapping]).Value()
		if found == nil {
			// The weak pointer was nil, so the cache entry is awaiting cleanup. To
			// move forward, we'll delete it now and retry.
			m.mappings.CompareAndDelete(hash, cached)
			goto Retry
		}

		// [maphash] says that it's "collision resistant." If we believe that, then
		// the birthday paradox tells us there's 50% of a collision when we have
		// 2^32 cached schemas.
		//
		// It seems extremely unlikely for us to have anywhere close to 4.3 billion
		// schemas in practice, so for now we panic on detecting a collision.
		//
		// If we ever hit this, we can consider using a more complex structure to
		// allow for multiple mappings per hash.
		if !found.Validate(schema) {
			panic("arrowagg.Mapper: detected hash collision between two schemas")
		}
		return found
	}

	// Lazily initialize created to defer allocating anything until we know we
	// might need it.
	if created == nil {
		created = newMapping(schema, m.target)
	}

	wp := weak.Make(created)

	if _, loaded := m.mappings.LoadOrStore(hash, wp); loaded {
		// Someone else stored a mapping before us; return to Retry.
		goto Retry
	}

	// We successfully stored the mapping; add a cleanup to remove the entry when
	// the schema is garbage collected.
	runtime.AddCleanup(schema, func(hash uint64) {
		// We use CompareAndDelete here to ensure that we're only deleting the weak
		// pointer we contributed. Another goroutine may have already deleted our
		// entry and replaced it with a new one.
		m.mappings.CompareAndDelete(hash, wp)
	}, hash)

	return created
}

func (m *Mapper) hashSchema(schema *arrow.Schema) uint64 {
	m.hash.Reset()
	hashSchema(&m.hash, schema)
	return m.hash.Sum64()
}

// Reset resets the mapper, immediately clearing any cached mappings.
func (m *Mapper) Reset() {
	m.mappings.Clear()
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

// Validate checks if m is valid for the given schema. This is used to cheaply
// detect hash collisions.
func (m *mapping) Validate(other *arrow.Schema) bool {
	// Check if we've already checked this schema against the mapping.
	if _, ok := m.checked[other]; ok {
		return true
	}

	// If we haven't checked it yet, do so now.
	if m.schema.Equal(other) {
		m.checked[other] = struct{}{}
		return true
	}

	return false
}
