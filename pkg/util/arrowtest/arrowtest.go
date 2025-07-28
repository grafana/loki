// Package arrowtest provides utilities for testing Arrow records.
package arrowtest

import (
	"cmp"
	"fmt"
	"slices"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/arrow/scalar"
)

type (
	// Rows is a slice of [Row].
	Rows []Row

	// Row represents a single record row as a map of column name to value.
	Row map[string]any
)

// Schema inferrs a [arrow.Schema] from each row in Rows. Columns in rows
// are sorted alphabetically.
//
// Schema panics if any of the following conditions are true:
//
// - A value cannot be converted into an Arrow data type.
// - Two rows have different sets of columns.
// - Two columns do not have the same Go type across rows.
func (rows Rows) Schema() *arrow.Schema {
	if len(rows) == 0 {
		// Empty schema.
		return arrow.NewSchema(nil, nil)
	}

	var (
		fields        = make([]arrow.Field, 0, len(rows[0]))
		columnToField = make(map[string]int)
	)

	// Get all the fields from the first row.
	for key, value := range rows[0] {
		// If value is nil, we will replace it with the first non-nil value we see
		// in the following loop.
		field := arrow.Field{
			Name:     key,
			Type:     determineDatatype(value),
			Nullable: true,
		}

		columnToField[key] = len(fields)
		fields = append(fields, field)
	}

	// Check the rest of the rows for consistency.
	for _, row := range rows[1:] {
		for key, value := range row {
			index, ok := columnToField[key]
			if !ok {
				panic(fmt.Sprintf("arrowtest.Schema: column %q not found in first row", key))
			}
			field := &fields[index]

			gotType := determineDatatype(value)

			if !arrow.TypeEqual(field.Type, gotType) {
				// The types don't match. We need to check for nulls here:
				//
				// If the expected type is null, we should replace it with a concrete
				// type. If gotType is null, we can ignore it (null scalars can be
				// casted appropriately).

				if arrow.TypeEqual(field.Type, arrow.Null) {
					field.Type = gotType
				} else if !arrow.TypeEqual(gotType, arrow.Null) {
					panic(fmt.Sprintf("arrowtest.Schema: column %q has different types across rows: got=%s, want=%s", key, gotType, field.Type))
				}
			}
		}
	}

	slices.SortFunc(fields, func(a, b arrow.Field) int {
		return cmp.Compare(a.Name, b.Name)
	})

	return arrow.NewSchema(fields, nil)
}

func determineDatatype(value any) arrow.DataType {
	switch value := value.(type) {
	case time.Time:
		return &arrow.TimestampType{Unit: arrow.Nanosecond, TimeZone: value.Location().String()}
	default:
		return scalar.MakeScalar(value).DataType()
	}
}

// Record converts rows into an [arrow.Record] with the provided schema. A
// schema can be inferred from rows by using [Rows.Schema].
//
// The returned record must be Release()'d after use.
//
// Record panics if schema references a column not found in one of the rows.
func (rows Rows) Record(alloc memory.Allocator, schema *arrow.Schema) arrow.Record {
	builder := array.NewRecordBuilder(alloc, schema)
	defer builder.Release()

	for i := range schema.NumFields() {
		field := schema.Field(i)
		fieldBuilder := builder.Field(i)

		for _, row := range rows {
			value, ok := row[field.Name]
			if !ok {
				panic(fmt.Sprintf("arrowtest.Record: column %q not found in row %d", field.Name, i))
			}

			if value == nil {
				fieldBuilder.AppendNull()
				continue
			}

			var s scalar.Scalar

			switch v := value.(type) {
			case time.Time:
				s = scalar.NewTimestampScalar(arrow.Timestamp(v.UnixNano()), determineDatatype(v))
			case *time.Time:
				s = scalar.NewTimestampScalar(arrow.Timestamp(v.UnixNano()), determineDatatype(v))
			default:
				s = scalar.MakeScalar(v)
			}

			if err := scalar.Append(fieldBuilder, s); err != nil {
				panic(fmt.Sprintf("arrowtest.Record: failed to append value %v for column %q: %v", value, field.Name, err))
			}
		}
	}

	return builder.NewRecord()
}

// RecordRows converts an [arrow.Record] into [Rows] for comparison in tests.
// RecordRows requires all columns in the record to have a unique name.
//
// All values are converted to their direct Go equivalents.
//
// Callers building expected [Rows] must use the same functions.
func RecordRows(rec arrow.Record) (Rows, error) {
	rows := make(Rows, rec.NumRows())

	for i := range int(rec.NumRows()) {
		row := make(Row, rec.NumCols())

		for j := range int(rec.NumCols()) {
			row[rec.Schema().Field(j).Name] = getArrayValue(rec.Column(j), i)
		}

		rows[i] = row
	}

	return rows, nil
}

// getArrayValue converts a value from an [arrow.Array] at the given index back
// into a Go value. Timestamps have a special case so they are converted into a
// [time.Time].
func getArrayValue(arr arrow.Array, index int) any {
	switch arr := arr.(type) {
	case *array.Timestamp:
		toTimestamp, err := arr.DataType().(*arrow.TimestampType).GetToTimeFunc()
		if err != nil {
			panic(err)
		}
		return toTimestamp(arr.Value(index))

	default:
		return arr.GetOneForMarshal(index)
	}
}

// TableRows concatenates all chunks of the [arrow.Table] into a single
// [arrow.Record], and then returns it as [Rows]. TableRows requires all
// columns in the table to have a unique name.
//
// See [RecordRows] for specifies on how values are converted into Go values
// for a [Row].
func TableRows(alloc memory.Allocator, table arrow.Table) (Rows, error) {
	rec, err := mergeTable(alloc, table)
	if err != nil {
		return nil, err
	}
	defer rec.Release()

	return RecordRows(rec)
}

// mergeTable merges all chunks in an [arrow.Table] into a single
// [arrow.Record].
func mergeTable(alloc memory.Allocator, table arrow.Table) (arrow.Record, error) {
	recordColumns := make([]arrow.Array, table.NumCols())

	for i := range int(table.NumCols()) {
		column, err := array.Concatenate(table.Column(i).Data().Chunks(), alloc)
		if err != nil {
			return nil, err
		}
		defer column.Release()
		recordColumns[i] = column
	}

	return array.NewRecord(table.Schema(), recordColumns, table.NumRows()), nil
}
