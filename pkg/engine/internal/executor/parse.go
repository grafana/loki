package executor

import (
	"fmt"
	"sort"
	"unsafe"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
	"github.com/grafana/loki/v3/pkg/engine/internal/util"
)

func parseFn(op types.VariadicOp) VariadicFunction {
	return VariadicFunctionFunc(func(args ...arrow.Array) (arrow.Array, error) {
		sourceCol, requestedKeys, err := extractParseFnParameters(args)
		if err != nil {
			panic(err)
		}

		var headers []string
		var parsedColumns []arrow.Array
		switch op {
		case types.VariadicOpParseLogfmt:
			headers, parsedColumns = buildLogfmtColumns(sourceCol, requestedKeys)
		case types.VariadicOpParseJSON:
			headers, parsedColumns = buildJSONColumns(sourceCol, requestedKeys)
		default:
			return nil, fmt.Errorf("unsupported parser kind: %v", op)
		}

		// Build new schema with original fields plus parsed fields
		newFields := make([]arrow.Field, 0, len(headers))
		for _, header := range headers {
			ct := types.ColumnTypeParsed
			if header == semconv.ColumnIdentError.ShortName() || header == semconv.ColumnIdentErrorDetails.ShortName() {
				ct = types.ColumnTypeGenerated
			}
			ident := semconv.NewIdentifier(header, ct, types.Loki.String)
			newFields = append(newFields, semconv.FieldFromIdent(ident, true))
		}

		if len(parsedColumns) == 0 {
			return nil, nil
		}

		return array.NewStructArrayWithFields(parsedColumns, newFields)
	})
}

func extractParseFnParameters(args []arrow.Array) (*array.String, []string, error) {
	// Valid signatures:
	//parse(sourceColVec)
	//parse(sourceColVec, requestedKeys)
	if len(args) < 1 || len(args) > 2 {
		return nil, nil, fmt.Errorf("parse function expected 1 or 2 arguments, got %d", len(args))
	}

	var sourceColArr, requestedKeysArr arrow.Array
	sourceColArr = args[0]
	if len(args) == 2 {
		requestedKeysArr = args[1]
	}

	if sourceColArr == nil {
		return nil, nil, fmt.Errorf("parse function arguments did not include a source ColumnVector to parse")
	}

	sourceCol, ok := sourceColArr.(*array.String)
	if !ok {
		return nil, nil, fmt.Errorf("parse can only operate on string column types, got %T", sourceColArr)
	}

	var requestedKeys []string

	// Rquested keys will be the same for all rows, so we only need the first one
	reqKeysIdx := 0
	if requestedKeysArr == nil || requestedKeysArr.IsNull(reqKeysIdx) {
		return sourceCol, requestedKeys, nil
	}

	reqKeysList, ok := requestedKeysArr.(*array.List)
	if !ok {
		return nil, nil, fmt.Errorf("requested keys must be a list of string arrays, got %T", requestedKeysArr)
	}

	firstRow, ok := util.ArrayListValue(reqKeysList, reqKeysIdx).([]string)
	if !ok {
		return nil, nil, fmt.Errorf("requested keys must be a list of string arrays, got a list of %T", firstRow)
	}
	requestedKeys = append(requestedKeys, firstRow...)
	return sourceCol, requestedKeys, nil
}

// parseFunc represents a function that parses a single line and returns key-value pairs
type parseFunc func(line string, requestedKeys []string) (map[string]string, error)

// buildColumns builds Arrow columns from input lines using the provided parser
// Returns the column headers, the Arrow columns, and any error
func buildColumns(input *array.String, requestedKeys []string, parseFunc parseFunc, errorType string) ([]string, []arrow.Array) {
	columnBuilders := make(map[string]*array.StringBuilder)
	columnOrder := parseLines(input, requestedKeys, columnBuilders, parseFunc, errorType)

	// Build final arrays
	columns := make([]arrow.Array, 0, len(columnOrder))
	headers := make([]string, 0, len(columnOrder))

	for _, key := range columnOrder {
		builder := columnBuilders[key]
		columns = append(columns, builder.NewArray())
		headers = append(headers, key)
	}

	return headers, columns
}

// parseLines discovers columns dynamically as lines are parsed
func parseLines(input *array.String, requestedKeys []string, columnBuilders map[string]*array.StringBuilder, parseFunc parseFunc, errorType string) []string {
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
				errorBuilder = array.NewStringBuilder(memory.DefaultAllocator)
				errorDetailsBuilder = array.NewStringBuilder(memory.DefaultAllocator)
				columnBuilders[semconv.ColumnIdentError.ShortName()] = errorBuilder
				columnBuilders[semconv.ColumnIdentErrorDetails.ShortName()] = errorDetailsBuilder
				columnOrder = append(
					columnOrder,
					semconv.ColumnIdentError.ShortName(),
					semconv.ColumnIdentErrorDetails.ShortName(),
				)
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
		seenKeys := make(map[string]struct{})
		if hasErrorColumns {
			// Mark error columns as seen so we don't append nulls for them
			seenKeys[semconv.ColumnIdentError.ShortName()] = struct{}{}
			seenKeys[semconv.ColumnIdentErrorDetails.ShortName()] = struct{}{}
		}

		// Add values for parsed keys (only if no error)
		if err == nil {
			for key, value := range parsed {
				seenKeys[key] = struct{}{}
				builder, exists := columnBuilders[key]
				if !exists {
					// New column discovered - create and backfill
					builder = array.NewStringBuilder(memory.DefaultAllocator)
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
			if _, found := seenKeys[key]; !found {
				columnBuilders[key].AppendNull()
			}
		}
	}

	// Sort column order for consistency (excluding error columns)
	if hasErrorColumns {
		// Keep error columns at the end, sort the rest
		nonErrorColumns := make([]string, 0, len(columnOrder)-2)
		for _, key := range columnOrder {
			if key != semconv.ColumnIdentError.ShortName() && key != semconv.ColumnIdentErrorDetails.ShortName() {
				nonErrorColumns = append(nonErrorColumns, key)
			}
		}
		sort.Strings(nonErrorColumns)
		columnOrder = append(nonErrorColumns, semconv.ColumnIdentError.ShortName(), semconv.ColumnIdentErrorDetails.ShortName())
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
