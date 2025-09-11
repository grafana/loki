package executor

import (
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

// changeSchema creates a record with the same data as input, but with the
// schema set to newSchema.
//
// changeSchema can be used for changing field names, field metadata, or
// overall schema metadata. Field datatypes must be consistent between the
// previous and new schemas.
//
// changeSchema returns an error if the new schema is not compatible with the
// old schema.
//
// The returned record must be Release()d by the caller. changeSchema does not
// change the number of references to the input record.
func changeSchema(input arrow.Record, newSchema *arrow.Schema) (arrow.Record, error) {
	if err := validateSchemaCompatibility(input.Schema(), newSchema); err != nil {
		return nil, fmt.Errorf("incompatible schema: %w", err)
	}

	var (
		numCols = input.NumCols()
		numRows = input.NumRows()
	)

	cols := make([]arrow.Array, numCols)
	for i := range numCols {
		cols[i] = input.Column(int(i))
	}

	return array.NewRecord(newSchema, cols, numRows), nil
}

// validateSchemaCompatibility checks if two schemas are compatible:
//
// - Both schemas have the same endianness.
// - Both schemas have the same number of fields.
// - The data type of each field matches between the two schemas.
// - The nullability of each field matches between the two schemas.
//
// validateSchemaCompatibility returns nil if the schemas are compatible.
func validateSchemaCompatibility(a, b *arrow.Schema) error {
	if a.NumFields() != b.NumFields() {
		return fmt.Errorf("schemas have different number of fields: %d vs %d", a.NumFields(), b.NumFields())
	}

	if a.Endianness() != b.Endianness() {
		return fmt.Errorf("schemas have different endianness: %s vs %s", a.Endianness(), b.Endianness())
	}

	for i := range a.NumFields() {
		aField, bField := a.Field(i), b.Field(i)

		if !arrow.TypeEqual(aField.Type, bField.Type) {
			return fmt.Errorf("field %d has different types: %s vs %s", i, aField.Type, bField.Type)
		} else if aField.Nullable != bField.Nullable {
			return fmt.Errorf("field %d has different nullability: %t vs %t", i, aField.Nullable, bField.Nullable)
		}
	}

	return nil
}
