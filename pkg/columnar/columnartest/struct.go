package columnartest

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/columnar/types"
	"github.com/grafana/loki/v3/pkg/memory"
)

// StructField pairs a field name, kind, and values for use with [Struct].
type StructField struct {
	Name   string
	Kind   types.Kind
	Values []any
}

// Field creates a [StructField] for use with [Struct].
func Field(name string, kind types.Kind, values ...any) StructField {
	return StructField{Name: name, Kind: kind, Values: values}
}

// Struct builds a [*columnar.Struct] from named fields.
func Struct(t testing.TB, alloc *memory.Allocator, fields ...StructField) *columnar.Struct {
	t.Helper()
	return buildStruct(t, alloc, nil, fields...)
}

// StructWithValidity builds a [*columnar.Struct] from named fields with the
// given row-level validity. valid[i] indicates whether row i is non-null at
// the struct level.
func StructWithValidity(t testing.TB, alloc *memory.Allocator, valid []bool, fields ...StructField) *columnar.Struct {
	t.Helper()
	return buildStruct(t, alloc, valid, fields...)
}

func buildStruct(t testing.TB, alloc *memory.Allocator, valid []bool, fields ...StructField) *columnar.Struct {
	t.Helper()

	if alloc == nil {
		alloc = memory.NewAllocator(nil)
	}

	columns := make([]columnar.Column, len(fields))
	arrays := make([]columnar.Array, len(fields))
	var length int

	for i, f := range fields {
		columns[i] = columnar.Column{Name: f.Name}
		arrays[i] = Array(t, f.Kind, alloc, f.Values...)
		if i == 0 {
			length = arrays[i].Len()
		} else {
			require.Equal(t, length, arrays[i].Len(), "field %q length mismatch", f.Name)
		}
	}

	var validity memory.Bitmap
	if valid != nil {
		require.Equal(t, length, len(valid), "validity length mismatch")
		validity = memory.NewBitmap(alloc, length)
		validity.AppendValues(valid...)
	}

	schema := columnar.NewSchema(columns)
	return columnar.NewStruct(schema, arrays, length, validity)
}
