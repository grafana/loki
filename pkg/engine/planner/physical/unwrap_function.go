package physical

import (
	"fmt"
	"strconv"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/dustin/go-humanize"
	"github.com/grafana/loki/v3/pkg/engine/internal/datatype"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

// UnwrapFunction represents an unwrap operation in the physical plan.
// It implements the [FunctionExpression] interface.
type UnwrapFunction struct {
	operation  UnwrapOperation
	identifier string
	function   func(string) (float64, error)
}

func NewUnwrapFunction(operation UnwrapOperation, identifier string) func(arrow.Record, memory.Allocator) (arrow.Record, error) {
	var unwrapFunction UnwrapFunction
	switch operation {
	case UnwrapOperationBytes:
		unwrapFunction = UnwrapFunction{
			operation:  operation,
			identifier: identifier,
			function:   ConvertBytes,
		}
	case UnwrapOperationDuration, UnwrapOperationDurationSeconds:
		unwrapFunction = UnwrapFunction{
			operation:  operation,
			identifier: identifier,
			function:   ConvertDuration,
		}
	default:
		unwrapFunction = UnwrapFunction{
			operation:  operation,
			identifier: identifier,
			function:   ConvertFloat,
		}
	}

	return unwrapFunction.Function
}

func (f *UnwrapFunction) Function(record arrow.Record, allocator memory.Allocator) (arrow.Record, error) {
	// Find the source column
	sourceFieldIndex, err := findSourceColumn(record.Schema(), f.identifier)
	if err != nil {
		return nil, err
	}
	sourceCol := record.Column(sourceFieldIndex).(*array.String)

	// Get conversion function and process values
	conversionFn := getConversionFunction(f.operation)
	unwrappedCol, errTracker := convertValues(sourceCol, conversionFn, allocator)
	defer unwrappedCol.Release()

	// Build error columns if needed
	errorCol, errorDetailsCol := errTracker.buildArrays()

	// Build output schema and record
	outputSchema := buildOutputSchema(record.Schema(), errTracker.hasErrors)
	newRecord := buildOutputRecord(
		record,
		unwrappedCol,
		errorCol,
		errorDetailsCol,
		outputSchema,
		int64(sourceCol.Len()),
	)

	return newRecord, nil
}

func findSourceColumn(schema *arrow.Schema, identifier string) (int, error) {
	for i := 0; i < schema.NumFields(); i++ {
		if schema.Field(i).Name == identifier {
			return i, nil
		}
	}
	return -1, fmt.Errorf("column %s not found", identifier)
}

func getConversionFunction(operation UnwrapOperation) conversionFn {
	switch operation {
	case UnwrapOperationBytes:
		return convertBytes
	case UnwrapOperationDuration, UnwrapOperationDurationSeconds:
		return convertDuration
	default:
		return convertFloat
	}
}

func convertValues(
	sourceCol *array.String,
	conversionFn conversionFn,
	allocator memory.Allocator,
) (arrow.Array, *errorTracker) {
	unwrappedBuilder := array.NewFloat64Builder(allocator)
	defer unwrappedBuilder.Release()

	tracker := newErrorTracker(allocator)

	for i := 0; i < sourceCol.Len(); i++ {
		if sourceCol.IsNull(i) {
			unwrappedBuilder.AppendNull()
			tracker.recordSuccess()
		} else {
			valueStr := sourceCol.Value(i)
			if val, err := conversionFn(valueStr); err == nil {
				unwrappedBuilder.Append(val)
				tracker.recordSuccess()
			} else {
				unwrappedBuilder.AppendNull()
				tracker.recordError(i, err)
			}
		}
	}

	return unwrappedBuilder.NewArray(), tracker
}

func buildOutputSchema(
	originalSchema *arrow.Schema,
	hasErrors bool,
) *arrow.Schema {
	fields := make([]arrow.Field, 0, originalSchema.NumFields()+3)

	// Copy original fields
	for i := 0; i < originalSchema.NumFields(); i++ {
		fields = append(fields, originalSchema.Field(i))
	}

	// Add value field
	fields = append(fields, arrow.Field{
		Name: types.ColumnNameGeneratedValue,
		Type: arrow.PrimitiveTypes.Float64,
		Metadata: datatype.ColumnMetadata(
			types.ColumnTypeGenerated,
			datatype.Loki.Float,
		),
		Nullable: true,
	})

	// Add error fields if needed
	if hasErrors {
		fields = append(fields,
			arrow.Field{
				Name: types.ColumnNameError,
				Type: arrow.BinaryTypes.String,
				Metadata: datatype.ColumnMetadata(
					types.ColumnTypeParsed,
					datatype.Loki.String,
				),
				Nullable: true,
			},
			arrow.Field{
				Name: types.ColumnNameErrorDetails,
				Type: arrow.BinaryTypes.String,
				Metadata: datatype.ColumnMetadata(
					types.ColumnTypeParsed,
					datatype.Loki.String,
				),
				Nullable: true,
			},
		)
	}

	return arrow.NewSchema(fields, nil)
}

func buildOutputRecord(
	batch arrow.Record,
	unwrappedCol arrow.Array,
	errorCol, errorDetailsCol arrow.Array,
	schema *arrow.Schema,
	numRows int64,
) arrow.Record {
	numOriginalCols := int(batch.NumCols())
	hasErrors := errorCol != nil

	totalCols := numOriginalCols + 1
	if hasErrors {
		totalCols += 2
	}

	columns := make([]arrow.Array, totalCols)

	// Copy original columns
	for i := range numOriginalCols {
		col := batch.Column(i)
		col.Retain()
		columns[i] = col
	}

	// Add new columns
	columns[numOriginalCols] = unwrappedCol
	if hasErrors {
		columns[numOriginalCols+1] = errorCol
		columns[numOriginalCols+2] = errorDetailsCol
	}

	record := array.NewRecord(schema, columns, numRows)

	// Release all columns
	for _, col := range columns {
		col.Release()
	}

	return record
}

type conversionFn func(value string) (float64, error)

func convertFloat(v string) (float64, error) {
	return strconv.ParseFloat(v, 64)
}

func convertDuration(v string) (float64, error) {
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, err
	}
	return d.Seconds(), nil
}

