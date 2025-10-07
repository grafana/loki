package executor

import (
	"fmt"
	"strconv"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/dustin/go-humanize"

	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

func unwrap(operation types.UnwrapOp, identifier string, record arrow.Record, alloc memory.Allocator) (*array.Struct, error) {
	// Find the source column
	sourceFieldIndex, err := findSourceColumn(record.Schema(), identifier)
	if err != nil {
		return nil, err
	}
	sourceCol := record.Column(sourceFieldIndex).(*array.String)

	// Get conversion function and process values
	conversionFn := getConversionFunction(operation)
	unwrappedCol, errTracker := convertValues(sourceCol, conversionFn, alloc)

	// Build error columns if needed
	errorCol, errorDetailsCol := errTracker.buildArrays()
	defer errTracker.releaseBuilders()

	// Build output schema and record
	fields := buildOutputFields(record.Schema(), errTracker.hasErrors)
	return buildResult(record, unwrappedCol, errorCol, errorDetailsCol, fields)
}

func findSourceColumn(schema *arrow.Schema, identifier string) (int, error) {
	for i := 0; i < schema.NumFields(); i++ {
		if schema.Field(i).Name == identifier {
			return i, nil
		}
	}
	return -1, fmt.Errorf("column %s not found", identifier)
}

type conversionFn func(value string) (float64, error)

func getConversionFunction(operation types.UnwrapOp) conversionFn {
	switch operation {
	case types.UnwrapBytes:
		return convertBytes
	case types.UnwrapDuration, types.UnwrapDurationSeconds:
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
				// Use 0.0 as default for errors, for backwards compatibility with old engine
				unwrappedBuilder.Append(0.0)
				tracker.recordError(i, err)
			}
		}
	}

	return unwrappedBuilder.NewArray(), tracker
}

func buildOutputFields(
	originalSchema *arrow.Schema,
	hasErrors bool,
) []arrow.Field {
	fields := make([]arrow.Field, 0, originalSchema.NumFields()+3)

	// Copy original fields
	for i := 0; i < originalSchema.NumFields(); i++ {
		fields = append(fields, originalSchema.Field(i))
	}

	// Add value field
	fields = append(fields, arrow.Field{
		Name: types.ColumnNameGeneratedValue,
		Type: arrow.PrimitiveTypes.Float64,
		Metadata: types.ColumnMetadata(
			types.ColumnTypeGenerated,
			types.Loki.Float,
		),
		Nullable: true,
	})

	// Add error fields if needed
	if hasErrors {
		fields = append(fields,
			arrow.Field{
				Name: types.ColumnNameError,
				Type: arrow.BinaryTypes.String,
				Metadata: types.ColumnMetadata(
					types.ColumnTypeParsed,
					types.Loki.String,
				),
				Nullable: true,
			},
			arrow.Field{
				Name: types.ColumnNameErrorDetails,
				Type: arrow.BinaryTypes.String,
				Metadata: types.ColumnMetadata(
					types.ColumnTypeParsed,
					types.Loki.String,
				),
				Nullable: true,
			},
		)
	}

	return fields
}

func buildResult(
	batch arrow.Record,
	unwrappedCol, errorCol, errorDetailsCol arrow.Array,
	fields []arrow.Field,
) (*array.Struct, error) {
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
		columns[i] = col
	}

	// Add new columns - these are newly created so don't need extra retain
	columns[numOriginalCols] = unwrappedCol
	if hasErrors {
		columns[numOriginalCols+1] = errorCol
		columns[numOriginalCols+2] = errorDetailsCol
	}

	// NewStructArrayWithFields will retain all columns
	result, err := array.NewStructArrayWithFields(columns, fields)

	// Release our references to newly created columns (result now owns them)
	unwrappedCol.Release()
	if hasErrors {
		errorCol.Release()
		errorDetailsCol.Release()
	}

	return result, err
}

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

func (et *errorTracker) releaseBuilders() {
	if et.hasErrors {
		et.errorBuilder.Release()
		et.detailsBuilder.Release()
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
