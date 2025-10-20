package executor

import (
	"fmt"
	"strconv"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/dustin/go-humanize"

	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

func castFn(operation types.UnaryOp) UnaryFunction {
	return UnaryFunc(func(input ColumnVector, allocator memory.Allocator) (ColumnVector, error) {
		arr := input.ToArray()
		defer arr.Release()

		sourceCol, ok := arr.(*array.String)
		if !ok {
			return nil, fmt.Errorf("expected column to be of type string, got %T", arr)
		}

		// Get conversion function and process values
		conversionFn := getConversionFunction(operation)
		castCol, errTracker := castValues(sourceCol, conversionFn, allocator)
		defer castCol.Release()

		// Build error columns if needed
		errorCol, errorDetailsCol := errTracker.buildArrays()
		defer errTracker.releaseBuilders()
		if errTracker.hasErrors {
			defer errorCol.Release()
			defer errorDetailsCol.Release()
		}

		// Build output schema and record
		fields := buildValueAndErrorFields(errTracker.hasErrors)
		result, err := buildValueAndErrorStruct(castCol, errorCol, errorDetailsCol, fields)
		if err != nil {
			return nil, err
		}
		return &ArrayStruct{
			array: result,
			ct:    types.ColumnTypeGenerated,
			rows:  input.Len(),
		}, nil
	})
}

type conversionFn func(value string) (float64, error)

func getConversionFunction(operation types.UnaryOp) conversionFn {
	switch operation {
	case types.UnaryOpCastBytes:
		return convertBytes
	case types.UnaryOpCastDuration:
		return convertDuration
	default:
		return convertFloat
	}
}

func castValues(
	sourceCol *array.String,
	conversionFn conversionFn,
	allocator memory.Allocator,
) (arrow.Array, *errorTracker) {
	castBuilder := array.NewFloat64Builder(allocator)
	defer castBuilder.Release()

	tracker := newErrorTracker(allocator)

	for i := 0; i < sourceCol.Len(); i++ {
		if sourceCol.IsNull(i) {
			castBuilder.AppendNull()
			tracker.recordSuccess()
		} else {
			valueStr := sourceCol.Value(i)
			if val, err := conversionFn(valueStr); err == nil {
				castBuilder.Append(val)
				tracker.recordSuccess()
			} else {
				// Use 0.0 as default for errors, for backwards compatibility with old engine
				castBuilder.Append(0.0)
				tracker.recordError(i, err)
			}
		}
	}

	return castBuilder.NewArray(), tracker
}

func buildValueAndErrorFields(
	hasErrors bool,
) []arrow.Field {
	fields := make([]arrow.Field, 0, 3)

	// Add value field. Not nullable in practice since we use 0.0 when conversion fails, but as of
	// writing all coumns are marked as nullable, even Timestamp and Message, so staying consistent
	fields = append(fields, semconv.FieldFromIdent(semconv.ColumnIdentValue, true))

	// Add error fields if needed
	if hasErrors {
		fields = append(fields,
			semconv.FieldFromIdent(semconv.ColumnIdentError, true),
			semconv.FieldFromIdent(semconv.ColumnIdentErrorDetails, true),
		)
	}

	return fields
}

func buildValueAndErrorStruct(
	valVol, errorCol, errorDetailsCol arrow.Array,
	fields []arrow.Field,
) (*array.Struct, error) {
	hasErrors := errorCol != nil

	totalCols := 1
	if hasErrors {
		totalCols += 2
	}

	columns := make([]arrow.Array, totalCols)

	// Add new columns - these are newly created so don't need extra retain
	columns[0] = valVol
	if hasErrors {
		columns[1] = errorCol
		columns[2] = errorDetailsCol
	}

	// NewStructArrayWithFields will retain all columns
	return array.NewStructArrayWithFields(columns, fields)
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
