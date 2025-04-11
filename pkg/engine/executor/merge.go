package executor

import (
	"fmt"

	"github.com/apache/arrow/go/v18/arrow"
	"github.com/apache/arrow/go/v18/arrow/array"
	"github.com/apache/arrow/go/v18/arrow/memory"
)

// NOTE(chaudum): Use at own risk!
// This function is more or less copy-paste from different sources and input from Claude 3.7 Sonnet.
// I haven't fully analyzed the correctness and performance implications.
// It is not tested properly and likely contains a lot of bugs.
func mergeRecords(records []arrow.Record) (arrow.Record, error) {
	if len(records) == 0 {
		return nil, fmt.Errorf("no records to merge")
	}
	if len(records) == 1 {
		return records[0], nil
	}

	// Create a map to track all fields across all schemas
	allFields := make(map[string]arrow.Field)

	// First pass: collect all unique fields from all schemas
	for _, rec := range records {
		schema := rec.Schema()
		for i := 0; i < schema.NumFields(); i++ {
			field := schema.Field(i)
			// If we already have this field, ensure types are compatible
			if existingField, ok := allFields[field.Name]; ok {
				if !arrow.TypeEqual(existingField.Type, field.Type) {
					return nil, fmt.Errorf("field '%s' has conflicting types: %s vs %s",
						field.Name, existingField.Type, field.Type)
				}
			} else {
				allFields[field.Name] = field
			}
		}
	}

	// Convert map to slice and create unified schema
	fields := make([]arrow.Field, 0, len(allFields))
	for _, field := range allFields {
		fields = append(fields, field)
	}
	mergedSchema := arrow.NewSchema(fields, nil)

	// Create arrays for each field in the merged schema
	mem := memory.NewGoAllocator()
	builders := make([]array.Builder, len(fields))
	for i, field := range fields {
		builders[i] = array.NewBuilder(mem, field.Type)
	}

	rowCount := int64(0)
	// Second pass: populate the builders
	for _, rec := range records {
		rowCount += rec.NumRows()
		// For each row in the current record
		for rowIdx := int64(0); rowIdx < rec.NumRows(); rowIdx++ {
			// For each field in the merged schema
			for fieldIdx, field := range fields {
				builder := builders[fieldIdx]

				// Check if the current record has this field
				colIdx := rec.Schema().FieldIndices(field.Name)
				if len(colIdx) > 0 {
					// Get the column and append the value
					col := rec.Column(colIdx[0])
					appendToBuilder(builder, col, int(rowIdx))
				} else {
					// Field not in this record, append null
					builder.AppendNull()
				}
			}
		}
	}

	// Build the arrays
	columns := make([]arrow.Array, len(builders))
	for i, builder := range builders {
		columns[i] = builder.NewArray()
		defer columns[i].Release()
		defer builder.Release()
	}

	// Create the record batch
	mergedRecord := array.NewRecord(mergedSchema, columns, rowCount)
	for i, col := range columns {
		mergedRecord.SetColumn(i, col)
	}

	return mergedRecord, nil
}

// Helper function to append a value from a source array to a builder
func appendToBuilder(builder array.Builder, sourceArray arrow.Array, index int) {
	if sourceArray.IsNull(index) {
		builder.AppendNull()
		return
	}

	switch b := builder.(type) {
	case *array.Int64Builder:
		b.Append(sourceArray.(*array.Int64).Value(index))
	case *array.Uint64Builder:
		b.Append(sourceArray.(*array.Uint64).Value(index))
	case *array.StringBuilder:
		b.Append(sourceArray.(*array.String).Value(index))
	default:
		builder.AppendNull()
	}
}