func convertBytes(v string) (float64, error) {
	b, err := humanize.ParseBytes(v)
	if err != nil {
		return 0, err
	}
	return float64(b), nil
}

type errorTracker struct {
	hasErrors      bool
	errorBuilder   *array.StringBuilder
	detailsBuilder *array.StringBuilder
	allocator      memory.Allocator
}

func newErrorTracker(allocator memory.Allocator) *errorTracker {
	return &errorTracker{allocator: allocator}
}

func (et *errorTracker) recordError(rowIndex int, err error) {
	if !et.hasErrors {
		et.errorBuilder = array.NewStringBuilder(et.allocator)
		et.detailsBuilder = array.NewStringBuilder(et.allocator)
		// Backfill nulls for previous rows
		for range rowIndex {
			et.errorBuilder.AppendNull()
			et.detailsBuilder.AppendNull()
		}
		et.hasErrors = true
	}
	et.errorBuilder.Append(types.SampleExtractionErrorType)
	et.detailsBuilder.Append(err.Error())
}

func (et *errorTracker) recordSuccess() {
	if et.hasErrors {
		et.errorBuilder.AppendNull()
		et.detailsBuilder.AppendNull()
	}
}

func (et *errorTracker) buildArrays() (arrow.Array, arrow.Array) {
	if !et.hasErrors {
		return nil, nil
	}
	return et.errorBuilder.NewArray(), et.detailsBuilder.NewArray()
}

// UnwrapOperation represents the type of unwrap operation to perform
type UnwrapOperation int

func ParseUnwrapOperation(op string) UnwrapOperation {
	switch op {
	case "bytes":
		return UnwrapOperationBytes
	case "duration":
		return UnwrapOperationDuration
	case "duration_seconds":
		return UnwrapOperationDurationSeconds
	default:
		return UnwrapOperationNone
	}
}

const (
	UnwrapOperationNone            UnwrapOperation = iota // Extract raw numeric value
	UnwrapOperationBytes                                  // Extract byte size and convert to bytes (OpConvBytes)
	UnwrapOperationDuration                               // Extract duration and convert to seconds (OpConvDuration)
	UnwrapOperationDurationSeconds                        // Extract duration with explicit _seconds suffix (OpConvDurationSeconds)
)

func (o UnwrapOperation) String() string {
	switch o {
	case UnwrapOperationBytes:
		return "bytes"
	case UnwrapOperationDuration:
		return "duration"
	case UnwrapOperationDurationSeconds:
		return "duration_seconds"
	default:
		return ""
	}
}

func ConvertFloat(v string) (float64, error) {
	return strconv.ParseFloat(v, 64)
}

func ConvertDuration(v string) (float64, error) {
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, err
	}
	return d.Seconds(), nil
}

func ConvertBytes(v string) (float64, error) {
	b, err := humanize.ParseBytes(v)
	if err != nil {
		return 0, err
	}
	return float64(b), nil
}
