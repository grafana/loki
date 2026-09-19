package columnar

import (
	"github.com/grafana/loki/v3/pkg/columnar/types"
	"github.com/grafana/loki/v3/pkg/memory"
)

// Struct is an [Array] of struct values. Each struct contains a fixed set of
// named fields, where each field is itself an Array of equal length.
type Struct struct {
	schema    *Schema
	fields    []Array
	length    int
	validity  memory.Bitmap
	nullCount int
	typ       types.Type
}

var _ Array = (*Struct)(nil)

// NewStruct creates a new Struct array.
//
// All field arrays must have the same length, which must equal length. If
// validity is of length zero, all elements are considered valid. Otherwise,
// validity must have the same length as length.
func NewStruct(schema *Schema, fields []Array, length int, validity memory.Bitmap) *Struct {
	s := &Struct{
		schema:   schema,
		fields:   fields,
		length:   length,
		validity: validity,
	}
	s.init()
	return s
}

//go:noinline
func (s *Struct) init() {
	if s.validity.Len() > 0 && s.validity.Len() != s.length {
		panic("validity length does not match struct length")
	}
	if len(s.schema.columns) != len(s.fields) {
		panic("number of columns does not match number of fields")
	}

	typ := &types.Struct{
		Fields:   make([]types.StructField, 0, len(s.fields)),
		Nullable: true,
	}

	for i, f := range s.fields {
		if f.Len() != s.length {
			panic("field " + s.schema.Column(i).Name + " length does not match struct length")
		}
		typ.Fields = append(typ.Fields, types.StructField{
			Name: s.schema.columns[i].Name,
			Type: f.Type(),
		})
	}
	s.nullCount = s.validity.ClearCount()
	s.typ = typ
}

// Kind implements [Datum] and returns [types.KindStruct].
func (s *Struct) Kind() types.Kind { return types.KindStruct }

// Type returns a [*types.Struct].
func (s *Struct) Type() types.Type { return s.typ }

// Len returns the number of rows in the struct.
func (s *Struct) Len() int { return s.length }

// Nulls returns the number of null struct rows.
func (s *Struct) Nulls() int { return s.nullCount }

// IsNull returns true if the struct at row i is null.
func (s *Struct) IsNull(i int) bool {
	if s.nullCount == 0 {
		return false
	}
	return !s.validity.Get(i)
}

// Size returns the total size of all field arrays plus the validity bitmap.
func (s *Struct) Size() int {
	total := s.validity.Len() / 8
	for _, f := range s.fields {
		total += f.Size()
	}
	return total
}

// Slice returns a slice of the struct from row i to row j.
func (s *Struct) Slice(i, j int) Array {
	if i < 0 || j < i || j > s.length {
		panic(errorSliceBounds{i, j, s.length})
	}
	fields := make([]Array, len(s.fields))
	for idx, f := range s.fields {
		fields[idx] = f.Slice(i, j)
	}
	validity := sliceValidity(s.validity, i, j)
	return NewStruct(s.schema, fields, j-i, validity)
}

// Validity returns the struct-level validity bitmap.
func (s *Struct) Validity() memory.Bitmap { return s.validity }

// NumFields returns the number of fields in the struct.
func (s *Struct) NumFields() int { return len(s.fields) }

// Field returns the array for field at index i.
func (s *Struct) Field(i int) Array { return s.fields[i] }

// Schema returns the schema describing the struct's fields.
func (s *Struct) Schema() *Schema { return s.schema }

func (s *Struct) isDatum() {}
func (s *Struct) isArray() {}
