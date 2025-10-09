package executor

import (
	"context"
	"fmt"
	"sort"
	"unsafe"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

func NewParsePipeline(parse *physical.ParseNode, input Pipeline, allocator memory.Allocator) *GenericPipeline {
	return newGenericPipeline(Local, func(ctx context.Context, inputs []Pipeline) state {
		// Pull the next item from the input pipeline
		input := inputs[0]
		batch, err := input.Read(ctx)
		if err != nil {
			return failureState(err)
		}

		// Batch needs to be released here since it won't be passed to the caller and won't be reused after
		// this call to newGenericPipeline.
		defer batch.Release()

		// Find the message column
		messageColIdx := -1
		schema := batch.Schema()
		for i := 0; i < schema.NumFields(); i++ {
			if schema.Field(i).Name == types.ColumnNameBuiltinMessage {
				messageColIdx = i
				break
			}
		}
		if messageColIdx == -1 {
			return failureState(fmt.Errorf("message column not found"))
		}

		messageCol := batch.Column(messageColIdx)
		stringCol, ok := messageCol.(*array.String)
		if !ok {
			return failureState(fmt.Errorf("message column is not a string column"))
		}

		var headers []string
		var parsedColumns []arrow.Array
		switch parse.Kind {
		case physical.ParserLogfmt:
			headers, parsedColumns = buildLogfmtColumns(stringCol, parse.RequestedKeys, allocator)
		case physical.ParserJSON:
			headers, parsedColumns = buildJSONColumns(stringCol, parse.RequestedKeys, allocator)
		default:
			return failureState(fmt.Errorf("unsupported parser kind: %v", parse.Kind))
		}

		// Build new schema with original fields plus parsed fields
		newFields := make([]arrow.Field, 0, schema.NumFields()+len(headers))
		for i := 0; i < schema.NumFields(); i++ {
			newFields = append(newFields, schema.Field(i))
		}
		for _, header := range headers {
			// Add metadata to mark these as parsed columns
			metadata := types.ColumnMetadata(
				types.ColumnTypeParsed,
				types.Loki.String,
			)

			newFields = append(newFields, arrow.Field{
				Name:     header,
				Type:     arrow.BinaryTypes.String,
				Metadata: metadata,
				Nullable: true,
			})
		}
		newSchema := arrow.NewSchema(newFields, nil)

		// Build new record with all columns
		numOriginalCols := int(batch.NumCols())
		numParsedCols := len(parsedColumns)
		allColumns := make([]arrow.Array, numOriginalCols+numParsedCols)

		// Copy original columns
		for i := range numOriginalCols {
			col := batch.Column(i)
			col.Retain() // Retain since we're releasing the batch
			allColumns[i] = col
		}

		// Add parsed columns
		for i, col := range parsedColumns {
			// Defenisve check added for clarity and safety, but BuildLogfmtColumns should already guarantee this
			if col.Len() != stringCol.Len() {
				return failureState(fmt.Errorf("parsed column %d (%s) has %d rows but expected %d",
					i, headers[i], col.Len(), stringCol.Len()))
			}
			allColumns[numOriginalCols+i] = col
		}

		// Create the new record
		newRecord := array.NewRecord(newSchema, allColumns, int64(stringCol.Len()))

		// Release the columns we retained/created
		for _, col := range allColumns {
			col.Release()
		}

		return successState(newRecord)
	}, input)
}

// parseFunc represents a function that parses a single line and returns key-value pairs
type parseFunc func(line string, requestedKeys []string) (map[string]string, error)

// buildColumns builds Arrow columns from input lines using the provided parser
// Returns the column headers, the Arrow columns, and any error
func buildColumns(input *array.String, requestedKeys []string, allocator memory.Allocator, parseFunc parseFunc, errorType string) ([]string, []arrow.Array) {
	columnBuilders := make(map[string]*array.StringBuilder)
	columnOrder := parseLines(input, requestedKeys, columnBuilders, allocator, parseFunc, errorType)

	// Build final arrays
	columns := make([]arrow.Array, 0, len(columnOrder))
	headers := make([]string, 0, len(columnOrder))

	for _, key := range columnOrder {
		builder := columnBuilders[key]
		columns = append(columns, builder.NewArray())
		headers = append(headers, key)
		builder.Release()
	}

	return headers, columns
}

// parseLines discovers columns dynamically as lines are parsed
func parseLines(input *array.String, requestedKeys []string, columnBuilders map[string]*array.StringBuilder, allocator memory.Allocator, parseFunc parseFunc, errorType string) []string {
	columnOrder := []string{}
	var errorBuilder, errorDetailsBuilder *array.StringBuilder
	hasErrorColumns := false

	for i := 0; i < input.Len(); i++ {
		line := input.Value(i)
		parsed, err := parseFunc(line, requestedKeys)

		// Handle error columns
		if err != nil {
			// Create error columns on first error
			if !hasErrorColumns {
				errorBuilder = array.NewStringBuilder(allocator)
				errorDetailsBuilder = array.NewStringBuilder(allocator)
				columnBuilders[types.ColumnNameParsedError] = errorBuilder
				columnBuilders[types.ColumnNameParsedErrorDetails] = errorDetailsBuilder
				columnOrder = append(columnOrder, types.ColumnNameParsedError, types.ColumnNameParsedErrorDetails)
				hasErrorColumns = true

				// Backfill NULLs for previous rows
				for j := 0; j < i; j++ {
					errorBuilder.AppendNull()
					errorDetailsBuilder.AppendNull()
				}
			}
			// Append error values
			errorBuilder.Append(errorType)
			errorDetailsBuilder.Append(err.Error())

			// When there's an error, don't create new columns for the failed parse
			// Only add NULLs for columns that already exist
		} else if hasErrorColumns {
			// No error on this row, but we have error columns
			errorBuilder.AppendNull()
			errorDetailsBuilder.AppendNull()
		}

		// Track which keys we've seen this row
		seenKeys := make(map[string]bool)
		if hasErrorColumns {
			// Mark error columns as seen so we don't append nulls for them
			seenKeys[types.ColumnNameParsedError] = true
			seenKeys[types.ColumnNameParsedErrorDetails] = true
		}

		// Add values for parsed keys (only if no error)
		if err == nil {
			for key, value := range parsed {
				seenKeys[key] = true
				builder, exists := columnBuilders[key]
				if !exists {
					// New column discovered - create and backfill
					builder = array.NewStringBuilder(allocator)
					columnBuilders[key] = builder
					columnOrder = append(columnOrder, key)

					// Backfill NULLs for previous rows
					builder.AppendNulls(i)
				}
				builder.Append(value)
			}
		}
		// For error cases, don't mark the failed keys as seen - let them get NULLs below

		// Append NULLs for columns not in this row
		for _, key := range columnOrder {
			if !seenKeys[key] {
				columnBuilders[key].AppendNull()
			}
		}
	}

	// Sort column order for consistency (excluding error columns)
	if hasErrorColumns {
		// Keep error columns at the end, sort the rest
		nonErrorColumns := make([]string, 0, len(columnOrder)-2)
		for _, key := range columnOrder {
			if key != types.ColumnNameParsedError && key != types.ColumnNameParsedErrorDetails {
				nonErrorColumns = append(nonErrorColumns, key)
			}
		}
		sort.Strings(nonErrorColumns)
		columnOrder = append(nonErrorColumns, types.ColumnNameParsedError, types.ColumnNameParsedErrorDetails)
	} else {
		sort.Strings(columnOrder)
	}

	return columnOrder
}

// unsafeBytes converts a string to []byte without allocation
func unsafeBytes(s string) []byte {
	return unsafe.Slice(unsafe.StringData(s), len(s))
}

// unsafeString converts a []byte to string without allocation
func unsafeString(b []byte) string {
	return unsafe.String(unsafe.SliceData(b), len(b))
}
