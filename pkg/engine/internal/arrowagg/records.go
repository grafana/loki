package arrowagg

import (
	"fmt"
	"hash/maphash"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// Records allows for aggregating a set of [arrow.Record]s together into a
// single, combined record with a combined schema.
//
// The returned record will have a schema composed of the union of all fields
// from the input records. Fields will be placed in the order in which they are
// first seen. When aggregating records, fields that do not exist in the source
// record will be filled with null values in the output record.
type Records struct {
	mem memory.Allocator

	fields  []arrow.Field
	records []arrow.Record
	rows    int64

	hash        maphash.Hash
	seenFields  map[uint64]arrow.Field   // Maps seen field hashes
	seenSchemas map[uint64]*arrow.Schema // Maps seen schema hashes
}

// NewRecords creates a new [Records] that aggregates a set of records.
func NewRecords(mem memory.Allocator) *Records {
	return &Records{
		mem:         mem,
		seenFields:  make(map[uint64]arrow.Field),
		seenSchemas: make(map[uint64]*arrow.Schema),
	}
}

// Append appends the entirety of rec to r. If record contains a field that has
// not been seen before, it will be added to the schema of r.
//
// Fields are compared based on endianness, name, type, and metadata; two
// fields that are equal except for metadata are treated as different.
//
// Any metadata from rec.Schema().Metadata() is ignored when computing the
// combined schema for r.
//
// rec is Retaine'd by this method until calling [Records.Aggregate].
//
// Append panics if encountering an unlikely hash collision between two
// different schemas.
func (r *Records) Append(rec arrow.Record) {
	r.processSchema(rec.Schema())

	rec.Retain()
	r.records = append(r.records, rec)
	r.rows += rec.NumRows()
}

func (r *Records) processSchema(schema *arrow.Schema) {
	schemaHash := r.hashSchema(schema)
	if seen, ok := r.seenSchemas[schemaHash]; ok {
		// See the comment in [Mapper.getMapper] for the likelihood of hash
		// collisions.
		if !seen.Equal(schema) {
			panic("arrowagg.Records: detected hash collision between two schemas")
		}

		return
	}

	for i := range schema.NumFields() {
		field := schema.Field(i)
		fieldHash := r.hashField(field)

		if seen, ok := r.seenFields[fieldHash]; ok {
			if !seen.Equal(field) {
				panic(fmt.Sprintf("arrowagg.Records: unexpected field hash %X collision between %s and %s", fieldHash, field, seen))
			}
			continue
		}

		r.fields = append(r.fields, field)
		r.seenFields[fieldHash] = field
	}

	r.seenSchemas[schemaHash] = schema
}

func (r *Records) hashSchema(schema *arrow.Schema) uint64 {
	r.hash.Reset()
	hashSchema(&r.hash, schema)
	return r.hash.Sum64()
}

func (r *Records) hashField(field arrow.Field) uint64 {
	r.hash.Reset()
	hashField(&r.hash, field)
	return r.hash.Sum64()
}

// AppendSlice behaves like [Records.Append] but instead appends a slice of the
// input record from i to j.
func (r *Records) AppendSlice(rec arrow.Record, i, j int64) {
	slice := rec.NewSlice(i, j)
	defer slice.Release()
	r.Append(slice)
}

// Aggregate all appended records into a single record. The returned record
// must be Release'd after use. If no records have been appended, Aggregate
// returns an error.
//
// The returned record will have a schema composed of the union of all fields
// from the input records, sorted by the order in which each field was first
// seen.
//
// Fields that do not exist in source records will be filled with null values
// in the output record.
//
// After calling Aggregate, r is reset and can be reused to append more arrays.
// This reset is done even if Aggregate returns an error.
func (r *Records) Aggregate() (arrow.Record, error) {
	if len(r.records) == 0 {
		return nil, fmt.Errorf("no records to flush")
	}
	defer r.Reset()

	mapper := NewMapper(r.fields)
	defer mapper.Reset() // Allow immediately freeing memory used by the mapper.

	var columns []arrow.Array
	defer func() {
		for _, column := range columns {
			column.Release()
		}
	}()

	for fieldIndex, field := range r.fields {
		columnAgg := NewArrays(r.mem, field.Type)

		for _, rec := range r.records {
			columnIndex := mapper.FieldIndex(rec.Schema(), fieldIndex)
			if columnIndex == -1 {
				columnAgg.AppendNulls(int(rec.NumRows()))
				continue
			}
			columnAgg.Append(rec.Column(columnIndex))
		}

		arr, err := columnAgg.Aggregate()
		if err != nil {
			return nil, fmt.Errorf("failed to flush array builder for field %d: %w", fieldIndex, err)
		}
		columns = append(columns, arr)
	}

	combinedSchema := arrow.NewSchema(r.fields, nil)
	return array.NewRecord(combinedSchema, columns, r.rows), nil
}

// Reset releases all resources held by r and clears its state, allowing it to
// be reused for aggregating new records.
func (r *Records) Reset() {
	for _, rec := range r.records {
		rec.Release()
	}
	clear(r.records)

	r.records = r.records[:0]
	r.fields = r.fields[:0]
	r.rows = 0

	clear(r.seenFields)
	clear(r.seenSchemas)
}
